package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// carryOver syncs the save of the game with id before an update replaces its files, and returns what
// moves the save to the name the update's files give it, run once they're in place and recorded. It
// returns nil when the game's save doesn't sync.
func (s *Saves) carryOver(ctx context.Context, id int) func(context.Context) {
	if s.store == nil {
		return nil
	}
	if _, err := s.ready(); err == nil {
		if _, err := s.Sync(ctx, []int{id}); err != nil {
			s.log.Warn("sync save before update", "rom_id", id, "err", err)
		}
	}
	old, why, err := s.target(ctx, id)
	if err != nil || why != "" {
		return nil
	}
	return func(ctx context.Context) {
		moved, err := s.carry(ctx, old)
		if err != nil {
			s.log.Warn("carry save over update", "rom_id", id, "err", err)
			s.logActivity(ctx, id, domain.ActivityFailed, "The update renamed the game, and its save couldn't follow: "+err.Error()+". The save is still at its old name.")
			return
		}
		if moved {
			s.request([]int{id}, false)
		}
	}
}

// carry moves old, a game's save as it was before an update, to where the game's save is now, when
// the update changed that. The save it replaces stays in the game's backups. A save already at the
// new name is the game's own, and nothing moves.
func (s *Saves) carry(ctx context.Context, old saveTarget) (bool, error) {
	s.passMu.Lock()
	defer s.passMu.Unlock()
	t, why, err := s.target(ctx, old.romID)
	if err != nil || why != "" || t.path == old.path {
		return false, err
	}
	if t.rule.Emulator.ID != old.rule.Emulator.ID {
		// Another emulator keeps saves of its own, which this one's can't stand in for.
		return false, nil
	}
	local, err := old.format.read()
	if err != nil || !local.exists {
		return false, err
	}
	if now, err := t.format.read(); err != nil {
		return false, err
	} else if now.exists {
		s.logActivity(ctx, old.romID, domain.ActivityMoved, fmt.Sprintf("The update renamed the game, and it already has a save at its new name, %s. The save at its old name, %s, stays where it is.",
			filepath.Base(t.path), filepath.Base(old.path)))
		return false, nil
	}
	if err := s.backupSave(old.romID, old.path, local.data); err != nil {
		return false, fmt.Errorf("back up the save: %w", err)
	}
	s.mu.Lock()
	for _, f := range t.format.files() {
		s.wrote[f] = s.now()
	}
	s.mu.Unlock()
	if err := t.format.write(local.data); err != nil {
		return false, fmt.Errorf("write the save: %w", err)
	}
	for _, f := range old.format.files() {
		if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.log.Warn("remove save the update renamed", "path", f, "err", err)
		}
	}
	s.log.Info("save carried over update", "rom_id", old.romID, "from", old.path, "to", t.path)
	s.logActivity(ctx, old.romID, domain.ActivityMoved, fmt.Sprintf("The update renamed the game, so its save moved from %s to %s. A copy is in this game's backups.",
		filepath.Base(old.path), filepath.Base(t.path)))
	return true, nil
}
