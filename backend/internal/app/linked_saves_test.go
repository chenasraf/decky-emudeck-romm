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

// linkMetroid links the user's own copy of Super Metroid to RomM, and returns where its save is.
func (f *saveFixture) linkMetroid(t *testing.T) (string, string) {
	t.Helper()
	rom := filepath.Join(f.snes, "Super Metroid (Japan, USA).sfc")
	writeFile(t, rom, []byte("the user's own dump"))
	if err := f.owned.SaveLinked(context.Background(), domain.LinkedGame{ROMID: 2, PlatformID: 1, Path: rom, LinkedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return rom, filepath.Join(f.raSaves, "Super Metroid (Japan, USA).srm")
}

func TestLinkedGameSavesSync(t *testing.T) {
	f := newSaveFixture(t)
	_, save := f.linkMetroid(t)
	if st := f.statusOf(t, 2); !st.Syncs || st.Path != save {
		t.Fatalf("status %+v", st)
	}
	writeFile(t, save, []byte("ceres station"))
	// A sync of every game covers it alongside the downloaded ones.
	s, err := f.saves.Sync(context.Background(), nil)
	if err != nil || s.Uploaded != 1 {
		t.Fatalf("summary %+v %v", s, err)
	}
	if head, ok := f.romm.head(2); !ok || string(head.data) != "ceres station" {
		t.Fatal("not uploaded")
	}
}

func TestUnlinkStopsSyncAndLeavesTheFile(t *testing.T) {
	f := newSaveFixture(t)
	rom, save := f.linkMetroid(t)
	writeFile(t, save, []byte("ceres station"))
	if err := f.downloads.Unlink(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, rom); got != "the user's own dump" {
		t.Fatalf("file holds %q", got)
	}
	if got := readFile(t, save); got != "ceres station" {
		t.Fatalf("save holds %q", got)
	}
	if st := f.statusOf(t, 2); st.Syncs || !strings.Contains(st.Why, "linked") {
		t.Fatalf("status %+v", st)
	}
	if s, err := f.saves.Sync(context.Background(), nil); err != nil || s.Uploaded != 0 {
		t.Fatalf("summary %+v %v", s, err)
	}
	if err := f.downloads.Unlink(context.Background(), 2); err == nil {
		t.Fatal("unlinked a game that isn't linked")
	}
}

// A linked file that went stops its game's saves from syncing, and says so.
func TestLinkedFileGone(t *testing.T) {
	f := newSaveFixture(t)
	rom, _ := f.linkMetroid(t)
	if err := os.Remove(rom); err != nil {
		t.Fatal(err)
	}
	if st := f.statusOf(t, 2); st.Syncs || !strings.Contains(st.Why, "isn't there") {
		t.Fatalf("status %+v", st)
	}
}
