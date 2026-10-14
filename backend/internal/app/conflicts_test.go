package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// conflicted syncs Chrono Trigger's save, then changes it both on the Deck and on RomM, and syncs
// into a conflict. It returns RomM's save the conflict is with.
func (f *saveFixture) conflicted(t *testing.T) fakeVersion {
	t.Helper()
	f.writeSave(t, "shared start", time.Now().Add(-time.Hour))
	f.sync(t)
	head := f.romm.otherDevice(1, "snes9x", []byte("handheld went on"))
	f.writeSave(t, "deck went on", time.Now().Add(time.Hour))
	if s := f.sync(t); s.Conflicts != 1 {
		t.Fatalf("no conflict: %+v", s)
	}
	return head
}

func (f *saveFixture) resolve(keep domain.SaveChoice, seen int) error {
	return f.saves.Resolve(context.Background(), 1, keep, seen)
}

func TestKeepTheDecksSave(t *testing.T) {
	f := newSaveFixture(t)
	head := f.conflicted(t)

	if err := f.resolve(domain.KeepDeck, head.ID); err != nil {
		t.Fatal(err)
	}
	if now, _ := f.romm.head(1); string(now.data) != "deck went on" {
		t.Fatalf("RomM holds %q", now.data)
	}
	if st := f.status(t); st.State != domain.SaveSynced || st.Conflict != nil {
		t.Fatalf("status %+v", st)
	}
	if s := f.sync(t); s.Uploaded+s.Downloaded+s.Conflicts != 0 {
		t.Fatalf("next sync %+v", s)
	}
}

func TestKeepRomMsSave(t *testing.T) {
	f := newSaveFixture(t)
	head := f.conflicted(t)

	if err := f.resolve(domain.KeepServer, head.ID); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, f.chronoSave()); got != "handheld went on" {
		t.Fatalf("save holds %q", got)
	}
	backups, _ := os.ReadDir(filepath.Join(f.backups, "1"))
	if len(backups) != 1 || readFile(t, filepath.Join(f.backups, "1", backups[0].Name())) != "deck went on" {
		t.Fatalf("backups %v", backups)
	}
	if st := f.status(t); st.State != domain.SaveSynced {
		t.Fatalf("status %+v", st)
	}
	if s := f.sync(t); s.Uploaded+s.Downloaded+s.Conflicts != 0 {
		t.Fatalf("next sync %+v", s)
	}
}

// A choice made against a save RomM has since replaced changes nothing, and the conflict shows the
// newer one.
func TestStaleChoiceIsRefused(t *testing.T) {
	f := newSaveFixture(t)
	seen := f.conflicted(t)
	newer := f.romm.otherDevice(1, "snes9x", []byte("handheld went further"))

	if err := f.resolve(domain.KeepDeck, seen.ID); kindOf(err) != domain.KindStaleConflict {
		t.Fatalf("resolve: %v", err)
	}
	if now, _ := f.romm.head(1); now.ID != newer.ID {
		t.Fatal("RomM's save touched")
	}
	if got := readFile(t, f.chronoSave()); got != "deck went on" {
		t.Fatalf("save holds %q", got)
	}
	st := f.status(t)
	if st.State != domain.SaveConflict || st.Conflict.ServerSaveID != newer.ID {
		t.Fatalf("status %+v, conflict %+v", st, st.Conflict)
	}
	if err := f.resolve(domain.KeepDeck, newer.ID); err != nil {
		t.Fatal(err)
	}
}

// When the two sides came to match since, there's nothing left to choose.
func TestConflictThatSettledOnItsOwn(t *testing.T) {
	f := newSaveFixture(t)
	head := f.conflicted(t)
	f.writeSave(t, "handheld went on", time.Now().Add(2*time.Hour))

	if err := f.resolve(domain.KeepDeck, head.ID); err != nil {
		t.Fatal(err)
	}
	if st := f.status(t); st.State != domain.SaveSynced || st.Conflict != nil {
		t.Fatalf("status %+v", st)
	}
	if now, _ := f.romm.head(1); now.ID != head.ID {
		t.Fatal("uploaded although both matched")
	}
}

func TestKeepRomMsSaveWaitsForRetroArch(t *testing.T) {
	f := newSaveFixture(t)
	head := f.conflicted(t)
	f.retroarch.Store(true)

	if err := f.resolve(domain.KeepServer, head.ID); kindOf(err) != domain.KindInvalidInput {
		t.Fatalf("resolve: %v", err)
	}
	if got := readFile(t, f.chronoSave()); got != "deck went on" {
		t.Fatalf("save holds %q", got)
	}
	if st := f.status(t); st.State != domain.SaveConflict {
		t.Fatalf("status %+v", st)
	}
}

func TestConflictsList(t *testing.T) {
	f := newSaveFixture(t)
	if got, err := f.saves.Conflicts(context.Background()); err != nil || len(got) != 0 {
		t.Fatalf("before: %v, %v", got, err)
	}
	head := f.conflicted(t)
	got, err := f.saves.Conflicts(context.Background())
	if err != nil || len(got) != 1 || got[0].ROMID != 1 || got[0].Name != "Chrono Trigger" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if err := f.resolve(domain.KeepServer, head.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.saves.Conflicts(context.Background()); len(got) != 0 {
		t.Fatalf("after: %+v", got)
	}
}

// RomM's saves outside the slot the plugin syncs, such as a conflict it parked, are never taken
// for the game's save.
func TestOwnOperationIgnoresOtherSlots(t *testing.T) {
	plan := domain.SyncPlan{Operations: []domain.SyncOperation{
		{Action: domain.SyncDownload, ROMID: 1, SaveID: 9, Slot: ""},
		{Action: domain.SyncDownload, ROMID: 1, SaveID: 8, Slot: "manual"},
		{Action: domain.SyncNoOp, ROMID: 1, SaveID: 7, Slot: domain.AutosaveSlot},
	}}
	if op, ok := ownOperation(plan, saveTarget{romID: 1, slot: domain.AutosaveSlot}); !ok || op.SaveID != 7 {
		t.Fatalf("got %+v, %v", op, ok)
	}
	if op, ok := ownOperation(plan, saveTarget{romID: 1, slot: "manual"}); !ok || op.SaveID != 8 {
		t.Fatalf("picked slot: got %+v, %v", op, ok)
	}
	if _, ok := ownOperation(plan, saveTarget{romID: 2, slot: domain.AutosaveSlot}); ok {
		t.Fatal("found an operation for another game")
	}
}
