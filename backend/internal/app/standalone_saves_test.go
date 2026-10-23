package app

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

var (
	melonDS = domain.GameLauncher{Label: "melonDS (Standalone)", Command: "%EMULATOR_MELONDS% -f %ROM%", Known: true}
	mGBA    = domain.GameLauncher{Label: "mGBA (Standalone)", Command: "%EMULATOR_MGBA% -f %ROM%", Known: true}
)

// ownGame records the game with id as downloaded by the plugin into the ROM folder of the platform
// with platformID: file, as a zip of inner when inner is set.
func (f *saveFixture) ownGame(t *testing.T, id, platformID int, folder, file, inner string) {
	t.Helper()
	path := filepath.Join(f.root, "roms", folder, file)
	if inner == "" {
		writeFile(t, path, []byte("rom"))
	} else {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		out, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		zw := zip.NewWriter(out)
		w, _ := zw.Create(inner)
		_, _ = w.Write([]byte("rom"))
		_ = zw.Close()
		_ = out.Close()
	}
	f.catalog.mu.Lock()
	f.catalog.roms[id] = domain.ROM{ID: id, PlatformID: platformID, Name: strings.TrimSuffix(file, filepath.Ext(file)), FileName: file}
	f.catalog.mu.Unlock()
	if err := f.owned.SaveOwned(context.Background(), domain.OwnedROM{ROMID: id, PlatformID: platformID, Files: []domain.PlacedFile{{Path: path}}}); err != nil {
		t.Fatal(err)
	}
}

