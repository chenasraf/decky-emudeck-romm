package emudeck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMGBASavePlayer(t *testing.T) {
	cases := map[string]struct {
		config  string
		problem bool
	}{
		"no config":   {"", false},
		"no player":   {"[ports.qt]\nsavegamePath=/e/saves/mgba/saves\n", false},
		"player 1":    {"[ports.qt]\nsavePlayerId=1\n", false},
		"player 3":    {"[ports.qt]\nsavePlayerId=3\n", true},
		"elsewhere 3": {"[gba]\nsavePlayerId=3\n", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			if c.config != "" {
				p := filepath.Join(home, ".config", "mgba", "config.ini")
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(c.config), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got := NewSaveConfigs(home).SaveProblem("mgba")
			if (got != "") != c.problem || c.problem && !strings.Contains(got, ".sa3") {
				t.Fatalf("got %q", got)
			}
		})
	}
	if got := NewSaveConfigs(t.TempDir()).SaveProblem("melonds"); got != "" {
		t.Fatalf("melonDS: %q", got)
	}
}
