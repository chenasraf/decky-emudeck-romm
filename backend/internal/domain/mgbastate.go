package domain

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"slices"
)

// An mGBA Game Boy Advance save state. mGBA keeps the console's state in a fixed 0x61000-byte
// block, followed by tagged extra data (the cartridge's save, the clock); its libretro core, which
// RetroArch and the Server's web player run, hands RetroArch exactly that. Standalone mGBA writes
// the same pieces into a PNG of the screen instead, and reads either.
//
// The block's layout carries a version. mGBA loads states of its own version or older, and refuses
// newer ones, so a state from a newer mGBA is rewritten as the older version wrote it: every
// version up to mgbaStateNewest only gave meaning to bytes the older ones left zero or ignore, and
// moved the one field the older ones read, the clock's transfer step, into the pins it follows from.

const (
	mgbaStateSize   = 0x61000
	mgbaStateMagic  = 0x01000000
	mgbaStateNewest = 0x0B
	// MGBAWebStateVersion is the state version of the mGBA core in the Server's web player
	// (EmulatorJS 4.2.3, as RomM ships it), the newest that player loads.
	MGBAWebStateVersion = 0x07
	// MGBAAppStateVersion is the state version of standalone mGBA as EmuDeck installs it (0.10.x),
	// the newest it loads.
	MGBAAppStateVersion = 0x07

	mgbaExtHeaderSize = 16
	mgbaExtMaxTag     = 0x200
)

// Reserved bytes each state version gave meaning to, which older versions write as zero. The GPIO
// write latch and flags v8 and v9 put in the high bytes of v7's 16-bit pin state and direction
// stay: v7 reads only the low 4 bits, and a newer mGBA reads them back from a round trip.
var mgbaStateFieldsSince = []struct {
	version  uint32
	from, to int
}{
	{0x0A, 0x3D4, 0x3E0}, // DMA 1 to 3 latches
	{0x0B, 0x3E0, 0x3E8}, // DMA 0 to 3 count latches
}

const (
	mgbaPinState = 0x290
	// mgbaRTCTransferStep is where mGBA before v9 keeps how far a clock transfer has gone, which v9
	// leaves zero and follows from the pins instead.
	mgbaRTCTransferStep = 0x298
	mgbaRTCStateMachine = 0x09
)

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

var (
	// ErrNotGBAState means a file isn't an mGBA Game Boy Advance save state.
	ErrNotGBAState = errors.New("not an mGBA Game Boy Advance save state")
	// ErrGBAStateTooNew means a state comes from an mGBA newer than this plugin knows how to
	// rewrite for an older one.
	ErrGBAStateTooNew = errors.New("the save state is from a newer mGBA than this plugin knows")
)

// MGBAState returns an mGBA Game Boy Advance state in the layout mGBA's libretro core reads, from
// a state standalone mGBA or the core wrote.
func MGBAState(data []byte) ([]byte, error) {
	if bytes.HasPrefix(data, pngSignature) {
		var err error
		if data, err = mgbaStateFromPNG(data); err != nil {
			return nil, err
		}
	}
	if _, err := MGBAStateVersion(data); err != nil {
		return nil, err
	}
	return data, nil
}

// MGBAStateVersion returns the layout version of an mGBA Game Boy Advance state.
func MGBAStateVersion(state []byte) (uint32, error) {
	if len(state) < mgbaStateSize {
		return 0, ErrNotGBAState
	}
	magic := binary.LittleEndian.Uint32(state)
	if magic < mgbaStateMagic || magic > mgbaStateMagic+0xFF {
		return 0, ErrNotGBAState
	}
	return magic - mgbaStateMagic, nil
}

// MGBAStateROMCRC returns the CRC32 of the ROM a state was saved from. mGBA won't load a state into
// another ROM.
func MGBAStateROMCRC(state []byte) uint32 { return binary.LittleEndian.Uint32(state[8:]) }

// MGBAStateForVersion returns state as an mGBA whose states are of the given version loads it.
func MGBAStateForVersion(state []byte, version uint32) ([]byte, error) {
	have, err := MGBAStateVersion(state)
	if err != nil {
		return nil, err
	}
	if have <= version {
		return state, nil
	}
	if have > mgbaStateNewest {
		return nil, fmt.Errorf("%w: version %d", ErrGBAStateTooNew, have)
	}
	out := slices.Clone(state)
	binary.LittleEndian.PutUint32(out, mgbaStateMagic+version)
	for _, f := range mgbaStateFieldsSince {
		if f.version > version {
			clear(out[f.from:f.to])
		}
	}
	if have >= mgbaRTCStateMachine && version < mgbaRTCStateMachine {
		binary.LittleEndian.PutUint32(out[mgbaRTCTransferStep:], rtcTransferStep(out[mgbaPinState]))
	}
	return out, nil
}

