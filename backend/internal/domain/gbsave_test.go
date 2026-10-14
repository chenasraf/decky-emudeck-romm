package domain

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/chenasraf/saveporter/gb"
)

func gbCart(t *testing.T, cartType, romSize, ramSize byte) gb.Cart {
	t.Helper()
	h := make([]byte, gb.HeaderSize)
	copy(h[0x104:], []byte{0xCE, 0xED, 0x66, 0x66, 0xCC, 0x0D, 0x00, 0x0B})
	h[0x143], h[0x147], h[0x148], h[0x149] = 0x80, cartType, romSize, ramSize
	var sum byte
	for _, b := range h[0x134:0x14D] {
		sum -= b + 1
	}
	h[0x14D] = sum
	c, err := gb.ParseCart(h)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestGBSaveFormat(t *testing.T) {
	for _, tt := range []struct {
		emulator, tag string
		want          gb.Format
		ok            bool
	}{
		{RetroArchID, "gambatte", gb.Gambatte, true},
		{RetroArchID, "sameboy", gb.SameBoy, true},
		{RetroArchID, "mgba", gb.MGBA, true},
		{RetroArchID, "tgbdual", "", false},
		{RetroArchID, "mgba-app", "", false},
		{"mgba", "mgba", gb.MGBAApp, true},
		{"melonds", "melonds", "", false},
	} {
		if got, ok := GBSaveFormat(tt.emulator, tt.tag); got != tt.want || ok != tt.ok {
			t.Errorf("%s/%s: %q %v", tt.emulator, tt.tag, got, ok)
		}
	}
}

func TestGBServerSaveRoundTrip(t *testing.T) {
	now := time.Unix(1760400000, 0)
	cases := map[string]struct {
		cart gb.Cart
		save gb.Save
		size int
	}{
		"MBC3 with a clock": {gbCart(t, 0x10, 0x05, 0x03), gb.Save{RAM: bytes.Repeat([]byte{7}, 0x8000), Clock: &gb.Clock{Days: 300, Hours: 5, Time: now}}, 0x8000 + 48},
		"HuC3":              {gbCart(t, 0xFE, 0x05, 0x03), gb.Save{RAM: bytes.Repeat([]byte{8}, 0x8000), HuC3: &gb.HuC3Clock{Minutes: 61, Days: 4, Time: now}}, 0x8000 + 8},
		"2 KiB":             {gbCart(t, 0x03, 0x05, 0x01), gb.Save{RAM: bytes.Repeat([]byte{9}, 0x800)}, 0x2000},
		// Gambatte runs no MBC7: RomM gets the web player's other core's layout.
		"MBC7": {gbCart(t, 0x22, 0x05, 0x00), gb.Save{RAM: bytes.Repeat([]byte{1, 2}, 0x80)}, 0x100},
	}
	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			data, err := GBServerSave(tt.cart, tt.save, now)
			if err != nil || len(data) != tt.size {
				t.Fatalf("%d bytes, %v", len(data), err)
			}
			back, err := ParseGBServerSave(data, "", tt.cart, now)
			if err != nil || !bytes.Equal(back.RAM, tt.save.RAM) {
				t.Fatalf("read back: %v", err)
			}
			if tt.save.Clock != nil && (back.Clock == nil || back.Clock.Days != 300 || back.Clock.Hours != 5) {
				t.Fatalf("clock %+v", back.Clock)
			}
			if tt.save.HuC3 != nil && (back.HuC3 == nil || back.HuC3.Minutes != 61 || back.HuC3.Days != 4) {
				t.Fatalf("HuC3 clock %+v", back.HuC3)
			}
		})
	}
}

// Saves other emulators wrote: told apart by their size for the cartridge, the tag's first.
func TestParseGBServerSaveFromOtherEmulators(t *testing.T) {
	now := time.Unix(1760400000, 0)
	mbc2 := gbCart(t, 0x06, 0x03, 0x00)
	packed := bytes.Repeat([]byte{0x21}, 0x100)
	s, err := ParseGBServerSave(packed, "mgba", mbc2, now)
	if err != nil || s.RAM[0]&0xF != 1 || s.RAM[1]&0xF != 2 {
		t.Fatalf("mGBA's MBC2: %v", err)
	}
	sameboy := bytes.Repeat([]byte{0xF5}, 0x200)
	if s, err := ParseGBServerSave(sameboy, "sameboy", mbc2, now); err != nil || s.RAM[0]&0xF != 5 {
		t.Fatalf("SameBoy's MBC2: %v", err)
	}
	if _, err := ParseGBServerSave(make([]byte, 1234), "gambatte", mbc2, now); !errors.Is(err, ErrGBSaveMismatch) {
		t.Fatalf("a size no emulator writes: %v", err)
	}
}
