package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

func TestReplacedFileOnRomMShowsUpdate(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	f.installedChrono(t)
	f.server.mu.Lock()
	f.server.files[1][0].LastModified = time.Unix(9000, 0)
	f.server.files[1][0].SizeBytes++
	f.server.mu.Unlock()

	if err := f.downloads.CheckUpdates(ctx, []int{1, 2}); err != nil {
		t.Fatal(err)
	}
	if states, _ := f.downloads.States(ctx); states[1] != domain.StateUpdate {
		t.Fatalf("states %v", states)
	}
	if c := f.ownershipChange(t); c.ROMID != 1 || c.Kind != OwnershipUpdate || c.State != domain.StateUpdate {
		t.Fatalf("change %+v", c)
	}
	// Checking again changes nothing and says nothing.
	if err := f.downloads.CheckUpdates(ctx, []int{1}); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-f.ownership:
		t.Fatalf("change %+v", c)
	default:
	}
}

func TestMetadataEditShowsNoUpdate(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	f.installedChrono(t)
	// A rename on RomM touches the ROM, not its files.
	f.catalog.mu.Lock()
	r := f.catalog.roms[1]
	r.Name = "Chrono Trigger (Renamed)"
	f.catalog.roms[1] = r
	f.catalog.mu.Unlock()

	if err := f.downloads.CheckUpdates(ctx, []int{1}); err != nil {
		t.Fatal(err)
	}
	if states, _ := f.downloads.States(ctx); states[1] != domain.StateInstalled {
		t.Fatalf("states %v", states)
	}
}

func TestSweepRunsAtMostDaily(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	f.downloads.now = func() time.Time { return now }

	if wait, _ := f.downloads.untilSweep(ctx); wait != 0 {
		t.Fatalf("first sweep waits %v", wait)
	}
	if err := f.downloads.sweep(ctx); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	if wait, _ := f.downloads.untilSweep(ctx); wait != 23*time.Hour {
		t.Fatalf("an hour after a sweep, the next waits %v", wait)
	}
	now = now.Add(23 * time.Hour)
	if wait, _ := f.downloads.untilSweep(ctx); wait != 0 {
		t.Fatalf("a day after a sweep, the next waits %v", wait)
	}
}

// chronoWithUpdate installs Chrono Trigger, then gives RomM an update of it: data, named name.
func (f *downloadFixture) chronoWithUpdate(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := f.installedChrono(t)
	f.serve(1, name, data)
	if err := f.downloads.CheckUpdates(context.Background(), []int{1}); err != nil {
		t.Fatal(err)
	}
	if c := f.ownershipChange(t); c.State != domain.StateUpdate {
		t.Fatalf("change %+v", c)
	}
	return path
}

func TestUpdateReplacesTheFiles(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	revised := []byte("chrono trigger, revised")
	path := f.chronoWithUpdate(t, "Chrono Trigger (USA).sfc", revised)

	if _, err := f.downloads.Queue(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if u := f.end(t, 1); u.State != domain.StateInstalled {
		t.Fatalf("end %+v", u)
	}
	if data, _ := os.ReadFile(path); string(data) != string(revised) {
		t.Fatalf("file holds %q", data)
	}
	if _, err := os.Stat(path + newSuffix); !os.IsNotExist(err) {
		t.Fatalf("left the new file beside: %v", err)
	}
	if rec, _, _ := f.owned.Owned(ctx, 1); len(rec.Snapshot) != 1 || rec.Snapshot[0].SizeBytes != int64(len(revised)) {
		t.Fatalf("record %+v", rec)
	}
	if states, _ := f.downloads.States(ctx); states[1] != domain.StateInstalled {
		t.Fatalf("states %v", states)
	}
	f.stagingEmpty(t)
}

func TestUpdateUnderANewNameRemovesTheOldFile(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	path := f.chronoWithUpdate(t, "Chrono Trigger (USA) (Rev 1).sfc", []byte("rev 1"))
	f.catalog.mu.Lock()
	r := f.catalog.roms[1]
	r.FileName = "Chrono Trigger (USA) (Rev 1).sfc"
	f.catalog.roms[1] = r
	f.catalog.mu.Unlock()

	if _, err := f.downloads.Queue(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if u := f.end(t, 1); u.State != domain.StateInstalled {
		t.Fatalf("end %+v", u)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("old file kept: %v", err)
	}
	rec, _, _ := f.owned.Owned(ctx, 1)
	if len(rec.Files) != 1 || filepath.Base(rec.Files[0].Path) != "Chrono Trigger (USA) (Rev 1).sfc" {
		t.Fatalf("record %+v", rec)
	}
}

func TestCancelledUpdateKeepsTheOldVersion(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	path := f.chronoWithUpdate(t, "Chrono Trigger (USA).sfc", []byte("chrono trigger, revised"))
	f.server.mu.Lock()
	f.server.gates[10] = make(chan struct{})
	f.server.mu.Unlock()

	if _, err := f.downloads.Queue(ctx, 1); err != nil {
		t.Fatal(err)
	}
	f.started(t)
	f.downloads.Cancel(1)
	if u := f.end(t, 1); u.State != domain.StateUpdate {
		t.Fatalf("end %+v", u)
	}
	if data, _ := os.ReadFile(path); string(data) != string(chrono) {
		t.Fatalf("file holds %q", data)
	}
	if states, _ := f.downloads.States(ctx); states[1] != domain.StateUpdate {
		t.Fatalf("states %v", states)
	}
	f.stagingEmpty(t)
}
