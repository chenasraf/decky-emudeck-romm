package app

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

func TestActivityLogsWhatSyncDid(t *testing.T) {
	f := newSaveFixture(t)
	head := f.conflicted(t)
	// Syncing again while the conflict stands adds nothing.
	f.sync(t)
	if err := f.resolve(domain.KeepServer, head.ID); err != nil {
		t.Fatal(err)
	}

	want := []domain.ActivityKind{domain.ActivityUploaded, domain.ActivityConflict, domain.ActivityDownloaded, domain.ActivityResolved}
	if got := f.store.kinds(1); !slices.Equal(got, want) {
		t.Fatalf("logged %v", got)
	}
	entries, err := f.saves.Activity(context.Background(), 1, 10)
	if err != nil || len(entries) != 4 || entries[0].Kind != domain.ActivityResolved || entries[0].Name != "Chrono Trigger" {
		t.Fatalf("entries %+v, %v", entries, err)
	}
	for _, e := range entries {
		if e.Kind == domain.ActivityConflict && !e.Resolved {
			t.Fatal("settled conflict left unresolved")
		}
	}
}

// A sync that keeps failing the same way is logged once.
func TestRepeatedFailureLoggedOnce(t *testing.T) {
	f := newSaveFixture(t)
	f.romm.otherDevice(1, "duckstation", []byte("a memory card"))
	f.sync(t)
	f.sync(t)
	if got := f.store.kinds(1); !slices.Equal(got, []domain.ActivityKind{domain.ActivityFailed}) {
		t.Fatalf("logged %v", got)
	}
}

func TestRestoreBackup(t *testing.T) {
	f := newSaveFixture(t)
	// Every backup is taken in the same millisecond, so none may overwrite another.
	at := time.Now()
	f.saves.now = func() time.Time { return at }
	f.writeSave(t, "old progress", time.Now().Add(-time.Hour))
	f.sync(t)
	f.romm.otherDevice(1, "snes9x", []byte("newer progress"))
	f.sync(t)

	backups, err := f.saves.Backups(1)
	if err != nil || len(backups) != 1 || backups[0].SizeBytes != int64(len("old progress")) || backups[0].At.IsZero() {
		t.Fatalf("backups %+v, %v", backups, err)
	}
	if err := f.saves.RestoreBackup(context.Background(), 1, backups[0].Name); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, f.chronoSave()); got != "old progress" {
		t.Fatalf("save holds %q", got)
	}
	// The save it replaced is kept too.
	if after, _ := f.saves.Backups(1); len(after) != 2 {
		t.Fatalf("backups after restore %+v", after)
	}
	// The restored save uploads as the newest.
	if e := f.ended(t); e.Summary.Uploaded != 1 {
		t.Fatalf("sync %+v", e)
	}
	if head, _ := f.romm.head(1); string(head.data) != "old progress" {
		t.Fatalf("RomM holds %q", head.data)
	}
	if got := f.store.kinds(1); !slices.Contains(got, domain.ActivityRestored) {
		t.Fatalf("logged %v", got)
	}
}

func TestRestoreRefuses(t *testing.T) {
	f := newSaveFixture(t)
	f.writeSave(t, "old progress", time.Now().Add(-time.Hour))
	f.sync(t)
	f.romm.otherDevice(1, "snes9x", []byte("newer progress"))
	f.sync(t)
	backups, _ := f.saves.Backups(1)

	cases := map[string]struct {
		name      string
		retroarch bool
		kind      domain.Kind
	}{
		"a path outside the backups": {"../../saves/retroarch/saves/Chrono Trigger (USA).srm", false, domain.KindInvalidInput},
		"a backup that isn't there":  {"20260101-000000.000.srm", false, domain.KindNotFound},
		"while RetroArch runs":       {backups[0].Name, true, domain.KindInvalidInput},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f.retroarch.Store(c.retroarch)
			defer f.retroarch.Store(false)
			if err := f.saves.RestoreBackup(context.Background(), 1, c.name); kindOf(err) != c.kind {
				t.Fatalf("restore: %v", err)
			}
			if got := readFile(t, f.chronoSave()); got != "newer progress" {
				t.Fatalf("save holds %q", got)
			}
		})
	}
}
