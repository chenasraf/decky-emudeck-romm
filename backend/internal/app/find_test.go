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
	return f.findWith(t, func(ctx context.Context) (FindStatus, error) { return f.downloads.FindGames(ctx, 1, false) })
}

// findWith starts a search of the SNES platform with start and waits for it to finish.
func (f *downloadFixture) findWith(t *testing.T, start func(context.Context) (FindStatus, error)) FindStatus {
	t.Helper()
	ctx := context.Background()
	if _, err := start(ctx); err != nil {
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
	if _, err := f.downloads.FindGames(ctx, 2, false); kindOf(err) != domain.KindCantDownload {
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

// A disc image often has audio tracks with the same content. Each of RomM's tracks is matched to
// its own file, the one of the same name, so adopting never records one file twice.
func TestFindGivesIdenticalTracksTheirOwnFiles(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	silence := []byte("the same audio track")
	f.server.mu.Lock()
	f.server.files[1] = []domain.ROMFile{
		{ID: 10, Name: "ct.cue", Path: "ct.cue", SizeBytes: 3, Category: "game", SHA1: sha1Hex("cue")},
		{ID: 12, Name: "ct (Track 2).bin", Path: "ct (Track 2).bin", SizeBytes: int64(len(silence)), Category: "game", SHA1: sha1Hex(string(silence))},
		{ID: 14, Name: "ct (Track 4).bin", Path: "ct (Track 4).bin", SizeBytes: int64(len(silence)), Category: "game", SHA1: sha1Hex(string(silence))},
	}
	f.server.mu.Unlock()
	f.snesFile(t, "Chrono/ct.cue", []byte("cue"))
	track2 := f.snesFile(t, "Chrono/ct (Track 2).bin", silence)
	track4 := f.snesFile(t, "Chrono/ct (Track 4).bin", silence)

	if s := f.findDone(t); len(s.Found) != 1 || s.Found[0].ROMID != 1 {
		t.Fatalf("found %+v", s.Found)
	}
	if err := f.downloads.Adopt(ctx, 1); err != nil {
		t.Fatal(err)
	}
	rec, _, _ := f.owned.Owned(ctx, 1)
	paths := map[int]string{}
	for i, p := range rec.Files {
		paths[rec.Snapshot[i].FileID] = p.Path
	}
	if paths[12] != track2 || paths[14] != track4 {
		t.Fatalf("files %v", paths)
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

// RomM hashes a game it keeps as an archive by a file inside it, of a size it doesn't give. Only a
// full search finds that file extracted, and since the rest of the archive's files are unknown,
// the game can be linked but not adopted.
func TestFullSearchFindsAnArchivedGameExtracted(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	f.server.mu.Lock()
	f.server.files[1][0] = domain.ROMFile{ID: 10, Name: "Chrono Trigger (USA).7z", SizeBytes: 3, Category: "game", SHA1: sha1Hex(string(chrono))}
	f.server.mu.Unlock()
	f.snesFile(t, "Chrono/ct.sfc", chrono)
	f.snesFile(t, "Chrono/readme.txt", []byte("notes"))
	folder := filepath.Join(f.snes, "Chrono")

	if s := f.findDone(t); len(s.Found) != 0 {
		t.Fatalf("a search by size found %+v", s.Found)
	}
	s := f.findWith(t, func(ctx context.Context) (FindStatus, error) { return f.downloads.FindGames(ctx, 1, true) })
	if !s.Full || len(s.Found) != 1 || s.Found[0].ROMID != 1 || s.Found[0].Path != folder || !s.Found[0].LinkOnly {
		t.Fatalf("status %+v", s)
	}
	if c := f.checked(t, 1); c.Matches || c.Why != whyLinkOnly {
		t.Fatalf("check %+v", c)
	}
	if err := f.downloads.Adopt(ctx, 1); kindOf(err) != domain.KindInvalidInput {
		t.Fatalf("adopt: %v", err)
	}
	if adopted, failed := f.downloads.AdoptFound(ctx, 1); adopted != 0 || len(failed) != 0 {
		t.Fatalf("adopt all: %d, %v", adopted, failed)
	}
	if err := f.downloads.Link(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if g, ok, _ := f.owned.Linked(ctx, 1); !ok || g.Path != folder {
		t.Fatalf("link %+v", g)
	}
}

// A search for one game looks for it alone, and leaves what earlier searches found of the others.
func TestFindOneGame(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.server.files[1][0].SHA1 = sha1Hex(string(chrono))
	f.server.mu.Lock()
	f.server.files[2][0] = domain.ROMFile{ID: 20, Name: "Super Metroid (USA).zip", SizeBytes: 3, Category: "game", SHA1: sha1Hex(string(metroid))}
	f.server.mu.Unlock()
	chronoPath := f.snesFile(t, "ct.sfc", chrono)
	smPath := f.snesFile(t, "sm/sm.sfc", metroid)

	if s := f.findDone(t); len(s.Found) != 1 || s.Found[0].Path != chronoPath {
		t.Fatalf("found %+v", s.Found)
	}
	s := f.findWith(t, func(ctx context.Context) (FindStatus, error) { return f.downloads.FindGame(ctx, 2) })
	if s.ROMID != 2 || !s.Full || s.Total != 1 {
		t.Fatalf("status %+v", s)
	}
	got := map[int]string{}
	for _, g := range s.Found {
		got[g.ROMID] = g.Path
	}
	if len(got) != 2 || got[1] != chronoPath || got[2] != filepath.Dir(smPath) {
		t.Fatalf("found %+v", got)
	}
}
