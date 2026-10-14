package app

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
	"github.com/chenasraf/saveporter/gb"
)

// gbcROM is a Game Boy Color ROM's start: the logo the header check wants, the cartridge type and
// RAM size codes, and the header checksum.
func gbcROM(cartType, ramSize byte) []byte {
	h := make([]byte, 0x8000)
	copy(h[0x104:], []byte{0xCE, 0xED, 0x66, 0x66, 0xCC, 0x0D, 0x00, 0x0B})
	copy(h[0x134:], "TESTCART")
	h[0x143], h[0x147], h[0x148], h[0x149] = 0x80, cartType, 0x05, ramSize
	var sum byte
	for _, b := range h[0x134:0x14D] {
		sum -= b + 1
	}
	h[0x14D] = sum
	return h
}

// gbcGame records a Game Boy Color game with id, cartridge type and RAM size, downloaded by the
// plugin, and returns the path its save is kept at without the extension.
func (f *saveFixture) gbcGame(t *testing.T, id int, cartType, ramSize byte, dir string) string {
	t.Helper()
	path := filepath.Join(f.root, "roms", "gbc", "Crystal.gbc")
	writeFile(t, path, gbcROM(cartType, ramSize))
	f.catalog.mu.Lock()
	f.catalog.roms[id] = domain.ROM{ID: id, PlatformID: 12, Name: "Crystal", FileName: "Crystal.gbc"}
	f.catalog.mu.Unlock()
	if err := f.owned.SaveOwned(context.Background(), domain.OwnedROM{ROMID: id, PlatformID: 12, Files: []domain.PlacedFile{{Path: path}}}); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "Crystal")
}

func pattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7 + i>>8)
	}
	return b
}

