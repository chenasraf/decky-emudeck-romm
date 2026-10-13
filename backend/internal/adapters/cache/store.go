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
	"strings"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// schemaVersion is stored as SQLite's user_version. A file with any other version is rebuilt, so
// changing the schema below means bumping it.
const schemaVersion = 3

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
	created_at  INTEGER NOT NULL,
	updated_at  INTEGER NOT NULL,
	cover_small TEXT NOT NULL,
	cover_large TEXT NOT NULL
);
CREATE INDEX roms_by_platform ON roms (platform_id, name COLLATE NOCASE, id);
CREATE INDEX roms_by_created ON roms (created_at DESC, id DESC);
CREATE TABLE collections (
	kind     TEXT NOT NULL,
	id       INTEGER NOT NULL,
	name     TEXT NOT NULL,
	favorite INTEGER NOT NULL,
	PRIMARY KEY (kind, id)
);
CREATE TABLE collection_roms (
	kind          TEXT NOT NULL,
	collection_id INTEGER NOT NULL,
	rom_id        INTEGER NOT NULL,
	PRIMARY KEY (kind, collection_id, rom_id)
);
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

	if err := replaceCollections(ctx, tx, sync.Collections); err != nil {
		return err
	}

	if sync.Full {
		if _, err := tx.ExecContext(ctx, "DELETE FROM roms"); err != nil {
			return err
		}
	}
	upsert, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO roms
		(id, platform_id, name, file_name, extension, size_bytes, regions, revision, multi_file, created_at, updated_at, cover_small, cover_large)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
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
			string(regions), r.Revision, r.MultiFile, unixOrZero(r.CreatedAt), r.UpdatedAt.Unix(), r.CoverSmall, r.CoverLarge); err != nil {
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

func replaceCollections(ctx context.Context, tx *sql.Tx, collections []domain.Collection) error {
	for _, table := range []string{"collections", "collection_roms"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return err
		}
	}
	for _, c := range collections {
		if _, err := tx.ExecContext(ctx, "INSERT INTO collections (kind, id, name, favorite) VALUES (?, ?, ?, ?)",
			c.Kind, c.ID, c.Name, c.Favorite); err != nil {
			return err
		}
		for _, id := range c.ROMIDs {
			if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO collection_roms (kind, collection_id, rom_id) VALUES (?, ?, ?)",
				c.Kind, c.ID, id); err != nil {
				return err
			}
		}
	}
	return nil
}

