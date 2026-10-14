package domain

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/klauspost/compress/zstd"
)

// A RetroArch save state, as RetroArch and the Server's web player (EmulatorJS, built on
// RetroArch) write it: the core's own state in a "RASTATE" container, which RetroArch may compress
// into an RZIP file. RetroArch reads every shape, compressed or not, so a state is written back as
// a plain container.

const (
	raStateMagic   = "RASTATE"
	raStateVersion = 1
	raBlockHeader  = 8

	rzipMagic        = "#RZIPv"
	rzipHeaderSize   = 20
	rzipDeflate      = 1
	rzipZstd         = 2
	rzipMaxChunkSize = 64 << 20
)

var (
	raBlockMem = []byte("MEM ")
	raBlockEnd = []byte("END ")
)

// ErrNotRetroArchState means a file isn't a save state RetroArch wrote.
var ErrNotRetroArchState = errors.New("not a RetroArch save state")

// RetroArchCoreState returns the core's state from a RetroArch save state. A state with no
// container, as RetroArch wrote them before it had one, is the core's state as is.
func RetroArchCoreState(data []byte) ([]byte, error) {
	data, err := unRZIP(data)
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(data, []byte(raStateMagic)) {
		return data, nil
	}
	if len(data) < len(raStateMagic)+1 || data[len(raStateMagic)] != raStateVersion {
		return nil, fmt.Errorf("%w: unknown container version", ErrNotRetroArchState)
	}
	for rest := data[len(raStateMagic)+1:]; len(rest) >= raBlockHeader; {
		tag, size := rest[:4], int(binary.LittleEndian.Uint32(rest[4:8]))
		rest = rest[raBlockHeader:]
		if bytes.Equal(tag, raBlockEnd) {
			break
		}
		if size > len(rest) {
			return nil, fmt.Errorf("%w: block %q runs past the end", ErrNotRetroArchState, tag)
		}
		if bytes.Equal(tag, raBlockMem) {
			return rest[:size], nil
		}
		rest = rest[min(raAlign(size), len(rest)):]
	}
	return nil, fmt.Errorf("%w: no core state in it", ErrNotRetroArchState)
}

// RetroArchState wraps a core's state in RetroArch's container, as RetroArch and the Server's web
// player write it with compression off.
func RetroArchState(core []byte) []byte {
	out := make([]byte, 0, len(raStateMagic)+1+2*raBlockHeader+raAlign(len(core)))
	out = append(out, raStateMagic...)
	out = append(out, raStateVersion)
	out = append(out, raBlockMem...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(core))) //nolint:gosec // a core state is far below 4 GiB
	out = append(out, core...)
	out = append(out, make([]byte, raAlign(len(core))-len(core))...)
	out = append(out, raBlockEnd...)
	return binary.LittleEndian.AppendUint32(out, 0)
}

// raAlign rounds a block's size up to the 8 bytes RetroArch pads every block to.
func raAlign(n int) int { return (n + 7) &^ 7 }

// unRZIP decompresses an RZIP file, and returns anything else as is.
func unRZIP(data []byte) ([]byte, error) {
	if len(data) < rzipHeaderSize || !bytes.HasPrefix(data, []byte(rzipMagic)) || data[7] != '#' {
		return data, nil
	}
	version := data[6]
	if version != rzipDeflate && version != rzipZstd {
		return data, nil
	}
	chunkSize := binary.LittleEndian.Uint32(data[8:12])
	total := binary.LittleEndian.Uint64(data[12:20])
	if chunkSize == 0 || chunkSize > rzipMaxChunkSize || total == 0 || total > 1<<32 {
		return nil, fmt.Errorf("%w: bad RZIP header", ErrNotRetroArchState)
	}

	var zd *zstd.Decoder
	if version == rzipZstd {
		var err error
		if zd, err = zstd.NewReader(nil, zstd.WithDecoderConcurrency(1)); err != nil {
			return nil, err
		}
		defer zd.Close()
	}
	out := make([]byte, 0, total)
	for rest := data[rzipHeaderSize:]; uint64(len(out)) < total; {
		if len(rest) < 4 {
			return nil, fmt.Errorf("%w: RZIP file ends early", ErrNotRetroArchState)
		}
		size := binary.LittleEndian.Uint32(rest[:4])
		rest = rest[4:]
		if size == 0 || uint64(size) > uint64(len(rest)) {
			return nil, fmt.Errorf("%w: bad RZIP chunk", ErrNotRetroArchState)
		}
		chunk := rest[:size]
		rest = rest[size:]
		var err error
		if zd != nil {
			out, err = zd.DecodeAll(chunk, out)
		} else {
			var inflated []byte
			inflated, err = inflate(chunk, int(chunkSize))
			out = append(out, inflated...)
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrNotRetroArchState, err)
		}
	}
	if uint64(len(out)) != total {
		return nil, fmt.Errorf("%w: RZIP file holds %d bytes, not %d", ErrNotRetroArchState, len(out), total)
	}
	return out, nil
}
