package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// install downloads the games with ids and waits for each to be installed.
func (f *downloadFixture) install(t *testing.T, ids ...int) {
	t.Helper()
	for _, id := range ids {
		if _, err := f.downloads.Queue(context.Background(), id); err != nil {
			t.Fatal(err)
		}
		if u := f.end(t, id); u.State != domain.StateInstalled {
			t.Fatalf("install %d: %+v", id, u)
		}
	}
}

// deleted waits for the deletions of ids to end, in any order, and returns the last update of the
// first.
func (f *downloadFixture) deleted(t *testing.T, ids ...int) DeleteUpdate {
	t.Helper()
	ended := map[int]DeleteUpdate{}
	for len(ended) < len(ids) {
		select {
		case u := <-f.deletes:
			if u.State != domain.StateDeleting {
				ended[u.ROMID] = u
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("deletes of %v didn't end; ended %v", ids, ended)
		}
	}
	return ended[ids[0]]
}

func TestDeleteRemovesRecordedFiles(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.install(t, 1)
	ctx := context.Background()

	// Saves and another file the user put next to the ROM.
	saves := filepath.Join(f.root, "saves", "retroarch", "saves", "Chrono Trigger (USA).srm")
	neighbour := filepath.Join(f.snes, "Chrono Trigger (USA).sfc.bak")
	for _, p := range []string{saves, neighbour} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("mine"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	n, err := f.downloads.Delete(ctx, []int{1}, 0)
	if err != nil || n != 1 {
		t.Fatalf("delete: %d, %v", n, err)
	}
	if u := f.deleted(t, 1); u.Problem != nil || u.State != domain.StateOnRomM || u.Name != "Chrono Trigger" {
		t.Fatalf("end: %+v", u)
	}
	if _, err := os.Stat(filepath.Join(f.snes, "Chrono Trigger (USA).sfc")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ROM still there: %v", err)
	}
	for _, p := range []string{saves, neighbour} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("deleted %s: %v", p, err)
		}
	}
	if _, ok, _ := f.owned.Owned(ctx, 1); ok {
		t.Fatal("record kept")
	}
	if states, _ := f.downloads.States(ctx); len(states) != 0 {
		t.Fatalf("states %v", states)
	}
}

func TestDeleteRefusesChangedFile(t *testing.T) {
	for name, change := range map[string][]byte{
		"different size":    []byte("patched with a translation"),
		"same size, edited": []byte("CHRONO TRIGGER ROM BYTES"),
	} {
		t.Run(name, func(t *testing.T) {
			f := newDownloadFixture(t, 2)
			f.install(t, 1)
			rom := filepath.Join(f.snes, "Chrono Trigger (USA).sfc")
			if err := os.WriteFile(rom, change, 0o644); err != nil {
				t.Fatal(err)
			}

			if _, err := f.downloads.Delete(context.Background(), []int{1}, 0); err != nil {
				t.Fatal(err)
			}
			if u := f.deleted(t, 1); kindOf(u.Problem) != domain.KindChangedOnDisk {
				t.Fatalf("end: %+v", u)
			}
			if data, _ := os.ReadFile(rom); string(data) != string(change) {
				t.Fatalf("the user's file was touched: %q", data)
			}
			// It's User-managed now: no record, and the plugin won't delete it.
			if _, ok, _ := f.owned.Owned(context.Background(), 1); ok {
				t.Fatal("still owned")
			}
			if _, err := f.downloads.Delete(context.Background(), []int{1}, 0); kindOf(err) != domain.KindInvalidInput {
				t.Fatalf("delete again: %v", err)
			}
		})
	}
}

func TestDeleteFileAlreadyGone(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.install(t, 1)
	if err := os.Remove(filepath.Join(f.snes, "Chrono Trigger (USA).sfc")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.downloads.Delete(context.Background(), []int{1}, 0); err != nil {
		t.Fatal(err)
	}
	if u := f.deleted(t, 1); u.Problem != nil {
		t.Fatalf("end: %+v", u)
	}
	if _, ok, _ := f.owned.Owned(context.Background(), 1); ok {
		t.Fatal("record kept")
	}
}

func TestDeletePlatform(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.install(t, 1, 2)
	// A game on another platform the plugin also owns.
	f.owned.roms[4] = domain.OwnedROM{ROMID: 4, PlatformID: 2}

	n, err := f.downloads.Delete(context.Background(), nil, 1)
	if err != nil || n != 2 {
		t.Fatalf("delete: %d, %v", n, err)
	}
	f.deleted(t, 1, 2)
	if entries, _ := os.ReadDir(f.snes); len(entries) != 0 {
		t.Fatalf("left in the platform folder: %v", entries)
	}
	if ids, _ := f.owned.OwnedIDs(context.Background()); len(ids) != 1 || ids[0] != 4 {
		t.Fatalf("owned after: %v", ids)
	}
}

func TestDeleteSkipsDownloading(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.install(t, 1)
	f.server.gates[20] = make(chan struct{})
	if _, err := f.downloads.Queue(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	f.started(t)
	ctx := context.Background()

	if _, err := f.downloads.Delete(ctx, []int{2}, 0); kindOf(err) != domain.KindInvalidInput {
		t.Fatalf("delete a downloading game: %v", err)
	}
	// While a game is being deleted it reads as deleting; then the game it was is gone.
	if _, err := f.downloads.Delete(ctx, []int{1}, 0); err != nil {
		t.Fatal(err)
	}
	f.deleted(t, 1)
	close(f.server.gates[20])
	f.end(t, 2)
}
