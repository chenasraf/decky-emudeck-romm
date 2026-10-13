package emudeck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// deck builds a fake home directory and /proc for a probe to look at.
type deck struct {
	t    *testing.T
	home string
	proc string
}

func newDeck(t *testing.T) *deck {
	t.Helper()
	root := t.TempDir()
	d := &deck{t: t, home: filepath.Join(root, "home/deck"), proc: filepath.Join(root, "proc")}
	d.mkdir(d.home)
	d.mkdir(d.proc)
	return d
}

func (d *deck) mkdir(path string) {
	d.t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		d.t.Fatal(err)
	}
}

func (d *deck) write(path, content string) {
	d.t.Helper()
	d.mkdir(filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		d.t.Fatal(err)
	}
}

func (d *deck) symlink(target, path string) {
	d.t.Helper()
	d.mkdir(filepath.Dir(path))
	if err := os.Symlink(target, path); err != nil {
		d.t.Fatal(err)
	}
}

func (d *deck) h(rel string) string { return filepath.Join(d.home, rel) }

func (d *deck) probe(arch string) domain.InstallState {
	p := New(d.home)
	p.proc = d.proc
	p.arch = arch
	p.flatpakRoots = []string{d.h(".local/share/flatpak/app")}
	p.now = func() time.Time { return time.Unix(0, 0) }
	return p.Probe()
}

func (d *deck) flatpak(id string) {
	d.mkdir(d.h(filepath.Join(".local/share/flatpak/app", id, "current/active")))
}

func emulator(t *testing.T, s domain.InstallState, id string) domain.EmulatorState {
	t.Helper()
	for _, e := range s.Emulators {
		if e.ID == id {
			return e
		}
	}
	t.Fatalf("no %s in %+v", id, s.Emulators)
	return domain.EmulatorState{}
}

func system(t *testing.T, s domain.InstallState, folder string) domain.SystemState {
	t.Helper()
	for _, sys := range s.Systems {
		if sys.Folder == folder {
			return sys
		}
	}
	t.Fatalf("no %s", folder)
	return domain.SystemState{}
}

func TestRootFromSettingsSh(t *testing.T) {
	d := newDeck(t)
	sd := filepath.Join(filepath.Dir(d.home), "run/media/deck/SD")
	d.write(d.h(".config/EmuDeck/settings.sh"), strings.Join([]string{
		"# comment",
		`emulationPath="` + sd + `"/Emulation`,
		`romsPath="` + sd + `"/Emulation/roms`,
		"savesPath=" + sd + "/Emulation/saves/",
		`system="linux"`,
	}, "\n"))
	d.mkdir(filepath.Join(sd, "Emulation/roms/psx"))
	// A stale ~/Emulation must not win over settings.sh.
	d.mkdir(d.h("Emulation/roms"))

	s := d.probe("amd64")
	ed := s.EmuDeck
	if ed.Root != filepath.Join(sd, "Emulation") || ed.Source != domain.RootFromSettings {
		t.Fatalf("root %+v", ed)
	}
	if ed.SavesPath != filepath.Join(sd, "Emulation/saves") || ed.BIOSPath != filepath.Join(sd, "Emulation/bios") {
		t.Fatalf("paths %+v", ed)
	}
	if psx := system(t, s, "psx"); !psx.ROMPathExists || psx.Emulator != "duckstation" {
		t.Fatalf("psx %+v", psx)
	}
	if n64 := system(t, s, "n64"); n64.ROMPathExists {
		t.Fatalf("n64 folder doesn't exist: %+v", n64)
	}
}

func TestRootExpandsHome(t *testing.T) {
	d := newDeck(t)
	d.write(d.h(".config/EmuDeck/settings.sh"), "emulationPath=$HOME/Emulation\n")
	d.mkdir(d.h("Emulation/roms"))

	if ed := d.probe("amd64").EmuDeck; ed.Root != d.h("Emulation") || ed.Source != domain.RootFromSettings {
		t.Fatalf("root %+v", ed)
	}
}

func TestRootFallsBackToSettingsJSON(t *testing.T) {
	d := newDeck(t)
	// settings.sh points somewhere without roms/, so the app's storagePath is next.
	d.write(d.h(".config/EmuDeck/settings.sh"), "emulationPath=/nowhere/Emulation\n")
	d.write(d.h(".config/EmuDeck/settings.json"), `{"storagePath": "$HOME/games"}`)
	d.mkdir(d.h("games/Emulation/roms"))

	ed := d.probe("amd64").EmuDeck
	if ed.Root != d.h("games/Emulation") || ed.Source != domain.RootFromAppSettings || ed.ROMsPath != d.h("games/Emulation/roms") {
		t.Fatalf("root %+v", ed)
	}
}

