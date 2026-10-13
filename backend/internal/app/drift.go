package app

import (
	"context"
	"errors"
	"os"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// CheckOwned compares every Plugin-owned game's files with what the plugin placed, by size and
// modification time; hashing every large file on each refresh would take minutes. A game whose files
// are all gone stops being recorded, so it's On RomM again. One with a file changed, replaced or
// gone while others remain becomes User-managed: what's left is the user's now. Files moved or
// renamed aren't followed. Games downloading or being deleted are skipped, their records in flux.
func (d *Downloads) CheckOwned(ctx context.Context) error {
	if d.store == nil {
		return nil
	}
	ids, err := d.store.OwnedIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		d.mu.Lock()
		busy := d.find(id) != nil || d.deleting[id]
		d.mu.Unlock()
		if busy {
			continue
		}
		rec, ok, err := d.store.Owned(ctx, id)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		kind, err := drift(rec)
		if err != nil {
			d.log.Warn("check owned files", "rom_id", id, "err", err)
			continue
		}
		if kind == "" {
			continue
		}
		if err := d.store.DeleteOwned(ctx, id); err != nil {
			return err
		}
		change := OwnershipChange{ROMID: id, Name: d.gameName(ctx, id), State: domain.StateOnRomM, Kind: kind}
		if kind == OwnershipChanged {
			d.log.Info("owned game changed on disk, now user-managed", "rom_id", id)
			if states, err := d.States(ctx); err == nil {
				change.State = domain.StateOf(states, id)
			}
		} else {
			d.log.Info("owned game gone from disk", "rom_id", id)
		}
		d.announceOwnership(change)
	}
	return nil
}

// drift says how a Plugin-owned game's files differ from what the plugin placed: OwnershipGone when
// none is left, OwnershipChanged when any other differs, and empty when none does.
func drift(rec domain.OwnedROM) (OwnershipKind, error) {
	gone, changed := 0, false
	for _, f := range rec.Files {
		info, err := os.Lstat(f.Path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			gone++
		case err != nil:
			return "", err
		case !info.Mode().IsRegular() || info.Size() != f.SizeBytes || !info.ModTime().Equal(f.ModTime):
			changed = true
		}
	}
	switch {
	case gone == len(rec.Files):
		return OwnershipGone, nil
	case changed || gone > 0:
		return OwnershipChanged, nil
	}
	return "", nil
}
