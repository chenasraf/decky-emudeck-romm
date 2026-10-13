package app

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// conflict marks a game whose save changed both on the Deck and on RomM, and parks the Deck's copy
// on RomM outside every slot, so it outlives whatever happens to the Deck until the user chooses.
// Neither side's save is touched.
func (p *savePass) conflict(ctx context.Context, t saveTarget, op domain.SyncOperation, local localSave, r *domain.SaveRecord) {
	if r.State != domain.SaveConflict {
		p.logActivity(ctx, t.romID, domain.ActivityConflict,
			"The save changed both on this Deck and on RomM. Neither was overwritten; this Deck's copy is also kept on RomM.")
	}
	r.State, r.Problem = domain.SaveConflict, ""
	r.ConflictSaveID, r.ConflictServerAt = op.SaveID, op.ServerUpdatedAt
	p.summary.Conflicts++
	p.log.Info("save conflict", "rom_id", t.romID, "save_id", op.SaveID, "reason", op.Reason)
	if !local.exists || r.ParkedHash == local.hash {
		return
	}
	parked, err := p.server.UploadSave(ctx, domain.SaveUpload{
		ROMID: t.romID, FileName: domain.ParkedSaveName(filepath.Base(t.path), p.now()), Emulator: t.emulator,
		DeviceID: p.in.DeviceID, ContentHash: local.hash, Data: local.data,
	})
	if err != nil {
		// The conflict stands either way; the next sync tries parking again.
		p.log.Warn("park conflicting save", "rom_id", t.romID, "err", p.conn.observe(p.in, err))
		return
	}
	r.ParkedHash = local.hash
	p.log.Info("conflicting save parked", "rom_id", t.romID, "save_id", parked.ID, "file", parked.FileName)
}

// errStaleConflict is a choice made against a version of RomM's save that's no longer the newest.
var errStaleConflict = &domain.Error{
	Kind:    domain.KindStaleConflict,
	Message: "RomM's save changed since you looked at it. Compare the two again before choosing.",
}

// Resolve settles a game's save conflict with the copy the user keeps. seen is RomM's save the user
// compared with: when RomM has a newer one since, nothing changes and the conflict shows the newer
// one. A conflict that settled on its own since, or that RomM no longer sees, is synced as usual.
// Whatever save is replaced is backed up first.
func (s *Saves) Resolve(ctx context.Context, id int, keep domain.SaveChoice, seen int) error {
	if keep != domain.KeepDeck && keep != domain.KeepServer {
		return &domain.Error{Kind: domain.KindInvalidInput, Message: "Choose which save to keep."}
	}
	s.passMu.Lock()
	defer s.passMu.Unlock()
	in, err := s.ready()
	if err != nil {
		return err
	}
	t, why, err := s.target(ctx, id)
	if err != nil {
		return err
	}
	if why != "" {
		return &domain.Error{Kind: domain.KindInvalidInput, Message: why}
	}
	if keep == domain.KeepServer {
		if s.runs(t.rule.Emulator.ID) {
			return closeFirst(t.rule.Emulator)
		}
	}
	records, err := s.records(ctx)
	if err != nil {
		return err
	}
	p := &savePass{Saves: s, in: *in, server: s.dial(in.Server, in.Token), records: records, local: map[int]localSave{}}
	plan, err := p.plan(ctx, []saveTarget{t})
	if err != nil {
		return err
	}
	defer p.complete(ctx, plan.SessionID)
	local, read := p.local[id]
	if !read {
		return errors.New("couldn't read the save on this Deck")
	}
	// RomM plans by time and refuses the upload rather than planning a conflict when another
	// device's save is the slot's newest, so what's left to choose is read from the save the plan
	// names, not from its action.
	op, ok := ownOperation(plan, id)
	switch {
	case !ok:
		r := p.record(records, t)
		r.State, r.Problem = domain.SaveSynced, ""
		clearConflict(&r)
		p.put(ctx, r)
		s.settled(ctx, id, "The conflict settled on its own: RomM has no save of this game left to compare with.")
	case op.SaveID == 0, local.exists && local.hash == op.ServerHash:
		p.log.Info("save conflict settled on its own", "rom_id", id, "action", op.Action)
		p.step(ctx, t, op, plan.SessionID)
		s.settled(ctx, id, "The conflict settled on its own: the two saves match now.")
	case op.SaveID != seen:
		r := p.record(records, t)
		p.conflict(ctx, t, op, local, &r)
		p.put(ctx, r)
		return errStaleConflict
	default:
		r := p.record(records, t)
		if keep == domain.KeepDeck {
			err = p.upload(ctx, t, local, op, plan.SessionID, true, &r)
		} else {
			err = p.download(ctx, t, op, local, plan.SessionID, &r)
		}
		if err != nil {
			return err
		}
		clearConflict(&r)
		p.put(ctx, r)
		p.log.Info("save conflict resolved", "rom_id", id, "kept", keep)
		if keep == domain.KeepDeck {
			s.settled(ctx, id, "Kept this Deck's save in the conflict. RomM keeps its own as an older version.")
		} else {
			s.settled(ctx, id, "Used RomM's save in the conflict. This Deck's is in this game's backups.")
		}
	}
	s.onChange(SaveSyncEvent{ROMIDs: []int{id}, Summary: p.summary})
	return nil
}

// SaveConflictGame is a game whose save is in conflict.
type SaveConflictGame struct {
	ROMID int
	Name  string
}

// Conflicts lists the games whose saves are in conflict.
func (s *Saves) Conflicts(ctx context.Context) ([]SaveConflictGame, error) {
	if s.store == nil {
		return nil, nil
	}
	records, err := s.store.SaveRecords(ctx)
	if err != nil {
		return nil, err
	}
	var out []SaveConflictGame
	for _, r := range records {
		if r.State != domain.SaveConflict {
			continue
		}
		g := SaveConflictGame{ROMID: r.ROMID, Name: filepath.Base(r.Path)}
		if rom, ok, err := s.catalog.ROM(ctx, r.ROMID); err == nil && ok {
			g.Name = rom.Name
		}
		out = append(out, g)
	}
	return out, nil
}
