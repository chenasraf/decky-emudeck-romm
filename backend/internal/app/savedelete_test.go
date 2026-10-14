package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

func (f *saveFixture) deletion(t *testing.T, id int) SaveDeletion {
	t.Helper()
	d, err := f.saves.Deletion(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// A save that isn't on RomM as it is on the Deck keeps the game from being deleted with its save.
func TestUnsyncedSaveBlocksDeletingIt(t *testing.T) {
	f := newSaveFixture(t)
	f.downloads.KeepSavesWith(f.saves)
	ctx := context.Background()

	if d := f.deletion(t, 1); d.Has || d.Why != "" {
		t.Fatalf("no save %+v", d)
	}
	f.writeSave(t, "unsynced progress", time.Now())
	if d := f.deletion(t, 1); !d.Has || !strings.Contains(d.Why, "Sync first") {
		t.Fatalf("unsynced %+v", d)
	}
	if err := f.downloads.DeleteWithSaves(ctx, 1); kindOf(err) != domain.KindInvalidInput {
		t.Fatalf("delete: %v", err)
	}
	if _, ok, _ := f.owned.Owned(ctx, 1); !ok {
		t.Fatal("game deleted")
	}

	f.sync(t)
	if d := f.deletion(t, 1); !d.Has || d.Why != "" {
		t.Fatalf("synced %+v", d)
	}
	// Changed again in game: RomM no longer holds it as it is.
	f.writeSave(t, "newer progress", time.Now().Add(time.Minute))
	if d := f.deletion(t, 1); !strings.Contains(d.Why, "Sync first") {
		t.Fatalf("changed %+v", d)
	}
	f.retroarch.Store(true)
	f.sync(t)
	if d := f.deletion(t, 1); !strings.Contains(d.Why, "Close RetroArch") {
		t.Fatalf("while RetroArch runs %+v", d)
	}
}

// A synced save deleted with its game goes into the game's backups.
func TestDeletedSaveIsInBackups(t *testing.T) {
	f := newSaveFixture(t)
	f.downloads.KeepSavesWith(f.saves)
	ctx := context.Background()
	f.writeSave(t, "synced progress", time.Now())
	f.sync(t)

	if err := f.downloads.DeleteWithSaves(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if u := f.deleted(t, 1); u.Problem != nil {
		t.Fatalf("end %+v", u)
	}
	if _, err := os.Stat(f.chronoSave()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("save still there: %v", err)
	}
	backups, err := f.saves.Backups(1)
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups %+v, %v", backups, err)
	}
	if got := readFile(t, filepath.Join(f.backups, "1", backups[0].Name)); got != "synced progress" {
		t.Fatalf("backup holds %q", got)
	}
	if a, _ := f.saves.Activity(ctx, 1, 1); len(a) != 1 || a[0].Kind != domain.ActivityDeleted {
		t.Fatalf("activity %+v", a)
	}
}

// A plain delete leaves the save where it is.
func TestDeleteKeepsSaves(t *testing.T) {
	f := newSaveFixture(t)
	f.downloads.KeepSavesWith(f.saves)
	f.writeSave(t, "progress", time.Now())
	f.sync(t)
	if _, err := f.downloads.Delete(context.Background(), []int{1}, 0); err != nil {
		t.Fatal(err)
	}
	f.deleted(t, 1)
	if got := readFile(t, f.chronoSave()); got != "progress" {
		t.Fatalf("save holds %q", got)
	}
}
