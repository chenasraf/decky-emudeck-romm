package app

import (
	"context"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

func names(roms []domain.ROM) []string {
	out := make([]string, 0, len(roms))
	for _, r := range roms {
		out = append(out, r.Name)
	}
	return out
}

func TestPlanAllListsMissingGames(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	if _, err := f.downloads.Queue(ctx, 1); err != nil {
		t.Fatal(err)
	}
	f.end(t, 1)

	plan, err := f.downloads.PlanAll(ctx, 1)
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
	if _, err := f.downloads.PlanAll(ctx, 2); kindOf(err) != domain.KindCantDownload {
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
	defer close(gate)

	plan, err := f.downloads.PlanAll(ctx, 1)
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

	if q, _, err := f.downloads.QueueAll(ctx, 1, false); kindOf(err) != domain.KindNoSpace || q != 0 {
		t.Fatalf("queue all: %d, %v", q, err)
	}
	if len(f.downloads.List()) != 0 {
		t.Fatalf("queued %+v", f.downloads.List())
	}

	queued, failed, err := f.downloads.QueueAll(ctx, 1, true)
	if err != nil || queued != 2 || failed != 0 {
		t.Fatalf("queue what fits: %d queued, %d failed, %v", queued, failed, err)
	}
	if states, _ := f.downloads.States(ctx); states[2] != "" || states[1] == "" || states[7] == "" {
		t.Fatalf("states %v", states)
	}

	// What's queued is no longer missing, and the room it takes is spoken for.
	plan, _ = f.downloads.PlanAll(ctx, 1)
	if got := names(plan.Games); len(got) != 1 || got[0] != "Super Metroid" || plan.Fits() {
		t.Fatalf("after queueing %v, %+v", got, plan)
	}
}
