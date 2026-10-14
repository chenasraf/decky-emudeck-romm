package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// logActivity adds an entry to the activity log. The log is for the user to see what happened; a
// write that fails costs only the entry.
func (s *Saves) logActivity(ctx context.Context, id int, kind domain.ActivityKind, message string) {
	a := domain.SaveActivity{ROMID: id, At: s.now().UTC(), Kind: kind, Message: message}
	if err := s.store.AddActivity(context.WithoutCancel(ctx), a); err != nil {
		s.log.Warn("save activity", "rom_id", id, "err", err)
	}
}

// settled logs a conflict ending, and marks the game's conflict entries settled so pruning may drop
// them.
func (s *Saves) settled(ctx context.Context, id int, message string) {
	if err := s.store.ResolveConflicts(context.WithoutCancel(ctx), id); err != nil {
		s.log.Warn("settle conflict entries", "rom_id", id, "err", err)
	}
	s.logActivity(ctx, id, domain.ActivityResolved, message)
}

// pruneActivity trims the activity log to what it keeps.
func (s *Saves) pruneActivity(ctx context.Context) {
	if s.store == nil {
		return
	}
	n, err := s.store.PruneActivity(ctx, s.now().Add(-domain.ActivityKeepFor), domain.ActivityKeepMax)
	if err != nil {
		s.log.Warn("prune save activity", "err", err)
		return
	}
	if n > 0 {
		s.log.Info("pruned save activity", "entries", n)
	}
}

// ActivityEntry is an activity log entry with the name of its game.
type ActivityEntry struct {
	domain.SaveActivity
	Name string
}

// Activity returns the newest limit entries of the activity log, of the game with id or of every
// game when id is zero, newest first.
func (s *Saves) Activity(ctx context.Context, id, limit int) ([]ActivityEntry, error) {
	if s.store == nil {
		return nil, nil
	}
	list, err := s.store.Activity(ctx, id, limit)
	if err != nil {
		return nil, err
	}
	out := make([]ActivityEntry, 0, len(list))
	names := map[int]string{}
	for _, a := range list {
		name, ok := names[a.ROMID]
		if !ok {
			name = s.gameName(ctx, a.ROMID)
			names[a.ROMID] = name
		}
		out = append(out, ActivityEntry{SaveActivity: a, Name: name})
	}
	return out, nil
}

func (s *Saves) gameName(ctx context.Context, id int) string {
	if rom, ok, err := s.catalog.ROM(ctx, id); err == nil && ok {
		return rom.Name
	}
	return "A game no longer on RomM"
}

// Backups lists the backups of a game's save, newest first.
func (s *Saves) Backups(id int) ([]domain.SaveBackup, error) {
	entries, err := os.ReadDir(s.backupDir(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []domain.SaveBackup
	for _, e := range entries {
		b, ok := backupOf(e)
		if ok {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

// backupAt is when the backup named name was taken.
func backupAt(name string) (time.Time, bool) {
	at, err := time.Parse(backupTime, strings.TrimSuffix(name, filepath.Ext(name)))
	return at.UTC(), err == nil
}

func backupOf(e os.DirEntry) (domain.SaveBackup, bool) {
	at, ok := backupAt(e.Name())
	if e.IsDir() || !ok {
		return domain.SaveBackup{}, false
	}
	b := domain.SaveBackup{Name: e.Name(), At: at}
	if info, err := e.Info(); err == nil {
		b.SizeBytes = info.Size()
	}
	return b, true
}

// RestoreBackup puts a backup of a game's save back in place of the save, backing up the save it
// replaces first, then syncs the game: the restored save uploads like any other change.
func (s *Saves) RestoreBackup(ctx context.Context, id int, name string) error {
	if s.store == nil {
		return s.problem
	}
	if filepath.Base(name) != name || name == "." || name == ".." {
		return &domain.Error{Kind: domain.KindInvalidInput, Message: "That isn't one of this game's backups."}
	}
	t, why, err := s.target(ctx, id)
	if err != nil {
		return err
	}
	if why != "" {
		return &domain.Error{Kind: domain.KindInvalidInput, Message: why}
	}
	if s.runs(t.rule.Emulator.ID) {
		return closeFirst(t.rule.Emulator)
	}
	s.passMu.Lock()
	err = s.restore(ctx, t, name)
	s.passMu.Unlock()
	if err != nil {
		return err
	}
	s.request([]int{id}, true)
	return nil
}

func (s *Saves) restore(ctx context.Context, t saveTarget, name string) error {
	path := filepath.Join(s.backupDir(t.romID), name)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &domain.Error{Kind: domain.KindNotFound, Message: "That backup is gone."}
	}
	if err != nil {
		return err
	}
	local, err := t.format.read()
	if err != nil {
		return err
	}
	if local.exists {
		if local.hash == domain.ContentHash(data) {
			return &domain.Error{Kind: domain.KindInvalidInput, Message: "The save is already that backup."}
		}
		if err := s.backupSave(t.romID, t.path, local.data); err != nil {
			return err
		}
	}
	if err := t.format.write(data); err != nil {
		return err
	}
	s.log.Info("save backup restored", "rom_id", t.romID, "backup", name)
	msg := "Restored a backup of the save."
	if at, ok := backupAt(name); ok {
		msg = "Restored the backup from " + at.Local().Format("Jan 2, 15:04") + "."
	}
	if local.exists {
		msg += " The save it replaced is in this game's backups."
	}
	s.logActivity(ctx, t.romID, domain.ActivityRestored, msg)
	return nil
}
