package esde

import (
	"os"
	"path/filepath"
	"testing"
)

// home makes a home folder with ES-DE's AppImage from testdata, which bundles snes and psx systems.
func home(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	image, err := os.ReadFile("testdata/ES-DE.AppImage")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(h, "Applications", "ES-DE.AppImage"), string(image))
	return h
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gamelistFile(h, system string) string {
	return filepath.Join(h, "ES-DE", "gamelists", system, "gamelist.xml")
}

func TestLauncherFromTheAppImage(t *testing.T) {
	l := New(home(t))
	got, err := l.Launcher("snes", "Chrono Trigger.sfc")
	if err != nil || !got.Known || got.Label != "Snes9x - Current" || got.RetroArchCore() != "snes9x" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestLauncherFollowsGamelists(t *testing.T) {
	h := home(t)
	write(t, gamelistFile(h, "snes"), `<?xml version="1.0"?>
<alternativeEmulator>
	<label>bsnes</label>
</alternativeEmulator>
<gameList>
	<game>
		<path>./Super Metroid.sfc</path>
		<name>Super Metroid</name>
		<altemulator>Snes9x (Standalone)</altemulator>
	</game>
	<game>
		<path>./Zelda.sfc</path>
		<name>Zelda</name>
	</game>
</gameList>
`)
	l := New(h)

	cases := map[string]struct{ label, core string }{
		"the system's alternative": {"bsnes", "bsnes"},
		"the game's alternative":   {"Snes9x (Standalone)", ""},
	}
	roms := map[string]string{"the system's alternative": "Zelda.sfc", "the game's alternative": "Super Metroid.sfc"}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := l.Launcher("snes", roms[name])
			if err != nil || !got.Known || got.Label != want.label || got.RetroArchCore() != want.core {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}

func TestCustomSystemsReplaceBundledOnes(t *testing.T) {
	h := home(t)
	write(t, filepath.Join(h, "ES-DE", "custom_systems", "es_systems.xml"), `<?xml version="1.0"?>
<systemList>
	<system>
		<name>psx</name>
		<command label="SwanStation">%EMULATOR_RETROARCH% -L %CORE_RETROARCH%/swanstation_libretro.so %ROM%</command>
	</system>
</systemList>
`)
	l := New(h)
	if got, _ := l.Launcher("psx", "Ridge Racer.chd"); got.RetroArchCore() != "swanstation" {
		t.Fatalf("psx %+v", got)
	}
	if got, _ := l.Launcher("snes", "Chrono Trigger.sfc"); got.RetroArchCore() != "snes9x" {
		t.Fatalf("snes %+v", got)
	}
}

// Without ES-DE's AppImage only the label a gamelist names is known.
func TestLauncherWithoutTheAppImage(t *testing.T) {
	h := t.TempDir()
	write(t, gamelistFile(h, "psx"), "<alternativeEmulator><label>DuckStation (Standalone)</label></alternativeEmulator><gameList />")
	got, err := New(h).Launcher("psx", "Ridge Racer.chd")
	if err == nil || got.Known || got.Label != "DuckStation (Standalone)" || !got.NotRetroArch() {
		t.Fatalf("got %+v, %v", got, err)
	}
}

// A gamelist edited while the plugin runs is read again.
func TestGamelistChangesAreSeen(t *testing.T) {
	h := home(t)
	l := New(h)
	if got, _ := l.Launcher("snes", "Zelda.sfc"); got.Label != "Snes9x - Current" {
		t.Fatalf("before %+v", got)
	}
	write(t, gamelistFile(h, "snes"), "<alternativeEmulator><label>bsnes</label></alternativeEmulator><gameList />")
	if got, _ := l.Launcher("snes", "Zelda.sfc"); got.Label != "bsnes" {
		t.Fatalf("after %+v", got)
	}
}

// A label the system definitions don't have is returned on its own.
func TestUnknownLabel(t *testing.T) {
	h := home(t)
	write(t, gamelistFile(h, "snes"), "<alternativeEmulator><label>Mesen-S</label></alternativeEmulator><gameList />")
	got, err := New(h).Launcher("snes", "Zelda.sfc")
	if err != nil || got.Known || got.Label != "Mesen-S" {
		t.Fatalf("got %+v, %v", got, err)
	}
}
