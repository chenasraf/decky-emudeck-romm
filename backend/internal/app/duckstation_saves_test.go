package app

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// card is a memory card image holding data.
func card(data string) []byte {
	out := make([]byte, domain.PSXCardSize)
	copy(out, "MC")
	copy(out[128:], data)
	return out
}

// psxGame records a PlayStation game with id, whose files are placed at rels under the psx ROM
// folder, as downloaded by the plugin.
func (f *saveFixture) psxGame(t *testing.T, id int, name string, rels ...string) {
	t.Helper()
	dir := filepath.Join(f.root, "roms", "psx")
	var files []domain.PlacedFile
	for _, rel := range rels {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		writeFile(t, path, []byte("disc"))
		files = append(files, domain.PlacedFile{Path: path})
	}
	f.catalog.mu.Lock()
	f.catalog.roms[id] = domain.ROM{ID: id, PlatformID: 3, Name: name, FileName: filepath.Base(rels[0])}
	f.catalog.mu.Unlock()
	if err := f.owned.SaveOwned(context.Background(), domain.OwnedROM{ROMID: id, PlatformID: 3, Files: files}); err != nil {
		t.Fatal(err)
	}
}

func (f *saveFixture) duckSaves() string {
	return filepath.Join(f.root, "saves", "duckstation", "saves")
}

func TestDuckStationCardSyncs(t *testing.T) {
	f := newSaveFixture(t)
	f.psx["Crash Bandicoot 2 (USA).chd"] = domain.PSXDisc{Read: true, Serial: "SCUS-94154"}
	f.duck["SCUS-94154"] = domain.DuckStationGame{Title: "Crash Bandicoot 2 - Cortex Strikes Back (USA)"}
	f.psxGame(t, 70, "Crash Bandicoot 2", "Crash Bandicoot 2 (USA).chd")
	path := filepath.Join(f.duckSaves(), "Crash Bandicoot 2 - Cortex Strikes Back (USA)_1.mcd")
	if st := f.statusOf(t, 70); !st.Syncs || st.Path != path || st.Emulator != "duckstation" || st.Saver != "DuckStation" {
		t.Fatalf("status %+v", st)
	}
	writeFile(t, path, card("warped"))
	if s := f.syncGame(t, 70); s.Uploaded != 1 {
		t.Fatalf("summary %+v", s)
	}
	head, _ := f.romm.head(70)
	if !bytes.Equal(head.data, card("warped")) || head.Emulator != "duckstation" {
		t.Fatalf("RomM holds %s by %s", head.FileName, head.Emulator)
	}

	// Another device's card, RetroArch's PlayStation cores' included, lands in place.
	f.romm.otherDevice(70, "swanstation", card("from handheld"))
	if s := f.syncGame(t, 70); s.Downloaded != 1 {
		t.Fatalf("summary %+v", s)
	}
	if got := readFile(t, path); got != string(card("from handheld")) {
		t.Fatal("card not downloaded")
	}
}

// What isn't a memory card never replaces one.
func TestDuckStationRefusesOtherSaves(t *testing.T) {
	f := newSaveFixture(t)
	f.psx["Crash.cue"] = domain.PSXDisc{Read: true, Serial: "SCUS-94154"}
	f.duck["SCUS-94154"] = domain.DuckStationGame{Title: "Crash"}
	f.psxGame(t, 70, "Crash", "Crash.cue", "Crash.bin")
	path := filepath.Join(f.duckSaves(), "Crash_1.mcd")
	f.romm.otherDevice(70, "pcsx_rearmed", []byte("a save state, say"))
	if s := f.syncGame(t, 70); s.Downloaded != 0 || s.Failed != 1 {
		t.Fatalf("summary %+v", s)
	}
	gone(t, path)
}

// The discs of a game in a playlist folder share the card DuckStation names after the disc set.
func TestDuckStationDiscSetCard(t *testing.T) {
	f := newSaveFixture(t)
	f.psx["Final Fantasy VII (USA).m3u"] = domain.PSXDisc{Read: true, Serial: "SCUS-94163"}
	f.duck["SCUS-94163"] = domain.DuckStationGame{Title: "Final Fantasy VII (USA) (Disc 1)", DiscSet: "Final Fantasy VII (USA)"}
	f.psxGame(t, 8, "Final Fantasy VII", "Final Fantasy VII (USA).m3u/Final Fantasy VII (USA).m3u",
		"Final Fantasy VII (USA).m3u/Final Fantasy VII (USA) (Disc 1).chd", "Final Fantasy VII (USA).m3u/Final Fantasy VII (USA) (Disc 2).chd")
	if st := f.statusOf(t, 8); !st.Syncs || st.Path != filepath.Join(f.duckSaves(), "Final Fantasy VII (USA)_1.mcd") {
		t.Fatalf("status %+v", st)
	}
}

// Discs RomM keeps as a game each share one card, which syncs with the first of them.
func TestDuckStationLooseDiscsShareACard(t *testing.T) {
	f := newSaveFixture(t)
	for serial, disc := range map[string]string{"SCUS-94163": "Disc 1", "SCUS-94164": "Disc 2"} {
		f.psx["Final Fantasy VII (USA) ("+disc+").chd"] = domain.PSXDisc{Read: true, Serial: serial}
		f.duck[serial] = domain.DuckStationGame{Title: "Final Fantasy VII (USA) (" + disc + ")", DiscSet: "Final Fantasy VII (USA)"}
	}
	f.psxGame(t, 72, "Final Fantasy VII (Disc 2)", "Final Fantasy VII (USA) (Disc 2).chd")
	f.psxGame(t, 71, "Final Fantasy VII (Disc 1)", "Final Fantasy VII (USA) (Disc 1).chd")
	shared := filepath.Join(f.duckSaves(), "Final Fantasy VII (USA)_1.mcd")
	if st := f.statusOf(t, 71); !st.Syncs || st.Path != shared {
		t.Fatalf("disc 1 %+v", st)
	}
	if st := f.statusOf(t, 72); st.Syncs || !strings.Contains(st.Why, "Final Fantasy VII (Disc 1)") {
		t.Fatalf("disc 2 %+v", st)
	}

	// A card of disc 2's own is its, and syncs with it.
	writeFile(t, filepath.Join(f.duckSaves(), "Final Fantasy VII (USA) (Disc 2)_1.mcd"), card("old"))
	if st := f.statusOf(t, 72); !st.Syncs || filepath.Base(st.Path) != "Final Fantasy VII (USA) (Disc 2)_1.mcd" {
		t.Fatalf("disc 2's own card %+v", st)
	}
}

func TestDuckStationCantTellTheCard(t *testing.T) {
	f := newSaveFixture(t)
	f.psxGame(t, 73, "Odd", "Odd.pbp")
	if st := f.statusOf(t, 73); st.Syncs || !strings.Contains(st.Why, "disc couldn't be read") {
		t.Fatalf("unreadable %+v", st)
	}
	f.psx["Crash.chd"] = domain.PSXDisc{Read: true, Serial: "SCUS-94154"}
	f.psxGame(t, 74, "Crash", "Crash.chd")
	f.configs["duckstation"] = "Card 1 isn't per game."
	if st := f.statusOf(t, 74); st.Syncs || st.Why != "Card 1 isn't per game." {
		t.Fatalf("settings %+v", st)
	}
}
