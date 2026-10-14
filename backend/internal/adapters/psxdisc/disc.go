// Package psxdisc reads PlayStation discs as DuckStation does to tell their game: the executable
// their SYSTEM.CNF boots, from the ISO 9660 filesystem on their first track.
package psxdisc

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ZaparooProject/go-gameid/chd"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

const (
	sectorSize = 2048
	rawSize    = 2352
	// pregap is the two seconds of pregap DuckStation counts before a data track that names none.
	pregap = 150
	// maxFile bounds a file read off a disc; PlayStation executables are at most 2 MB.
	maxFile = 8 << 20
)

// Discs reads PlayStation discs.
type Discs struct{}

// Disc reads the disc at path, a .cue, .bin, .img, .iso or .chd, or an .m3u whose first disc is one.
func (Discs) Disc(path string) (domain.PSXDisc, error) {
	t, err := open(path, 0)
	if err != nil {
		return domain.PSXDisc{}, err
	}
	defer func() { _ = t.close() }()
	if t.audio {
		// DuckStation looks no further than an audio first track.
		return domain.PSXDisc{}, nil
	}
	return identify(t)
}

// track1 is a disc's first track: its user data sector by sector, and its length in sectors,
// pregap included, as DuckStation counts it.
type track1 struct {
	sector func(lba int64) ([]byte, error)
	length uint32
	audio  bool
	close  func() error
}

func open(path string, depth int) (*track1, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".m3u":
		if depth > 0 {
			return nil, errors.New("a playlist lists another playlist")
		}
		first, err := firstEntry(path)
		if err != nil {
			return nil, err
		}
		return open(first, depth+1)
	case ".cue":
		return openCue(path)
	case ".bin", ".img", ".iso":
		return openBin(path)
	case ".chd":
		return openCHD(path)
	}
	return nil, fmt.Errorf("%s discs can't be read", filepath.Ext(path))
}

