package system

import (
	"path/filepath"
	"testing"
)

func TestFreeBytes(t *testing.T) {
	dir := t.TempDir()
	if n, err := FreeBytes(dir); err != nil || n <= 0 {
		t.Fatalf("free %d, %v", n, err)
	}
	if _, err := FreeBytes(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("measured a path that doesn't exist")
	}
}
