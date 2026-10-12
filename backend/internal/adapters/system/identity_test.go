package system

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIdentifierFollowsMachineID(t *testing.T) {
	dir := t.TempDir()
	machineID := filepath.Join(dir, "machine-id")
	osRelease := filepath.Join(dir, "os-release")
	write(t, machineID, "0123456789abcdef0123456789abcdef\n")
	write(t, osRelease, "NAME=\"SteamOS\"\nID=steamos\n")

	// Two fresh instances stand in for a plugin reinstall with its data wiped.
	a, err := (&Identity{machineIDPath: machineID, osReleasePath: osRelease, fallbackDir: t.TempDir()}).Device()
	if err != nil {
		t.Fatal(err)
	}
	b, err := (&Identity{machineIDPath: machineID, osReleasePath: osRelease, fallbackDir: t.TempDir()}).Device()
	if err != nil {
		t.Fatal(err)
	}
	if a.Identifier != b.Identifier || len(a.Identifier) != 64 {
		t.Fatalf("identifiers %q and %q", a.Identifier, b.Identifier)
	}
	if strings.Contains(a.Identifier, "0123456789abcdef") {
		t.Fatal("identifier exposes the raw machine id")
	}
	if a.Platform != "steamos" || !strings.HasPrefix(a.Name, "Steam Deck (") {
		t.Fatalf("got %+v", a)
	}

	write(t, machineID, "fedcba9876543210fedcba9876543210\n")
	c, _ := (&Identity{machineIDPath: machineID, osReleasePath: osRelease}).Device()
	if c.Identifier == a.Identifier {
		t.Fatal("a different machine id gave the same identifier")
	}
}

func TestFallbackIdentifierIsKept(t *testing.T) {
	dir := t.TempDir()
	id := &Identity{machineIDPath: filepath.Join(dir, "missing"), osReleasePath: filepath.Join(dir, "missing"), fallbackDir: dir}
	a, err := id.Device()
	if err != nil {
		t.Fatal(err)
	}
	b, err := id.Device()
	if err != nil || a.Identifier != b.Identifier {
		t.Fatalf("got %q then %q, %v", a.Identifier, b.Identifier, err)
	}
}
