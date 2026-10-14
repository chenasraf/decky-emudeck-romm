package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/settings"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// lifecycleFixture lays out every file the plugin keeps, plus a ROM and a save that are never its
// to delete.
type lifecycleFixture struct {
	dirs dataDirs
	rom  string
	save string
}

func newLifecycleFixture(t *testing.T) lifecycleFixture {
	t.Helper()
	root := t.TempDir()
	f := lifecycleFixture{
		dirs: dataDirs{settings: filepath.Join(root, "settings"), runtime: filepath.Join(root, "runtime"), staging: filepath.Join(root, "Emulation", ".romm-staging")},
		rom:  filepath.Join(root, "Emulation", "roms", "snes", "Chrono Trigger.sfc"),
		save: filepath.Join(root, "Emulation", "saves", "retroarch", "saves", "Chrono Trigger.srm"),
	}
	for _, p := range []string{
		f.rom, f.save,
		filepath.Join(f.dirs.staging, "5", "part"),
		filepath.Join(f.dirs.runtime, "cache.db"), filepath.Join(f.dirs.runtime, "cache.db-wal"),
		filepath.Join(f.dirs.runtime, "covers", "1-small"),
		filepath.Join(f.dirs.runtime, "state.db"), filepath.Join(f.dirs.runtime, "state.db.bak-2"),
		filepath.Join(f.dirs.runtime, "backups", "1", "20261014-000000.000.srm"),
		filepath.Join(f.dirs.settings, "identity"),
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := settings.New(f.dirs.settings)
	if err := s.SaveSignIn(domain.SignIn{Server: domain.Server{URL: "https://romm.lan"}, Token: "rmm_x", DeviceID: "dev-1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAutoBIOS(false); err != nil {
		t.Fatal(err)
	}
	return f
}

func exists(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s gone: %v", p, err)
		}
	}
}

func gone(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s still there: %v", p, err)
		}
	}
}

func TestUninstallKeepsOwnership(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := uninstall(f.dirs); err != nil {
		t.Fatal(err)
	}
	rt := f.dirs.runtime
	gone(t, f.dirs.staging, filepath.Join(rt, "cache.db"), filepath.Join(rt, "cache.db-wal"), filepath.Join(rt, "covers"),
		filepath.Join(f.dirs.settings, "token"))
	exists(t, f.rom, f.save, filepath.Join(rt, "state.db"), filepath.Join(rt, "state.db.bak-2"), filepath.Join(rt, "backups"))
	// The server stays, Signed out, so a reinstall signs in again to the same one.
	s := settings.New(f.dirs.settings)
	in, err := s.SignIn()
	if err != nil || in == nil || !in.SignedOut || in.Token != "" || in.DeviceID != "dev-1" {
		t.Fatalf("sign-in %+v, %v", in, err)
	}
	if s.AutoBIOS() {
		t.Fatal("settings lost")
	}
}

func TestResetKeepsGamesAndSaves(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := reset(f.dirs); err != nil {
		t.Fatal(err)
	}
	rt := f.dirs.runtime
	gone(t, f.dirs.staging, filepath.Join(rt, "cache.db"), filepath.Join(rt, "covers"), filepath.Join(rt, "state.db"),
		filepath.Join(rt, "state.db.bak-2"), filepath.Join(rt, "backups"),
		filepath.Join(f.dirs.settings, "settings.json"), filepath.Join(f.dirs.settings, "token"))
	exists(t, f.rom, f.save, filepath.Join(f.dirs.settings, "identity"))
	if in, err := settings.New(f.dirs.settings).SignIn(); in != nil || err != nil {
		t.Fatalf("sign-in %+v, %v", in, err)
	}
}

// Without EmuDeck's folder or Decky's dirs, there's less to clean, and nothing fails.
func TestLifecycleWithoutDirs(t *testing.T) {
	if err := uninstall(dataDirs{}); err != nil {
		t.Fatal(err)
	}
	if err := reset(dataDirs{}); err != nil {
		t.Fatal(err)
	}
}
