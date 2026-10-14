package domain

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// mgbaTestState is a raw mGBA GBA state of the given version, with every byte of the newer
// versions' fields set, a cartridge save and a clock as extra data.
func mgbaTestState(version uint32) []byte {
	s := make([]byte, mgbaStateSize)
	for i := range s {
		s[i] = byte(i * 7)
	}
	binary.LittleEndian.PutUint32(s, mgbaStateMagic+version)
	binary.LittleEndian.PutUint32(s[8:], 0x27B2E7AE)
	save := bytes.Repeat([]byte{0xA5}, 0x20000)
	rtc := bytes.Repeat([]byte{0x11}, 16)
	offset := mgbaStateSize + 3*mgbaExtHeaderSize
	for _, e := range []struct {
		tag  uint32
		data []byte
	}{{2, save}, {4, rtc}} {
		s = binary.LittleEndian.AppendUint32(s, e.tag)
		s = binary.LittleEndian.AppendUint32(s, uint32(len(e.data)))
		s = binary.LittleEndian.AppendUint64(s, uint64(offset))
		offset += len(e.data)
	}
	s = append(s, make([]byte, mgbaExtHeaderSize)...)
	s = append(s, save...)
	return append(s, rtc...)
}

func zlibBytes(t testing.TB, data []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zlib.NewWriter(&b)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// rzipFile compresses data as RetroArch's RZIP stream does, in chunks of 128 KiB.
func rzipFile(t testing.TB, version byte, data []byte) []byte {
	t.Helper()
	const chunk = 128 << 10
	out := []byte(rzipMagic)
	out = append(out, version, '#')
	out = binary.LittleEndian.AppendUint32(out, chunk)
	out = binary.LittleEndian.AppendUint64(out, uint64(len(data)))
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = enc.Close() }()
	for i := 0; i < len(data); i += chunk {
		part := data[i:min(i+chunk, len(data))]
		var z []byte
		if version == rzipZstd {
			z = enc.EncodeAll(part, nil)
		} else {
			z = zlibBytes(t, part)
		}
		out = binary.LittleEndian.AppendUint32(out, uint32(len(z)))
		out = append(out, z...)
	}
	return out
}

// mgbaPNGState is a state as standalone mGBA writes it: a screenshot with the state and each
// extra data item in private chunks.
func mgbaPNGState(t testing.TB, raw []byte) []byte {
	t.Helper()
	var img bytes.Buffer
	if err := png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 240, 160))); err != nil {
		t.Fatal(err)
	}
	chunk := func(kind string, body []byte) []byte {
		c := binary.BigEndian.AppendUint32(nil, uint32(len(body)))
		c = append(c, kind...)
		c = append(c, body...)
		return binary.BigEndian.AppendUint32(c, crc32.ChecksumIEEE(append([]byte(kind), body...)))
	}
	extra := chunk("gbAs", zlibBytes(t, raw[:mgbaStateSize]))
	// Written in reverse tag order, to check the raw layout comes out sorted.
	for _, e := range []struct {
		tag      uint32
		from, to int
	}{{4, mgbaStateSize + 48 + 0x20000, len(raw)}, {2, mgbaStateSize + 48, mgbaStateSize + 48 + 0x20000}} {
		body := binary.LittleEndian.AppendUint32(nil, e.tag)
		body = binary.LittleEndian.AppendUint32(body, uint32(e.to-e.from))
		extra = append(extra, chunk("gbAx", append(body, zlibBytes(t, raw[e.from:e.to])...))...)
	}
	b := img.Bytes()
	iend := len(b) - 12
	return append(append(append([]byte{}, b[:iend]...), extra...), b[iend:]...)
}

func TestRetroArchStateRoundTrip(t *testing.T) {
	core := mgbaTestState(7)
	wrapped := RetroArchState(core)
	if !bytes.HasPrefix(wrapped, []byte("RASTATE\x01MEM ")) || !bytes.HasSuffix(wrapped, []byte("END \x00\x00\x00\x00")) {
		t.Fatalf("container starts %q", wrapped[:12])
	}
	got, err := RetroArchCoreState(wrapped)
	if err != nil || !bytes.Equal(got, core) {
		t.Fatalf("round trip: %v", err)
	}
}

