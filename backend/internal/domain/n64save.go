package domain

import (
	"archive/zip"
	"bytes"
	"crypto/md5" //nolint:gosec // RomM identifies save content by its MD5, not for security
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"path"
	"slices"
	"strings"
	"time"
)

// An N64 cartridge's save, as the unit RomM's clients share for it: a zip of the save types the
// game uses, each in the order the N64 reads it. The emulators keep the same types in files of their
// own, SRAM and flash with each 32-bit word byte-reversed.

const (
	n64EEPROM4K   = 0x200
	n64EEPROMMax  = 0x800
	n64PakSize    = 0x8000
	n64Paks       = 4
	n64SRAMSize   = 0x8000
	n64FlashSize  = 0x20000
	n64BlobSize   = 0x48800
	n64BlobPaks   = 0x800
	n64BlobSRAM   = 0x20800
	n64BlobFlash  = 0x28800
	n64PakIndex   = 0x10A
	n64PakIndexTo = 0x200
	n64InodeFree  = 0x0003
)

// N64 save types, in the order the unit and the libretro .srm hold them.
const (
	n64RegionEEPROM = iota
	n64RegionPak1
	n64RegionPak2
	n64RegionPak3
	n64RegionPak4
	n64RegionSRAM
	n64RegionFlash
	n64Regions
)

var n64RegionNames = [n64Regions]string{"eeprom", "pak1", "pak2", "pak3", "pak4", "sram", "flash"}

// n64FileExts are the files mupen64plus, and RMG, keep a game's save types in.
var n64FileExts = map[int]string{n64RegionEEPROM: ".eep", n64RegionSRAM: ".sra", n64RegionFlash: ".fla"}

// N64PakFileExt is the file holding all four controller paks.
const N64PakFileExt = ".mpk"

// N64SaveExts are the extensions of the files an N64 save is kept in.
var N64SaveExts = []string{".eep", ".sra", ".fla", N64PakFileExt}

// N64Save is an N64 game's save types, in bus order.
type N64Save struct {
	regions   [n64Regions][]byte
	present   [n64Regions]bool
	eepromLen int
}

func n64FullSize(region int) int {
	switch region {
	case n64RegionEEPROM:
		return n64EEPROMMax
	case n64RegionSRAM:
		return n64SRAMSize
	case n64RegionFlash:
		return n64FlashSize
	}
	return n64PakSize
}

func n64Swapped(region int) bool { return region == n64RegionSRAM || region == n64RegionFlash }

// swap32 reverses the bytes of each 32-bit word of data, in place.
func swap32(data []byte) {
	for i := 0; i+4 <= len(data); i += 4 {
		data[i], data[i+1], data[i+2], data[i+3] = data[i+3], data[i+2], data[i+1], data[i]
	}
}

// written says whether data holds anything an emulator wrote: a byte other than the 0x00 or 0xFF
// they start a save type as.
func written(data []byte) bool {
	return slices.ContainsFunc(data, func(b byte) bool { return b != 0x00 && b != 0xFF })
}

// pakUsed says whether a controller pak's index lists a note.
func pakUsed(pak []byte) bool {
	for at := n64PakIndex; at < n64PakIndexTo; at += 2 {
		if binary.BigEndian.Uint16(pak[at:]) != n64InodeFree {
			return written(pak)
		}
	}
	return false
}

// take sets a save type from an emulator's file of it: data as the emulator stores it, padded to
// the type's size as the emulator starts it.
func (s *N64Save) take(region int, data []byte) {
	full := n64FullSize(region)
	buf := bytes.Repeat([]byte{0xFF}, full)
	copy(buf, data)
	if n64Swapped(region) {
		swap32(buf)
	}
	s.regions[region] = buf
	switch {
	case region == n64RegionEEPROM:
		s.eepromLen = n64EEPROM4K
		if written(buf[n64EEPROM4K:]) {
			s.eepromLen = n64EEPROMMax
		}
		s.regions[region] = buf[:s.eepromLen]
		s.present[region] = written(s.regions[region])
	case region >= n64RegionPak1 && region <= n64RegionPak4:
		s.present[region] = pakUsed(buf)
	default:
		s.present[region] = written(buf)
	}
}

// TakeFile reads one of mupen64plus's save files, by its extension.
func (s *N64Save) TakeFile(ext string, data []byte) {
	ext = strings.ToLower(ext)
	if ext == N64PakFileExt {
		for p := range n64Paks {
			at := p * n64PakSize
			if at < len(data) {
				s.take(n64RegionPak1+p, data[at:min(len(data), at+n64PakSize)])
			} else {
				s.take(n64RegionPak1+p, nil)
			}
		}
		return
	}
	for region, e := range n64FileExts {
		if e == ext {
			s.take(region, data)
		}
	}
}

// takeBlob reads a libretro core's .srm, which holds every save type at a fixed offset.
func (s *N64Save) takeBlob(blob []byte) {
	at := [n64Regions]int{0, n64BlobPaks, n64BlobPaks + n64PakSize, n64BlobPaks + 2*n64PakSize, n64BlobPaks + 3*n64PakSize, n64BlobSRAM, n64BlobFlash}
	for r := range n64Regions {
		s.take(r, blob[at[r]:at[r]+n64FullSize(r)])
	}
}

// Empty says whether the game wrote no save type.
func (s *N64Save) Empty() bool { return !slices.Contains(s.present[:], true) }

// errN64Format is a save from RomM no N64 save layout matches.
var errN64Format = errors.New("RomM's save isn't one an N64 emulator wrote")

