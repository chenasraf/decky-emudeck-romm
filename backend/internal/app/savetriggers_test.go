package app

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// fakeWatch reports the paths sent to it as written.
type fakeWatch struct {
	dirs    chan []string
	changes chan string
}

func newFakeWatch() *fakeWatch {
	return &fakeWatch{dirs: make(chan []string, 4), changes: make(chan string, 16)}
}

func (w *fakeWatch) Watch(dirs ...string) error {
	w.dirs <- dirs
	return nil
}

func (w *fakeWatch) Changes() <-chan string { return w.changes }

// run starts the automatic syncs with quick timings, and waits for the one at start to end.
func (f *saveFixture) run(t *testing.T, watch SaveWatch) {
	t.Helper()
	f.saves.settle, f.saves.pollEvery, f.saves.openGap = 20*time.Millisecond, 10*time.Millisecond, time.Hour
	if f.saves.pullEvery == savePullEvery {
		f.saves.pullEvery = time.Hour
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go f.saves.Run(ctx, watch)
	f.ended(t)
}

// ended waits for the next sync to end and returns its event.
func (f *saveFixture) ended(t *testing.T) SaveSyncEvent {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-f.events:
			if !e.Syncing {
				return e
			}
		case <-deadline:
			t.Fatal("no sync ended")
			return SaveSyncEvent{}
		}
	}
}

// quiet fails when a sync starts within d.
func (f *saveFixture) quiet(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case e := <-f.events:
		t.Fatalf("unexpected sync %+v", e)
	case <-time.After(d):
	}
}

// Every emulator whose saves sync has its saves folder watched.
func TestRunWatchesSaveFolders(t *testing.T) {
	f := newSaveFixture(t)
	w := newFakeWatch()
	f.run(t, w)
	want := []string{f.raSaves, filepath.Join(f.root, "saves", "melonds", "saves"), filepath.Join(f.root, "saves", "mgba", "saves"),
		filepath.Join(f.root, "saves", "RMG", "saves"), filepath.Join(f.root, "saves", "duckstation", "saves")}
	if dirs := <-w.dirs; !slices.Equal(dirs, want) {
		t.Fatalf("watching %v", dirs)
	}
}

func TestWrittenSaveUploads(t *testing.T) {
	f := newSaveFixture(t)
	w := newFakeWatch()
	f.run(t, w)

	f.writeSave(t, "saved in game", time.Now())
	// RetroArch writes in several steps; one sync follows once they stop.
	for range 3 {
		w.changes <- f.chronoSave()
	}
	e := f.ended(t)
	if !e.Auto || e.Summary.Uploaded != 1 || len(e.ROMIDs) != 1 || e.ROMIDs[0] != 1 {
		t.Fatalf("sync %+v", e)
	}
	f.quiet(t, 100*time.Millisecond)
}

// A file that isn't a game's save starts no sync.
func TestOtherFilesDontSync(t *testing.T) {
	f := newSaveFixture(t)
	w := newFakeWatch()
	f.run(t, w)
	w.changes <- f.chronoSave() + ".romm-part"
	f.quiet(t, 100*time.Millisecond)
}

// The plugin's own download lands in the watched folder without uploading it back.
func TestOwnDownloadDoesntUpload(t *testing.T) {
	f := newSaveFixture(t)
	f.romm.otherDevice(1, "snes9x", []byte("handheld progress"))
	w := newFakeWatch()
	f.run(t, w)
	if got := readFile(t, f.chronoSave()); got != "handheld progress" {
		t.Fatalf("save holds %q", got)
	}
	w.changes <- f.chronoSave()
	f.quiet(t, 100*time.Millisecond)
}

func TestWaitingDownloadSyncsOnceRetroArchCloses(t *testing.T) {
	f := newSaveFixture(t)
	f.romm.otherDevice(1, "snes9x", []byte("handheld progress"))
	f.retroarch.Store(true)
	f.run(t, nil)
	gone(t, f.chronoSave())

	f.retroarch.Store(false)
	if e := f.ended(t); e.Summary.Downloaded != 1 || !e.Auto {
		t.Fatalf("sync %+v", e)
	}
	if got := readFile(t, f.chronoSave()); got != "handheld progress" {
		t.Fatalf("save holds %q", got)
	}
	f.quiet(t, 100*time.Millisecond)
}

func TestPeriodicPull(t *testing.T) {
	f := newSaveFixture(t)
	f.saves.pullEvery = 30 * time.Millisecond
	f.run(t, nil)
	f.romm.otherDevice(1, "snes9x", []byte("handheld progress"))
	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-f.events:
			if !e.Syncing && e.Summary.Downloaded == 1 {
				return
			}
		case <-deadline:
			t.Fatal("no pull downloaded the save")
		}
	}
}

func TestOpeningTheQAMSyncsUnlessJustSynced(t *testing.T) {
	f := newSaveFixture(t)
	f.run(t, nil)
	f.saves.Opened()
	f.quiet(t, 100*time.Millisecond)

	f.saves.mu.Lock()
	f.saves.openGap = 0
	f.saves.mu.Unlock()
	f.saves.Opened()
	if e := f.ended(t); !e.Auto {
		t.Fatalf("sync %+v", e)
	}
}

// A sync nobody asked for that can't run, without a Device say, tells the frontend nothing.
func TestAutoSyncThatCantRunIsSilent(t *testing.T) {
	f := newSaveFixture(t)
	f.settings.mu.Lock()
	f.settings.in.DeviceID = ""
	f.settings.mu.Unlock()
	f.saves.request(nil, false)
	f.quiet(t, 100*time.Millisecond)

	// One the user asked for says why.
	f.saves.request(nil, true)
	if e := f.ended(t); e.Problem == nil {
		t.Fatalf("sync %+v", e)
	}
}

// Syncs asked for while one runs gather into one more, not one each.
func TestQueuedSyncsGather(t *testing.T) {
	f := newSaveFixture(t)
	f.saves.passMu.Lock()
	f.saves.request([]int{1}, true)
	if e := <-f.events; !e.Syncing {
		t.Fatalf("first %+v", e)
	}
	f.saves.request(nil, false)
	f.saves.request([]int{1}, false)
	f.saves.passMu.Unlock()

	if e := f.ended(t); e.Auto {
		t.Fatalf("first end %+v", e)
	}
	if e := f.ended(t); len(e.ROMIDs) != 0 {
		t.Fatalf("second covers %v, not every game", e.ROMIDs)
	}
	f.quiet(t, 100*time.Millisecond)
}
