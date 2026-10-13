// Package state keeps state.db, the SQLite file of everything the plugin knows that the Server
// can't give back: which files it owns and what the Server had when it placed them. Unlike
// cache.db it is never rebuilt, so schema changes are migrations that only move forward, and the
// file is backed up before each one.
package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// migrations[i] takes the schema from version i to i+1. Released ones never change; a schema
// change is a new entry at the end.
var migrations = []string{
	`
CREATE TABLE owned_roms (
	rom_id       INTEGER PRIMARY KEY,
	platform_id  INTEGER NOT NULL,
	installed_at INTEGER NOT NULL
);
CREATE TABLE owned_files (
	rom_id     INTEGER NOT NULL REFERENCES owned_roms (rom_id) ON DELETE CASCADE,
	path       TEXT NOT NULL UNIQUE,
	size_bytes INTEGER NOT NULL,
	mod_time   INTEGER NOT NULL,
	sha1       TEXT NOT NULL
);
CREATE INDEX owned_files_by_rom ON owned_files (rom_id);
CREATE TABLE server_snapshots (
	rom_id        INTEGER NOT NULL REFERENCES owned_roms (rom_id) ON DELETE CASCADE,
	file_id       INTEGER NOT NULL,
	size_bytes    INTEGER NOT NULL,
	last_modified INTEGER NOT NULL,
	hash_kind     TEXT NOT NULL,
	hash          TEXT NOT NULL,
	PRIMARY KEY (rom_id, file_id)
);
`,
	`
CREATE TABLE download_queue (
	rom_id          INTEGER PRIMARY KEY,
	position        INTEGER NOT NULL,
	platform_id     INTEGER NOT NULL,
	platform_name   TEXT NOT NULL,
	name            TEXT NOT NULL,
	total_bytes     INTEGER NOT NULL,
	failed          INTEGER NOT NULL,
	problem_kind    TEXT NOT NULL,
	problem_message TEXT NOT NULL,
	file_id         INTEGER NOT NULL,
	file_size       INTEGER NOT NULL,
	file_modified   INTEGER NOT NULL
);
`,
	`
CREATE TABLE owned_bios (
	firmware_id  INTEGER PRIMARY KEY,
	platform_id  INTEGER NOT NULL,
	path         TEXT NOT NULL UNIQUE,
	size_bytes   INTEGER NOT NULL,
	mod_time     INTEGER NOT NULL,
	sha1         TEXT NOT NULL,
	installed_at INTEGER NOT NULL
);
CREATE INDEX owned_bios_by_platform ON owned_bios (platform_id);
`,
	`
CREATE TABLE linked_games (
	rom_id      INTEGER PRIMARY KEY,
	platform_id INTEGER NOT NULL,
	path        TEXT NOT NULL,
	linked_at   INTEGER NOT NULL
);
`,
}

// keepBackups is how many state.db.bak-<version> files are kept.
const keepBackups = 3

type Store struct {
	db *sql.DB
}

// Open opens state.db at path and migrates it to the schema this binary knows, backing it up
// first. It refuses, with domain.KindNewerData, a file from a newer version of the plugin.
func Open(ctx context.Context, path string, log *slog.Logger) (*Store, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := migrate(ctx, db, path, log); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// SchemaVersion is the state.db schema this binary writes.
func SchemaVersion() int { return len(migrations) }

func migrate(ctx context.Context, db *sql.DB, path string, log *slog.Logger) error {
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read state.db version: %w", err)
	}
	if version > len(migrations) {
		return &domain.Error{
			Kind:    domain.KindNewerData,
			Message: "Your plugin data is from a newer version of the plugin. Update the plugin to download games.",
			Err:     fmt.Errorf("state.db schema %d, this version knows %d", version, len(migrations)),
		}
	}
	if version == len(migrations) {
		return nil
	}
	if version > 0 {
		if err := backup(ctx, db, path, version); err != nil {
			return fmt.Errorf("back up state.db: %w", err)
		}
	}
	for v := version; v < len(migrations); v++ {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[v]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migrate state.db to %d: %w", v+1, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", v+1)); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	log.Info("migrated state.db", "from", version, "to", len(migrations))
	return nil
}

