package domain

import "github.com/chenasraf/saveporter"

// Game Boy Advance save states, converted between the emulators that keep them: RetroArch and
// standalone mGBA on the Deck, and the Server's web player (EmulatorJS). All three run mGBA, so a
// state converts losslessly; saveporter rewrites a newer mGBA's state for an older one.

// GBAStateForWeb returns a Game Boy Advance state from RetroArch or standalone mGBA as the Server's
// web player loads it.
func GBAStateForWeb(data []byte) ([]byte, error) {
	return saveporter.Convert(data, saveporter.Target{Emulator: saveporter.EmulatorJS})
}

// GBAStateForRetroArch returns a Game Boy Advance state from the Server's web player or standalone
// mGBA as RetroArch's mGBA core loads it.
func GBAStateForRetroArch(data []byte) ([]byte, error) {
	return saveporter.Convert(data, saveporter.Target{Emulator: saveporter.RetroArch})
}

// GBAStateForMGBA returns a Game Boy Advance state from the Server's web player or RetroArch as
// standalone mGBA loads it.
func GBAStateForMGBA(data []byte) ([]byte, error) {
	return saveporter.Convert(data, saveporter.Target{Emulator: saveporter.MGBA})
}