func TestRetroArchCoreStateReadsEveryShape(t *testing.T) {
	core := mgbaTestState(9)
	for name, file := range map[string][]byte{
		"raw core state": core,
		"container":      RetroArchState(core),
		"deflate RZIP":   rzipFile(t, rzipDeflate, RetroArchState(core)),
		"zstd RZIP":      rzipFile(t, rzipZstd, RetroArchState(core)),
	} {
		got, err := RetroArchCoreState(file)
		if err != nil || !bytes.Equal(got, core) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestRetroArchCoreStateSkipsOtherBlocks(t *testing.T) {
	core := []byte{1, 2, 3}
	file := []byte("RASTATE\x01ACHV\x05\x00\x00\x00abcde\x00\x00\x00MEM \x03\x00\x00\x00\x01\x02\x03")
	got, err := RetroArchCoreState(file)
	if err != nil || !bytes.Equal(got, core) {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestRetroArchCoreStateRejectsDamage(t *testing.T) {
	whole := rzipFile(t, rzipDeflate, RetroArchState(mgbaTestState(7)))
	for name, file := range map[string][]byte{
		"truncated RZIP": whole[:len(whole)/2],
		"block too long": []byte("RASTATE\x01MEM \xff\x00\x00\x00abc"),
		"no core block":  []byte("RASTATE\x01END \x00\x00\x00\x00"),
	} {
		if _, err := RetroArchCoreState(file); !errors.Is(err, ErrNotRetroArchState) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestMGBAStateForVersionRewritesNewerFields(t *testing.T) {
	newer := mgbaTestState(0x0B)
	got, err := MGBAStateForVersion(newer, 7)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := MGBAStateVersion(got); v != 7 {
		t.Fatalf("version %d", v)
	}
	for _, f := range mgbaStateFieldsSince {
		if !bytes.Equal(got[f.from:f.to], make([]byte, f.to-f.from)) {
			t.Errorf("bytes %#x-%#x kept", f.from, f.to)
		}
	}
	// Everything else, extra data and the GPIO bytes v7 masks off included, stays as it was.
	kept := bytes.Clone(newer)
	copy(kept, got[:4])
	copy(kept[mgbaRTCTransferStep:], got[mgbaRTCTransferStep:mgbaRTCTransferStep+4])
	for _, f := range mgbaStateFieldsSince {
		clear(kept[f.from:f.to])
	}
	if !bytes.Equal(got, kept) {
		t.Fatal("bytes outside the newer fields changed")
	}
	if binary.LittleEndian.Uint32(newer) != mgbaStateMagic+0x0B {
		t.Fatal("the input was changed")
	}
}

func TestMGBAStateForVersionKeepsWhatANewerVersionReads(t *testing.T) {
	// The DMA latches a v9 loader doesn't read go; the clock's transfer step, which v9 keeps
	// elsewhere, stays as the newer state wrote it.
	newer := mgbaTestState(0x0B)
	got, err := MGBAStateForVersion(newer, 9)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got[0x3D4:0x3E8], make([]byte, 0x14)) {
		t.Fatal("DMA latches kept")
	}
	if !bytes.Equal(got[mgbaRTCTransferStep:mgbaRTCTransferStep+4], newer[mgbaRTCTransferStep:mgbaRTCTransferStep+4]) {
		t.Fatal("transfer step rewritten for a version that has the state machine")
	}
}

func TestMGBAStateForVersionDerivesRTCTransferStep(t *testing.T) {
	for pins, want := range map[byte]uint32{0x00: 0, 0x02: 0, 0x01: 1, 0x03: 1, 0x04: 2, 0x05: 2, 0x07: 2} {
		newer := mgbaTestState(9)
		newer[mgbaPinState] = pins
		got, err := MGBAStateForVersion(newer, 7)
		if err != nil {
			t.Fatal(err)
		}
		if step := binary.LittleEndian.Uint32(got[mgbaRTCTransferStep:]); step != want {
			t.Errorf("pins %#x: step %d, want %d", pins, step, want)
		}
	}
}

func TestMGBAStateForVersionLeavesOlderStates(t *testing.T) {
	old := mgbaTestState(7)
	got, err := MGBAStateForVersion(old, 9)
	if err != nil || !bytes.Equal(got, old) {
		t.Fatalf("an older state changed: %v", err)
	}
}

func TestMGBAStateForVersionRefusesUnknownVersions(t *testing.T) {
	if _, err := MGBAStateForVersion(mgbaTestState(0x0C), 7); !errors.Is(err, ErrGBAStateTooNew) {
		t.Fatalf("got %v", err)
	}
}

func TestMGBAStateRejectsOtherData(t *testing.T) {
	gb := mgbaTestState(7)
	binary.LittleEndian.PutUint32(gb, 0x00400002) // a Game Boy state
	for name, data := range map[string][]byte{"short": make([]byte, 100), "game boy": gb} {
		if _, err := MGBAState(data); !errors.Is(err, ErrNotGBAState) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestMGBAStateFromPNG(t *testing.T) {
	raw := mgbaTestState(0x0B)
	got, err := MGBAState(mgbaPNGState(t, raw))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatal("the PNG's state didn't come out in the raw layout")
	}
}

func TestGBAStateConversions(t *testing.T) {
	deck := rzipFile(t, rzipDeflate, RetroArchState(mgbaTestState(9)))
	web, err := GBAStateForWeb(deck)
	if err != nil {
		t.Fatal(err)
	}
	core, err := RetroArchCoreState(web)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := MGBAStateVersion(core); v != MGBAWebStateVersion || MGBAStateROMCRC(core) != 0x27B2E7AE {
		t.Fatalf("web state is version %d for ROM %08x", v, MGBAStateROMCRC(core))
	}

	fromWeb := RetroArchState(mgbaTestState(7))
	ra, err := GBAStateForRetroArch(fromWeb)
	if err != nil || !bytes.Equal(ra, fromWeb) {
		t.Fatalf("a web state should reach RetroArch as is: %v", err)
	}
	standalone, err := GBAStateForMGBA(fromWeb)
	if err != nil || !bytes.Equal(standalone, mgbaTestState(7)) {
		t.Fatalf("standalone mGBA should get the raw state: %v", err)
	}
	fromDeck, err := GBAStateForMGBA(rzipFile(t, rzipDeflate, RetroArchState(mgbaTestState(9))))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := MGBAStateVersion(fromDeck); v != MGBAAppStateVersion {
		t.Fatalf("standalone mGBA got version %d", v)
	}
	fromPNG, err := GBAStateForWeb(mgbaPNGState(t, mgbaTestState(0x0B)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(fromPNG, []byte("RASTATE")) {
		t.Fatal("a standalone mGBA state should reach the web player in RetroArch's container")
	}
}

// The parsers read files from other devices and the Server, so damaged ones must fail, not panic.
func FuzzGBAStateConversions(f *testing.F) {
	state := mgbaTestState(9)
	prefix := func(b []byte, n int) []byte { return b[:min(n, len(b))] }
	f.Add(prefix(RetroArchState(state), 4096))
	f.Add(prefix(rzipFile(f, rzipDeflate, RetroArchState(state)), 4096))
	f.Add(prefix(rzipFile(f, rzipZstd, RetroArchState(state)), 4096))
	f.Add(prefix(mgbaPNGState(f, state), 4096))
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, convert := range []func([]byte) ([]byte, error){GBAStateForWeb, GBAStateForRetroArch, GBAStateForMGBA} {
			if out, err := convert(data); err == nil && len(out) < mgbaStateSize {
				t.Fatalf("converted to %d bytes", len(out))
			}
		}
	})
}
