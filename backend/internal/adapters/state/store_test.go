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

	// Moving to another id keeps the same files, whose paths are unique, under the new one.
	rom.ROMID = 9
	if err := s.MoveOwned(ctx, 7, rom); err != nil {
		t.Fatal(err)
	}
	if ids, _ := s.OwnedIDs(ctx); len(ids) != 1 || ids[0] != 9 {
		t.Fatalf("ids after move %v", ids)
	}

	if err := s.DeleteOwned(ctx, 9); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Owned(ctx, 9); ok {
		t.Fatal("record still there")
	}
	// Its files and snapshot go with it.
	var rows int
	if err := s.db.QueryRow("SELECT (SELECT COUNT(*) FROM owned_files) + (SELECT COUNT(*) FROM server_snapshots)").Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("%d rows left, %v", rows, err)
	}
}

func TestQueueRoundTrip(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	s := open(t, path)

	at := time.Date(2026, 10, 13, 8, 0, 0, 123, time.UTC)
	first := domain.QueuedDownload{ROMID: 9, PlatformID: 1, PlatformName: "SNES", Name: "Chrono Trigger", TotalBytes: 100}
	second := domain.QueuedDownload{ROMID: 3, PlatformID: 1, PlatformName: "SNES", Name: "Super Metroid", TotalBytes: 50, Update: true}
	for _, d := range []domain.QueuedDownload{first, second} {
		if err := s.SaveQueued(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	// Failing the first keeps its place at the front.
	first.Problem = &domain.Error{Kind: domain.KindUnreachable, Message: "The download from RomM was interrupted."}
	first.Partial = domain.FileSnapshot{FileID: 90, SizeBytes: 100, LastModified: at}
	if err := s.SaveQueued(ctx, first); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	s = open(t, path)
	got, err := s.Queued(ctx)
	if err != nil || len(got) != 2 {
		t.Fatalf("queued %+v, %v", got, err)
	}
	if got[0].ROMID != 9 || got[0].Problem == nil || *got[0].Problem != *first.Problem || got[0].Partial != first.Partial {
		t.Fatalf("first %+v", got[0])
	}
	if got[1] != second {
		t.Fatalf("second %+v", got[1])
	}

	if err := s.DeleteQueued(ctx, 9); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Queued(ctx); len(got) != 1 || got[0].ROMID != 3 {
		t.Fatalf("after delete %+v", got)
	}
}

func TestOwnedBIOSRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "state.db"))

	at := time.Date(2026, 10, 13, 8, 0, 0, 456, time.UTC)
	b := domain.OwnedBIOS{
		FirmwareID:  31,
		PlatformID:  3,
		File:        domain.PlacedFile{Path: "/e/bios/scph5501.bin", SizeBytes: 512 << 10, ModTime: at, SHA1: "abc"},
		InstalledAt: at,
	}
	if err := s.SaveOwnedBIOS(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveOwnedBIOS(ctx, domain.OwnedBIOS{FirmwareID: 51, PlatformID: 5, File: domain.PlacedFile{Path: "/e/bios/dc/dc_boot.bin"}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.OwnedBIOS(ctx, 3)
	if err != nil || len(got) != 1 || got[0].FirmwareID != 31 || got[0].File != b.File || !got[0].InstalledAt.Equal(at) {
		t.Fatalf("got %+v, %v", got, err)
	}

	if err := s.DeleteOwnedBIOS(ctx, 31); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.OwnedBIOS(ctx, 3); len(got) != 0 {
		t.Fatalf("after delete %+v", got)
	}
}

func TestLinkedRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "state.db"))

	at := time.Date(2026, 10, 13, 9, 0, 0, 789, time.UTC)
	g := domain.LinkedGame{ROMID: 7, PlatformID: 2, Path: "/e/roms/gba/Golden Sun.gba", LinkedAt: at}
	if err := s.SaveLinked(ctx, g); err != nil {
		t.Fatal(err)
	}
	// Linking again replaces the link.
	g.Path = "/e/roms/gba/Golden Sun (USA).gba"
	if err := s.SaveLinked(ctx, g); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := s.Linked(ctx, 7); err != nil || !ok || got != g {
		t.Fatalf("got %+v, %v, %v", got, ok, err)
	}
	if all, err := s.LinkedGames(ctx); err != nil || len(all) != 1 || all[0] != g {
		t.Fatalf("all %+v, %v", all, err)
	}
	g.ROMID = 8
	if err := s.MoveLinked(ctx, 7, g); err != nil {
		t.Fatal(err)
	}
	if all, _ := s.LinkedGames(ctx); len(all) != 1 || all[0] != g {
		t.Fatalf("after move %+v", all)
	}
	if err := s.DeleteLinked(ctx, 8); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Linked(ctx, 8); ok {
		t.Fatal("link kept after delete")
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
	if base := len(saved); len(backups) != 3 || backups[0].Version != base+3 || backups[2].Version != base+1 {
		t.Fatalf("backups %+v", backups)
	}

	// A backup is a working copy of the file as it was before that migration.
	b := open(t, backups[0].Path)
	if _, ok, _ := b.Owned(ctx, 1); !ok {
		t.Fatal("backup lost the record")
	}
}

