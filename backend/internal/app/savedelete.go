package app

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// SaveDeletion is whether a game's save can go when the game is deleted.
type SaveDeletion struct {
	// Has is set when the game has a save on this Deck the plugin syncs.
	Has bool
	// Why says why the save can't go, empty when it can: only a save RomM holds as it is here may.
	Why string
}

// Deletion says whether the save of the game with id can be deleted with it.
func (s *Saves) Deletion(ctx context.Context, id int) (SaveDeletion, error) {
	d, _, err := s.deletion(ctx, id)
	return d, err
}

func (s *Saves) deletion(ctx context.Context, id int) (SaveDeletion, saveTarget, error) {
	if s.store == nil {
		return SaveDeletion{}, saveTarget{}, nil
	}
	t, why, err := s.target(ctx, id)
	if err != nil || why != "" {
		// A save the plugin doesn't sync isn't the plugin's to delete.
		return SaveDeletion{}, saveTarget{}, err
	}
	local, err := t.format.read()
	if err != nil {
		return SaveDeletion{}, t, err
	}
	if !local.exists {
		return SaveDeletion{}, t, nil
	}
	out := SaveDeletion{Has: true}
	records, err := s.records(ctx)
	if err != nil {
		return out, t, err
	}
	r, ok := records[id]
	switch {
	case s.runs(t.rule.Emulator.ID):
		out.Why = fmt.Sprintf("Close %s first: it writes the save it holds when the game closes.", t.rule.Emulator.Name)
	case !ok || r.Path != t.path || r.Slot != t.slot || r.State != domain.SaveSynced || r.ServerHash == "" || r.LocalHash != local.hash:
		out.Why = "This Deck's save isn't on RomM as it is here. Sync first."
	default:
		other, err := s.sharer(ctx, t)
		if err != nil {
			return out, t, err
		}
		if other != "" {
			out.Why = fmt.Sprintf("%s keeps its save in the same file, so it stays.", other)
		}
	}
	return out, t, nil
}

// sharer is the name of another game whose save is t's file, as the discs of a game DuckStation
// gives one memory card are.
func (s *Saves) sharer(ctx context.Context, t saveTarget) (string, error) {
	ids, err := s.gameIDs(ctx)
	if err != nil {
		return "", err
	}
	for _, id := range ids {
		if id == t.romID {
			continue
		}
		o, why, err := s.resolve(ctx, id, false)
		if err != nil {
			return "", err
		}
		if why == "" && o.path == t.path {
			return o.name, nil
		}
	}
	return "", nil
}

// setAside checks that the save of the game with id can go with it, and returns what moves the save
// into the game's backups, nil when the game has no save to move.
func (s *Saves) setAside(ctx context.Context, id int) (func() error, error) {
	d, t, err := s.deletion(ctx, id)
	if err != nil {
		return nil, err
	}
	if d.Why != "" {
		return nil, &domain.Error{Kind: domain.KindInvalidInput, Message: d.Why}
	}
	if !d.Has {
		return nil, nil
	}
	return func() error {
		s.passMu.Lock()
		defer s.passMu.Unlock()
		local, err := t.format.read()
		if err != nil || !local.exists {
			return err
		}
		if err := s.backupSave(id, t.path, local.data); err != nil {
			return fmt.Errorf("back up the save: %w", err)
		}
		for _, f := range t.format.files() {
			if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		s.log.Info("save deleted with its game", "rom_id", id, "path", t.path)
		s.logActivity(ctx, id, domain.ActivityDeleted, "Deleted with the game. A copy is in this game's backups.")
		return nil
	}, nil
}
