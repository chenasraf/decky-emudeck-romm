package app

import (
	"context"
	"slices"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// goneRecord is the record of a game whose id the Server no longer has: a Plugin-owned one, or a
// Linked one when owned is nil.
type goneRecord struct {
	id         int
	platformID int
	owned      *domain.OwnedROM
	link       *domain.LinkedGame
}

// Rematch looks after the games this Deck holds a record of whose ids the Server no longer has,
// present being the ids it has. Re-importing a game on RomM gives it a new id, so one of the
// platforms in changed, those the sync brought new or changed games of, may hold the same files
// again: the record then moves to that id without a word. A Plugin-owned game is matched by the
// hashes RomM had when the plugin placed it, a Linked one by hashing its files. The ids of the games
// not found again are returned; they are Removed from RomM, still on this Deck and still deletable.
func (d *Downloads) Rematch(ctx context.Context, present, changed map[int]bool) ([]int, error) {
	if d.store == nil {
		return nil, nil
	}
	gone, recorded, err := d.goneRecords(ctx, present)
	if err != nil {
		return nil, err
	}
	byPlatform := map[int][]goneRecord{}
	var removed []int
	for _, r := range gone {
		d.mu.Lock()
		busy := d.find(r.id) != nil || d.deleting[r.id]
		d.mu.Unlock()
		if busy || !changed[r.platformID] {
			removed = append(removed, r.id)
			continue
		}
		byPlatform[r.platformID] = append(byPlatform[r.platformID], r)
	}
	for platformID, recs := range byPlatform {
		left, err := d.rematchIn(ctx, platformID, recs, recorded)
		if err != nil {
			return nil, err
		}
		removed = append(removed, left...)
	}
	slices.Sort(removed)
	return removed, nil
}

// goneRecords returns the records whose ids aren't in present, by id, and every id recorded.
func (d *Downloads) goneRecords(ctx context.Context, present map[int]bool) ([]goneRecord, map[int]bool, error) {
	recorded := map[int]bool{}
	var gone []goneRecord
	ids, err := d.store.OwnedIDs(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, id := range ids {
		recorded[id] = true
		if present[id] {
			continue
		}
		rec, ok, err := d.store.Owned(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			gone = append(gone, goneRecord{id: id, platformID: rec.PlatformID, owned: &rec})
		}
	}
	links, err := d.store.LinkedGames(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, g := range links {
		if recorded[g.ROMID] {
			continue
		}
		recorded[g.ROMID] = true
		if !present[g.ROMID] {
			gone = append(gone, goneRecord{id: g.ROMID, platformID: g.PlatformID, link: &g})
		}
	}
	slices.SortFunc(gone, func(a, b goneRecord) int { return a.id - b.id })
	return gone, recorded, nil
}

// rematchIn looks for a platform's gone records among its ROMs on the Server, moving each one found
// to its new id, and returns the ids of the rest. Not reaching the Server leaves them all as they
// are, for a later sync to try again.
func (d *Downloads) rematchIn(ctx context.Context, platformID int, recs []goneRecord, recorded map[int]bool) ([]int, error) {
	idsOf := func(recs []goneRecord) []int {
		out := make([]int, 0, len(recs))
		for _, r := range recs {
			out = append(out, r.id)
		}
		return out
	}
	in, err := d.conn.activeSignIn()
	if err != nil || in == nil {
		return idsOf(recs), nil //nolint:nilerr // signed out: nothing to match against
	}
	skip := func(id int) bool {
		if recorded[id] {
			return true
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.find(id) != nil
	}
	games, err := platformGames(ctx, d.dial(in.Server, in.Token), platformID, skip)
	if err != nil {
		d.log.Warn("look for games re-imported on RomM", "platform_id", platformID, "err", d.conn.observe(*in, err))
		return idsOf(recs), nil
	}
	byHash, sizes := indexGames(games)

	var left []int
	for _, r := range recs {
		var to int
		var ok bool
		if r.owned != nil {
			var snapshot []domain.FileSnapshot
			if to, snapshot, ok = matchSnapshot(r.owned.Snapshot, games); ok {
				moved := *r.owned
				moved.ROMID, moved.Snapshot = to, snapshot
				if err := d.store.MoveOwned(ctx, r.id, moved); err != nil {
					return nil, err
				}
			}
		} else if to, ok = d.matchLinked(r.link.Path, games, byHash, sizes); ok {
			moved := *r.link
			moved.ROMID = to
			if err := d.store.MoveLinked(ctx, r.id, moved); err != nil {
				return nil, err
			}
		}
		if !ok {
			left = append(left, r.id)
			continue
		}
		// The new id is spoken for now.
		delete(games, to)
		d.mu.Lock()
		delete(d.checks, r.id)
		delete(d.checks, to)
		delete(d.found, to)
		d.mu.Unlock()
		d.log.Info("game re-imported on RomM, record moved", "from_rom_id", r.id, "to_rom_id", to)
	}
	return left, nil
}

// matchSnapshot finds the ROM among games whose game files have the hashes snapshot holds, one for
// one, and returns its id and the snapshot of its files. The lowest id wins a tie.
func matchSnapshot(snapshot []domain.FileSnapshot, games map[int][]domain.ROMFile) (int, []domain.FileSnapshot, bool) {
	keys := make([]string, 0, len(snapshot))
	for _, s := range snapshot {
		if s.Hash == "" {
			// RomM had no hash for the file, so nothing can be told to be the same.
			return 0, nil, false
		}
		keys = append(keys, snapshotKey(s))
	}
	if len(keys) == 0 {
		return 0, nil, false
	}
	ids := make([]int, 0, len(games))
	for id := range games {
		ids = append(ids, id)
	}
	slices.Sort(ids)
next:
	for _, id := range ids {
		game := games[id]
		if len(game) != len(keys) {
			continue
		}
		used := make([]bool, len(game))
		out := make([]domain.FileSnapshot, 0, len(game))
		for _, key := range keys {
			j := -1
			for i, f := range game {
				if !used[i] && f.Hashed() && hashKey(f) == key {
					j = i
					break
				}
			}
			if j < 0 {
				continue next
			}
			used[j] = true
			out = append(out, domain.SnapshotOf(game[j]))
		}
		return id, out, true
	}
	return 0, nil, false
}

// snapshotKey is a snapshot's hash as hashKey writes it.
func snapshotKey(s domain.FileSnapshot) string {
	switch s.HashKind {
	case "sha1":
		return hashKey(domain.ROMFile{SHA1: s.Hash})
	case "md5":
		return hashKey(domain.ROMFile{MD5: s.Hash})
	default:
		return hashKey(domain.ROMFile{CRC: s.Hash})
	}
}

// matchLinked hashes the file or folder at path and returns the ROM among games it holds every game
// file of. The lowest id wins a tie.
func (d *Downloads) matchLinked(path string, games map[int][]domain.ROMFile, byHash map[string][]serverFile, sizes map[int64]bool) (int, bool) {
	files, err := candidateFiles(path, sizes)
	if err != nil {
		return 0, false
	}
	hits := map[int]map[int]bool{}
	for _, f := range files {
		for _, h := range d.matchFile(f, byHash) {
			if hits[h.romID] == nil {
				hits[h.romID] = map[int]bool{}
			}
			hits[h.romID][h.file.file.ID] = true
		}
	}
	ids := make([]int, 0, len(hits))
	for id := range hits {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if _, ok := games[id]; ok && len(hits[id]) == len(games[id]) {
			return id, true
		}
	}
	return 0, false
}
