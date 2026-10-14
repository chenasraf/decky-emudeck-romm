package domain

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/chenasraf/saveporter"
	"github.com/chenasraf/saveporter/retroarch"
)

// deckGBAState is a RetroArch GBA state from the mGBA core the Deck runs, which writes v9.
func deckGBAState() []byte {
	core := make([]byte, 0x61000+16) // the state block and an empty extra-data table
	binary.LittleEndian.PutUint32(core, 0x01000009)
	return retroarch.Wrap(core)
}

// deckGambatteState is a RetroArch Game Boy Color state from the Gambatte core the Deck runs, whose
// build keeps the network link's two records.
func deckGambatteState() []byte {
	s := []byte{0x00, 0x01, 0, 0, 0}
	for _, r := range []struct {
		label string
		value []byte
	}{
		{"hram", make([]byte, 0x200)}, {"netsfc", []byte{0x00}}, {"netsv", []byte{0xFF}}, {"pc", []byte{0x01, 0x50}},
		{"sp", []byte{0xDF, 0xF0}}, {"sram", make([]byte, 0x2000)}, {"vram", make([]byte, 0x4000)}, {"wram", make([]byte, 0x8000)},
	} {
		s = append(s, r.label...)
		s = append(s, 0, byte(len(r.value)>>16), byte(len(r.value)>>8), byte(len(r.value)))
		s = append(s, r.value...)
	}
	return retroarch.Wrap(s)
}

func TestStateCore(t *testing.T) {
	for _, tt := range []struct {
		system, emulator, core string
		want                   string
		ok                     bool
	}{
		{"gba", RetroArchID, "mgba", "mgba", true},
		{"gba", "mgba", "", "mgba", true},
		{"gba", RetroArchID, "gambatte", "", false},
		{"gbc", RetroArchID, "gambatte", "gambatte", true},
		{"gb", RetroArchID, "mgba", "mgba", true},
		{"gb", "mgba", "", "mgba", true},
		{"gbc", RetroArchID, "sameboy", "", false},
		{"snes", RetroArchID, "snes9x", "", false},
	} {
		if got, ok := StateCore(tt.system, tt.emulator, tt.core); got != tt.want || ok != tt.ok {
			t.Errorf("%s/%s/%s: %q %v", tt.system, tt.emulator, tt.core, got, ok)
		}
	}
}

func TestStateFor(t *testing.T) {
	for name, c := range map[string]struct {
		data         []byte
		system, core string
		to           saveporter.Emulator
		container    saveporter.Container
		version      uint32
		layout       string
	}{
		"GBA for the web player":      {deckGBAState(), "gba", "mgba", saveporter.EmulatorJS, saveporter.ContainerRetroArch, 7, ""},
		"GBA for RetroArch":           {deckGBAState(), "gba", "mgba", saveporter.RetroArch, saveporter.ContainerRetroArch, 9, ""},
		"GBA for standalone mGBA":     {deckGBAState(), "gba", "mgba", saveporter.MGBA, saveporter.ContainerRaw, 7, ""},
		"Gambatte for the web player": {deckGambatteState(), "gbc", "gambatte", saveporter.EmulatorJS, saveporter.ContainerRetroArch, 0, "no optional parts"},
		"Gambatte for RetroArch":      {deckGambatteState(), "gb", "gambatte", saveporter.RetroArch, saveporter.ContainerRetroArch, 0, "with network link"},
	} {
		out, err := StateFor(c.data, c.system, c.core, c.to)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		info, err := saveporter.Detect(out)
		if err != nil || info.Container != c.container || info.StateVersion != c.version || info.Layout != c.layout {
			t.Errorf("%s: %s v%d %q, %v", name, info.Container, info.StateVersion, info.Layout, err)
		}
	}
	if _, err := StateFor([]byte("not a state"), "gba", "mgba", saveporter.EmulatorJS); err == nil {
		t.Error("converted a file that isn't a state")
	}
	// A state of the other core, or of the other console, isn't the game's.
	for name, c := range map[string]struct {
		data         []byte
		system, core string
	}{
		"Gambatte's for mGBA":   {deckGambatteState(), "gbc", "mgba"},
		"a GBA state for a GBC": {deckGBAState(), "gbc", "mgba"},
		"a GBC state for a GBA": {deckGambatteState(), "gba", "gambatte"},
	} {
		if _, err := StateFor(c.data, c.system, c.core, saveporter.RetroArch); !errors.Is(err, ErrStateMismatch) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// The web player's state carries to the Deck's build and back unchanged.
	web, _ := StateFor(deckGambatteState(), "gbc", "gambatte", saveporter.EmulatorJS)
	back, err := StateFor(web, "gbc", "gambatte", saveporter.RetroArch)
	if err != nil || !bytes.Equal(back, deckGambatteState()) {
		t.Errorf("round trip: %v", err)
	}
}