// backup copies the database to path.bak-<version> and deletes all but the newest keepBackups.
// VACUUM INTO makes a consistent copy even with writes still in the WAL.
func backup(ctx context.Context, db *sql.DB, path string, version int) error {
	dest := fmt.Sprintf("%s.bak-%d", path, version)
	if err := os.Remove(dest); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", dest); err != nil {
		return err
	}
	backups, err := Backups(path)
	if err != nil {
		return err
	}
	for _, old := range backups[min(keepBackups, len(backups)):] {
		if err := os.Remove(old.Path); err != nil {
			return err
		}
	}
	return nil
}

// Backup is a copy of state.db taken before a migration.
type Backup struct {
	Path    string
	Version int
}

// Backups lists the backups of the state.db at path, newest schema first.
func Backups(path string) ([]Backup, error) {
	matches, err := filepath.Glob(path + ".bak-*")
	if err != nil {
		return nil, err
	}
	var out []Backup
	for _, m := range matches {
		v, err := strconv.Atoi(strings.TrimPrefix(m, path+".bak-"))
		if err != nil {
			continue
		}
		out = append(out, Backup{Path: m, Version: v})
	}
	slices.SortFunc(out, func(a, b Backup) int { return b.Version - a.Version })
	return out, nil
}

func (s *Store) Close() error { return s.db.Close() }

// SaveOwned writes the record of a ROM the plugin placed, replacing any earlier one.
func (s *Store) SaveOwned(ctx context.Context, rom domain.OwnedROM) error {
	return s.MoveOwned(ctx, rom.ROMID, rom)
}

// MoveOwned replaces the record of the ROM with id from by rom in one transaction, so its files are
// never without a record.
func (s *Store) MoveOwned(ctx context.Context, from int, rom domain.OwnedROM) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Deleting the old rows cascades to their files and snapshots.
	if _, err := tx.ExecContext(ctx, "DELETE FROM owned_roms WHERE rom_id IN (?, ?)", from, rom.ROMID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO owned_roms (rom_id, platform_id, installed_at) VALUES (?, ?, ?)",
		rom.ROMID, rom.PlatformID, rom.InstalledAt.UnixNano()); err != nil {
		return err
	}
	for _, f := range rom.Files {
		if _, err := tx.ExecContext(ctx, "INSERT INTO owned_files (rom_id, path, size_bytes, mod_time, sha1) VALUES (?, ?, ?, ?, ?)",
			rom.ROMID, f.Path, f.SizeBytes, f.ModTime.UnixNano(), f.SHA1); err != nil {
			return err
		}
	}
	for _, f := range rom.Snapshot {
		if _, err := tx.ExecContext(ctx, `INSERT INTO server_snapshots
			(rom_id, file_id, size_bytes, last_modified, hash_kind, hash) VALUES (?, ?, ?, ?, ?, ?)`,
			rom.ROMID, f.FileID, f.SizeBytes, f.LastModified.UnixNano(), f.HashKind, f.Hash); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SaveLinked records a Linked game, replacing any earlier link of the same ROM.
func (s *Store) SaveLinked(ctx context.Context, g domain.LinkedGame) error {
	_, err := s.db.ExecContext(ctx, "INSERT OR REPLACE INTO linked_games (rom_id, platform_id, path, linked_at) VALUES (?, ?, ?, ?)",
		g.ROMID, g.PlatformID, g.Path, g.LinkedAt.UnixNano())
	return err
}

// MoveLinked replaces the link of the ROM with id from by g in one transaction.
func (s *Store) MoveLinked(ctx context.Context, from int, g domain.LinkedGame) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "DELETE FROM linked_games WHERE rom_id = ?", from); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT OR REPLACE INTO linked_games (rom_id, platform_id, path, linked_at) VALUES (?, ?, ?, ?)",
		g.ROMID, g.PlatformID, g.Path, g.LinkedAt.UnixNano()); err != nil {
		return err
	}
	return tx.Commit()
}