// firstEntry is the first disc an .m3u lists, which DuckStation boots.
func firstEntry(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = filepath.FromSlash(strings.ReplaceAll(line, `\`, "/"))
		if !filepath.IsAbs(line) {
			line = filepath.Join(filepath.Dir(path), line)
		}
		return line, nil
	}
	return "", errors.New("the playlist lists no discs")
}

// mode is how a track keeps its sectors: their size in the file, and where their user data starts.
type mode struct {
	size, data int
	audio      bool
}

var cueModes = map[string]mode{
	"MODE1/2048": {size: 2048}, "MODE2/2048": {size: 2048},
	"MODE1/2352": {size: rawSize, data: 16}, "MODE2/2352": {size: rawSize, data: 24}, "CDI/2352": {size: rawSize, data: 24},
	"MODE2/2336": {size: 2336, data: 8},
	"AUDIO":      {size: rawSize, audio: true},
}

func fileTrack(path string, start int64, m mode, length uint32) (*track1, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &track1{
		sector: func(lba int64) ([]byte, error) {
			buf := make([]byte, m.size)
			if _, err := f.ReadAt(buf, start+lba*int64(m.size)); err != nil {
				return nil, err
			}
			return buf[m.data : m.data+sectorSize], nil
		},
		length: length, audio: m.audio, close: f.Close,
	}, nil
}

// openBin opens a lone track file: raw sectors when it starts with a data sector's sync pattern and
// fits them, else 2048-byte ones.
func openBin(path string) (*track1, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	head := make([]byte, 16)
	_, err = f.ReadAt(head, 0)
	info, statErr := f.Stat()
	_ = f.Close()
	if err != nil || statErr != nil {
		return nil, errors.Join(err, statErr)
	}
	m := mode{size: 2048}
	switch {
	case bytes.Equal(head[:12], sync) && head[15] == 1 && info.Size()%rawSize == 0:
		m = mode{size: rawSize, data: 16}
	case bytes.Equal(head[:12], sync) && head[15] == 2 && info.Size()%rawSize == 0:
		m = mode{size: rawSize, data: 24}
	case info.Size()%rawSize == 0:
		m = mode{size: rawSize, audio: true}
	}
	return fileTrack(path, 0, m, uint32(info.Size()/int64(m.size))+pregap)
}

var sync = []byte{0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00}

// cueTrack is what a cue sheet says of a track.
type cueTrack struct {
	number       int
	file         string
	mode         mode
	index0       int64
	index1       int64
	hasIndex0    bool
	pregap       int64
	hasPregapCmd bool
}

func openCue(path string) (*track1, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var tracks []*cueTrack
	file := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := cueFields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		switch strings.ToUpper(fields[0]) {
		case "FILE":
			if len(fields) > 1 {
				file = fields[1]
			}
		case "TRACK":
			if len(fields) < 3 {
				continue
			}
			n, _ := strconv.Atoi(fields[1])
			m, ok := cueModes[strings.ToUpper(fields[2])]
			if !ok {
				return nil, fmt.Errorf("the cue sheet's track mode %s isn't known", fields[2])
			}
			tracks = append(tracks, &cueTrack{number: n, file: file, mode: m})
		case "INDEX":
			if len(tracks) == 0 || len(fields) < 3 {
				continue
			}
			t := tracks[len(tracks)-1]
			lba, err := msf(fields[2])
			if err != nil {
				return nil, err
			}
			switch fields[1] {
			case "00", "0":
				t.index0, t.hasIndex0 = lba, true
			case "01", "1":
				t.index1 = lba
			}
		case "PREGAP":
			if len(tracks) > 0 && len(fields) > 1 {
				lba, err := msf(fields[1])
				if err != nil {
					return nil, err
				}
				tracks[len(tracks)-1].pregap, tracks[len(tracks)-1].hasPregapCmd = lba, true
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(tracks) == 0 || tracks[0].number != 1 || tracks[0].file == "" {
		return nil, errors.New("the cue sheet lists no first track")
	}
	t := tracks[0]
	bin := filepath.Join(filepath.Dir(path), filepath.FromSlash(t.file))
	info, err := os.Stat(bin)
	if err != nil {
		return nil, err
	}
	length := info.Size()/int64(t.mode.size) - t.index1
	if len(tracks) > 1 && tracks[1].file == t.file {
		next := tracks[1].index1
		if tracks[1].hasIndex0 {
			next = tracks[1].index0
		}
		length = next - t.index1
	}
	gap := int64(pregap)
	switch {
	case t.hasIndex0:
		gap = t.index1 - t.index0
	case t.hasPregapCmd:
		gap = t.pregap
	}
	if length < 0 {
		return nil, errors.New("the cue sheet's first track lies past its file's end")
	}
	return fileTrack(bin, t.index1*int64(t.mode.size), t.mode, uint32(length+gap))
}

// cueFields splits a cue sheet line into its words, a quoted one taken whole.
func cueFields(line string) []string {
	var out []string
	line = strings.TrimSpace(line)
	for line != "" {
		if line[0] == '"' {
			end := strings.IndexByte(line[1:], '"')
			if end < 0 {
				out = append(out, line[1:])
				break
			}
			out = append(out, line[1:end+1])
			line = strings.TrimSpace(line[end+2:])
			continue
		}
		word, rest, _ := strings.Cut(line, " ")
		out = append(out, word)
		line = strings.TrimSpace(rest)
	}
	return out
}

// msf reads a cue sheet's mm:ss:ff position as a sector count.
func msf(s string) (int64, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("bad cue sheet position %q", s)
	}
	var n [3]int64
	for i, p := range parts {
		v, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("bad cue sheet position %q", s)
		}
		n[i] = v
	}
	return (n[0]*60+n[1])*75 + n[2], nil
}

func openCHD(path string) (*track1, error) {
	c, err := chd.Open(path)
	if err != nil {
		return nil, err
	}
	tracks := c.Tracks()
	if len(tracks) == 0 {
		_ = c.Close()
		return nil, errors.New("the CHD lists no tracks")
	}
	t := tracks[0]
	if !t.IsDataTrack() {
		return &track1{audio: true, close: c.Close}, nil
	}
	raw := c.RawSectorReader()
	sector := func(start int64) func(int64) ([]byte, error) {
		return func(lba int64) ([]byte, error) {
			buf := make([]byte, rawSize)
			if _, err := raw.ReadAt(buf, (start+lba)*rawSize); err != nil {
				return nil, err
			}
			if !bytes.Equal(buf[:12], sync) {
				// A track kept as user data only.
				return buf[:sectorSize], nil
			}
			if buf[15] == 2 {
				return buf[24 : 24+sectorSize], nil
			}
			return buf[16 : 16+sectorSize], nil
		}
	}
	// chdman keeps a first track's pregap in the file only sometimes, and the track's metadata as
	// read doesn't say: the data starts wherever the volume descriptor is.
	gap := t.Pregap
	if gap <= 0 {
		gap = pregap
	}
	length := uint32(t.Frames + gap)
	read := sector(0)
	if t.Pregap > 0 {
		if pvd, err := sector(int64(t.Pregap))(16); err == nil && isPVD(pvd) {
			read, length = sector(int64(t.Pregap)), uint32(t.Frames)
		}
	}
	return &track1{sector: read, length: length, close: c.Close}, nil
}

func isPVD(s []byte) bool { return len(s) >= 6 && s[0] == 1 && string(s[1:6]) == "CD001" }

// identify reads the disc's executable as DuckStation does, knowing it by its serial or, without
// one, by the hash its game database keys such discs with.
func identify(t *track1) (domain.PSXDisc, error) {
	pvd, err := t.sector(16)
	if err != nil {
		return domain.PSXDisc{}, fmt.Errorf("read the volume descriptor: %w", err)
	}
	if !isPVD(pvd) {
		// Not a disc DuckStation can read a filesystem off.
		return domain.PSXDisc{}, nil
	}
	exe := domain.PSXFallbackExe
	if cnf, err := readFile(t, pvd, "SYSTEM.CNF"); err == nil {
		exe = domain.PSXBootExe(cnf)
	}
	data, err := readFile(t, pvd, exe)
	if err != nil {
		return domain.PSXDisc{}, nil
	}
	return domain.PSXDisc{Read: true, Serial: domain.PSXSerial(exe), HashID: domain.PSXHashID(exe, data, pvd, t.length)}, nil
}

var errNotFound = errors.New("not on the disc")

// readFile reads the file at path, its parts split by \ or /, matched in any case and without
// their ;version.
func readFile(t *track1, pvd []byte, path string) ([]byte, error) {
	lba, size := le32(pvd[156+2:]), le32(pvd[156+10:])
	parts := strings.FieldsFunc(path, func(r rune) bool { return r == '\\' || r == '/' })
	if len(parts) == 0 {
		return nil, errNotFound
	}
	for i, part := range parts {
		var dir bool
		var ok bool
		lba, size, dir, ok = lookup(t, lba, size, part)
		if !ok || dir != (i < len(parts)-1) {
			return nil, errNotFound
		}
	}
	if size > maxFile {
		return nil, fmt.Errorf("%s is too large", path)
	}
	out := make([]byte, 0, size)
	for s := int64(lba); uint32(len(out)) < size; s++ {
		data, err := t.sector(s)
		if err != nil {
			return nil, err
		}
		out = append(out, data[:min(sectorSize, int(size)-len(out))]...)
	}
	return out, nil
}

// lookup finds name in the directory at lba, size bytes long.
func lookup(t *track1, lba, size uint32, name string) (uint32, uint32, bool, bool) {
	for s := uint32(0); s*sectorSize < size; s++ {
		data, err := t.sector(int64(lba + s))
		if err != nil {
			return 0, 0, false, false
		}
		for off := 0; off < sectorSize; {
			n := int(data[off])
			if n == 0 || off+n > sectorSize {
				// Records don't cross sectors; the rest of this one is padding.
				break
			}
			rec := data[off : off+n]
			off += n
			if n < 34 || 33+int(rec[32]) > n {
				continue
			}
			id := string(rec[33 : 33+int(rec[32])])
			if i := strings.IndexByte(id, ';'); i >= 0 {
				id = id[:i]
			}
			if strings.EqualFold(id, name) {
				return le32(rec[2:]), le32(rec[10:]), rec[25]&2 != 0, true
			}
		}
	}
	return 0, 0, false, false
}

func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}
