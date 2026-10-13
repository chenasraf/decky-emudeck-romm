package system

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFolderWatch(t *testing.T) {
	dir, other := t.TempDir(), t.TempDir()
	w, err := WatchFolders(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	if err := w.Watch(dir); err != nil {
		t.Fatal(err)
	}

	save := filepath.Join(dir, "Chrono Trigger.srm")
	if err := os.WriteFile(save, []byte("sram"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-w.Changes():
		if filepath.Base(got) != "Chrono Trigger.srm" {
			t.Fatalf("got %s", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no change reported")
	}

	// Watching another folder instead reports that one.
	if err := w.Watch(other); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "Metroid.srm"), []byte("sram"), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case got := <-w.Changes():
			if filepath.Base(got) == "Metroid.srm" {
				return
			}
		case <-deadline:
			t.Fatal("no change reported in the folder watched instead")
		}
	}
}