// rtcTransferStep is the step mGBA before v9 is at with the clock's pins as given: 2 while chip
// select is high (a transfer), 1 when it's low with the clock line high (ready to start one), 0
// otherwise.
func rtcTransferStep(pins byte) uint32 {
	const sck, cs = 1, 4
	switch {
	case pins&cs != 0:
		return 2
	case pins&sck != 0:
		return 1
	}
	return 0
}

// GBAStateForWeb returns a Game Boy Advance state from RetroArch or standalone mGBA as the Server's
// web player loads it.
func GBAStateForWeb(data []byte) ([]byte, error) {
	state, err := gbaCoreState(data)
	if err != nil {
		return nil, err
	}
	if state, err = MGBAStateForVersion(state, MGBAWebStateVersion); err != nil {
		return nil, err
	}
	return RetroArchState(state), nil
}

// GBAStateForRetroArch returns a Game Boy Advance state from the Server's web player or standalone
// mGBA as RetroArch's mGBA core loads it.
func GBAStateForRetroArch(data []byte) ([]byte, error) {
	state, err := gbaCoreState(data)
	if err != nil {
		return nil, err
	}
	return RetroArchState(state), nil
}

// GBAStateForMGBA returns a Game Boy Advance state from the Server's web player or RetroArch as
// standalone mGBA loads it.
func GBAStateForMGBA(data []byte) ([]byte, error) {
	state, err := gbaCoreState(data)
	if err != nil {
		return nil, err
	}
	return MGBAStateForVersion(state, MGBAAppStateVersion)
}

// gbaCoreState returns the mGBA state in a state any of the Game Boy Advance emulators wrote.
func gbaCoreState(data []byte) ([]byte, error) {
	if bytes.HasPrefix(data, pngSignature) {
		return MGBAState(data)
	}
	core, err := RetroArchCoreState(data)
	if err != nil {
		return nil, err
	}
	return MGBAState(core)
}

// mgbaStateFromPNG takes the state and its extra data out of a state standalone mGBA kept in a PNG.
func mgbaStateFromPNG(data []byte) ([]byte, error) {
	var state []byte
	type extItem struct {
		tag  uint32
		data []byte
	}
	var ext []extItem
	for rest := data[len(pngSignature):]; len(rest) >= 12; {
		size := binary.BigEndian.Uint32(rest)
		if uint64(size) > uint64(len(rest)-12) {
			return nil, fmt.Errorf("%w: PNG chunk runs past the end", ErrNotGBAState)
		}
		kind, body := string(rest[4:8]), rest[8:8+size]
		rest = rest[12+size:]
		switch kind {
		case "gbAs":
			var err error
			if state, err = inflate(body, mgbaStateSize); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrNotGBAState, err)
			}
		case "gbAx":
			if len(body) < 8 {
				continue
			}
			tag, n := binary.LittleEndian.Uint32(body), binary.LittleEndian.Uint32(body[4:])
			if tag == 0 || tag >= mgbaExtMaxTag || n > 1<<26 {
				continue
			}
			item, err := inflate(body[8:], int(n))
			if err != nil {
				continue // mGBA skips extra data it can't inflate too
			}
			ext = append(ext, extItem{tag, item})
		case "IEND":
			rest = nil
		}
	}
	if state == nil {
		return nil, fmt.Errorf("%w: the PNG holds no state", ErrNotGBAState)
	}
	if len(state) != mgbaStateSize {
		return nil, fmt.Errorf("%w: the PNG's state is %d bytes", ErrNotGBAState, len(state))
	}
	if len(ext) == 0 {
		return state, nil
	}

	// The raw layout: the state, a table of (tag, size, offset from the start) closed by a zero
	// entry, then each item's data, in tag order.
	slices.SortStableFunc(ext, func(a, b extItem) int { return int(a.tag) - int(b.tag) })
	offset := mgbaStateSize + (len(ext)+1)*mgbaExtHeaderSize
	out := slices.Grow(state, offset-len(state))
	for _, e := range ext {
		out = binary.LittleEndian.AppendUint32(out, e.tag)
		out = binary.LittleEndian.AppendUint32(out, uint32(len(e.data))) //nolint:gosec // capped above
		out = binary.LittleEndian.AppendUint64(out, uint64(offset))      //nolint:gosec // positive
		offset += len(e.data)
	}
	out = append(out, make([]byte, mgbaExtHeaderSize)...)
	for _, e := range ext {
		out = append(out, e.data...)
	}
	return out, nil
}

// inflate decompresses zlib data that inflates to at most limit bytes.
func inflate(data []byte, limit int) ([]byte, error) {
	zr, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()
	out, err := io.ReadAll(io.LimitReader(zr, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(out) > limit {
		return nil, errors.New("data inflates past its size")
	}
	return out, nil
}
