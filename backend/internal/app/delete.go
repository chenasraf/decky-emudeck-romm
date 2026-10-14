package app

import (
	"context"
	"crypto/sha1" //nolint:gosec // compares against the SHA-1 recorded at download, not for security
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// DeleteUpdate is a change to a game being deleted: StateDeleting when it starts, then
// StateOnRomM once its files are gone or when the plugin refused to delete them, with Problem
// saying why.
type DeleteUpdate struct {
	ROMID   int
	Name    string
	State   domain.GameState
	Problem error
}

// DeleteListener hears each change to a game being deleted.
type DeleteListener func(DeleteUpdate)

// Delete deletes the files of Plugin-owned games in the background, one game at a time: the games
// with ids, or with platformID every Plugin-owned game on that platform. Saves are never touched.
// It returns how many games it will delete; games the plugin doesn't own, or that are downloading
// or already being deleted, are skipped, and asking for a single such game fails.
func (d *Downloads) Delete(ctx context.Context, ids []int, platformID int) (int, error) {
	return d.delete(ctx, ids, platformID, false)
}

// KeepSavesWith makes saves the save sync DeleteWithSaves sets games' saves aside with.
func (d *Downloads) KeepSavesWith(saves *Saves) { d.saves = saves }

// DeleteWithSaves deletes a Plugin-owned game as Delete does, and its save with it: into the
// game's backups, and only when RomM holds the save as it is on this Deck.
func (d *Downloads) DeleteWithSaves(ctx context.Context, id int) error {
	if d.saves == nil {
		return &domain.Error{Kind: domain.KindInvalidInput, Message: "Save sync is off, so saves can't be deleted with the game."}
	}
	del, err := d.saves.Deletion(ctx, id)
	if err != nil {
		return err
	}
	if del.Why != "" {
		return &domain.Error{Kind: domain.KindInvalidInput, Message: del.Why}
	}
	_, err = d.delete(ctx, []int{id}, 0, true)
	return err
}

func (d *Downloads) delete(ctx context.Context, ids []int, platformID int, withSaves bool) (int, error) {
	if d.store == nil {
		return 0, d.problem
	}
	targets, err := d.deleteTargets(ctx, ids, platformID)
	if err != nil {
		return 0, err
	}
	names := make(map[int]string, len(targets))
	for _, id := range targets {
		names[id] = d.gameName(ctx, id)
	}

	d.mu.Lock()
	var queued []DeleteUpdate
	for _, id := range targets {
		if d.find(id) != nil || d.deleting[id] {
			continue
		}
		d.deleting[id] = true
		queued = append(queued, DeleteUpdate{ROMID: id, Name: names[id], State: domain.StateDeleting})
	}
	d.mu.Unlock()

	if len(queued) == 0 && len(ids) == 1 && platformID == 0 {
		return 0, &domain.Error{Kind: domain.KindInvalidInput, Message: "That game is downloading or already being deleted."}
	}
	for _, u := range queued {
		d.onDelete(u)
	}
	go d.runDeletes(queued, withSaves)
	return len(queued), nil
}

// deleteTargets resolves a delete request to the ids of Plugin-owned games.
func (d *Downloads) deleteTargets(ctx context.Context, ids []int, platformID int) ([]int, error) {
	if platformID > 0 {
		owned, err := d.store.OwnedIDs(ctx)
		if err != nil {
			return nil, err
		}
		var out []int
		for _, id := range owned {
			rec, ok, err := d.store.Owned(ctx, id)
			if err != nil {
				return nil, err
			}
			if ok && rec.PlatformID == platformID {
				out = append(out, id)
			}
		}
		return out, nil
	}
	var out []int
	for _, id := range ids {
		_, ok, err := d.store.Owned(ctx, id)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, id)
		} else if len(ids) == 1 {
			return nil, &domain.Error{Kind: domain.KindInvalidInput, Message: "The plugin didn't download that game, so it won't delete it."}
		}
	}
	return out, nil
}

func (d *Downloads) gameName(ctx context.Context, id int) string {
	if rom, ok, err := d.catalog.ROM(ctx, id); err == nil && ok {
		return rom.Name
	}
	return "Game " + strconv.Itoa(id)
}

func (d *Downloads) runDeletes(games []DeleteUpdate, withSaves bool) {
	// One delete at a time, across requests: each re-hashes its files, and hashing several large
	// games at once only makes all of them slower.
	d.deleteMu.Lock()
	defer d.deleteMu.Unlock()
	for _, u := range games {
		err := d.deleteGame(d.base, u.ROMID, withSaves)
		d.mu.Lock()
		delete(d.deleting, u.ROMID)
		d.mu.Unlock()

		u.State, u.Problem = domain.StateOnRomM, err
		if err != nil {
			d.log.Warn("delete refused", "rom_id", u.ROMID, "err", err)
		} else {
			d.log.Info("deleted", "rom_id", u.ROMID)
		}
		d.onDelete(u)
		removed := 0
		if err == nil {
			removed = 1
		}
		d.folderChanged(0, removed)
		// The freed space may be what a held queue waits for.
		d.release()
	}
}

// deleteGame removes the files recorded for a Plugin-owned game, then its record, and with
// withSaves sets its save aside. Before removing anything it checks every file still matches what
// the plugin placed; when one doesn't, the user changed it, so nothing is removed and the plugin
// stops managing the game instead. A save that changed since it was asked for keeps the game too.
func (d *Downloads) deleteGame(ctx context.Context, id int, withSaves bool) error {
	rec, ok, err := d.store.Owned(ctx, id)
	if err != nil || !ok {
		return err
	}
	for _, f := range rec.Files {
		same, err := unchanged(f)
		if err != nil {
			return err
		}
		if !same {
			if err := d.store.DeleteOwned(ctx, id); err != nil {
				return err
			}
			return &domain.Error{
				Kind: domain.KindChangedOnDisk,
				Message: fmt.Sprintf("%s changed since the plugin downloaded it, so it wasn't deleted. The plugin no longer manages it.",
					filepath.Base(f.Path)),
			}
		}
	}
	// The save is found from the game's files, so it's looked for before they go.
	var setAside func() error
	if withSaves && d.saves != nil {
		if setAside, err = d.saves.setAside(ctx, id); err != nil {
			return err
		}
	}
	for _, f := range rec.Files {
		if err := os.Remove(f.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("delete %s: %w", f.Path, err)
		}
		// Folders an archive's contents brought with them go too.
		removeEmptyDirs(filepath.Dir(f.Path), d.dirHolding(ctx, rec.PlatformID, f.Path))
	}
	d.removeMedia(rec.Media)
	if err := d.store.DeleteOwned(ctx, id); err != nil {
		return err
	}
	if setAside != nil {
		if err := setAside(); err != nil {
			return fmt.Errorf("the game is deleted, but its save couldn't be: %w", err)
		}
	}
	return nil
}

// unchanged says whether a placed file is still what the plugin placed. A file that's already
// gone counts as unchanged: there is nothing of the user's to protect.
func unchanged(f domain.PlacedFile) (bool, error) {
	info, err := os.Lstat(f.Path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Size() != f.SizeBytes {
		return false, nil
	}
	sum, err := fileSHA1(f.Path)
	if err != nil {
		return false, err
	}
	return sum == f.SHA1, nil
}

func fileSHA1(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	h := sha1.New() //nolint:gosec // see import
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
