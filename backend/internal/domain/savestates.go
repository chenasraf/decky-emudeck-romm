package domain

import (
	"errors"
	"fmt"
	"slices"

	"github.com/chenasraf/saveporter"
)

// Save states are converted between the emulators that keep them: RetroArch and standalone mGBA on
// the Deck, and the Server's web player (EmulatorJS). A state is a snapshot of one core's model of
// the console, so it carries only between emulators running the same core: mGBA for the Game Boy
// Advance, Gambatte or mGBA for the Game Boy and Game Boy Color. saveporter rewrites a state for the
// target's build of the core.

// StateCore is the core whose states the emulator keeps for the games of an EmuDeck system, from the
// emulator's ID and, for RetroArch, the core it runs the game in. The Server's web player keeps a
// state under its core's name and lists only those of the core it plays the game in, so the core is
// also the tag a state syncs under. ok is false when the game's states don't sync.
func StateCore(system, emulator, retroArchCore string) (string, bool) {
	cores := []string{saveporter.CoreMGBA}
	switch {
	case slices.Contains(GBSystems, system):
		cores = append(cores, saveporter.CoreGambatte)
	case system != "gba":
		return "", false
	}
	switch emulator {
	case "mgba":
		return saveporter.CoreMGBA, true
	case RetroArchID:
		if slices.Contains(cores, retroArchCore) {
			return retroArchCore, true
		}
	}
	return "", false
}

// ErrStateMismatch means a state is of another core or console than the one the game plays on.
var ErrStateMismatch = errors.New("the state is of another emulator core or console")

// StateFor returns a state as the emulator to loads it, for a game of the EmuDeck system that plays
// in core.
func StateFor(data []byte, system, core string, to saveporter.Emulator) ([]byte, error) {
	info, err := saveporter.Detect(data)
	if err != nil {
		return nil, err
	}
	gb := info.System == saveporter.GB || info.System == saveporter.GBC
	if info.Core != core || gb != slices.Contains(GBSystems, system) {
		return nil, fmt.Errorf("%w: a %s state of %s, and the game plays in %s", ErrStateMismatch, info.System, info.Core, core)
	}
	return saveporter.Convert(data, saveporter.Target{Emulator: to})
}