func (f *saveFixture) statusOf(t *testing.T, id int) SaveStatus {
	t.Helper()
	s, err := f.saves.Status(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestMelonDSSavesSync(t *testing.T) {
	f := newSaveFixture(t)
	f.launchers["nds"] = melonDS
	f.saves = f.newSaves()
	f.ownGame(t, 50, 9, "nds", "Mario Kart DS (USA).nds", "")
	save := filepath.Join(f.root, "saves", "melonds", "saves", "Mario Kart DS (USA).sav")

	st := f.statusOf(t, 50)
	if !st.Syncs || st.Path != save || st.Emulator != "melonds" || st.Saver != "melonDS" {
		t.Fatalf("status %+v", st)
	}
	writeFile(t, save, []byte("ds progress"))
	if s, err := f.saves.Sync(context.Background(), []int{50}); err != nil || s.Uploaded != 1 {
		t.Fatalf("sync %+v, %v", s, err)
	}
	if head, _ := f.romm.head(50); string(head.data) != "ds progress" || head.Emulator != "melonds" {
		t.Fatalf("RomM holds %+v", head)
	}

	f.romm.otherDevice(50, "melondsds", []byte("ds progress elsewhere"))
	if s, _ := f.saves.Sync(context.Background(), []int{50}); s.Downloaded != 1 {
		t.Fatalf("download %+v", s)
	}
	if got := readFile(t, save); got != "ds progress elsewhere" {
		t.Fatalf("save holds %q", got)
	}
}

// Kept as an archive, mGBA names the save after the archive and melonDS after the game inside it.
func TestStandaloneArchiveNames(t *testing.T) {
	f := newSaveFixture(t)
	f.launchers["gba"], f.launchers["nds"] = mGBA, melonDS
	f.saves = f.newSaves()
	f.ownGame(t, 51, 10, "gba", "Metroid Fusion (USA).zip", "mfusion.gba")
	f.ownGame(t, 52, 9, "nds", "Pokemon Platinum.zip", "Pokemon - Platinum (USA).nds")

	if st := f.statusOf(t, 51); st.Path != filepath.Join(f.root, "saves", "mgba", "saves", "Metroid Fusion (USA).sav") {
		t.Fatalf("mGBA %+v", st)
	}
	if st := f.statusOf(t, 52); st.Path != filepath.Join(f.root, "saves", "melonds", "saves", "Pokemon - Platinum (USA).sav") {
		t.Fatalf("melonDS %+v", st)
	}
}

// RetroArch names the save of a game kept as an archive after the archive.
func TestRetroArchArchiveSyncs(t *testing.T) {
	f := newSaveFixture(t)
	f.ownGame(t, 54, 1, "snes", "Super Metroid (USA).zip", "sm.sfc")
	save := filepath.Join(f.raSaves, "Super Metroid (USA).srm")
	if st := f.statusOf(t, 54); !st.Syncs || st.Path != save || st.Emulator != "snes9x" {
		t.Fatalf("status %+v", st)
	}

	writeFile(t, save, []byte("zebes"))
	if s, err := f.saves.Sync(context.Background(), []int{54}); err != nil || s.Uploaded != 1 {
		t.Fatalf("upload %+v, %v", s, err)
	}
	f.romm.otherDevice(54, "snes9x", []byte("zebes elsewhere"))
	if s, _ := f.saves.Sync(context.Background(), []int{54}); s.Downloaded != 1 {
		t.Fatalf("download %+v", s)
	}
	if got := readFile(t, save); got != "zebes elsewhere" {
		t.Fatalf("save holds %q", got)
	}
}

// Without ES-DE's files, EmuDeck's default emulator for the system stands in.
func TestDefaultEmulatorWithoutESDE(t *testing.T) {
	f := newSaveFixture(t)
	f.ownGame(t, 50, 9, "nds", "Mario Kart DS (USA).nds", "")
	f.ownGame(t, 53, 10, "gba", "Golden Sun (USA).gba", "")

	if st := f.statusOf(t, 50); st.Saver != "melonDS" || st.Emulator != "melonds" {
		t.Fatalf("nds %+v", st)
	}
	if st := f.statusOf(t, 53); st.Saver != "RetroArch" || st.Emulator != "mgba" ||
		st.Path != filepath.Join(f.raSaves, "Golden Sun (USA).srm") {
		t.Fatalf("gba %+v", st)
	}
}

func TestEmulatorSettingsCanStopSync(t *testing.T) {
	f := newSaveFixture(t)
	f.launchers["gba"] = mGBA
	f.configs["mgba"] = domain.SaveConfig{Problem: "mGBA uses player 2's save files."}
	f.saves = f.newSaves()
	f.ownGame(t, 53, 10, "gba", "Golden Sun (USA).gba", "")
	if st := f.statusOf(t, 53); st.Syncs || st.Why != "mGBA uses player 2's save files." {
		t.Fatalf("status %+v", st)
	}
	if p := f.saves.Paused(); len(p) != 1 || p[0].Emulator.ID != "mgba" || p[0].Why != "mGBA uses player 2's save files." {
		t.Fatalf("paused %+v", p)
	}
}

// Saves follow the folder an emulator's settings move them to.
func TestEmulatorSettingsMoveSaves(t *testing.T) {
	f := newSaveFixture(t)
	f.launchers["gba"] = mGBA
	moved := filepath.Join(f.root, "elsewhere")
	f.configs["mgba"] = domain.SaveConfig{Dir: moved}
	f.saves = f.newSaves()
	f.ownGame(t, 53, 10, "gba", "Golden Sun (USA).gba", "")
	if st := f.statusOf(t, 53); !st.Syncs || st.Path != filepath.Join(moved, "Golden Sun (USA).sav") {
		t.Fatalf("status %+v", st)
	}
	if p := f.saves.Paused(); len(p) != 0 {
		t.Fatalf("paused %+v", p)
	}
}

// A library bigger than RomM takes in one negotiation syncs in several, each within its limit.
func TestSyncBatchesManyGames(t *testing.T) {
	f := newSaveFixture(t)
	ids := []int{1}
	for i := range domain.SyncBatch {
		id := 1000 + i
		f.ownGame(t, id, 1, "snes", fmt.Sprintf("Game %d.sfc", i), "")
		ids = append(ids, id)
	}
	last := ids[len(ids)-1]
	f.writeSave(t, "chrono", time.Now())
	writeFile(t, filepath.Join(f.raSaves, fmt.Sprintf("Game %d.srm", domain.SyncBatch-1)), []byte("last"))

	s, err := f.saves.Sync(context.Background(), nil)
	if err != nil || s.Games != len(ids) || s.Uploaded != 2 {
		t.Fatalf("sync %+v, %v", s, err)
	}
	f.romm.mu.Lock()
	requests := slices.Clone(f.romm.requests)
	f.romm.mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("%d negotiations", len(requests))
	}
	for _, r := range requests {
		if len(r.ROMIDs) > domain.SyncBatch {
			t.Fatalf("negotiation of %d ROMs", len(r.ROMIDs))
		}
	}
	if head, _ := f.romm.head(last); string(head.data) != "last" {
		t.Fatalf("RomM holds %q for the last game", head.data)
	}
}

// A download waits only for the emulator that writes the game's save.
func TestDownloadWaitsForTheGamesOwnEmulator(t *testing.T) {
	f := newSaveFixture(t)
	f.launchers["nds"] = melonDS
	f.saves = f.newSaves()
	f.ownGame(t, 50, 9, "nds", "Mario Kart DS (USA).nds", "")
	f.romm.otherDevice(50, "melonds", []byte("ds progress"))

	f.playing.Store("melonds")
	if s, _ := f.saves.Sync(context.Background(), []int{50}); s.Waiting != 1 {
		t.Fatalf("while melonDS runs %+v", s)
	}
	f.playing.Store("mgba")
	if s, _ := f.saves.Sync(context.Background(), []int{50}); s.Downloaded != 1 {
		t.Fatalf("while mGBA runs %+v", s)
	}
}
