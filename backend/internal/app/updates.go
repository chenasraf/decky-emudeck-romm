package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

const (
	// sweepEvery is how often every Plugin-owned game is checked for updates, on top of the games a
	// library refresh finds changed.
	sweepEvery = 24 * time.Hour
	// sweepRetry is how long a sweep that couldn't finish waits to try again.
	sweepRetry = time.Hour
)

// CheckUpdates compares the Server's game files of the Plugin-owned games among ids with what the
// plugin downloaded, and records which have an update. Games downloading or being deleted are
// skipped, and so are the ones the Server no longer has: those are Removed from RomM instead.
func (d *Downloads) CheckUpdates(ctx context.Context, ids []int) error {
	if d.store == nil {
		return nil
	}
	in, err := d.conn.activeSignIn()
	if err != nil || in == nil {
		return err
	}
	owned, err := d.store.OwnedIDs(ctx)
	if err != nil {
		return err
	}
	flagged, err := d.store.UpdateIDs(ctx)
	if err != nil {
		return err
	}
	server := d.dial(in.Server, in.Token)
	for _, id := range ids {
		if !slices.Contains(owned, id) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
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
		files, err := server.ROMFiles(ctx, id)
		if kindOf(err) == domain.KindNotFound {
			continue
		}
		if err != nil {
			return d.conn.observe(*in, err)
		}
		available := domain.UpdateAvailable(rec.Snapshot, files)
		if available == slices.Contains(flagged, id) {
			continue
		}
		if err := d.store.SetUpdateAvailable(ctx, id, available); err != nil {
			return fmt.Errorf("record update: %w", err)
		}
		state := domain.StateInstalled
		if available {
			state = domain.StateUpdate
			d.log.Info("update available", "rom_id", id)
		}
		d.announceOwnership(OwnershipChange{ROMID: id, Name: d.gameName(ctx, id), State: state, Kind: OwnershipUpdate})
	}
	return nil
}

// recorded says whether a queued download already landed: the backend stopped between recording
// it and taking it out of the queue. An update has landed once its record no longer has an update.
func (d *Downloads) recorded(ctx context.Context, q domain.QueuedDownload) bool {
	if _, owned, err := d.store.Owned(ctx, q.ROMID); err != nil || !owned {
		return false
	}
	if !q.Update {
		return true
	}
	updates, err := d.store.UpdateIDs(ctx)
	return err == nil && !slices.Contains(updates, q.ROMID)
}

// checkTarget fails when something other than the files an update replaces is at path.
func checkTarget(path string, old []domain.PlacedFile) error {
	if replaces(path, old) {
		return nil
	}
	return checkFree(path)
}

// replaces says whether path is one of old, or a folder holding one of them.
func replaces(path string, old []domain.PlacedFile) bool {
	return slices.ContainsFunc(old, func(f domain.PlacedFile) bool {
		return f.Path == path || strings.HasPrefix(f.Path, path+string(filepath.Separator))
	})
}

// newSuffix marks an update's file beside the one it replaces, until the swap.
const newSuffix = ".romm-new"

// swap moves an update's staged files into dir in place of old. Each lands beside its target first,
// copied there when staging is on another drive, so the swap itself is one rename per file and the
// game is never half-written. Old files the update no longer has go only after. Nothing but old is
// ever replaced; when anything else is in the way, or a file can't be moved beside its target, the
// old version stays as it was.
func (d *Downloads) swap(staged []stagedFile, dir string, old []domain.PlacedFile) ([]domain.PlacedFile, error) {
	for _, f := range staged {
		if err := checkTarget(filepath.Join(dir, f.Rel), old); err != nil {
			return nil, err
		}
	}
	var ready []string
	unready := func() {
		for _, p := range ready {
			_ = os.Remove(p)
		}
	}
	for _, f := range staged {
		target := filepath.Join(dir, f.Rel)
		tmp := target + newSuffix
		err := os.MkdirAll(filepath.Dir(target), 0o755)
		if err == nil {
			// One left by an update that stopped partway.
			err = os.Remove(tmp)
			if errors.Is(err, os.ErrNotExist) {
				err = nil
			}
		}
		if err == nil {
			err = moveFile(f.From, tmp)
		}
		if err != nil {
			unready()
			return nil, fmt.Errorf("move into %s: %w", dir, err)
		}
		ready = append(ready, tmp)
	}

	placed := make([]domain.PlacedFile, 0, len(staged))
	kept := map[string]bool{}
	for _, f := range staged {
		target := filepath.Join(dir, f.Rel)
		if err := os.Rename(target+newSuffix, target); err != nil {
			// The files swapped so far are the update's; the record still lists the old ones, so the
			// next library refresh finds the game changed and leaves it to the user.
			return nil, fmt.Errorf("swap in %s: %w", target, err)
		}
		info, err := os.Stat(target)
		if err != nil {
			return nil, err
		}
		kept[target] = true
		placed = append(placed, domain.PlacedFile{Path: target, SizeBytes: info.Size(), ModTime: info.ModTime().UTC(), SHA1: f.SHA1})
	}
	gone := slices.DeleteFunc(slices.Clone(old), func(f domain.PlacedFile) bool { return kept[f.Path] })
	d.removeFiles(gone, dir)
	return placed, nil
}

// SweepUpdates checks every Plugin-owned game for updates once a day, starting right away when
// the last sweep finished a day ago or more. It waits while Signed out, and returns when ctx ends.
func (d *Downloads) SweepUpdates(ctx context.Context) {
	if d.store == nil {
		return
	}
	for {
		wait, err := d.untilSweep(ctx)
		if err != nil {
			d.log.Warn("read last update sweep", "err", err)
			wait = sweepRetry
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if err := d.conn.WaitSignedIn(ctx); err != nil {
			return
		}
		if err := d.sweep(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			d.log.Warn("sweep for updates", "err", err, "retry_in", sweepRetry)
			select {
			case <-ctx.Done():
				return
			case <-time.After(sweepRetry):
			}
		}
	}
}

// untilSweep is how long until the next sweep is due, zero when it's due now.
func (d *Downloads) untilSweep(ctx context.Context) (time.Duration, error) {
	last, err := d.store.LastSweep(ctx)
	if err != nil {
		return 0, err
	}
	return max(last.Add(sweepEvery).Sub(d.now()), 0), nil
}

// sweep checks every Plugin-owned game for updates and records when it finished.
func (d *Downloads) sweep(ctx context.Context) error {
	ids, err := d.store.OwnedIDs(ctx)
	if err != nil {
		return err
	}
	if err := d.CheckUpdates(ctx, ids); err != nil {
		return err
	}
	d.log.Info("swept for updates", "games", len(ids))
	return d.store.SetLastSweep(ctx, d.now())
}
