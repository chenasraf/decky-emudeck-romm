package app

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

var (
	melonDS = domain.GameLauncher{ESDECommand: domain.ESDECommand{Label: "melonDS (Standalone)", Command: "%EMULATOR_MELONDS% -f %ROM%"}, Known: true}
	mGBA    = domain.GameLauncher{ESDECommand: domain.ESDECommand{Label: "mGBA (Standalone)", Command: "%EMULATOR_MGBA% -f %ROM%"}, Known: true}
	mgbaRA  = domain.GameLauncher{ESDECommand: domain.ESDECommand{
		Label: "mGBA", Command: "%EMULATOR_RETROARCH% -L %CORE_RETROARCH%/mgba_libretro.so %ROM%",
	}, Known: true}
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

// RetroArch's name for an archive's save isn't known, so such games don't sync with it.
func TestRetroArchArchiveDoesntSync(t *testing.T) {
	f := newSaveFixture(t)
	f.launchers["gba"] = mgbaRA
	f.saves = f.newSaves()
	f.ownGame(t, 51, 10, "gba", "Metroid Fusion (USA).zip", "mfusion.gba")
	if st := f.statusOf(t, 51); st.Syncs || !strings.Contains(st.Why, "RetroArch") {
		t.Fatalf("status %+v", st)
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
