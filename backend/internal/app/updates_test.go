package app

import (
	"context"
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