func TestRootMissing(t *testing.T) {
	d := newDeck(t)
	// An Emulation folder without roms/ isn't EmuDeck's.
	d.mkdir(d.h("Emulation/bios"))

	s := d.probe("amd64")
	if s.EmuDeckFound() || len(s.Systems) != 0 || len(s.EmuDeck.Tried) != 1 {
		t.Fatalf("state %+v", s.EmuDeck)
	}
	// Emulators are still listed, so the status page can show what is installed.
	if len(s.Emulators) == 0 {
		t.Fatal("no emulators")
	}
}

func TestEmulatorInstalls(t *testing.T) {
	d := newDeck(t)
	d.mkdir(d.h("Emulation/roms"))
	d.flatpak("org.libretro.RetroArch")
	d.write(d.h("Applications/DuckStation.AppImage"), "")
	d.write(d.h("Applications/publish/Ryujinx"), "")
	d.write(d.h("Emulation/roms/xbox360/xenia_canary.exe"), "")

	s := d.probe("amd64")
	for id, want := range map[string]bool{"retroarch": true, "duckstation": true, "ryujinx": true, "xenia": true, "dolphin": false, "pcsx2": false} {
		if got := emulator(t, s, id).Installed; got != want {
			t.Errorf("%s installed = %v, want %v", id, got, want)
		}
	}
	if sys := system(t, s, "xbox360"); sys.ROMPath != d.h("Emulation/roms/xbox360/roms") {
		t.Errorf("xbox360 ROMs go in its roms subfolder, got %s", sys.ROMPath)
	}
}

func TestEmulatorFollowsArch(t *testing.T) {
	d := newDeck(t)
	d.mkdir(d.h("Emulation/roms"))

	for arch, want := range map[string]string{"amd64": "pcsx2", "arm64": "armsx2"} {
		s := d.probe(arch)
		if got := system(t, s, "ps2").Emulator; got != want {
			t.Errorf("%s: ps2 emulator %s, want %s", arch, got, want)
		}
		for _, e := range s.Emulators {
			if e.ID == "pcsx2" && arch == "arm64" || e.ID == "armsx2" && arch == "amd64" {
				t.Errorf("%s lists %s", arch, e.ID)
			}
		}
	}
}

func TestSavePaths(t *testing.T) {
	d := newDeck(t)
	d.mkdir(d.h("Emulation/roms"))
	native := d.h(".var/app/org.libretro.RetroArch/config/retroarch/saves")
	d.mkdir(native)
	d.symlink(native, d.h("Emulation/saves/retroarch/saves"))
	d.mkdir(d.h("Emulation/saves/retroarch/states"))
	// DuckStation's setup never ran: no folders at all. melonDS's link dangles.
	d.symlink(d.h("gone"), d.h("Emulation/saves/melonds/saves"))
	d.mkdir(d.h("Emulation/saves/melonds/states"))

	s := d.probe("amd64")
	ra := emulator(t, s, "retroarch")
	// The temp dir itself may sit behind a symlink, as on macOS.
	want, _ := filepath.EvalSymlinks(native)
	if !ra.SaveSyncReady() || ra.Saves[0].Resolved != want {
		t.Fatalf("retroarch %+v", ra)
	}
	if emulator(t, s, "duckstation").SaveSyncReady() || emulator(t, s, "melonds").SaveSyncReady() {
		t.Fatal("unresolved save folders reported ready")
	}
	// mGBA and RMG default to no system but have save sync, so they're listed.
	emulator(t, s, "mgba")
	emulator(t, s, "rmg")
}

func TestESDESettings(t *testing.T) {
	d := newDeck(t)
	if s := d.probe("amd64").ESDE; s.SettingsPath != "" || s.MediaDirectory != d.h("ES-DE/downloaded_media") || s.ParseGamelistOnly {
		t.Fatalf("no settings: %+v", s)
	}

	d.write(d.h("ES-DE/settings/es_settings.xml"), `<?xml version="1.0"?>
<bool name="ParseGamelistOnly" value="true" />
<string name="MediaDirectory" value="~/Emulation/tools/downloaded_media" />
<string name="ROMDirectory" value="" />
`)
	s := d.probe("amd64").ESDE
	if s.SettingsPath == "" || !s.ParseGamelistOnly || s.MediaDirectory != d.h("Emulation/tools/downloaded_media") {
		t.Fatalf("settings: %+v", s)
	}
}

func TestESDERunning(t *testing.T) {
	d := newDeck(t)
	d.write(filepath.Join(d.proc, "1/cmdline"), "/usr/lib/systemd/systemd\x00")
	d.write(filepath.Join(d.proc, "self/cmdline"), "/tmp/.mount_ES-DEabc/usr/bin/es-de\x00")
	if d.probe("amd64").ESDE.Running {
		t.Fatal("not running, but found")
	}

	d.write(filepath.Join(d.proc, "4242/cmdline"), "/tmp/.mount_ES-DEabc/usr/bin/es-de\x00--home\x00/home/deck\x00")
	if !d.probe("amd64").ESDE.Running {
		t.Fatal("running, but not found")
	}
}
