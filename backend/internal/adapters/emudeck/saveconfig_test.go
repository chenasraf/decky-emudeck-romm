package emudeck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

func TestMGBASavePlayer(t *testing.T) {
	cases := map[string]struct {
		config  string
		problem bool
	}{
		"no config":   {"", false},
		"no player":   {"[ports.qt]\n", false},
		"player 1":    {"[ports.qt]\nsavePlayerId=1\n", false},
		"player 3":    {"[ports.qt]\nsavePlayerId=3\n", true},
		"elsewhere 3": {"[gba]\nsavePlayerId=3\n", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			if c.config != "" {
				put(t, filepath.Join(home, mGBAConfig), c.config)
			}
			got := NewSaveConfigs(home).SaveConfig("mgba").Problem
			if (got != "") != c.problem || c.problem && !strings.Contains(got, ".sa3") {
				t.Fatalf("got %q", got)
			}
		})
	}
	if got := NewSaveConfigs(t.TempDir()).SaveConfig("melonds"); got != (domain.SaveConfig{}) {
		t.Fatalf("melonDS: %+v", got)
	}
}

// Each emulator's saves folder setting is followed through links; one set to nothing, or to a
// folder that isn't there, stops its saves from syncing.
func TestSaveFolderSettings(t *testing.T) {
	type file struct{ path, body string }
	cases := map[string]struct {
		id     string
		config func(saves string) file
		// dir is where the saves are, relative to the test's saves folder; empty for the problem.
		dir     string
		problem string
	}{
		"RetroArch": {"retroarch", func(saves string) file {
			return file{retroArchConfig, "savefile_directory = \"" + saves + "/retroarch/saves\"\n"}
		}, "retroarch/saves", ""},
		"RetroArch unset": {"retroarch", func(string) file {
			return file{retroArchConfig, "savefile_directory = \"default\"\n"}
		}, "", "Save Files"},
		"RetroArch beside games": {"retroarch", func(saves string) file {
			return file{retroArchConfig, "savefile_directory = \"" + saves + "\"\nsavefiles_in_content_dir = \"true\"\n"}
		}, "", "Content Directory"},
		"RetroArch sorted": {"retroarch", func(saves string) file {
			return file{retroArchConfig, "savefile_directory = \"" + saves + "\"\nsort_savefiles_enable = \"true\"\n"}
		}, "", "Sort Saves"},
		"melonDS": {"melonds", func(saves string) file {
			return file{melonDSConfig + "/melonDS.toml", "[Instance0]\nSaveFilePath = \"" + saves + "/melonds/saves\"\n"}
		}, "melonds/saves", ""},
		"melonDS ini": {"melonds", func(saves string) file {
			return file{melonDSConfig + "/melonDS.ini", "SaveFilePath=" + saves + "/melonds/saves\n"}
		}, "melonds/saves", ""},
		"melonDS beside games": {"melonds", func(string) file {
			return file{melonDSConfig + "/melonDS.toml", "[Instance0]\nSaveFilePath = \"\"\n"}
		}, "", "Path settings"},
		"mGBA": {"mgba", func(saves string) file {
			return file{mGBAConfig, "[ports.qt]\nsavegamePath=" + saves + "/mgba/saves\n"}
		}, "mgba/saves", ""},
		"mGBA moved away": {"mgba", func(saves string) file {
			return file{mGBAConfig, "[ports.qt]\nsavegamePath=" + saves + "/gone\n"}
		}, "", "isn't there"},
		"RMG": {"rmg", func(saves string) file {
			return file{rmgConfig, "[Core]\nSaveSRAMPath = \"" + saves + "/RMG/saves\"\n"}
		}, "RMG/saves", ""},
		"RMG inside its app": {"rmg", func(string) file {
			return file{rmgConfig, "[Core]\nSaveSRAMPath = \"\"\n"}
		}, "", "SRAM"},
		"DuckStation": {"duckstation", func(saves string) file {
			return file{duckStationData + "/settings.ini", "[MemoryCards]\nDirectory = " + saves + "/duckstation/saves\n"}
		}, "duckstation/saves", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			saves := filepath.Join(home, "Emulation", "saves")
			for _, d := range []string{"retroarch/saves", "melonds/saves", "mgba/saves", "RMG/saves", "duckstation/saves"} {
				if err := os.MkdirAll(filepath.Join(saves, d), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			f := c.config(saves)
			put(t, filepath.Join(home, f.path), f.body)
			got := NewSaveConfigs(home).SaveConfig(c.id)
			if c.problem != "" {
				if got.Dir != "" || !strings.Contains(got.Problem, c.problem) {
					t.Fatalf("got %+v", got)
				}
				return
			}
			want, _ := filepath.EvalSymlinks(filepath.Join(saves, c.dir))
			if got.Problem != "" || got.Dir != want {
				t.Fatalf("got %+v, want %s", got, want)
			}
		})
	}
}

// A saves folder set through a link is followed to where the link leads.
func TestSaveFolderSettingFollowsLinks(t *testing.T) {
	home := t.TempDir()
	native := filepath.Join(home, "native")
	if err := os.MkdirAll(native, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "Emulation", "saves", "mgba")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(native, link); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(home, mGBAConfig), "[ports.qt]\nsavegamePath="+link+"\n")
	want, _ := filepath.EvalSymlinks(native)
	if got := NewSaveConfigs(home).SaveConfig("mgba"); got.Dir != want {
		t.Fatalf("got %+v", got)
	}
}

// DuckStation keeps its cards under its data folder when its settings name none, or a relative one.
func TestDuckStationCardFolder(t *testing.T) {
	home := t.TempDir()
	data := filepath.Join(home, duckStationData)
	for _, d := range []string{"memcards", "cards"} {
		if err := os.MkdirAll(filepath.Join(data, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for config, dir := range map[string]string{"[MemoryCards]\n": "memcards", "[MemoryCards]\nDirectory = cards\n": "cards"} {
		put(t, filepath.Join(data, "settings.ini"), config)
		want, _ := filepath.EvalSymlinks(filepath.Join(data, dir))
		if got := NewSaveConfigs(home).SaveConfig("duckstation"); got.Dir != want {
			t.Fatalf("%q: got %+v", config, got)
		}
	}
}

func TestSaveConfigFiles(t *testing.T) {
	home := t.TempDir()
	files := NewSaveConfigs(home).Files()
	for _, want := range []string{filepath.Join(home, mGBAConfig), filepath.Join(home, duckStationData, "settings.ini")} {
		found := false
		for _, f := range files {
			found = found || f == want
		}
		if !found {
			t.Fatalf("%s not in %v", want, files)
		}
	}
}
