// Package cache keeps the Catalog in cache.db, a SQLite file that holds nothing the Server can't
// give back: when it is from another schema or damaged, it is deleted and rebuilt.
package cache

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// schemaVersion is stored as SQLite's user_version. A file with any other version is rebuilt, so
// changing the schema below means bumping it.
const schemaVersion = 1

const schema = `
CREATE TABLE platforms (
	id        INTEGER PRIMARY KEY,
	slug      TEXT NOT NULL,
	name      TEXT NOT NULL,
	rom_count INTEGER NOT NULL
);
CREATE TABLE roms (
	id          INTEGER PRIMARY KEY,
	platform_id INTEGER NOT NULL,
	name        TEXT NOT NULL,
	file_name   TEXT NOT NULL,
	extension   TEXT NOT NULL,
	size_bytes  INTEGER NOT NULL,
	regions     TEXT NOT NULL,
	revision    TEXT NOT NULL,
	multi_file  INTEGER NOT NULL,
	updated_at  INTEGER NOT NULL,
	cover_small TEXT NOT NULL,
	cover_large TEXT NOT NULL
);
CREATE INDEX roms_by_platform ON roms (platform_id, name COLLATE NOCASE, id);
CREATE TABLE meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`

// Keys in the meta table.
const (
	// metaCursor is the newest updated_at among the ROMs the last sync received, which the next
	// one asks the Server for changes since.
	metaCursor = "cursor"
	// metaSyncedAt is when the last sync finished.
	metaSyncedAt = "synced_at"
)

type Store struct {
	db *sql.DB
}

// Open opens the Catalog at path, rebuilding it empty when it is from another schema version or
// damaged.
func Open(ctx context.Context, path string, log *slog.Logger) (*Store, error) {
	s, err := open(ctx, path)
	if err == nil {
		return s, nil
	}
	log.Warn("rebuilding cache.db", "path", path, "reason", err)
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("remove %s: %w", p, err)
		}
	}
	return open(ctx, path)
}

func open(ctx context.Context, path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.prepare(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// prepare creates the schema in a new file, and checks an existing one is this schema and intact.
func (s *Store) prepare(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	switch version {
	case 0:
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.ExecContext(ctx, schema); err != nil {
			return fmt.Errorf("create schema: %w", err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
			return err
		}
		return tx.Commit()
	case schemaVersion:
		var result string
		if err := s.db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result); err != nil {
			return err
		}
		if result != "ok" {
			return fmt.Errorf("quick_check: %s", result)
		}
		return nil
	default:
		return fmt.Errorf("schema version %d, want %d", version, schemaVersion)
	}
}

func (s *Store) Close() error { return s.db.Close() }

// SyncState returns the cursor and finish time of the last sync, both zero before the first.
func (s *Store) SyncState(ctx context.Context) (cursor, syncedAt time.Time, err error) {
	rows, err := s.db.QueryContext(ctx, "SELECT key, value FROM meta WHERE key IN (?, ?)", metaCursor, metaSyncedAt)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return time.Time{}, time.Time{}, err
		}
		t, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			continue
		}
		switch key {
		case metaCursor:
			cursor = t
		case metaSyncedAt:
			syncedAt = t
		}
	}
	return cursor, syncedAt, rows.Err()
}

