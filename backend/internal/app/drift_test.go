package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// installedChrono downloads Chrono Trigger, named as RomM names it, and returns its file.
func (f *downloadFixture) installedChrono(t *testing.T) string {
	t.Helper()
	f.catalog.mu.Lock()
	r := f.catalog.roms[1]
	r.FileName = "Chrono Trigger (USA).sfc"
	f.catalog.roms[1] = r
	f.catalog.mu.Unlock()
	f.serve(1, "Chrono Trigger (USA).sfc", chrono)
	rec := f.installed(t, 1)
	if len(rec.Files) != 1 {
		t.Fatalf("record %+v", rec)
	}
	return rec.Files[0].Path
}

// ownershipChange waits for the next ownership change.
func (f *downloadFixture) ownershipChange(t *testing.T) OwnershipChange {
	t.Helper()
	select {
	case c := <-f.ownership:
		return c
	case <-time.After(3 * time.Second):
		t.Fatal("no ownership change")
		return OwnershipChange{}
	}
}

func TestUnchangedOwnedGameStaysInstalled(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	f.installedChrono(t)

	if err := f.downloads.CheckOwned(ctx); err != nil {
		t.Fatal(err)
	}
	if _, owned, _ := f.owned.Owned(ctx, 1); !owned {
		t.Fatal("dropped the record of an unchanged game")
	}
	select {
	case c := <-f.ownership:
		t.Fatalf("change %+v", c)
	default:
	}
}

func TestRemovedOwnedGameIsOnRomM(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	path := f.installedChrono(t)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	if err := f.downloads.CheckOwned(ctx); err != nil {
		t.Fatal(err)
	}
	if _, owned, _ := f.owned.Owned(ctx, 1); owned {
		t.Fatal("kept the record of a deleted game")
	}
	if c := f.ownershipChange(t); c.ROMID != 1 || c.Kind != OwnershipGone || c.State != domain.StateOnRomM || c.Name != "Chrono Trigger" {
		t.Fatalf("change %+v", c)
	}
	if states, _ := f.downloads.States(ctx); domain.StateOf(states, 1) != domain.StateOnRomM {
		t.Fatalf("states %v", states)
	}
}

func TestEditedOwnedGameIsUserManaged(t *testing.T) {
	for name, edit := range map[string]func(path string) error{
		"rewritten": func(path string) error { return os.WriteFile(path, []byte("patched with a translation"), 0o644) },
		// Same size, so only the modification time tells.
		"touched": func(path string) error {
			later := time.Now().Add(time.Hour)
			return os.Chtimes(path, later, later)
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newDownloadFixture(t, 2)
			ctx := context.Background()
			path := f.installedChrono(t)
			if err := edit(path); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)

			if err := f.downloads.CheckOwned(ctx); err != nil {
				t.Fatal(err)
			}
			if _, owned, _ := f.owned.Owned(ctx, 1); owned {
				t.Fatal("still owns an edited game")
			}
			if c := f.ownershipChange(t); c.Kind != OwnershipChanged || c.State != domain.StateOnDeck {
				t.Fatalf("change %+v", c)
			}
			if states, _ := f.downloads.States(ctx); states[1] != domain.StateOnDeck {
				t.Fatalf("states %v", states)
			}
			// User-managed, the plugin never deletes it.
			if _, err := f.downloads.Delete(ctx, []int{1}, 0); kindOf(err) != domain.KindInvalidInput {
				t.Fatalf("delete: %v", err)
			}
			if after, _ := os.ReadFile(path); string(after) != string(before) {
				t.Fatal("file touched")
			}
		})
	}
}

func TestCheckOwnedSkipsGamesBeingDeleted(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	path := f.installedChrono(t)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	f.downloads.mu.Lock()
	f.downloads.deleting[1] = true
	f.downloads.mu.Unlock()

	if err := f.downloads.CheckOwned(ctx); err != nil {
		t.Fatal(err)
	}
	if _, owned, _ := f.owned.Owned(ctx, 1); !owned {
		t.Fatal("dropped the record a delete is working on")
	}
}

func TestDrift(t *testing.T) {
	dir := t.TempDir()
	placed := func(name string) domain.PlacedFile {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return domain.PlacedFile{Path: path, SizeBytes: info.Size(), ModTime: info.ModTime().UTC()}
	}
	a, b := placed("Disc 1.chd"), placed("Disc 2.chd")
	gone := domain.PlacedFile{Path: filepath.Join(dir, "gone.chd"), SizeBytes: 1}
	larger := b
	larger.SizeBytes++

	for _, tc := range []struct {
		name  string
		files []domain.PlacedFile
		want  OwnershipKind
	}{
		{"unchanged", []domain.PlacedFile{a, b}, ""},
		{"all gone", []domain.PlacedFile{gone}, OwnershipGone},
		// What's left of a game missing a disc is the user's.
		{"partly gone", []domain.PlacedFile{a, gone}, OwnershipChanged},
		{"resized", []domain.PlacedFile{a, larger}, OwnershipChanged},
	} {
		got, err := drift(domain.OwnedROM{Files: tc.files})
		if err != nil || got != tc.want {
			t.Errorf("%s: %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
}
