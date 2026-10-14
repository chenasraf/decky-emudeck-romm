package psxdisc

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// isoImage builds an ISO 9660 image in 2048-byte sectors holding files in its root, the last
// sectors left over for padding.
func isoImage(files map[string][]byte) []byte {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	// Directory records in sorted order, as mastering tools write them.
	for i := range names {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	const rootLBA = 18
	next := uint32(rootLBA + 1)
	lbas := map[string]uint32{}
	for _, n := range names {
		lbas[n] = next
		next += uint32((len(files[n]) + sectorSize - 1) / sectorSize)
	}
	img := make([]byte, (int(next)+4)*sectorSize)
	record := func(lba, size uint32, flags byte, id string) []byte {
		r := make([]byte, 33+len(id)+(len(id)+1)%2)
		r[0] = byte(len(r))
		binary.LittleEndian.PutUint32(r[2:], lba)
		binary.BigEndian.PutUint32(r[6:], lba)
		binary.LittleEndian.PutUint32(r[10:], size)
		binary.BigEndian.PutUint32(r[14:], size)
		r[25] = flags
		r[32] = byte(len(id))
		copy(r[33:], id)
		return r
	}
	pvd := img[16*sectorSize:]
	pvd[0] = 1
	copy(pvd[1:], "CD001")
	pvd[6] = 1
	copy(pvd[40:], "TEST")
	copy(pvd[156:], record(rootLBA, sectorSize, 2, "\x00"))
	term := img[17*sectorSize:]
	term[0] = 255
	copy(term[1:], "CD001")
	dir := img[rootLBA*sectorSize : (rootLBA+1)*sectorSize]
	off := copy(dir, record(rootLBA, sectorSize, 2, "\x00"))
	off += copy(dir[off:], record(rootLBA, sectorSize, 2, "\x01"))
	for _, n := range names {
		off += copy(dir[off:], record(lbas[n], uint32(len(files[n])), 0, n+";1"))
	}
	for _, n := range names {
		copy(img[int(lbas[n])*sectorSize:], files[n])
	}
	return img
}

// raw turns 2048-byte sectors into raw ones of mode.
func raw(img []byte, mode byte) []byte {
	var out bytes.Buffer
	for i := 0; i < len(img); i += sectorSize {
		s := make([]byte, rawSize)
		copy(s, sync)
		s[15] = mode
		at := 16
		if mode == 2 {
			at = 24
		}
		copy(s[at:], img[i:i+sectorSize])
		out.Write(s)
	}
	return out.Bytes()
}

func write(t *testing.T, path string, data []byte) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

var game = map[string][]byte{
	"SYSTEM.CNF":  []byte("BOOT = cdrom:\\SLUS_007.13;1\r\nTCB = 4\r\n"),
	"SLUS_007.13": bytes.Repeat([]byte("exe"), 1000),
}

func TestReadsTheSerialFromEveryKindOfImage(t *testing.T) {
	dir := t.TempDir()
	img := isoImage(game)
	mode2 := raw(img, 2)
	write(t, filepath.Join(dir, "cooked.iso"), img)
	write(t, filepath.Join(dir, "raw.bin"), mode2)
	write(t, filepath.Join(dir, "Game (USA).bin"), mode2)
	write(t, filepath.Join(dir, "Game (USA).cue"), []byte("FILE \"Game (USA).bin\" BINARY\n  TRACK 01 MODE2/2352\n    INDEX 01 00:00:00\n"))
	write(t, filepath.Join(dir, "Game.m3u", "Game.m3u"), []byte("# discs\n../Game (USA).cue\n"))
	write(t, filepath.Join(dir, "game.chd"), chdImage(mode2))

	for _, name := range []string{"cooked.iso", "raw.bin", "Game (USA).cue", "Game.m3u/Game.m3u", "game.chd"} {
		t.Run(name, func(t *testing.T) {
			disc, err := Discs{}.Disc(filepath.Join(dir, filepath.FromSlash(name)))
			if err != nil {
				t.Fatal(err)
			}
			if !disc.Read || disc.Serial != "SLUS-00713" || !strings.HasPrefix(disc.HashID, "HASH-") {
				t.Fatalf("disc %+v", disc)
			}
		})
	}
}

// The hash DuckStation keys serial-less discs by counts the first track's length with its pregap,
// which every image of the same disc agrees on.
func TestHashIsTheSameAcrossImages(t *testing.T) {
	dir := t.TempDir()
	img := isoImage(map[string][]byte{"PSX.EXE": []byte("no serial")})
	write(t, filepath.Join(dir, "a.iso"), img)
	write(t, filepath.Join(dir, "b.bin"), raw(img, 1))
	write(t, filepath.Join(dir, "c.bin"), raw(img, 2))
	write(t, filepath.Join(dir, "c.cue"), []byte("FILE c.bin BINARY\nTRACK 01 MODE2/2352\nINDEX 01 00:00:00\n"))
	write(t, filepath.Join(dir, "d.chd"), chdImage(raw(img, 2)))
	var hashes []string
	for _, name := range []string{"a.iso", "b.bin", "c.cue", "d.chd"} {
		disc, err := Discs{}.Disc(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(name, err)
		}
		if !disc.Read || disc.Serial != "" {
			t.Fatalf("%s: %+v", name, disc)
		}
		hashes = append(hashes, disc.HashID)
	}
	for _, h := range hashes[1:] {
		if h != hashes[0] {
			t.Fatalf("hashes differ: %v", hashes)
		}
	}
	// It matches DuckStation's: XXH64 of the executable's path and bytes, the volume descriptor and
	// the track's length.
	pvd := img[16*sectorSize : 17*sectorSize]
	if want := domain.PSXHashID("PSX.EXE", []byte("no serial"), pvd, uint32(len(img)/sectorSize+pregap)); hashes[0] != want {
		t.Fatalf("hash %s, want %s", hashes[0], want)
	}
}

// A cue sheet's first track ends where the next one in its file starts, its index 0 if it has one.
func TestCueTrackLength(t *testing.T) {
	dir := t.TempDir()
	img := isoImage(game)
	data := append(raw(img, 2), make([]byte, 10*rawSize)...)
	write(t, filepath.Join(dir, "g.bin"), data)
	sectors := uint32(len(img) / sectorSize)
	cue := "FILE \"g.bin\" BINARY\nTRACK 01 MODE2/2352\nINDEX 01 00:00:00\nTRACK 02 AUDIO\nINDEX 00 00:00:" +
		pad2(sectors) + "\nINDEX 01 00:00:" + pad2(sectors+2) + "\n"
	write(t, filepath.Join(dir, "g.cue"), []byte(cue))
	tr, err := open(filepath.Join(dir, "g.cue"), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tr.close() }()
	if tr.length != sectors+pregap {
		t.Fatalf("length %d, want %d", tr.length, sectors+pregap)
	}
}

func pad2(n uint32) string {
	s := "0" + string(rune('0'+n/10)) + string(rune('0'+n%10))
	return s[len(s)-2:]
}

func TestUnknownDiscs(t *testing.T) {
	dir := t.TempDir()
	cases := map[string][]byte{
		// BOOT names an executable the disc lacks: DuckStation knows the game by its file only.
		"missing.iso": isoImage(map[string][]byte{"SYSTEM.CNF": []byte("BOOT=cdrom:\\SLUS_999.99;1\n")}),
		"empty.iso":   make([]byte, 40*sectorSize),
	}
	for name, data := range cases {
		disc, err := Discs{}.Disc(write(t, filepath.Join(dir, name), data))
		if err != nil || disc.Read {
			t.Fatalf("%s: %+v %v", name, disc, err)
		}
	}
	audio := write(t, filepath.Join(dir, "music.cue"), []byte("FILE \"music.bin\" BINARY\nTRACK 01 AUDIO\nINDEX 01 00:00:00\n"))
	write(t, filepath.Join(dir, "music.bin"), make([]byte, 20*rawSize))
	if disc, err := (Discs{}).Disc(audio); err != nil || disc.Read {
		t.Fatalf("audio: %+v %v", disc, err)
	}
	if _, err := (Discs{}).Disc(write(t, filepath.Join(dir, "game.pbp"), []byte("PBP"))); err == nil {
		t.Fatal("read a .pbp")
	}
}

// chdImage wraps raw sectors in an uncompressed version 4 CHD with one MODE2_RAW track.
func chdImage(sectors []byte) []byte {
	const unit, perHunk, headerSize = 2448, 8, 108
	frames := len(sectors) / rawSize
	hunks := (frames + perHunk - 1) / perHunk
	hunkBytes := unit * perHunk
	mapAt := headerSize
	dataAt := mapAt + hunks*16
	metaAt := dataAt + hunks*hunkBytes
	meta := []byte("TRACK:1 TYPE:MODE2_RAW SUBTYPE:NONE FRAMES:" + itoa(frames) + " PREGAP:0 PGTYPE:MODE1 PGSUB:RW POSTGAP:0\x00")
	out := make([]byte, metaAt+16+len(meta))
	copy(out, "MComprHD")
	binary.BigEndian.PutUint32(out[8:], headerSize)
	binary.BigEndian.PutUint32(out[12:], 4)
	binary.BigEndian.PutUint32(out[24:], uint32(hunks))
	binary.BigEndian.PutUint64(out[28:], uint64(hunks*hunkBytes))
	binary.BigEndian.PutUint64(out[36:], uint64(metaAt))
	binary.BigEndian.PutUint32(out[44:], uint32(hunkBytes))
	for h := range hunks {
		e := out[mapAt+h*16:]
		binary.BigEndian.PutUint64(e, uint64(dataAt+h*hunkBytes))
		binary.BigEndian.PutUint16(e[12:], uint16(hunkBytes))
		for s := 0; s < perHunk && h*perHunk+s < frames; s++ {
			copy(out[dataAt+h*hunkBytes+s*unit:], sectors[(h*perHunk+s)*rawSize:(h*perHunk+s+1)*rawSize])
		}
	}
	m := out[metaAt:]
	copy(m, "CHT2")
	m[5], m[6], m[7] = byte(len(meta)>>16), byte(len(meta)>>8), byte(len(meta))
	copy(m[16:], meta)
	return out
}

func itoa(n int) string {
	var b []byte
	for {
		b = append([]byte{byte('0' + n%10)}, b...)
		if n /= 10; n == 0 {
			return string(b)
		}
	}
}
