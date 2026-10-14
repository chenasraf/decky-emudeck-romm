package state

import (
	"context"
	"database/sql"
	"errors"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// StateFiles returns what state sync last saw of the ROM with id's state files.
func (s *Store) StateFiles(ctx context.Context, id int) ([]domain.StateFile, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT path, hash FROM state_files WHERE rom_id = ? ORDER BY path", id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.StateFile
	for rows.Next() {
		f := domain.StateFile{ROMID: id}
		if err := rows.Scan(&f.Path, &f.Hash); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// PutStateFile records what state sync saw of a state file.
func (s *Store) PutStateFile(ctx context.Context, f domain.StateFile) error {
	_, err := s.db.ExecContext(ctx, "INSERT OR REPLACE INTO state_files (rom_id, path, hash) VALUES (?, ?, ?)", f.ROMID, f.Path, f.Hash)
	return err
}

// UploadedStates returns the save states this Deck put on RomM of the ROM with id, oldest first.
func (s *Store) UploadedStates(ctx context.Context, id int) ([]domain.UploadedState, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT state_id, path, uploaded_at FROM state_uploads WHERE rom_id = ? ORDER BY uploaded_at, state_id", id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.UploadedState
	for rows.Next() {
		u := domain.UploadedState{ROMID: id}
		var at int64
		if err := rows.Scan(&u.StateID, &u.Path, &at); err != nil {
			return nil, err
		}
		u.At = fromUnixNano(at)
		out = append(out, u)
	}
	return out, rows.Err()
}

// AddUploadedState records a save state this Deck put on RomM.
func (s *Store) AddUploadedState(ctx context.Context, u domain.UploadedState) error {
	_, err := s.db.ExecContext(ctx, "INSERT OR REPLACE INTO state_uploads (state_id, rom_id, path, uploaded_at) VALUES (?, ?, ?, ?)",
		u.StateID, u.ROMID, u.Path, unixNano(u.At))
	return err
}

// ForgetUploadedStates drops the records of uploaded states, once RomM no longer has them.
func (s *Store) ForgetUploadedStates(ctx context.Context, ids []int) error {
	for _, id := range ids {
		if _, err := s.db.ExecContext(ctx, "DELETE FROM state_uploads WHERE state_id = ?", id); err != nil {
			return err
		}
	}
	return nil
}

// StateGame returns where the ROM with id's state sync stands; a game never looked at is zero.
func (s *Store) StateGame(ctx context.Context, id int) (domain.StateGame, error) {
	g := domain.StateGame{ROMID: id}
	var checked int64
	err := s.db.QueryRowContext(ctx, "SELECT placed, waiting, problem, checked_at FROM state_games WHERE rom_id = ?", id).
		Scan(&g.Placed, &g.Waiting, &g.Problem, &checked)
	if errors.Is(err, sql.ErrNoRows) {
		return g, nil
	}
	g.CheckedAt = fromUnixNano(checked)
	return g, err
}

// StateGames returns where every game state sync looked at stands.
func (s *Store) StateGames(ctx context.Context) ([]domain.StateGame, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT rom_id, placed, waiting, problem, checked_at FROM state_games ORDER BY rom_id")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.StateGame
	for rows.Next() {
		var g domain.StateGame
		var checked int64
		if err := rows.Scan(&g.ROMID, &g.Placed, &g.Waiting, &g.Problem, &checked); err != nil {
			return nil, err
		}
		g.CheckedAt = fromUnixNano(checked)
		out = append(out, g)
	}
	return out, rows.Err()
}

// PutStateGame records where a game's state sync stands.
func (s *Store) PutStateGame(ctx context.Context, g domain.StateGame) error {
	_, err := s.db.ExecContext(ctx, "INSERT OR REPLACE INTO state_games (rom_id, placed, waiting, problem, checked_at) VALUES (?, ?, ?, ?, ?)",
		g.ROMID, g.Placed, g.Waiting, g.Problem, unixNano(g.CheckedAt))
	return err
}

// StatePrefs returns the ROM with id's own state settings.
func (s *Store) StatePrefs(ctx context.Context, id int) (domain.StatePrefs, error) {
	var p domain.StatePrefs
	var sync, keep sql.NullInt64
	var target, extra sql.NullString
	err := s.db.QueryRowContext(ctx, "SELECT sync, target, extra, keep FROM state_prefs WHERE rom_id = ?", id).
		Scan(&sync, &target, &extra, &keep)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if sync.Valid {
		on := sync.Int64 != 0
		p.Sync = &on
	}
	if target.Valid {
		p.Target = &target.String
	}
	if extra.Valid {
		p.Extra = &extra.String
	}
	if keep.Valid {
		k := int(keep.Int64)
		p.Keep = &k
	}
	return p, nil
}

// SetStatePrefs records the ROM with id's own state settings; empty ones drop its record.
func (s *Store) SetStatePrefs(ctx context.Context, id int, p domain.StatePrefs) error {
	if p.Empty() {
		_, err := s.db.ExecContext(ctx, "DELETE FROM state_prefs WHERE rom_id = ?", id)
		return err
	}
	_, err := s.db.ExecContext(ctx, "INSERT OR REPLACE INTO state_prefs (rom_id, sync, target, extra, keep) VALUES (?, ?, ?, ?, ?)",
		id, p.Sync, p.Target, p.Extra, p.Keep)
	return err
}
