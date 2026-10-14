package domain

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cespare/xxhash/v2"
)

// PSXFallbackExe is what a PlayStation boots when its disc's SYSTEM.CNF names nothing.
const PSXFallbackExe = "PSX.EXE"

// PSXBootExe is the path on the disc of the executable a SYSTEM.CNF boots, as DuckStation reads it:
// every blank dropped, the BOOT key matched in any case, its cdrom: prefix, leading slashes and
// ;version cut.
func PSXBootExe(cnf []byte) string {
	for line := range strings.FieldsFuncSeq(string(cnf), func(r rune) bool { return r == '\r' || r == '\n' }) {
		line = strings.Map(func(r rune) rune {
			if r == ' ' || r >= 0x09 && r <= 0x0D {
				return -1
			}
			return r
		}, line)
		key, value, _ := strings.Cut(line, "=")
		if !strings.EqualFold(key, "boot") {
			continue
		}
		value = strings.TrimPrefix(value, "cdrom:")
		value = strings.TrimLeft(value, `/\`)
		if i := strings.LastIndexByte(value, ';'); i >= 0 {
			value = value[:i]
		}
		return value
	}
	return PSXFallbackExe
}

// PSXSerial is the serial DuckStation takes from the path of a disc's executable, SLUS_007.13 giving
// SLUS-00713. A disc booting PSX.EXE has none.
func PSXSerial(exe string) string {
	if exe == PSXFallbackExe {
		return ""
	}
	if i := strings.LastIndexByte(exe, '\\'); i >= 0 {
		exe = exe[i+1:]
	}
	exe = strings.ReplaceAll(exe, ".", "")
	return strings.ToUpper(strings.ReplaceAll(exe, "_", "-"))
}

// PSXHashID is the ID DuckStation's game database knows a disc by when its serial isn't one: a hash
// of the executable's path and bytes, the disc's primary volume descriptor, and the length of its
// first track in sectors.
func PSXHashID(exe string, data, pvd []byte, track1 uint32) string {
	h := xxhash.NewWithSeed(0x4242D00C)
	_, _ = h.WriteString(exe)
	_, _ = h.Write(data)
	_, _ = h.Write(pvd)
	_ = binary.Write(h, binary.LittleEndian, track1)
	return fmt.Sprintf("HASH-%X", h.Sum64())
}

// PSXDisc is what DuckStation reads off a PlayStation disc to tell its game.
type PSXDisc struct {
	// Read is false when the disc's executable couldn't be read, so DuckStation knows the game only
	// by its file's name.
	Read bool
	// Serial is empty for a disc booting PSX.EXE.
	Serial string
	HashID string
}

// DuckStationGame is how DuckStation names a game's memory card: after Title, the game's own, or
// after DiscSet, the title shared by the discs of a game on several, when there is one.
type DuckStationGame struct {
	Title   string
	DiscSet string
}

// Card is the file DuckStation keeps the game's memory card in, in slot 1. A game on several discs
// shares one card, unless a card of the disc's own title is already there, which exists says.
func (g DuckStationGame) Card(exists func(name string) bool) string {
	own := DuckStationCardName(g.Title)
	if g.DiscSet == "" {
		return own
	}
	if shared := DuckStationCardName(g.DiscSet); shared != own && !exists(own) {
		return shared
	}
	return own
}

// DuckStationCardName is the file of a slot 1 memory card named after title, sanitised as DuckStation
// does on Linux.
func DuckStationCardName(title string) string {
	return strings.Map(func(r rune) rune {
		if r <= 31 || r == '/' || r == '*' {
			return '_'
		}
		return r
	}, title) + "_1.mcd"
}

// FileTitle is the name DuckStation gives a game it doesn't know: its file's name without the last
// extension.
func FileTitle(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// PSXCardSize is the size of a PlayStation memory card image.
const PSXCardSize = 128 << 10

// IsPSXCard says whether data is a raw PlayStation memory card image, as DuckStation and RetroArch's
// PlayStation cores keep one.
func IsPSXCard(data []byte) bool {
	return len(data) == PSXCardSize && string(data[:2]) == "MC"
}