// Linked returns the link of the ROM with id.
func (s *Store) Linked(ctx context.Context, id int) (domain.LinkedGame, bool, error) {
	g := domain.LinkedGame{ROMID: id}
	var at int64
	err := s.db.QueryRowContext(ctx, "SELECT platform_id, path, linked_at FROM linked_games WHERE rom_id = ?", id).
		Scan(&g.PlatformID, &g.Path, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return g, false, nil
	}
	if err != nil {
		return g, false, err
	}
	g.LinkedAt = time.Unix(0, at).UTC()
	return g, true, nil
}

// LinkedGames returns every Linked game.
func (s *Store) LinkedGames(ctx context.Context) ([]domain.LinkedGame, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT rom_id, platform_id, path, linked_at FROM linked_games ORDER BY rom_id")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.LinkedGame
	for rows.Next() {
		var g domain.LinkedGame
		var at int64
		if err := rows.Scan(&g.ROMID, &g.PlatformID, &g.Path, &at); err != nil {
			return nil, err
		}
		g.LinkedAt = time.Unix(0, at).UTC()
		out = append(out, g)
	}
	return out, rows.Err()
}

// DeleteLinked drops the link of the ROM with id. Its file stays as it is.
func (s *Store) DeleteLinked(ctx context.Context, id int) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM linked_games WHERE rom_id = ?", id)
	return err
}

// DeleteOwned drops the record of the ROM with id, making whatever is left of its files
// User-managed.
func (s *Store) DeleteOwned(ctx context.Context, id int) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM owned_roms WHERE rom_id = ?", id)
	return err
}

// Owned returns the record of the ROM with id.
func (s *Store) Owned(ctx context.Context, id int) (domain.OwnedROM, bool, error) {
	rom := domain.OwnedROM{ROMID: id}
	var installed int64
	err := s.db.QueryRowContext(ctx, "SELECT platform_id, installed_at FROM owned_roms WHERE rom_id = ?", id).
		Scan(&rom.PlatformID, &installed)
	if errors.Is(err, sql.ErrNoRows) {
		return rom, false, nil
	}
	if err != nil {
		return rom, false, err
	}
	rom.InstalledAt = time.Unix(0, installed).UTC()

	rows, err := s.db.QueryContext(ctx, "SELECT path, size_bytes, mod_time, sha1 FROM owned_files WHERE rom_id = ? ORDER BY path", id)
	if err != nil {
		return rom, false, err
	}
	for rows.Next() {
		var f domain.PlacedFile
		var mod int64
		if err := rows.Scan(&f.Path, &f.SizeBytes, &mod, &f.SHA1); err != nil {
			_ = rows.Close()
			return rom, false, err
		}
		f.ModTime = time.Unix(0, mod).UTC()
		rom.Files = append(rom.Files, f)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return rom, false, err
	}

	rows, err = s.db.QueryContext(ctx, `SELECT file_id, size_bytes, last_modified, hash_kind, hash
		FROM server_snapshots WHERE rom_id = ? ORDER BY file_id`, id)
	if err != nil {
		return rom, false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var f domain.FileSnapshot
		var mod int64
		if err := rows.Scan(&f.FileID, &f.SizeBytes, &mod, &f.HashKind, &f.Hash); err != nil {
			return rom, false, err
		}
		f.LastModified = time.Unix(0, mod).UTC()
		rom.Snapshot = append(rom.Snapshot, f)
	}
	return rom, true, rows.Err()
}

