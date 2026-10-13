package app

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeESDE is ES-DE as the tests run it: instance 0 while closed.
type fakeESDE struct {
	instance atomic.Int64
	mu       sync.Mutex
	batches  []ESDENotice
}

func (f *fakeESDE) running() (int, bool) {
	n := int(f.instance.Load())
	return n, n != 0
}

func (f *fakeESDE) heard() []ESDENotice {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ESDENotice{}, f.batches...)
}

func newFakeESDE(instance int) (*fakeESDE, *ESDE) {
	f := &fakeESDE{}
	f.instance.Store(int64(instance))
	e := NewESDE(f.running, func(batch, _ ESDENotice) {
		f.mu.Lock()
		f.batches = append(f.batches, batch)
		f.mu.Unlock()
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return f, e
}

func TestDownloadsWithESDEOpenShowOneNotice(t *testing.T) {
	f := newDownloadFixture(t, 1)
	ctx := context.Background()
	esde, watch := newFakeESDE(1200)
	f.downloads.onFolder = watch.FolderChanged
	f.serve(1, "Chrono Trigger (USA).sfc", chrono)
	f.serve(2, "Super Metroid (USA).sfc", metroid)

	for _, id := range []int{1, 2} {
		if _, err := f.downloads.Queue(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	f.end(t, 1)
	f.end(t, 2)
	f.stagingEmpty(t)

	if got := esde.heard(); len(got) != 1 || got[0] != (ESDENotice{Added: 2}) {
		t.Fatalf("batches %+v", got)
	}
	if n := watch.Notice(); n != (ESDENotice{Added: 2}) {
		t.Fatalf("notice %+v", n)
	}
	// ES-DE restarted, so it scanned the new games.
	esde.instance.Store(1300)
	if n := watch.Notice(); n != (ESDENotice{}) {
		t.Fatalf("notice after restart %+v", n)
	}
}

func TestESDEClosedHearsNothing(t *testing.T) {
	esde, watch := newFakeESDE(0)
	watch.FolderChanged(FolderChange{Added: 1})
	watch.FolderChanged(FolderChange{Removed: 1, Idle: true})
	if got := esde.heard(); len(got) != 0 {
		t.Fatalf("batches %+v", got)
	}
	if n := watch.Notice(); n != (ESDENotice{}) {
		t.Fatalf("notice %+v", n)
	}
}

func TestESDENoticeAddsUpBatches(t *testing.T) {
	esde, watch := newFakeESDE(7)
	watch.FolderChanged(FolderChange{Added: 1, Idle: true})
	watch.FolderChanged(FolderChange{Removed: 1, Idle: true})
	// A batch with nothing in it, a cancelled download say, says nothing.
	watch.FolderChanged(FolderChange{Idle: true})
	if got := esde.heard(); len(got) != 2 {
		t.Fatalf("batches %+v", got)
	}
	if n := watch.Notice(); n != (ESDENotice{Added: 1, Removed: 1}) {
		t.Fatalf("notice %+v", n)
	}
	esde.instance.Store(0)
	if n := watch.Notice(); n != (ESDENotice{}) {
		t.Fatalf("notice once closed %+v", n)
	}
}
