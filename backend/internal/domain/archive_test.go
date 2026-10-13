package domain

import "testing"

func system(t *testing.T, slug string) System {
	t.Helper()
	s, ok := SystemForSlug(slug)
	if !ok {
		t.Fatalf("no system for %s", slug)
	}
	return s
}

func TestExtracts(t *testing.T) {
	for _, c := range []struct {
		slug    string
		members int
		keep    bool
		want    bool
	}{
		{"arcade", 5, false, false},
		{"dos", 3, false, false},
		{"psx", 1, true, true},
		{"ngc", 1, true, true},
		{"snes", 1, true, false},
		{"snes", 1, false, true},
		{"snes", 2, true, true},
		{"nds", 1, true, false},
		{"gba", 1, false, true},
	} {
		if got := system(t, c.slug).Extracts(c.members, c.keep); got != c.want {
			t.Errorf("%s with %d members, keep %v: extracts %v, want %v", c.slug, c.members, c.keep, got, c.want)
		}
	}
}

func TestIsArchive(t *testing.T) {
	for name, want := range map[string]bool{
		"Game.zip": true, "Game.ZIP": true, "Game.7z": true, "Game.sfc": false, "Game.rar": false, "zip": false,
	} {
		if IsArchive(name) != want {
			t.Errorf("IsArchive(%q) != %v", name, want)
		}
	}
}
