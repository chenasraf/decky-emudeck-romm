package app

import (
	"context"
	"fmt"
	"slices"
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
