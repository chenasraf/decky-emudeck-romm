package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// slotOf is the RomM slot the save of the ROM with id syncs in.
func (s *Saves) slotOf(ctx context.Context, id int) (string, error) {
	if s.store == nil {
		return domain.AutosaveSlot, nil
	}
	slot, err := s.store.SaveSlot(ctx, id)
	if err != nil || slot == "" {
		return domain.AutosaveSlot, err
	}
	return slot, nil
}

// Slots lists the game with id's save slots on RomM, and returns the one its save syncs in.
func (s *Saves) Slots(ctx context.Context, id int) ([]domain.SaveSlot, string, error) {
	in, err := s.ready()
	if err != nil {
		return nil, "", err
	}
	t, why, err := s.target(ctx, id)
	if err != nil {
		return nil, "", err
	}
	if why != "" {
		return nil, "", &domain.Error{Kind: domain.KindInvalidInput, Message: why}
	}
	slots, err := s.dial(in.Server, in.Token).SaveSlots(ctx, id)
	if err != nil {
		return nil, "", s.conn.observe(*in, err)
	}
	return slots, t.slot, nil
}

// SwitchSlot makes the game with id's save sync in slot. The save syncs in the slot it leaves
// first, so the switch strands nothing only this Deck had. Then the newest save in slot replaces
// the Deck's, which is backed up; an empty slot starts from the Deck's save instead. Nothing is
// deleted on either side.
func (s *Saves) SwitchSlot(ctx context.Context, id int, slot string) error {
	slot = strings.TrimSpace(slot)
	if slot == "" || len(slot) > domain.MaxSlotName {
		return &domain.Error{Kind: domain.KindInvalidInput, Message: fmt.Sprintf("Name the slot in 1 to %d characters.", domain.MaxSlotName)}
	}
	if _, err := s.ready(); err != nil {
		return err
	}
	t, why, err := s.target(ctx, id)
	if err != nil {
		return err
	}
	if why != "" {
		return &domain.Error{Kind: domain.KindInvalidInput, Message: why}
	}
	if t.slot == slot {
		return nil
	}
	if s.runs(t.rule.Emulator.ID) {
		return closeFirst(t.rule.Emulator)
	}
	if _, err := s.Sync(ctx, []int{id}); err != nil {
		return err
	}

	s.passMu.Lock()
	defer s.passMu.Unlock()
	in, err := s.ready()
	if err != nil {
		return err
	}
	records, err := s.records(ctx)
	if err != nil {
		return err
	}
	local, err := t.format.read()
	if err != nil {
		return fmt.Errorf("read the save: %w", err)
	}
	if err := switchable(records, t, local); err != nil {
		return err
	}
	server := s.dial(in.Server, in.Token)
	slots, err := server.SaveSlots(ctx, id)
	if err != nil {
		return s.conn.observe(*in, err)
	}

	from := t.slot
	if err := s.store.SetSaveSlot(ctx, id, slot); err != nil {
		return err
	}
	t.slot = slot
	p := &savePass{Saves: s, in: *in, server: server, records: records, local: map[int]localSave{}}
	r := p.record(records, t)
	msg := fmt.Sprintf("Switched to RomM's save slot %q.", slot)
	if head, ok := slotHead(slots, slot); ok {
		if err := p.adopt(ctx, t, head, local, &r); err != nil {
			if undo := s.store.SetSaveSlot(context.WithoutCancel(ctx), id, from); undo != nil {
				s.log.Error("restore save slot", "rom_id", id, "slot", from, "err", undo)
			}
			return err
		}
	} else if local.exists {
		msg += " It was empty, so this Deck's save starts it."
	}
	p.put(ctx, r)
	s.log.Info("save slot switched", "rom_id", id, "from", from, "to", slot)
	s.logActivity(ctx, id, domain.ActivitySlot, msg)
	// An empty slot takes the Deck's save in a sync.
	s.request([]int{id}, true)
	return nil
}

// switchable says why t's save can't leave its slot yet, nil when it can: it must match RomM's, or
// be gone from RomM, as the sync just before left it.
func switchable(records map[int]domain.SaveRecord, t saveTarget, local localSave) error {
	r, ok := records[t.romID]
	if !ok || r.Path != t.path || r.Slot != t.slot {
		return &domain.Error{Kind: domain.KindInvalidInput, Message: "The save hasn't synced yet. Switch slots once it has."}
	}
	switch r.State {
	case domain.SaveSynced, domain.SaveRemovedOnServer:
	case domain.SaveConflict:
		return &domain.Error{Kind: domain.KindInvalidInput, Message: "Settle the save's conflict before switching slots."}
	case domain.SaveWaiting:
		return closeFirst(t.rule.Emulator)
	default:
		return &domain.Error{Kind: domain.KindInvalidInput, Message: "The save has to sync before it switches slots, and it didn't: " + r.Problem}
	}
	if local.exists && local.hash != r.LocalHash {
		return &domain.Error{Kind: domain.KindInvalidInput, Message: "The save changed while syncing. Try again."}
	}
	return nil
}

// slotHead is the newest save in the slot named name, if it holds any.
func slotHead(slots []domain.SaveSlot, name string) (domain.ServerSave, bool) {
	for _, sl := range slots {
		if sl.Name == name && sl.Count > 0 && sl.Latest.ID != 0 {
			return sl.Latest, true
		}
	}
	return domain.ServerSave{}, false
}

// adopt puts head, the newest save of t's slot, in place of the Deck's save, which is backed up.
func (p *savePass) adopt(ctx context.Context, t saveTarget, head domain.ServerSave, local localSave, r *domain.SaveRecord) error {
	if local.exists && head.ContentHash != "" && local.hash == head.ContentHash {
		r.LocalHash, r.ServerSaveID, r.ServerHash, r.SyncedAt = local.hash, head.ID, head.ContentHash, head.UpdatedAt
		r.State, r.Problem = domain.SaveSynced, ""
		return nil
	}
	op := domain.SyncOperation{
		Action: domain.SyncDownload, ROMID: t.romID, SaveID: head.ID, FileName: head.FileName, Slot: t.slot,
		Emulator: head.Emulator, ServerUpdatedAt: head.UpdatedAt, ServerHash: head.ContentHash,
	}
	if err := p.download(ctx, t, op, local, 0, r); err != nil {
		return err
	}
	if r.State == domain.SaveWaiting {
		return closeFirst(t.rule.Emulator)
	}
	return nil
}