// Apply writes a sync in one transaction, so a reader never sees half of one.
func (s *Store) Apply(ctx context.Context, sync domain.CatalogSync) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "DELETE FROM platforms"); err != nil {
		return err
	}
	for _, p := range sync.Platforms {
		if _, err := tx.ExecContext(ctx, "INSERT INTO platforms (id, slug, name, rom_count) VALUES (?, ?, ?, ?)",
			p.ID, p.Slug, p.Name, p.ROMCount); err != nil {
			return err
		}
	}

	if sync.Full {
		if _, err := tx.ExecContext(ctx, "DELETE FROM roms"); err != nil {
			return err
		}
	}
	upsert, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO roms
		(id, platform_id, name, file_name, extension, size_bytes, regions, revision, multi_file, updated_at, cover_small, cover_large)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = upsert.Close() }()
	for _, r := range sync.ROMs {
		regions, err := json.Marshal(r.Regions)
		if err != nil {
			return err
		}
		if _, err := upsert.ExecContext(ctx, r.ID, r.PlatformID, r.Name, r.FileName, r.Extension, r.SizeBytes,
			string(regions), r.Revision, r.MultiFile, r.UpdatedAt.Unix(), r.CoverSmall, r.CoverLarge); err != nil {
			return err
		}
	}

	if sync.KeepIDs != nil {
		if err := deleteMissing(ctx, tx, sync.KeepIDs); err != nil {
			return err
		}
	}

	for key, t := range map[string]time.Time{metaCursor: sync.Cursor, metaSyncedAt: sync.At} {
		if t.IsZero() {
			continue
		}
		if _, err := tx.ExecContext(ctx, "INSERT OR REPLACE INTO meta (key, value) VALUES (?, ?)", key, t.UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// deleteMissing deletes every ROM whose id isn't in keep, through a temporary table so tens of
// thousands of ids don't hit SQLite's variable limit.
func deleteMissing(ctx context.Context, tx *sql.Tx, keep []int) error {
	if _, err := tx.ExecContext(ctx, "CREATE TEMP TABLE IF NOT EXISTS keep_ids (id INTEGER PRIMARY KEY)"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM keep_ids"); err != nil {
		return err
	}
	insert, err := tx.PrepareContext(ctx, "INSERT OR IGNORE INTO keep_ids (id) VALUES (?)")
	if err != nil {
		return err
	}
	defer func() { _ = insert.Close() }()
	for _, id := range keep {
		if _, err := insert.ExecContext(ctx, id); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, "DELETE FROM roms WHERE id NOT IN (SELECT id FROM keep_ids)")
	return err
}

// Platforms lists the platforms the Catalog holds games for, by name.
func (s *Store) Platforms(ctx context.Context) ([]domain.PlatformSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.slug, p.name, p.rom_count, COUNT(r.id)
		FROM platforms p JOIN roms r ON r.platform_id = p.id
		GROUP BY p.id
		ORDER BY p.name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.PlatformSummary
	for rows.Next() {
		var p domain.PlatformSummary
		if err := rows.Scan(&p.ID, &p.Slug, &p.Name, &p.ROMCount, &p.Games); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Platform returns the platform with id.
func (s *Store) Platform(ctx context.Context, id int) (domain.Platform, bool, error) {
	var p domain.Platform
	err := s.db.QueryRowContext(ctx, "SELECT id, slug, name, rom_count FROM platforms WHERE id = ?", id).
		Scan(&p.ID, &p.Slug, &p.Name, &p.ROMCount)
	if errors.Is(err, sql.ErrNoRows) {
		return p, false, nil
	}
	return p, err == nil, err
}

// Counts returns how many platforms and ROMs the Catalog holds.
func (s *Store) Counts(ctx context.Context) (platforms, roms int, err error) {
	err = s.db.QueryRowContext(ctx,
		"SELECT (SELECT COUNT(DISTINCT platform_id) FROM roms), (SELECT COUNT(*) FROM roms)").Scan(&platforms, &roms)
	return platforms, roms, err
}

const romColumns = "id, platform_id, name, file_name, extension, size_bytes, regions, revision, multi_file, updated_at, cover_small, cover_large"

// ROMs returns a page of a platform's ROMs, by name.
func (s *Store) ROMs(ctx context.Context, platformID, offset, limit int) (domain.ROMPage, error) {
	var page domain.ROMPage
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM roms WHERE platform_id = ?", platformID).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+romColumns+` FROM roms WHERE platform_id = ?
		ORDER BY name COLLATE NOCASE, id LIMIT ? OFFSET ?`, platformID, limit, offset)
	if err != nil {
		return page, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		r, err := scanROM(rows)
		if err != nil {
			return page, err
		}
		page.ROMs = append(page.ROMs, r)
	}
	return page, rows.Err()
}

// ROM returns the ROM with id.
func (s *Store) ROM(ctx context.Context, id int) (domain.ROM, bool, error) {
	r, err := scanROM(s.db.QueryRowContext(ctx, "SELECT "+romColumns+" FROM roms WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	return r, err == nil, err
}

type scanner interface{ Scan(dest ...any) error }

func scanROM(row scanner) (domain.ROM, error) {
	var r domain.ROM
	var regions string
	var updated int64
	if err := row.Scan(&r.ID, &r.PlatformID, &r.Name, &r.FileName, &r.Extension, &r.SizeBytes, &regions,
		&r.Revision, &r.MultiFile, &updated, &r.CoverSmall, &r.CoverLarge); err != nil {
		return r, err
	}
	if err := json.Unmarshal([]byte(regions), &r.Regions); err != nil {
		return r, fmt.Errorf("rom %d regions: %w", r.ID, err)
	}
	r.UpdatedAt = time.Unix(updated, 0).UTC()
	return r, nil
}
