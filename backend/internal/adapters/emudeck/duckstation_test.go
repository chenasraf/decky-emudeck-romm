package emudeck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// duckHome makes a home folder with DuckStation's AppImage from testdata, whose game database lists
// Crash Bandicoot 2, Final Fantasy VII's two discs as a disc set, a game answering to two codes, and
// one known by its hash.
func duckHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	image, err := os.ReadFile("testdata/DuckStation.AppImage")
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(home, "Applications", "DuckStation.AppImage"), string(image))
	return home
}

func put(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const romPath = "/home/deck/Emulation/roms/psx/Game.chd"

func TestDuckStationNamesCards(t *testing.T) {
	cases := map[string]struct {
		disc domain.PSXDisc
		want domain.DuckStationGame
	}{
		"known game":        {domain.PSXDisc{Read: true, Serial: "SLUS-00713"}, domain.DuckStationGame{Title: "Crash Bandicoot 2 - Cortex Strikes Back (USA)"}},
		"disc of a set":     {domain.PSXDisc{Read: true, Serial: "SCUS-94164"}, domain.DuckStationGame{Title: "Final Fantasy VII (USA) (Disc 2)", DiscSet: "Final Fantasy VII (USA)"}},
		"code of an entry":  {domain.PSXDisc{Read: true, Serial: "SLPS-02546"}, domain.DuckStationGame{Title: `Kotoba no Puzzle: "Mojipittan"`}},
		"known by its hash": {domain.PSXDisc{Read: true, HashID: "HASH-66FFEBF68485E094"}, domain.DuckStationGame{Title: "Homebrew Demo (World)"}},
		"unknown serial":    {domain.PSXDisc{Read: true, Serial: "SLUS-99999", HashID: "HASH-1"}, domain.DuckStationGame{Title: "Game"}},
		"unreadable disc":   {domain.PSXDisc{}, domain.DuckStationGame{Title: "Game"}},
	}
	d := NewDuckStation(duckHome(t))
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, why, err := d.Game(romPath, c.disc)
			if err != nil || why != "" || got != c.want {
				t.Fatalf("got %+v %q %v", got, why, err)
			}
		})
	}
}

func TestDuckStationCustomTitleAndSerial(t *testing.T) {
	home := duckHome(t)
	put(t, filepath.Join(home, duckStationData, "custom_properties.ini"),
		"["+romPath+"]\nTitle = My Crash\n\n[/other.chd]\nSerial = SLUS-00713\n")
	d := NewDuckStation(home)
	got, _, err := d.Game(romPath, domain.PSXDisc{Read: true, Serial: "SCUS-94163"})
	if err != nil || got != (domain.DuckStationGame{Title: "My Crash", DiscSet: "Final Fantasy VII (USA)"}) {
		t.Fatalf("custom title: %+v %v", got, err)
	}
	got, _, err = d.Game("/other.chd", domain.PSXDisc{})
	if err != nil || got.Title != "Crash Bandicoot 2 - Cortex Strikes Back (USA)" {
		t.Fatalf("custom serial: %+v %v", got, err)
	}
}

// A game with memory card settings of its own names its card some other way.
func TestDuckStationGameSettings(t *testing.T) {
	home := duckHome(t)
	put(t, filepath.Join(home, duckStationData, "gamesettings", "SLUS-00713.ini"), "[MemoryCards]\nCard1Type = Shared\n")
	put(t, filepath.Join(home, duckStationData, "gamesettings", "SCUS-94163.ini"), "[Display]\nAspectRatio = 16:9\n")
	d := NewDuckStation(home)
	if _, why, err := d.Game(romPath, domain.PSXDisc{Read: true, Serial: "SLUS-00713"}); err != nil || !strings.Contains(why, "own memory card settings") {
		t.Fatalf("why %q %v", why, err)
	}
	if _, why, err := d.Game(romPath, domain.PSXDisc{Read: true, Serial: "SCUS-94163"}); err != nil || why != "" {
		t.Fatalf("other settings: %q %v", why, err)
	}
}

func TestDuckStationReadsTheNewestAppImage(t *testing.T) {
	home := duckHome(t)
	put(t, filepath.Join(home, "Applications", "duckstation-old.appimage.bak"), "not it")
	image, _ := os.ReadFile(filepath.Join(home, "Applications", "DuckStation.AppImage"))
	put(t, filepath.Join(home, "Applications", "DuckStation-x64.AppImage"), string(image))
	if got := NewDuckStation(home).appImage(); filepath.Base(got) != "DuckStation.AppImage" {
		t.Fatalf("picked %s", got)
	}
	if _, _, err := NewDuckStation(t.TempDir()).Game(romPath, domain.PSXDisc{}); err == nil {
		t.Fatal("no AppImage, no error")
	}
}

func TestDuckStationCardSettings(t *testing.T) {
	cases := map[string]struct {
		config  string
		problem string
	}{
		"no config":            {"", ""},
		"EmuDeck's":            {"[MemoryCards]\nCard1Type = PerGameTitle\nCard2Type = None\nUsePlaylistTitle = true\n", ""},
		"by serial":            {"[MemoryCards]\nCard1Type = PerGame\n", "Card 1"},
		"shared":               {"[MemoryCards]\nCard1Type = Shared\n", "Card 1"},
		"a card for each disc": {"[MemoryCards]\nUsePlaylistTitle = false\n", "Multi-Disc"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			if c.config != "" {
				put(t, filepath.Join(home, duckStationData, "settings.ini"), c.config)
			}
			got := NewSaveConfigs(home).SaveProblem("duckstation")
			if (got != "") != (c.problem != "") || !strings.Contains(got, c.problem) {
				t.Fatalf("got %q", got)
			}
		})
	}
}