// SaveQueued writes a download in the queue. A new one goes to the back; one already there keeps
// its place.
func (s *Store) SaveQueued(ctx context.Context, d domain.QueuedDownload) error {
	failed, kind, message := 0, "", ""
	if d.Problem != nil {
		failed, kind, message = 1, string(d.Problem.Kind), d.Problem.Message
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO download_queue
		(rom_id, position, platform_id, platform_name, name, total_bytes, failed, problem_kind, problem_message,
		 file_id, file_size, file_modified)
		VALUES (?, (SELECT COALESCE(MAX(position), 0) + 1 FROM download_queue), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (rom_id) DO UPDATE SET
			platform_id = excluded.platform_id, platform_name = excluded.platform_name, name = excluded.name,
			total_bytes = excluded.total_bytes, failed = excluded.failed, problem_kind = excluded.problem_kind,
			problem_message = excluded.problem_message, file_id = excluded.file_id, file_size = excluded.file_size,
			file_modified = excluded.file_modified`,
		d.ROMID, d.PlatformID, d.PlatformName, d.Name, d.TotalBytes, failed, kind, message,
		d.Partial.FileID, d.Partial.SizeBytes, d.Partial.LastModified.UnixNano())
	return err
}

// DeleteQueued takes the download of the ROM with id out of the queue.
func (s *Store) DeleteQueued(ctx context.Context, id int) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM download_queue WHERE rom_id = ?", id)
	return err
}

// Queued returns the downloads in the queue, in queue order.
func (s *Store) Queued(ctx context.Context) ([]domain.QueuedDownload, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT rom_id, platform_id, platform_name, name, total_bytes, failed,
		problem_kind, problem_message, file_id, file_size, file_modified FROM download_queue ORDER BY position`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.QueuedDownload
	for rows.Next() {
		var d domain.QueuedDownload
		var failed bool
		var kind, message string
		var modified int64
		if err := rows.Scan(&d.ROMID, &d.PlatformID, &d.PlatformName, &d.Name, &d.TotalBytes, &failed,
			&kind, &message, &d.Partial.FileID, &d.Partial.SizeBytes, &modified); err != nil {
			return nil, err
		}
		if failed {
			d.Problem = &domain.Error{Kind: domain.Kind(kind), Message: message}
		}
		if d.Partial.FileID != 0 {
			d.Partial.LastModified = time.Unix(0, modified).UTC()
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SaveOwnedBIOS writes the record of a BIOS file the plugin placed, replacing any earlier one for
// the same firmware or path.
func (s *Store) SaveOwnedBIOS(ctx context.Context, b domain.OwnedBIOS) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO owned_bios
		(firmware_id, platform_id, path, size_bytes, mod_time, sha1, installed_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		b.FirmwareID, b.PlatformID, b.File.Path, b.File.SizeBytes, b.File.ModTime.UnixNano(), b.File.SHA1, b.InstalledAt.UnixNano())
	return err
}

// DeleteOwnedBIOS drops the record of the BIOS file of the firmware with id, making the file
// User-managed.
func (s *Store) DeleteOwnedBIOS(ctx context.Context, id int) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM owned_bios WHERE firmware_id = ?", id)
	return err
}

// OwnedBIOS returns the records of a platform's Plugin-owned BIOS files.
func (s *Store) OwnedBIOS(ctx context.Context, platformID int) ([]domain.OwnedBIOS, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT firmware_id, path, size_bytes, mod_time, sha1, installed_at
		FROM owned_bios WHERE platform_id = ? ORDER BY path`, platformID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.OwnedBIOS
	for rows.Next() {
		b := domain.OwnedBIOS{PlatformID: platformID}
		var mod, installed int64
		if err := rows.Scan(&b.FirmwareID, &b.File.Path, &b.File.SizeBytes, &mod, &b.File.SHA1, &installed); err != nil {
			return nil, err
		}
		b.File.ModTime = time.Unix(0, mod).UTC()
		b.InstalledAt = time.Unix(0, installed).UTC()
		out = append(out, b)
	}
	return out, rows.Err()
}

// ForgetServer drops every record tied to the Server: Plugin-owned ROMs and BIOS files, Linked
// games and the download queue. The files stay on disk, User-managed.
func (s *Store) ForgetServer(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, table := range []string{"owned_roms", "owned_bios", "linked_games", "download_queue"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// OwnedIDs returns the id of every Plugin-owned ROM.
func (s *Store) OwnedIDs(ctx context.Context) ([]int, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT rom_id FROM owned_roms ORDER BY rom_id")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