// ParseN64Unit reads a save as RomM keeps it: the shared unit, a zip of an emulator's files, a
// libretro core's .srm, or a lone EEPROM file.
func ParseN64Unit(data []byte) (*N64Save, error) {
	s := &N64Save{eepromLen: n64EEPROM4K}
	if !isZip(data) {
		switch {
		case len(data) >= n64BlobSize:
			s.takeBlob(data)
		case len(data) == n64EEPROM4K || len(data) == n64EEPROMMax:
			s.take(n64RegionEEPROM, data)
		default:
			return nil, errN64Format
		}
		return s, nil
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errN64Format
	}
	for _, f := range zr.File {
		member, err := readZipFile(f)
		if err != nil {
			return nil, errN64Format
		}
		if region := slices.Index(n64RegionNames[:], f.Name); region >= 0 {
			// The unit's members are in bus order already, at their own sizes.
			want := n64FullSize(region)
			if region == n64RegionEEPROM && len(member) != n64EEPROM4K && len(member) != n64EEPROMMax || region != n64RegionEEPROM && len(member) != want {
				return nil, errN64Format
			}
			s.regions[region], s.present[region] = member, true
			if region == n64RegionEEPROM {
				s.eepromLen = len(member)
			}
			continue
		}
		ext := strings.ToLower(path.Ext(f.Name))
		switch {
		case ext == ".srm":
			if len(member) < n64BlobSize {
				return nil, errN64Format
			}
			s.takeBlob(member)
		case slices.Contains(N64SaveExts, ext):
			s.TakeFile(ext, member)
		default:
			return nil, errN64Format
		}
	}
	return s, nil
}

func isZip(data []byte) bool { return len(data) >= 4 && binary.LittleEndian.Uint32(data) == 0x04034b50 }

// maxZipMember bounds a save zip's member as read; save files are a few hundred KB at most.
const maxZipMember = 64 << 20

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(io.LimitReader(rc, maxZipMember+1))
	if err == nil && len(data) > maxZipMember {
		err = errors.New("zip member too large")
	}
	return data, err
}

// Unit is the save as the unit RomM's clients share: a zip of the types the game used. Members are
// stored, not compressed, at a fixed time, so the same save always gives the same bytes.
func (s *N64Save) Unit() []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for r := range n64Regions {
		if !s.present[r] {
			continue
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: n64RegionNames[r], Method: zip.Store, Modified: time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)})
		if err != nil {
			panic(err) // writing to a bytes.Buffer doesn't fail
		}
		_, _ = w.Write(s.regions[r])
	}
	_ = zw.Close()
	return buf.Bytes()
}

// N64File is a file of mupen64plus's to write, or to remove when Data is nil.
type N64File struct {
	Ext  string
	Data []byte
}

// Files are mupen64plus's files holding s: one per save type it has, the four paks in one file, and
// a removal for each type it lacks. old is the save the files hold now, whose unused paks are kept
// rather than formatted anew.
func (s *N64Save) Files(old *N64Save, oldPaks []byte) []N64File {
	var out []N64File
	for _, region := range []int{n64RegionEEPROM, n64RegionSRAM, n64RegionFlash} {
		ext := n64FileExts[region]
		if !s.present[region] {
			out = append(out, N64File{Ext: ext})
			continue
		}
		data := bytes.Repeat([]byte{0xFF}, n64FullSize(region))
		copy(data, s.regions[region])
		if n64Swapped(region) {
			swap32(data)
		}
		out = append(out, N64File{Ext: ext, Data: data})
	}
	if !slices.Contains(s.present[n64RegionPak1:n64RegionPak4+1], true) {
		return append(out, N64File{Ext: N64PakFileExt})
	}
	paks := make([]byte, 0, n64Paks*n64PakSize)
	for p := range n64Paks {
		region := n64RegionPak1 + p
		at := p * n64PakSize
		switch {
		case s.present[region]:
			paks = append(paks, s.regions[region]...)
		case old != nil && !old.present[region] && len(oldPaks) >= at+n64PakSize:
			paks = append(paks, oldPaks[at:at+n64PakSize]...)
		default:
			paks = append(paks, formattedPak()...)
		}
	}
	return append(out, N64File{Ext: N64PakFileExt, Data: paks})
}

// formattedPak is a controller pak formatted with no notes.
func formattedPak() []byte {
	out := make([]byte, n64PakSize)
	tail := []byte{0x00, 0x01, 0x01, 0x00, 0x01, 0x01, 0xFE, 0xF1}
	for _, block := range []int{0x20, 0x60, 0x80, 0xC0} {
		copy(out[block+0x18:], tail)
	}
	for at := n64PakIndex; at < n64PakIndexTo; at += 2 {
		out[at+1] = n64InodeFree
	}
	out[0x101] = 0x71
	copy(out[0x200:0x300], out[0x100:0x200])
	return out
}

// ContentHash is the hash RomM keeps a save's content under: the MD5 of a zip's sorted
// "<member>:<member MD5>" lines, else the MD5 of the bytes.
func ContentHash(data []byte) string {
	if h, ok := zipContentHash(data); ok {
		return h
	}
	return md5Hex(data)
}

func zipContentHash(data []byte) (string, bool) {
	if !isZip(data) {
		return "", false
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", false
	}
	files := slices.Clone(zr.File)
	slices.SortFunc(files, func(a, b *zip.File) int { return strings.Compare(a.Name, b.Name) })
	var lines []string
	for _, f := range files {
		if strings.HasSuffix(f.Name, "/") {
			continue
		}
		member, err := readZipFile(f)
		if err != nil {
			return "", false
		}
		lines = append(lines, f.Name+":"+md5Hex(member))
	}
	return md5Hex([]byte(strings.Join(lines, "\n"))), true
}

func md5Hex(data []byte) string {
	sum := md5.Sum(data) //nolint:gosec // see import
	return hex.EncodeToString(sum[:])
}
