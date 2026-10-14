package covers

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// clock steps a second each time it's read, so every use is newer than the last.
func clock() func() time.Time {
	t := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	return func() time.Time {
		t = t.Add(time.Second)
		return t
	}
}

func open(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.now = clock()
	return s
}

func TestGetPut(t *testing.T) {
	s := open(t, t.TempDir())
	if _, ok, err := s.Get(1, domain.CoverSmall, "/a.png"); ok || err != nil {
		t.Fatalf("empty cache: %v, %v", ok, err)
	}
	if err := s.Put(1, domain.CoverSmall, "/a.png", []byte("small")); err != nil {
		t.Fatal(err)
	}
	if got, ok, _ := s.Get(1, domain.CoverSmall, "/a.png"); !ok || string(got) != "small" {
		t.Fatalf("get: %q, %v", got, ok)
	}
	if _, ok, _ := s.Get(1, domain.CoverLarge, "/a.png"); ok {
		t.Fatal("large served the small cover")
	}

	// The Server changed the cover: the old path misses, and the old copy goes.
	if err := s.Put(1, domain.CoverSmall, "/a.png?ts=2", []byte("newer")); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Get(1, domain.CoverSmall, "/a.png"); ok {
		t.Fatal("old path still served")
	}
	if got, ok, _ := s.Stale(1, domain.CoverSmall); !ok || string(got) != "newer" {
		t.Fatalf("stale: %q, %v", got, ok)
	}
	if s.Size() != int64(len("newer")) {
		t.Fatalf("size %d", s.Size())
	}
}

func TestReopenKeepsIndex(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	if err := s.Put(7, domain.CoverLarge, "/c.jpg", []byte("large")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".put-123"), []byte("cut short"), 0o644); err != nil {
		t.Fatal(err)
	}

	s = open(t, dir)
	if got, ok, _ := s.Get(7, domain.CoverLarge, "/c.jpg"); !ok || string(got) != "large" || s.Size() != 5 {
		t.Fatalf("reopened: %q, %v, size %d", got, ok, s.Size())
	}
	if _, err := os.Stat(filepath.Join(dir, ".put-123")); !os.IsNotExist(err) {
		t.Fatal("leftover write kept")
	}
}

func TestClear(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	for id := 1; id <= 2; id++ {
		if err := s.Put(id, domain.CoverSmall, "/a.png", []byte("cover")); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Stale(1, domain.CoverSmall); ok || s.Size() != 0 {
		t.Fatalf("after clear: %v, size %d", ok, s.Size())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("files left: %v", entries)
	}
	if err := s.Put(1, domain.CoverSmall, "/a.png", []byte("again")); err != nil {
		t.Fatal(err)
	}
	if got, ok, _ := s.Get(1, domain.CoverSmall, "/a.png"); !ok || string(got) != "again" {
		t.Fatalf("after refill: %q, %v", got, ok)
	}
}

func TestTrimEvictsLeastRecentlyUsed(t *testing.T) {
	s := open(t, t.TempDir())
	for id := 1; id <= 4; id++ {
		if err := s.Put(id, domain.CoverSmall, "/x", []byte("1234567890")); err != nil {
			t.Fatal(err)
		}
	}
	// 1 is the oldest put, but was used since; 2 is pinned.
	if _, ok, _ := s.Get(1, domain.CoverSmall, "/x"); !ok {
		t.Fatal("missing 1")
	}
	s.Trim(20, func(id int) bool { return id == 2 })

	if s.Size() > 20 {
		t.Fatalf("size %d over the limit", s.Size())
	}
	for id, want := range map[int]bool{1: true, 2: true, 3: false, 4: false} {
		if _, ok, _ := s.Get(id, domain.CoverSmall, "/x"); ok != want {
			t.Errorf("cover %d kept %v, want %v", id, ok, want)
		}
	}
}

func TestTrimNeverEvictsPinned(t *testing.T) {
	s := open(t, t.TempDir())
	for id := 1; id <= 3; id++ {
		if err := s.Put(id, domain.CoverSmall, "/x", []byte("1234567890")); err != nil {
			t.Fatal(err)
		}
	}
	s.Trim(5, func(int) bool { return true })
	if s.Size() != 30 {
		t.Fatalf("evicted a pinned cover: size %d", s.Size())
	}
}
