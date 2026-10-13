package app

import (
	"context"
	"crypto/md5" //nolint:gosec // matches the code under test
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// userFile names ROM id's file romFile in the Catalog and puts the user's own file, name holding
// data, in the SNES folder.
func (f *downloadFixture) userFile(t *testing.T, id int, romFile, name string, data []byte) string {
	t.Helper()
	f.catalog.mu.Lock()
	r := f.catalog.roms[id]
	r.FileName = romFile
	f.catalog.roms[id] = r
	f.catalog.mu.Unlock()
	if err := os.MkdirAll(f.snes, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.snes, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// checked waits for the check of id to finish hashing.
func (f *downloadFixture) checked(t *testing.T, id int) OnDeckCheck {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		c, err := f.downloads.CheckOnDeck(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if !c.Checking {
			return c
		}
		if time.Now().After(deadline) {
			t.Fatal("check didn't finish")
		}
	}
}

func TestOnDeckIsNeverDownloaded(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	f.userFile(t, 1, "Chrono Trigger (USA).sfc", "Chrono Trigger (USA).sfc", []byte("the user's own dump"))

	if states, _ := f.downloads.States(ctx); states[1] != domain.StateOnDeck || domain.StateOf(states, 2) != domain.StateOnRomM {
		t.Fatalf("states %v", states)
	}
	if _, err := f.downloads.Queue(ctx, 1); kindOf(err) != domain.KindTargetExists {
		t.Fatalf("queue: %v", err)
	}
	plan, err := f.downloads.PlanAll(ctx, Batch{PlatformID: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range plan.Games {
		if r.ID == 1 {
			t.Fatal("download all counts a game already on this Deck")
		}
	}
	if len(f.server.froms) != 0 {
		t.Fatal("downloaded over the user's file")
	}
}

func TestAdoptMatchingFileThenDelete(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	path := f.userFile(t, 1, "Chrono Trigger (USA).sfc", "Chrono Trigger (USA).sfc", chrono)
	f.server.files[1][0].SHA1 = sha1Hex(string(chrono))

	c := f.checked(t, 1)
	if !c.Matches || c.Path != path || c.Why != "" {
		t.Fatalf("check %+v", c)
	}
	if err := f.downloads.Adopt(ctx, 1); err != nil {
		t.Fatal(err)
	}
	rec, ok, _ := f.owned.Owned(ctx, 1)
	if !ok || len(rec.Files) != 1 || rec.Files[0].Path != path || rec.Files[0].SHA1 != sha1Hex(string(chrono)) ||
		len(rec.Snapshot) != 1 || rec.Snapshot[0].FileID != 10 {
		t.Fatalf("record %+v", rec)
	}
	if states, _ := f.downloads.States(ctx); states[1] != domain.StateInstalled {
		t.Fatalf("states %v", states)
	}

	// Adopted, it's the plugin's to delete like a game it downloaded.
	if _, err := f.downloads.Delete(ctx, []int{1}, 0); err != nil {
		t.Fatal(err)
	}
	if u := f.deleted(t, 1); u.Problem != nil {
		t.Fatalf("delete: %+v", u)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file still there: %v", err)
	}
}

func TestAdoptZippedCopy(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	// RomM hashes a zipped ROM's contents, and the user keeps the same ROM zipped.
	zipped := zipOf(t, member{"Super Metroid (USA).sfc", string(metroid)})
	path := f.userFile(t, 2, "Super Metroid (USA).sfc", "Super Metroid (USA).zip", zipped)
	sum := md5.Sum(metroid) //nolint:gosec // see import
	f.server.files[2][0].MD5 = hex.EncodeToString(sum[:])

	if c := f.checked(t, 2); !c.Matches || c.Path != path {
		t.Fatalf("check %+v", c)
	}
	if err := f.downloads.Adopt(ctx, 2); err != nil {
		t.Fatal(err)
	}
	// The record is of the zip as it is on disk, which deleting checks against.
	if rec, _, _ := f.owned.Owned(ctx, 2); len(rec.Files) != 1 || rec.Files[0].Path != path || rec.Files[0].SHA1 != sha1Hex(string(zipped)) {
		t.Fatalf("record %+v", rec)
	}
}

func TestDifferentFileCanOnlyBeLinked(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	path := f.userFile(t, 1, "Chrono Trigger (USA).sfc", "Chrono Trigger (USA).sfc", []byte("a romhack"))
	f.server.files[1][0].SHA1 = sha1Hex(string(chrono))

	if c := f.checked(t, 1); c.Matches || c.Why == "" || c.Linked {
		t.Fatalf("check %+v", c)
	}
	if err := f.downloads.Adopt(ctx, 1); kindOf(err) != domain.KindInvalidInput {
		t.Fatalf("adopt: %v", err)
	}
	if err := f.downloads.Link(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if g, ok, _ := f.owned.Linked(ctx, 1); !ok || g.Path != path || g.PlatformID != 1 {
		t.Fatalf("link %+v, %v", g, ok)
	}
	if c := f.checked(t, 1); !c.Linked {
		t.Fatalf("check after link %+v", c)
	}
	// Linked, the file is still the user's: never owned, never downloaded over.
	if states, _ := f.downloads.States(ctx); states[1] != domain.StateOnDeck {
		t.Fatalf("states %v", states)
	}
	if _, owned, _ := f.owned.Owned(ctx, 1); owned {
		t.Fatal("linking made the file Plugin-owned")
	}
	if data, _ := os.ReadFile(path); string(data) != "a romhack" {
		t.Fatalf("file changed: %q", data)
	}
}

func TestAdoptRefusesFileChangedSinceCheck(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	path := f.userFile(t, 1, "Chrono Trigger (USA).sfc", "Chrono Trigger (USA).sfc", chrono)
	f.server.files[1][0].SHA1 = sha1Hex(string(chrono))
	if c := f.checked(t, 1); !c.Matches {
		t.Fatalf("check %+v", c)
	}
	if err := os.WriteFile(path, []byte("edited since"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A new size is a stale check; the next one hashes anew and finds it different.
	if err := f.downloads.Adopt(ctx, 1); err == nil {
		t.Fatal("adopted a file that changed since its check")
	}
	if _, owned, _ := f.owned.Owned(ctx, 1); owned {
		t.Fatal("recorded the changed file")
	}
}
