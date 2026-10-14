package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// update gives RomM an update of Chrono Trigger, a file named name, and installs it.
func (f *saveFixture) update(t *testing.T, name string) {
	t.Helper()
	ctx := context.Background()
	f.serve(1, name, []byte("chrono trigger, "+name))
	if err := f.downloads.CheckUpdates(ctx, []int{1}); err != nil {
		t.Fatal(err)
	}
	if c := f.ownershipChange(t); c.State != domain.StateUpdate {
		t.Fatalf("change %+v", c)
	}
	f.catalog.mu.Lock()
	r := f.catalog.roms[1]
	r.FileName = name
	f.catalog.roms[1] = r
	f.catalog.mu.Unlock()
	if _, err := f.downloads.Queue(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if u := f.end(t, 1); u.State != domain.StateInstalled {
		t.Fatalf("end %+v", u)
	}
}

// An update that renames the game syncs its save first, then moves it to the new name, which syncs
// as the same save.
func TestUpdateCarriesTheSaveToItsNewName(t *testing.T) {
	f := newSaveFixture(t)
	f.downloads.KeepSavesWith(f.saves)
	f.writeSave(t, "progress", time.Now())

	f.update(t, "Chrono Trigger (USA) (Rev 1).sfc")
	if head, _ := f.romm.head(1); string(head.data) != "progress" {
		t.Fatalf("RomM holds %q before the update", head.data)
	}
	moved := filepath.Join(f.raSaves, "Chrono Trigger (USA) (Rev 1).srm")
	if got := readFile(t, moved); got != "progress" {
		t.Fatalf("new save holds %q", got)
	}
	if _, err := os.Stat(f.chronoSave()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old save kept: %v", err)
	}
	if b, _ := f.saves.Backups(1); len(b) != 1 {
		t.Fatalf("backups %+v", b)
	}
	if st := f.status(t); st.Path != moved {
		t.Fatalf("status %+v", st)
	}
	if s := f.sync(t); s.Uploaded+s.Downloaded+s.Conflicts+s.Failed != 0 {
		t.Fatalf("sync after %+v", s)
	}
	if a, _ := f.saves.Activity(context.Background(), 1, 1); len(a) != 1 || a[0].Kind != domain.ActivityMoved {
		t.Fatalf("activity %+v", a)
	}
}

// A save already at the new name is the game's own: neither save moves.
func TestUpdateLeavesASaveAtTheNewName(t *testing.T) {
	f := newSaveFixture(t)
	f.downloads.KeepSavesWith(f.saves)
	f.writeSave(t, "progress", time.Now())
	other := filepath.Join(f.raSaves, "Chrono Trigger (USA) (Rev 1).srm")
	writeFile(t, other, []byte("rev 1 progress"))

	f.update(t, "Chrono Trigger (USA) (Rev 1).sfc")
	if got := readFile(t, other); got != "rev 1 progress" {
		t.Fatalf("new name holds %q", got)
	}
	if got := readFile(t, f.chronoSave()); got != "progress" {
		t.Fatalf("old name holds %q", got)
	}
}

// An update under the same name leaves the save as it is.
func TestUpdateUnderTheSameNameKeepsTheSave(t *testing.T) {
	f := newSaveFixture(t)
	f.downloads.KeepSavesWith(f.saves)
	f.writeSave(t, "progress", time.Now())

	f.update(t, "Chrono Trigger (USA).sfc")
	if got := readFile(t, f.chronoSave()); got != "progress" {
		t.Fatalf("save holds %q", got)
	}
	if b, _ := f.saves.Backups(1); len(b) != 0 {
		t.Fatalf("backups %+v", b)
	}
}
