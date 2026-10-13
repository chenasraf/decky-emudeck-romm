package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// waiting waits for the download of id to start waiting for a game to close.
func (f *downloadFixture) waiting(t *testing.T, id int) {
	t.Helper()
	for {
		select {
		case u := <-f.updates:
			if u.ROMID == id && u.WaitingForGame {
				return
			}
			if u.ROMID == id && (u.State == domain.StateInstalled || u.State == domain.StateFailed) {
				t.Fatalf("ended without waiting: %+v", u)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("download %d never waited", id)
		}
	}
}

func TestExtractionWaitsForGameToClose(t *testing.T) {
	f := newDownloadFixture(t, 1)
	f.serve(5, "Ridge Racer.zip", zipOf(t, member{"Ridge Racer.cue", "cue"}, member{"Ridge Racer.bin", "bin"}))
	f.playing.Store(true)

	for _, id := range []int{5, 1} {
		if _, err := f.downloads.Queue(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}
	f.waiting(t, 5)
	if got := listDir(t, filepath.Join(f.root, "roms", "psx")); len(got) != 0 {
		t.Fatalf("extracted while playing: %v", got)
	}

	// The waiting download gave up its slot, so the next one transferred and placed a game that
	// needs no extracting.
	if u := f.end(t, 1); u.State != domain.StateInstalled {
		t.Fatalf("chrono: %+v", u)
	}
	if !f.downloadState(5).WaitingForGame {
		t.Fatal("stopped waiting while the game runs")
	}

	f.playing.Store(false)
	if u := f.end(t, 5); u.State != domain.StateInstalled || u.WaitingForGame {
		t.Fatalf("ridge racer: %+v", u)
	}
	if got := listDir(t, filepath.Join(f.root, "roms", "psx")); len(got) != 2 {
		t.Fatalf("psx: %v", got)
	}
	f.stagingEmpty(t)
}

func TestExtractionGoesAheadWithSettingOff(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.serve(5, "Ridge Racer.zip", zipOf(t, member{"Ridge Racer.cue", "cue"}, member{"Ridge Racer.bin", "bin"}))
	f.playing.Store(true)
	f.gaming.wait.Store(false)

	f.installed(t, 5)
}

func TestTurningSettingOffReleasesWait(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.serve(5, "Ridge Racer.zip", zipOf(t, member{"Ridge Racer.cue", "cue"}, member{"Ridge Racer.bin", "bin"}))
	f.playing.Store(true)

	if _, err := f.downloads.Queue(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	f.waiting(t, 5)
	if err := f.games.SetWaitForGames(false); err != nil {
		t.Fatal(err)
	}
	if u := f.end(t, 5); u.State != domain.StateInstalled {
		t.Fatalf("end: %+v", u)
	}
}

func TestCancelWhileWaitingForGame(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.serve(5, "Ridge Racer.zip", zipOf(t, member{"Ridge Racer.cue", "cue"}, member{"Ridge Racer.bin", "bin"}))
	f.playing.Store(true)

	if _, err := f.downloads.Queue(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	f.waiting(t, 5)
	f.downloads.Cancel(5)
	if u := f.end(t, 5); u.State != domain.StateOnRomM {
		t.Fatalf("end: %+v", u)
	}
	f.stagingEmpty(t)

	// The slot it gave up while waiting came back, so the queue still runs.
	if _, err := f.downloads.Queue(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if u := f.end(t, 1); u.State != domain.StateInstalled {
		t.Fatalf("chrono: %+v", u)
	}
}

func TestGameNeedingNoExtractionDoesntWait(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.playing.Store(true)

	f.installed(t, 1)
}

// downloadState returns the download of id as the queue lists it.
func (f *downloadFixture) downloadState(id int) Download {
	for _, d := range f.downloads.List() {
		if d.ROMID == id {
			return d
		}
	}
	return Download{}
}
