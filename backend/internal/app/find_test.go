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

// findDone runs Find my games on the SNES platform and waits for it to finish.
func (f *downloadFixture) findDone(t *testing.T) FindStatus {
	t.Helper()
	ctx := context.Background()
	if _, err := f.downloads.FindGames(ctx, 1); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		s := f.downloads.FindStatus(ctx, 1)
		if !s.Running {
			if s.Problem != nil {
				t.Fatalf("find: %v", s.Problem)
			}
			return s
		}
		if time.Now().After(deadline) {
			t.Fatal("find didn't finish")
		}
		time.Sleep(time.Millisecond)
	}
}

// snesFile puts the user's own file, name holding data, in the SNES folder.
func (f *downloadFixture) snesFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(f.snes, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFindRenamedGameThenAdopt(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	f.server.files[1][0].SHA1 = sha1Hex(string(chrono))
	path := f.snesFile(t, "ct.sfc", chrono)
	f.snesFile(t, "not a game.sfc", []byte("something else"))

	s := f.findDone(t)
	if !s.Done || len(s.Found) != 1 || s.Found[0].ROMID != 1 || s.Found[0].Path != path || s.Found[0].Name != "Chrono Trigger" {
		t.Fatalf("status %+v", s)
	}
	// Nothing changes until the user says so: the game is On Deck (not managed), and the plugin
	// won't download a second copy of it.
	if _, owned, _ := f.owned.Owned(ctx, 1); owned {
		t.Fatal("finding made the game Plugin-owned")
	}
	if states, _ := f.downloads.States(ctx); states[1] != domain.StateOnDeck {
		t.Fatalf("states %v", states)
	}
	if _, err := f.downloads.Queue(ctx, 1); kindOf(err) != domain.KindTargetExists {
		t.Fatalf("queue: %v", err)
	}
	if c := f.checked(t, 1); !c.Matches || c.Path != path {
		t.Fatalf("check %+v", c)
	}

	if err := f.downloads.Adopt(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if rec, ok, _ := f.owned.Owned(ctx, 1); !ok || len(rec.Files) != 1 || rec.Files[0].Path != path || rec.Snapshot[0].FileID != 10 {
		t.Fatalf("record %+v", rec)
	}
	if s := f.downloads.FindStatus(ctx, 1); len(s.Found) != 0 {
		t.Fatalf("adopted game still listed: %+v", s.Found)
	}
}

func TestFindZippedAndFolderedCopies(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.server.files[1][0].SHA1 = sha1Hex(string(chrono))
	sum := md5.Sum(metroid) //nolint:gosec // see import
	f.server.files[2][0].MD5 = hex.EncodeToString(sum[:])
	zipped := f.snesFile(t, "sm.zip", zipOf(t, member{"sm.sfc", string(metroid)}))
	f.snesFile(t, "My Games/ct/ct.sfc", chrono)

	s := f.findDone(t)
	if len(s.Found) != 2 {
		t.Fatalf("found %+v", s.Found)
	}
	got := map[int]string{}
	for _, g := range s.Found {
		got[g.ROMID] = g.Path
	}
	if got[1] != filepath.Join(f.snes, "My Games") || got[2] != zipped {
		t.Fatalf("found %+v", got)
	}
}

// A game kept under RomM's own name is found too when its content matches, so a whole library
// copied over from elsewhere can be adopted at once. One that only shares the name isn't.
func TestFindSameNamedGamesThatMatch(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	f.server.files[1][0].SHA1 = sha1Hex(string(chrono))
	f.server.files[2][0].SHA1 = sha1Hex(string(metroid))
	chronoPath := f.userFile(t, 1, "Chrono Trigger (USA).sfc", "Chrono Trigger (USA).sfc", chrono)
	f.userFile(t, 2, "Super Metroid (USA).sfc", "Super Metroid (USA).sfc", []byte("a hack, not the game"))

	s := f.findDone(t)
	if len(s.Found) != 1 || s.Found[0].ROMID != 1 || s.Found[0].Path != chronoPath {
		t.Fatalf("found %+v", s.Found)
	}
	if err := f.downloads.Adopt(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if _, owned, _ := f.owned.Owned(ctx, 1); !owned {
		t.Fatal("not adopted")
	}
	if _, err := f.downloads.FindGames(ctx, 2); kindOf(err) != domain.KindCantDownload {
		t.Fatalf("platform without a folder: %v", err)
	}
}

// A second copy of a game is no second find: the one where the game belongs is the one found, even
// when another sorts first.
func TestFindPrefersTheCopyWhereTheGameBelongs(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.server.files[1][0].SHA1 = sha1Hex(string(chrono))
	f.snesFile(t, "A copy.sfc", chrono)
	chronoPath := f.userFile(t, 1, "Chrono Trigger (USA).sfc", "Chrono Trigger (USA).sfc", chrono)

	if s := f.findDone(t); len(s.Found) != 1 || s.Found[0].Path != chronoPath {
		t.Fatalf("found %+v", s.Found)
	}
}

func TestAdoptEveryFoundGame(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	f.server.files[1][0].SHA1 = sha1Hex(string(chrono))
	sum := md5.Sum(metroid) //nolint:gosec // see import
	f.server.files[2][0].MD5 = hex.EncodeToString(sum[:])
	f.userFile(t, 1, "Chrono Trigger (USA).sfc", "Chrono Trigger (USA).sfc", chrono)
	f.snesFile(t, "sm.zip", zipOf(t, member{"sm.sfc", string(metroid)}))
	f.findDone(t)

	adopted, failed := f.downloads.AdoptFound(ctx, 1)
	if adopted != 2 || len(failed) != 0 {
		t.Fatalf("adopted %d, failed %v", adopted, failed)
	}
	for _, id := range []int{1, 2} {
		if _, owned, _ := f.owned.Owned(ctx, id); !owned {
			t.Fatalf("game %d not adopted", id)
		}
	}
	if s := f.downloads.FindStatus(ctx, 1); len(s.Found) != 0 || s.Adopting || s.Adopted != 2 || s.AdoptTotal != 2 {
		t.Fatalf("status %+v", s)
	}
}

func TestFoundAndLinkedStaysOnDeck(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	f.server.files[1][0].SHA1 = sha1Hex(string(chrono))
	path := f.snesFile(t, "ct.sfc", chrono)
	f.findDone(t)

	if err := f.downloads.Link(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if g, ok, _ := f.owned.Linked(ctx, 1); !ok || g.Path != path {
		t.Fatalf("link %+v", g)
	}
	// What Find my games found is gone once the backend restarts; the link still holds the file.
	f.downloads.mu.Lock()
	clear(f.downloads.found)
	f.downloads.mu.Unlock()
	if states, _ := f.downloads.States(ctx); states[1] != domain.StateOnDeck {
		t.Fatalf("states %v", states)
	}
	if _, err := f.downloads.Queue(ctx, 1); kindOf(err) != domain.KindTargetExists {
		t.Fatalf("queue: %v", err)
	}
}
