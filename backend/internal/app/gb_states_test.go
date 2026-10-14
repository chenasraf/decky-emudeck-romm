package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
	"github.com/chenasraf/saveporter"
	"github.com/chenasraf/saveporter/gambatte"
	"github.com/chenasraf/saveporter/retroarch"
)

var mgbaCore = domain.GameLauncher{ESDECommand: domain.ESDECommand{
	Label: "mGBA", Command: "%EMULATOR_RETROARCH% -L %CORE_RETROARCH%/mgba_libretro.so %ROM%",
}, Known: true}

// gambatteState is a RetroArch Game Boy Color state of the Gambatte core, its work RAM marked with
// mark: with the network link's records, as the Deck's build writes it, or without, as the web
// player's does.
func gambatteState(netLink bool, mark byte) []byte {
	s := []byte{0x00, 0x01, 0, 0, 0}
	wram := make([]byte, 0x8000)
	wram[0] = mark
	recs := []struct {
		label string
		value []byte
	}{
		{"hram", make([]byte, 0x200)}, {"netsfc", []byte{0x00}}, {"netsv", []byte{0xFF}}, {"pc", []byte{0x01, 0x50}},
		{"sp", []byte{0xDF, 0xF0}}, {"sram", make([]byte, 0x2000)}, {"vram", make([]byte, 0x4000)}, {"wram", wram},
	}
	for _, r := range recs {
		if !netLink && (r.label == "netsfc" || r.label == "netsv") {
			continue
		}
		s = append(s, r.label...)
		s = append(s, 0, byte(len(r.value)>>16), byte(len(r.value)>>8), byte(len(r.value)))
		s = append(s, r.value...)
	}
	return retroarch.Wrap(s)
}

// gambatteMark is the mark gambatteState gave a state, and whether it has the network link's
// records.
func gambatteMark(t *testing.T, data []byte) (byte, bool) {
	t.Helper()
	core, err := retroarch.CoreState(data)
	if err != nil {
		t.Fatal(err)
	}
	s, err := gambatte.Parse(core)
	if err != nil {
		t.Fatal(err)
	}
	return s.Value("wram")[0], s.Layout().NetLink
}

// A Game Boy Color game in RetroArch's Gambatte core: its states go to RomM under Gambatte's name in
// the web player's build, and the web player's come back in the Deck's.
func TestGBCStatesInGambatte(t *testing.T) {
	f := newSaveFixture(t)
	f.gbcGame(t, 80, 0x10, 0x03, f.raSaves)
	dir := filepath.Join(f.root, "saves", "retroarch", "states")
	writeAt(t, filepath.Join(dir, "Crystal.state1"), gambatteState(true, 1), time.Now())

	st, err := f.saves.StateStatus(context.Background(), 80)
	if err != nil || !st.Syncs {
		t.Fatalf("status %+v, %v", st, err)
	}
	if sum := f.syncGame(t, 80); sum.StatesUploaded != 1 {
		t.Fatalf("summary %+v", sum)
	}
	up := f.romm.newestState(80)
	if mark, netLink := gambatteMark(t, up.data); up.Emulator != "gambatte" || mark != 1 || netLink {
		t.Fatalf("uploaded under %s: mark %d, network link %v", up.Emulator, mark, netLink)
	}

	// mGBA's state of the game is the web player's in its other core: neither placed nor listed.
	f.romm.otherStateOf(80, "mgba", time.Now().Add(time.Minute), gbaState(7, 9), nil)
	web := f.romm.otherStateOf(80, "gambatte", time.Now().Add(-time.Minute), gambatteState(false, 5), nil)
	if sum := f.syncGame(t, 80); sum.StatesDownloaded != 1 || sum.Failed != 0 {
		t.Fatalf("download %+v", sum)
	}
	if mark, netLink := gambatteMark(t, readState(t, filepath.Join(dir, "Crystal.state.auto"))); mark != 5 || !netLink {
		t.Fatalf("auto-state: mark %d, network link %v", mark, netLink)
	}
	list, err := f.saves.States(context.Background(), 80)
	if err != nil || len(list) != 2 || list[1].ID != web.ID {
		t.Fatalf("list %+v, %v", list, err)
	}
	for _, e := range list {
		if e.ID != web.ID && !e.FromDeck {
			t.Fatalf("listed mGBA's state %+v", e)
		}
	}
}

// A Game Boy game in RetroArch's mGBA core syncs mGBA's states, and a Gambatte state RomM has for it
// is left alone.
func TestGBStatesInMGBA(t *testing.T) {
	f := newSaveFixture(t)
	f.launchers["gbc"] = mgbaCore
	f.saves = f.newSaves()
	f.gbcGame(t, 81, 0x10, 0x03, f.raSaves)
	dir := filepath.Join(f.root, "saves", "retroarch", "states")
	f.romm.otherStateOf(81, "gambatte", time.Now(), gambatteState(false, 5), nil)
	if sum := f.syncGame(t, 81); sum.StatesDownloaded != 0 || sum.Failed != 0 {
		t.Fatalf("placed Gambatte's state %+v", sum)
	}
	// A state of the Deck's other core can't go to RomM as mGBA's.
	writeAt(t, filepath.Join(dir, "Crystal.state"), gambatteState(true, 1), time.Now())
	if sum := f.syncGame(t, 81); sum.StatesUploaded != 0 || sum.Failed != 1 {
		t.Fatalf("uploaded Gambatte's state as mGBA's %+v", sum)
	}
	if info := stateInfo(t, f.romm.newestState(81).data); info.Core != saveporter.CoreGambatte {
		t.Fatalf("RomM's newest is %+v", info)
	}
}

func TestGBStatesNotInOtherCores(t *testing.T) {
	f := newSaveFixture(t)
	f.launchers["gbc"] = domain.GameLauncher{ESDECommand: domain.ESDECommand{
		Label: "SameBoy", Command: "%EMULATOR_RETROARCH% -L %CORE_RETROARCH%/sameboy_libretro.so %ROM%",
	}, Known: true}
	f.saves = f.newSaves()
	f.gbcGame(t, 82, 0x10, 0x03, f.raSaves)
	st, err := f.saves.StateStatus(context.Background(), 82)
	if err != nil || st.Syncs || st.Why != "Save states sync for games played in RetroArch's Gambatte or mGBA core, or in mGBA." {
		t.Fatalf("status %+v, %v", st, err)
	}
}