func TestForgetServerDropsEveryRecord(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "state.db"))
	if err := s.SaveOwned(ctx, domain.OwnedROM{ROMID: 1, Files: []domain.PlacedFile{{Path: "/e/roms/snes/a.sfc"}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveLinked(ctx, domain.LinkedGame{ROMID: 2, Path: "/e/roms/snes/b.sfc"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveQueued(ctx, domain.QueuedDownload{ROMID: 3}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveOwnedBIOS(ctx, domain.OwnedBIOS{FirmwareID: 4, PlatformID: 1, File: domain.PlacedFile{Path: "/e/bios/x.bin"}}); err != nil {
		t.Fatal(err)
	}

	if err := s.ForgetServer(ctx); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := s.db.QueryRow(`SELECT (SELECT COUNT(*) FROM owned_roms) + (SELECT COUNT(*) FROM owned_files) +
		(SELECT COUNT(*) FROM linked_games) + (SELECT COUNT(*) FROM download_queue) + (SELECT COUNT(*) FROM owned_bios)`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("%d rows left, %v", rows, err)
	}
}

func TestUpdateFlagAndSweep(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "state.db"))
	rom := domain.OwnedROM{ROMID: 1, Files: []domain.PlacedFile{{Path: "/e/roms/snes/a.sfc"}}}
	if err := s.SaveOwned(ctx, rom); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUpdateAvailable(ctx, 1, true); err != nil {
		t.Fatal(err)
	}
	if ids, err := s.UpdateIDs(ctx); err != nil || len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("updates %v, %v", ids, err)
	}
	// Saving the record anew, as an update does, clears it.
	if err := s.SaveOwned(ctx, rom); err != nil {
		t.Fatal(err)
	}
	if ids, _ := s.UpdateIDs(ctx); len(ids) != 0 {
		t.Fatalf("updates after save %v", ids)
	}

	if at, err := s.LastSweep(ctx); err != nil || !at.IsZero() {
		t.Fatalf("first sweep %v, %v", at, err)
	}
	at := time.Date(2026, 10, 13, 9, 0, 0, 5, time.UTC)
	if err := s.SetLastSweep(ctx, at); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.LastSweep(ctx); !got.Equal(at) {
		t.Fatalf("last sweep %v", got)
	}
}

func TestIgnoreUpdate(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "state.db"))
	rom := domain.OwnedROM{ROMID: 1, Files: []domain.PlacedFile{{Path: "/e/roms/snes/a.sfc"}},
		Snapshot: []domain.FileSnapshot{{FileID: 10, HashKind: "sha1", Hash: "old"}}}
	if err := s.SaveOwned(ctx, rom); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s.Owned(ctx, 1); got.Ignored != nil {
		t.Fatalf("ignored before anything was %+v", got.Ignored)
	}
	if err := s.SetUpdateAvailable(ctx, 1, true); err != nil {
		t.Fatal(err)
	}
	ignored := []domain.FileSnapshot{{FileID: 11, HashKind: "sha1", Hash: "new"}}
	if err := s.IgnoreUpdate(ctx, 1, ignored); err != nil {
		t.Fatal(err)
	}
	got, _, _ := s.Owned(ctx, 1)
	if len(got.Ignored) != 1 || got.Ignored[0].Hash != "new" || len(got.Snapshot) != 1 || got.Snapshot[0].Hash != "old" {
		t.Fatalf("record %+v", got)
	}
	if ids, _ := s.UpdateIDs(ctx); len(ids) != 0 {
		t.Fatalf("updates %v", ids)
	}
	// An update replaces the record, which forgets what was ignored.
	if err := s.SaveOwned(ctx, rom); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s.Owned(ctx, 1); got.Ignored != nil {
		t.Fatalf("ignored after save %+v", got.Ignored)
	}
}