func setTime(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// serverClock reads the clock RomM's save carries after the cartridge's RAM.
func serverClock(t *testing.T, data []byte, ram int) gb.Clock {
	t.Helper()
	cart, _ := gb.ParseCart(gbcROM(0x10, 0x03))
	s, err := gb.Read(gb.VBAM, cart, gb.Files{gb.ExtSRM: data[:ram], gb.ExtRTC: data[ram:]}, time.Now())
	if err != nil || s.Clock == nil {
		t.Fatalf("no clock: %v", err)
	}
	return *s.Clock
}

// RetroArch's Gambatte keeps the clock beside the save as the time it read zero. RomM gets the RAM
// as the web player keeps it, then the clock.
func TestGBSaveUploadsWithItsClock(t *testing.T) {
	f := newSaveFixture(t)
	base := f.gbcGame(t, 70, 0x10, 0x03, f.raSaves)
	written := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	ram := pattern(0x8000)
	writeFile(t, base+".srm", ram)
	writeFile(t, base+".rtc", binary.LittleEndian.AppendUint64(nil, uint64(written.Unix()-(3*86400+3600))))
	setTime(t, base+".srm", written)
	setTime(t, base+".rtc", written)

	if st := f.statusOf(t, 70); !st.Syncs || st.Path != base+".srm" || st.Emulator != "gambatte" {
		t.Fatalf("status %+v", st)
	}
	if s := f.syncGame(t, 70); s.Uploaded != 1 {
		t.Fatalf("summary %+v", s)
	}
	head, _ := f.romm.head(70)
	if len(head.data) != 0x8000+48 || !bytes.Equal(head.data[:0x8000], ram) || head.Emulator != "gambatte" {
		t.Fatalf("RomM holds %d bytes, tagged %s", len(head.data), head.Emulator)
	}
	if c := serverClock(t, head.data, 0x8000); c.Days != 3 || c.Hours != 1 || !c.Time.Equal(written) {
		t.Fatalf("clock %+v", c)
	}
	// The clock is written as of the files' last write, so syncing later finds nothing new.
	if s := f.syncGame(t, 70); s.Uploaded+s.Downloaded+s.Conflicts != 0 {
		t.Fatalf("second sync %+v", s)
	}
}

// The web player keeps no clock: its save replaces the RAM and leaves the Deck's clock running.
func TestGBWebSaveKeepsTheDecksClock(t *testing.T) {
	f := newSaveFixture(t)
	base := f.gbcGame(t, 71, 0x10, 0x03, f.raSaves)
	rtc := binary.LittleEndian.AppendUint64(nil, uint64(time.Now().Unix()-86400))
	writeFile(t, base+".rtc", rtc)
	web := pattern(0x8000)
	web[0] = 0x42
	f.romm.otherDevice(71, "gambatte", web)

	if s := f.syncGame(t, 71); s.Downloaded != 1 {
		t.Fatalf("summary %+v", s)
	}
	if got := readFile(t, base+".srm"); got != string(web) {
		t.Fatal("RAM not replaced")
	}
	if got := readFile(t, base+".rtc"); got != string(rtc) {
		t.Fatal("the Deck's clock changed")
	}
}

// The mGBA app keeps RAM and clock in one .sav, its clock after the RAM. Another Deck's save from it
// lands in RetroArch's two files, the clock carried over.
func TestGBSaveFromTheMGBAApp(t *testing.T) {
	f := newSaveFixture(t)
	base := f.gbcGame(t, 72, 0x10, 0x03, f.raSaves)
	cart, _ := gb.ParseCart(gbcROM(0x10, 0x03))
	now := time.Now()
	app, err := gb.Write(gb.MGBAApp, cart, gb.Save{RAM: pattern(0x8000), Clock: &gb.Clock{Days: 10, Time: now}}, now)
	if err != nil {
		t.Fatal(err)
	}
	f.romm.otherDevice(72, "mgba", app[gb.ExtSav])

	if s := f.syncGame(t, 72); s.Downloaded != 1 {
		t.Fatalf("summary %+v", s)
	}
	if got := readFile(t, base+".srm"); got != string(pattern(0x8000)) {
		t.Fatal("RAM not written")
	}
	rtc := []byte(readFile(t, base+".rtc"))
	if len(rtc) != 8 {
		t.Fatalf("Gambatte's clock is %d bytes", len(rtc))
	}
	if days := (now.Unix() - int64(binary.LittleEndian.Uint64(rtc))) / 86400; days != 10 {
		t.Fatalf("the clock reads %d days", days)
	}
}

// A game the mGBA app plays: RomM's copy is the web player's layout (MBC2's 512 half-bytes one to a
// byte in 8 KiB), and RomM's comes back as the app's packed 256 bytes.
func TestGBMBC2BetweenTheAppAndTheWebPlayer(t *testing.T) {
	f := newSaveFixture(t)
	f.launchers["gbc"] = mGBA
	f.saves = f.newSaves()
	base := f.gbcGame(t, 73, 0x06, 0x00, filepath.Join(f.root, "saves", "mgba", "saves"))
	packed := pattern(0x100)
	writeFile(t, base+".sav", packed)

	if s := f.syncGame(t, 73); s.Uploaded != 1 {
		t.Fatalf("summary %+v", s)
	}
	head, _ := f.romm.head(73)
	if len(head.data) != 0x2000 || head.data[0]&0xF != packed[0]&0xF || head.data[1]&0xF != packed[0]>>4 {
		t.Fatalf("RomM holds %d bytes: % x", len(head.data), head.data[:2])
	}

	web := bytes.Repeat([]byte{0xFF}, 0x2000)
	for i := range 0x200 {
		web[i] = 0xF0 | byte(i)&0xF
	}
	f.romm.otherDevice(73, "gambatte", web)
	if s := f.syncGame(t, 73); s.Downloaded != 1 {
		t.Fatalf("download %+v", s)
	}
	sav := []byte(readFile(t, base+".sav"))
	if len(sav) != 0x100 || sav[0] != 0x10 || sav[1] != 0x32 {
		t.Fatalf("the app's save: %d bytes, % x", len(sav), sav[:2])
	}
}

func TestGBSaveNeedsItsROM(t *testing.T) {
	f := newSaveFixture(t)
	f.gbcGame(t, 74, 0x10, 0x03, f.raSaves)
	writeFile(t, filepath.Join(f.root, "roms", "gbc", "Crystal.gbc"), []byte("not a rom"))
	if st := f.statusOf(t, 74); st.Syncs {
		t.Fatalf("status %+v", st)
	}
}
