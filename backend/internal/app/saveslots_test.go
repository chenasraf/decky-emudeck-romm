package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// switchSlot switches Chrono Trigger's save to slot and waits for the sync that follows.
func (f *saveFixture) switchSlot(t *testing.T, slot string) {
	t.Helper()
	if err := f.saves.SwitchSlot(context.Background(), 1, slot); err != nil {
		t.Fatal(err)
	}
	f.ended(t)
}

func (f *saveFixture) backupsOf(t *testing.T) []string {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Join(f.backups, "1"))
	var out []string
	for _, e := range entries {
		out = append(out, readFile(t, filepath.Join(f.backups, "1", e.Name())))
	}
	return out
}

func TestSlotsListsRomMsSlots(t *testing.T) {
	f := newSaveFixture(t)
	f.writeSave(t, "deck progress", time.Now())
	f.sync(t)
	f.romm.otherDeviceIn(1, "handheld", "snes9x", []byte("handheld line"))

	slots, current, err := f.saves.Slots(context.Background(), 1)
	if err != nil || current != domain.AutosaveSlot || len(slots) != 2 {
		t.Fatalf("slots %+v, current %q, %v", slots, current, err)
	}
}

// Choosing a slot syncs that slot only: its newest save replaces the Deck's, backed up, and later
// changes go to it while autosave stays as it was.
func TestSwitchToASlotWithASave(t *testing.T) {
	f := newSaveFixture(t)
	f.writeSave(t, "deck progress", time.Now().Add(-time.Hour))
	f.sync(t)
	f.romm.otherDeviceIn(1, "handheld", "snes9x", []byte("handheld line"))

	f.switchSlot(t, "handheld")
	if got := readFile(t, f.chronoSave()); got != "handheld line" {
		t.Fatalf("save holds %q", got)
	}
	if got := f.backupsOf(t); len(got) != 1 || got[0] != "deck progress" {
		t.Fatalf("backups %q", got)
	}
	if st := f.status(t); st.Slot != "handheld" || st.State != domain.SaveSynced {
		t.Fatalf("status %+v", st)
	}
	if kinds := f.store.kinds(1); kinds[len(kinds)-1] != domain.ActivitySlot {
		t.Fatalf("activity %v", kinds)
	}

	f.writeSave(t, "handheld line, played on the Deck", time.Now().Add(time.Hour))
	if s := f.sync(t); s.Uploaded != 1 {
		t.Fatalf("summary %+v", s)
	}
	if head, _ := f.romm.headIn(1, "handheld"); string(head.data) != "handheld line, played on the Deck" {
		t.Fatalf("slot holds %q", head.data)
	}
	if head, _ := f.romm.head(1); string(head.data) != "deck progress" {
		t.Fatalf("autosave holds %q", head.data)
	}
}

func TestSwitchToAnEmptySlotStartsItFromTheDeck(t *testing.T) {
	f := newSaveFixture(t)
	f.writeSave(t, "deck progress", time.Now())
	f.sync(t)

	f.switchSlot(t, "deck only")
	if got := readFile(t, f.chronoSave()); got != "deck progress" {
		t.Fatalf("save holds %q", got)
	}
	if got := f.backupsOf(t); len(got) != 0 {
		t.Fatalf("backups %q", got)
	}
	if head, ok := f.romm.headIn(1, "deck only"); !ok || string(head.data) != "deck progress" {
		t.Fatalf("slot holds %+v", head)
	}
}

// A change only this Deck has goes up to the slot it leaves before the switch replaces it.
func TestSwitchSyncsTheSlotItLeavesFirst(t *testing.T) {
	f := newSaveFixture(t)
	f.writeSave(t, "shared start", time.Now().Add(-time.Hour))
	f.sync(t)
	f.romm.otherDeviceIn(1, "handheld", "snes9x", []byte("handheld line"))
	f.writeSave(t, "deck went on", time.Now().Add(time.Hour))

	f.switchSlot(t, "handheld")
	if head, _ := f.romm.head(1); string(head.data) != "deck went on" {
		t.Fatalf("autosave holds %q", head.data)
	}
	if got := readFile(t, f.chronoSave()); got != "handheld line" {
		t.Fatalf("save holds %q", got)
	}
}

func TestSwitchingBackToAutosaveClearsThePick(t *testing.T) {
	f := newSaveFixture(t)
	f.writeSave(t, "deck progress", time.Now())
	f.sync(t)
	f.switchSlot(t, "deck only")

	f.switchSlot(t, domain.AutosaveSlot)
	if f.store.slots[1] != "" {
		t.Fatalf("pick kept: %q", f.store.slots[1])
	}
	if st := f.status(t); st.Slot != domain.AutosaveSlot || st.State != domain.SaveSynced {
		t.Fatalf("status %+v", st)
	}
}

func TestSwitchIsRefusedInAConflict(t *testing.T) {
	f := newSaveFixture(t)
	f.conflicted(t)
	f.romm.otherDeviceIn(1, "handheld", "snes9x", []byte("handheld line"))

	err := f.saves.SwitchSlot(context.Background(), 1, "handheld")
	if kindOf(err) != domain.KindInvalidInput || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("switch: %v", err)
	}
	if st := f.status(t); st.Slot != domain.AutosaveSlot || readFile(t, f.chronoSave()) != "deck went on" {
		t.Fatalf("status %+v", st)
	}
}

func TestSwitchWaitsForTheEmulator(t *testing.T) {
	f := newSaveFixture(t)
	f.writeSave(t, "deck progress", time.Now())
	f.sync(t)
	f.retroarch.Store(true)

	if err := f.saves.SwitchSlot(context.Background(), 1, "handheld"); kindOf(err) != domain.KindInvalidInput {
		t.Fatalf("switch: %v", err)
	}
	if st := f.status(t); st.Slot != domain.AutosaveSlot {
		t.Fatalf("status %+v", st)
	}
}

func TestSlotNameMustFit(t *testing.T) {
	f := newSaveFixture(t)
	for _, name := range []string{"", "   ", strings.Repeat("x", domain.MaxSlotName+1)} {
		if err := f.saves.SwitchSlot(context.Background(), 1, name); kindOf(err) != domain.KindInvalidInput {
			t.Fatalf("%q: %v", name, err)
		}
	}
}
