package app

import (
	"context"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

func names(roms []domain.ROM) []string {
	out := make([]string, 0, len(roms))
	for _, r := range roms {
		out = append(out, r.Name)
	}
	return out
}

func platformBatch(id int) Batch { return Batch{PlatformID: id} }

func TestPlanAllListsMissingGames(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	if _, err := f.downloads.Queue(ctx, 1); err != nil {
		t.Fatal(err)
	}
	f.end(t, 1)

	plan, err := f.downloads.PlanAll(ctx, platformBatch(1))
	if err != nil {
		t.Fatal(err)
	}
	// Chrono Trigger is installed, and SNES has no layout for Disc Game's several files.
	if got := names(plan.Games); len(got) != 2 || got[0] != "Super Metroid" || got[1] != "Zipped" {
		t.Fatalf("games %v", got)
	}
	if plan.Unplaceable != 1 || plan.TotalBytes != int64(len(metroid)) || !plan.FreeKnown || !plan.Fits() {
		t.Fatalf("plan %+v", plan)
	}
	if _, err := f.downloads.PlanAll(ctx, platformBatch(2)); kindOf(err) != domain.KindCantDownload {
		t.Fatalf("platform without a folder: %v", err)
	}
}

func TestQueueAllQueuesWhatFits(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	// Room for Chrono Trigger and the empty Zipped, not Super Metroid as well.
	f.free.Store(spaceMargin + int64(len(chrono)) + 5)
	// Hold Chrono Trigger mid-download, so nothing leaves the queue during the test.
	gate := make(chan struct{})
	f.server.gates[10] = gate

	plan, err := f.downloads.PlanAll(ctx, platformBatch(1))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Fits() || len(plan.Games) != 3 {
		t.Fatalf("plan %+v", plan)
	}
	// Taken by name, Super Metroid doesn't fit, and Zipped after it still does.
	if got := names(plan.Fit); len(got) != 2 || got[0] != "Chrono Trigger" || got[1] != "Zipped" || plan.FitBytes != int64(len(chrono)) {
		t.Fatalf("fit %v, %d bytes", got, plan.FitBytes)
	}

	if q, _, err := f.downloads.QueueAll(ctx, platformBatch(1), false); kindOf(err) != domain.KindNoSpace || q != 0 {
		t.Fatalf("queue all: %d, %v", q, err)
	}
	if len(f.downloads.List()) != 0 {
		t.Fatalf("queued %+v", f.downloads.List())
	}

	queued, failed, err := f.downloads.QueueAll(ctx, platformBatch(1), true)
	if err != nil || queued != 2 || failed != 0 {
		t.Fatalf("queue what fits: %d queued, %d failed, %v", queued, failed, err)
	}
	if states, _ := f.downloads.States(ctx); states[2] != "" || states[1] == "" || states[7] == "" {
		t.Fatalf("states %v", states)
	}

	// What's queued is no longer missing, and the room it takes is spoken for.
	plan, _ = f.downloads.PlanAll(ctx, platformBatch(1))
	if got := names(plan.Games); len(got) != 1 || got[0] != "Super Metroid" || plan.Fits() {
		t.Fatalf("after queueing %v, %+v", got, plan)
	}

	// Both downloads end before the test's folders go.
	close(gate)
	f.endAll(t, 1, 7)
}

func TestCollectionBatchSpansPlatforms(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	ref := domain.CollectionRef{Kind: domain.CollectionRegular, ID: 4}
	f.catalog.mu.Lock()
	// Super Metroid (SNES), Ridge Racer (PlayStation), Mystery Game (a platform EmuDeck has no
	// folder for) and an id the library doesn't hold.
	f.catalog.collections = []domain.Collection{{CollectionRef: ref, Name: "Picks", ROMIDs: []int{2, 5, 4, 999}}}
	f.catalog.mu.Unlock()
	gate := make(chan struct{})
	f.server.gates[20] = gate

	plan, err := f.downloads.PlanAll(ctx, Batch{Collection: &ref})
	if err != nil {
		t.Fatal(err)
	}
	if got := names(plan.Games); plan.Name != "Picks" || len(got) != 2 || got[0] != "Super Metroid" || got[1] != "Ridge Racer" {
		t.Fatalf("plan %q games %v", plan.Name, got)
	}
	// The Mystery Game has nowhere to go, and Ridge Racer's zip is extracted, so it needs room twice.
	if plan.Unplaceable != 1 || plan.NeedBytes != int64(len(metroid))+2000 || !plan.Fits() {
		t.Fatalf("plan %+v", plan)
	}

	queued, failed, err := f.downloads.QueueAll(ctx, Batch{Collection: &ref}, false)
	if err != nil || queued != 2 || failed != 0 {
		t.Fatalf("queue all: %d queued, %d failed, %v", queued, failed, err)
	}
	close(gate)
	f.endAll(t, 2, 5)

	missing := domain.CollectionRef{Kind: domain.CollectionSmart, ID: 4}
	if _, err := f.downloads.PlanAll(ctx, Batch{Collection: &missing}); kindOf(err) != domain.KindNotFound {
		t.Fatalf("unknown collection: %v", err)
	}
}

// endAll waits for the downloads of ids to end, in whatever order.
func (f *downloadFixture) endAll(t *testing.T, ids ...int) {
	t.Helper()
	left := map[int]bool{}
	for _, id := range ids {
		left[id] = true
	}
	for len(left) > 0 {
		select {
		case u := <-f.updates:
			if u.State == domain.StateInstalled || u.State == domain.StateOnRomM || u.State == domain.StateFailed {
				delete(left, u.ROMID)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("downloads %v didn't end", left)
		}
	}
}