// Collections lists the collections the Catalog holds, favorites first, then by name. Each one's
// ROMIDs are those the Catalog holds, by name.
func (s *Store) Collections(ctx context.Context) ([]domain.Collection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT kind, id, name, favorite FROM collections
		ORDER BY favorite DESC, name COLLATE NOCASE, kind, id`)
	if err != nil {
		return nil, err
	}
	var out []domain.Collection
	for rows.Next() {
		var c domain.Collection
		if err := rows.Scan(&c.Kind, &c.ID, &c.Name, &c.Favorite); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, c)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].ROMIDs, err = s.collectionROMIDs(ctx, out[i].CollectionRef); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Collection returns the collection ref names, with the ROMIDs the Catalog holds.
func (s *Store) Collection(ctx context.Context, ref domain.CollectionRef) (domain.Collection, bool, error) {
	c := domain.Collection{CollectionRef: ref}
	err := s.db.QueryRowContext(ctx, "SELECT name, favorite FROM collections WHERE kind = ? AND id = ?", ref.Kind, ref.ID).
		Scan(&c.Name, &c.Favorite)
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	if err != nil {
		return c, false, err
	}
	c.ROMIDs, err = s.collectionROMIDs(ctx, ref)
	return c, err == nil, err
}

func (s *Store) collectionROMIDs(ctx context.Context, ref domain.CollectionRef) ([]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.id FROM collection_roms c JOIN roms r ON r.id = c.rom_id
		WHERE c.kind = ? AND c.collection_id = ? ORDER BY r.name COLLATE NOCASE, r.id`, ref.Kind, ref.ID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := []int{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
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

const romColumns = "id, platform_id, name, file_name, extension, size_bytes, regions, revision, multi_file, created_at, updated_at, cover_small, cover_large"

// ROMs returns a page of a platform's ROMs, or every platform's when platformID is zero, by name,
// narrowed by only when it isn't nil.
func (s *Store) ROMs(ctx context.Context, platformID int, only *domain.IDFilter, offset, limit int) (domain.ROMPage, error) {
	var page domain.ROMPage
	where := "platform_id = ?"
	args := []any{platformID}
	if platformID == 0 {
		where, args = "1", nil
	}
	if only != nil {
		ids, err := json.Marshal(nonNilIDs(only.IDs))
		if err != nil {
			return page, err
		}
		// json_each takes the ids as one parameter, however many there are.
		op := "IN"
		if only.Exclude {
			op = "NOT IN"
		}
		where += " AND id " + op + " (SELECT value FROM json_each(?))"
		args = append(args, string(ids))
	}
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM roms WHERE "+where, args...).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+romColumns+" FROM roms WHERE "+where+
		" ORDER BY name COLLATE NOCASE, id LIMIT ? OFFSET ?", append(args, limit, offset)...)
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

// Recent returns the limit ROMs added to the Server last, newest first. ROMs the Server gave no
// time for come after, highest id first.
func (s *Store) Recent(ctx context.Context, limit int) ([]domain.ROM, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+romColumns+" FROM roms ORDER BY created_at DESC, id DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	return scanROMs(rows)
}

// Search returns up to q.Limit ROMs, from q's platform or every platform, whose name or file name
// contains each word of q's term. Names that start with the term come first, then the rest by
// name.
func (s *Store) Search(ctx context.Context, q domain.SearchQuery) ([]domain.ROM, error) {
	words := strings.Fields(q.Term)
	if len(words) == 0 {
		return nil, nil
	}
	var where []string
	args := []any{}
	if q.PlatformID > 0 {
		where = append(where, "platform_id = ?")
		args = append(args, q.PlatformID)
	}
	for _, w := range words {
		where = append(where, `(name LIKE ? ESCAPE '\' OR file_name LIKE ? ESCAPE '\')`)
		pattern := "%" + likeEscaper.Replace(w) + "%"
		args = append(args, pattern, pattern)
	}
	args = append(args, likeEscaper.Replace(strings.Join(words, " "))+"%", q.Limit)
	rows, err := s.db.QueryContext(ctx, "SELECT "+romColumns+" FROM roms WHERE "+strings.Join(where, " AND ")+
		` ORDER BY name LIKE ? ESCAPE '\' DESC, name COLLATE NOCASE, id LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	return scanROMs(rows)
}

// CountByPlatform returns how many of the ROMs with ids each platform holds.
func (s *Store) CountByPlatform(ctx context.Context, ids []int) (map[int]int, error) {
	data, err := json.Marshal(nonNilIDs(ids))
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT platform_id, COUNT(*) FROM roms
		WHERE id IN (SELECT value FROM json_each(?)) GROUP BY platform_id`, string(data))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[int]int{}
	for rows.Next() {
		var platform, n int
		if err := rows.Scan(&platform, &n); err != nil {
			return nil, err
		}
		out[platform] = n
	}
	return out, rows.Err()
}

// nonNilIDs keeps an empty id list from marshalling to null: json_each reads a JSON null as one
// NULL row, and NOT IN a set holding NULL matches nothing.
func nonNilIDs(ids []int) []int {
	if ids == nil {
		return []int{}
	}
	return ids
}

// likeEscaper escapes LIKE's wildcards, so a search for "100%" matches the text "100%".
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// ROMsByID returns the ROMs with ids, in the order of ids, skipping any the Catalog doesn't hold.
func (s *Store) ROMsByID(ctx context.Context, ids []int) ([]domain.ROM, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	rows, err := s.db.QueryContext(ctx, "SELECT "+romColumns+" FROM roms WHERE id IN ("+placeholders+")", args...)
	if err != nil {
		return nil, err
	}
	found, err := scanROMs(rows)
	if err != nil {
		return nil, err
	}
	byID := make(map[int]domain.ROM, len(found))
	for _, r := range found {
		byID[r.ID] = r
	}
	out := make([]domain.ROM, 0, len(found))
	for _, id := range ids {
		if r, ok := byID[id]; ok {
			out = append(out, r)
		}
	}
	return out, nil
}

func scanROMs(rows *sql.Rows) ([]domain.ROM, error) {
	defer func() { _ = rows.Close() }()
	var out []domain.ROM
	for rows.Next() {
		r, err := scanROM(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
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
	var created, updated int64
	if err := row.Scan(&r.ID, &r.PlatformID, &r.Name, &r.FileName, &r.Extension, &r.SizeBytes, &regions,
		&r.Revision, &r.MultiFile, &created, &updated, &r.CoverSmall, &r.CoverLarge); err != nil {
		return r, err
	}
	if err := json.Unmarshal([]byte(regions), &r.Regions); err != nil {
		return r, fmt.Errorf("rom %d regions: %w", r.ID, err)
	}
	if created != 0 {
		r.CreatedAt = time.Unix(created, 0).UTC()
	}
	r.UpdatedAt = time.Unix(updated, 0).UTC()
	return r, nil
}

// unixOrZero stores an unknown time as 0, which sorts before every real one.
func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}
