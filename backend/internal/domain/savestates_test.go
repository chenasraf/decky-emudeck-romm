package domain

import (
	"encoding/binary"
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

func TestGBAStateConversions(t *testing.T) {
	for name, c := range map[string]struct {
		convert   func([]byte) ([]byte, error)
		container saveporter.Container
		version   uint32
	}{
		"web player":      {GBAStateForWeb, saveporter.ContainerRetroArch, 7},
		"RetroArch":       {GBAStateForRetroArch, saveporter.ContainerRetroArch, 9},
		"standalone mGBA": {GBAStateForMGBA, saveporter.ContainerRaw, 7},
	} {
		out, err := c.convert(deckGBAState())
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		info, err := saveporter.Detect(out)
		if err != nil || info.Container != c.container || info.StateVersion != c.version {
			t.Errorf("%s: %s v%d, %v", name, info.Container, info.StateVersion, err)
		}
	}
	if _, err := GBAStateForWeb([]byte("not a state")); err == nil {
		t.Error("converted a file that isn't a state")
	}
}
