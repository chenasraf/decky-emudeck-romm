package domain

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/chenasraf/saveporter/gb"
)

// Game Boy and Game Boy Color battery saves differ between emulators in layout (padding, MBC2's
// half-bytes, MBC7's word order) and most in the cartridge clock, which each keeps its own way and
// RetroArch keeps beside the save in a file of its own. saveporter converts them, from the game's
// cartridge header.
//
// On RomM a save is the cartridge's RAM as the web player's core keeps it, followed by the clock as
// VBA-M keeps it when the cartridge has one. RetroArch loads no more of a save file than the
// cartridge's RAM, so the web player plays it as its own, and the clock carries between the Deck's
// emulators and Decks.

// GBSystems are the EmuDeck folders whose saves are Game Boy and Game Boy Color battery saves.
var GBSystems = []string{"gb", "gbc"}

// GBSaveFormat is the format an emulator keeps a Game Boy save in, from the emulator and the tag
// RomM keeps its saves under (the core's name, for RetroArch).
func GBSaveFormat(emulator, tag string) (gb.Format, bool) {
	switch emulator {
	case "mgba":
		return gb.MGBAApp, true
	case RetroArchID:
		if f := gb.Format(tag); f != gb.MGBAApp && slices.Contains(gb.Formats, f) {
			return f, true
		}
	}
	return "", false
}

// gbServerRAM is the format of the RAM on RomM: the web player's default core's, or its other core's
// for a cartridge Gambatte doesn't run.
func gbServerRAM(cart gb.Cart) gb.Format {
	if _, err := gb.Write(gb.Gambatte, cart, blankGBSave(cart), time.Time{}); err == nil {
		return gb.Gambatte
	}
	return gb.MGBA
}

func blankGBSave(cart gb.Cart) gb.Save {
	n, _ := cart.SaveSize()
	s := gb.Save{RAM: make([]byte, n)}
	switch {
	case cart.Clock():
		s.Clock = &gb.Clock{}
	case cart.Mapper() == gb.HuC3:
		s.HuC3 = &gb.HuC3Clock{}
	}
	return s
}

// GBServerSave is a save as RomM keeps it. at is the time the clock is written as of: the save's
// own last write, so the same save always reads the same.
func GBServerSave(cart gb.Cart, s gb.Save, at time.Time) ([]byte, error) {
	ram, err := gb.Write(gbServerRAM(cart), cart, gb.Save{RAM: s.RAM}, at)
	if err != nil {
		return nil, err
	}
	out := ram[gb.ExtSRM]
	if s.Clock == nil && s.HuC3 == nil {
		return out, nil
	}
	clock, err := gb.Write(gb.VBAM, cart, s, at)
	if err != nil {
		return nil, err
	}
	return append(out, clock[gb.ExtRTC]...), nil
}

// ErrGBSaveMismatch means a save on RomM isn't one for the game's cartridge in any emulator's
// layout.
var ErrGBSaveMismatch = errors.New("RomM's save doesn't fit this game's cartridge")

// ParseGBServerSave reads a save from RomM: as the plugin keeps it, or as the emulator its tag
// names wrote it (the web player, other clients), or as any other emulator did.
func ParseGBServerSave(data []byte, tag string, cart gb.Cart, now time.Time) (gb.Save, error) {
	web := gbServerRAM(cart)
	blank := blankGBSave(cart)
	ram, err := gb.Write(web, cart, gb.Save{RAM: blank.RAM}, now)
	if err != nil {
		return gb.Save{}, err
	}
	ramLen := len(ram[gb.ExtSRM])
	clockLen := 0
	if blank.Clock != nil || blank.HuC3 != nil {
		if clock, err := gb.Write(gb.VBAM, cart, blank, now); err == nil {
			clockLen = len(clock[gb.ExtRTC])
		}
	}
	switch {
	case len(data) == ramLen:
		return gb.Read(web, cart, gb.Files{gb.ExtSRM: data}, now)
	case clockLen > 0 && len(data) == ramLen+clockLen:
		s, err := gb.Read(web, cart, gb.Files{gb.ExtSRM: data[:ramLen]}, now)
		if err != nil {
			return gb.Save{}, err
		}
		clock, err := gb.Read(gb.VBAM, cart, gb.Files{gb.ExtSRM: data[:ramLen], gb.ExtRTC: data[ramLen:]}, now)
		if err != nil {
			return gb.Save{}, err
		}
		s.Clock, s.HuC3 = clock.Clock, clock.HuC3
		return s, nil
	}
	// Another emulator's own file: the tag's first. A format matches when it writes a file of this
	// size for the cartridge.
	formats := slices.Clone(gb.Formats)
	if f, ok := GBSaveFormat(RetroArchID, tag); ok {
		formats = append([]gb.Format{f}, formats...)
	} else if tag == "mgba" {
		formats = append([]gb.Format{gb.MGBAApp}, formats...)
	}
	for _, f := range formats {
		files := gb.Files{gb.ExtSRM: data}
		if f == gb.MGBAApp {
			files = gb.Files{gb.ExtSav: data}
		}
		s, err := gb.Read(f, cart, files, now)
		if err != nil {
			continue
		}
		again, err := gb.Write(f, cart, s, now)
		if err != nil {
			continue
		}
		if len(again[gb.ExtSRM])+len(again[gb.ExtSav]) == len(data) {
			return s, nil
		}
	}
	return gb.Save{}, fmt.Errorf("%w: %d bytes", ErrGBSaveMismatch, len(data))
}
