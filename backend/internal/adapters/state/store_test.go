package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func open(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(context.Background(), path, discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOwnedRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "state.db"))

	at := time.Date(2026, 10, 13, 8, 0, 0, 123, time.UTC)
	rom := domain.OwnedROM{
		ROMID:       7,
		PlatformID:  2,
		Files:       []domain.PlacedFile{{Path: "/e/roms/snes/Chrono Trigger.sfc", SizeBytes: 4 << 20, ModTime: at, SHA1: "abc"}},
		Snapshot:    []domain.FileSnapshot{{FileID: 70, SizeBytes: 4 << 20, LastModified: at, HashKind: "sha1", Hash: "def"}},
		InstalledAt: at,
	}
	if err := s.SaveOwned(ctx, rom); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Owned(ctx, 7)
	if err != nil || !ok {
		t.Fatalf("owned: %v %v", ok, err)
	}
	if got.PlatformID != 2 || !got.InstalledAt.Equal(at) || len(got.Files) != 1 || got.Files[0] != rom.Files[0] ||
		len(got.Snapshot) != 1 || got.Snapshot[0] != rom.Snapshot[0] {
		t.Fatalf("got %+v", got)
	}

	// Saving again replaces the files and snapshot rather than adding to them.
	rom.Files[0].Path = "/e/roms/snes/Chrono Trigger (USA).sfc"
	if err := s.SaveOwned(ctx, rom); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s.Owned(ctx, 7); len(got.Files) != 1 || got.Files[0].Path != rom.Files[0].Path || len(got.Snapshot) != 1 {
		t.Fatalf("replaced: %+v", got)
	}
	if ids, err := s.OwnedIDs(ctx); err != nil || len(ids) != 1 || ids[0] != 7 {
		t.Fatalf("ids %v, %v", ids, err)
	}
	if _, ok, err := s.Owned(ctx, 8); ok || err != nil {
		t.Fatalf("missing: %v %v", ok, err)
	}
}

func TestReopenKeepsRecords(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	s := open(t, path)
	if err := s.SaveOwned(ctx, domain.OwnedROM{ROMID: 1, PlatformID: 1}); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	s = open(t, path)
	if _, ok, _ := s.Owned(ctx, 1); !ok {
		t.Fatal("record lost on reopen")
	}
	// A fresh file is created at the latest schema, so there was nothing to back up.
	if backups, _ := Backups(path); len(backups) != 0 {
		t.Fatalf("backups %v", backups)
	}
}

func setVersion(t *testing.T, path string, version int) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		t.Fatal(err)
	}
}

func TestNewerSchemaIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	_ = open(t, path).Close()
	setVersion(t, path, SchemaVersion()+1)

	_, err := Open(context.Background(), path, discard)
	var de *domain.Error
	if !errors.As(err, &de) || de.Kind != domain.KindNewerData {
		t.Fatalf("want newer_data, got %v", err)
	}
	// The file is left as it was, for the newer version or a restore.
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationBacksUpAndKeepsThree(t *testing.T) {
	saved := migrations
	t.Cleanup(func() { migrations = saved })
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	s := open(t, path)
	if err := s.SaveOwned(ctx, domain.OwnedROM{ROMID: 1, PlatformID: 1}); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	// Four releases, each adding a migration.
	for i := range 4 {
		migrations = append(migrations, fmt.Sprintf("CREATE TABLE extra_%d (id INTEGER)", i))
		s = open(t, path)
		_ = s.Close()
	}
	backups, err := Backups(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 3 || backups[0].Version != 4 || backups[2].Version != 2 {
		t.Fatalf("backups %+v", backups)
	}

	// A backup is a working copy of the file as it was before that migration.
	b := open(t, backups[0].Path)
	if _, ok, _ := b.Owned(ctx, 1); !ok {
		t.Fatal("backup lost the record")
	}
}
