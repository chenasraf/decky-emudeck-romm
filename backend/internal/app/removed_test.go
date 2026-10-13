package app

import (
	"context"
	"slices"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// reimport moves a ROM's files on the server to a new id, as re-importing it on RomM does.
func (f *downloadFixture) reimport(from, to int) {
	f.server.mu.Lock()
	defer f.server.mu.Unlock()
	files := slices.Clone(f.server.files[from])
	for i := range files {
		files[i].ID += to * 100
	}
	delete(f.server.files, from)
	f.server.files[to] = files
}

// present is every id the server has files for.
func (f *downloadFixture) present() map[int]bool {
	f.server.mu.Lock()
	defer f.server.mu.Unlock()
	out := map[int]bool{}
	for id := range f.server.files {
		out[id] = true
	}
	return out
}

func TestReimportedGameKeepsItsRecord(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	f.serve(1, "Chrono Trigger (USA).sfc", chrono)
	f.server.files[1][0].SHA1 = sha1Hex(string(chrono))
	path := f.installed(t, 1).Files[0].Path
	f.reimport(1, 30)

	removed, err := f.downloads.Rematch(ctx, f.present(), map[int]bool{1: true})
	if err != nil || len(removed) != 0 {
		t.Fatalf("removed %v, %v", removed, err)
	}
	if _, ok, _ := f.owned.Owned(ctx, 1); ok {
		t.Fatal("record still under the old id")
	}
	rec, ok, _ := f.owned.Owned(ctx, 30)
	if !ok || len(rec.Files) != 1 || rec.Files[0].Path != path || len(rec.Snapshot) != 1 || rec.Snapshot[0].FileID != 3010 {
		t.Fatalf("record %+v", rec)
	}
}

func TestDeletedGameIsRemovedFromRomM(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	f.installedChrono(t)
	f.server.mu.Lock()
	delete(f.server.files, 1)
	f.server.mu.Unlock()

	for _, changed := range []map[int]bool{{1: true}, {}} {
		removed, err := f.downloads.Rematch(ctx, f.present(), changed)
		if err != nil || !slices.Equal(removed, []int{1}) {
			t.Fatalf("removed %v, %v", removed, err)
		}
	}
	// It's still the plugin's to delete.
	if _, ok, _ := f.owned.Owned(ctx, 1); !ok {
		t.Fatal("record dropped")
	}
}

func TestReimportedLinkedGameKeepsItsLink(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	f.server.files[1][0].SHA1 = sha1Hex(string(chrono))
	path := f.snesFile(t, "ct.sfc", chrono)
	f.findDone(t)
	if err := f.downloads.Link(ctx, 1); err != nil {
		t.Fatal(err)
	}
	f.reimport(1, 30)

	removed, err := f.downloads.Rematch(ctx, f.present(), map[int]bool{1: true})
	if err != nil || len(removed) != 0 {
		t.Fatalf("removed %v, %v", removed, err)
	}
	if g, ok, _ := f.owned.Linked(ctx, 30); !ok || g.Path != path {
		t.Fatalf("link %+v", g)
	}
	if _, ok, _ := f.owned.Linked(ctx, 1); ok {
		t.Fatal("link still under the old id")
	}
}

func TestRemovedGameCantBeDownloaded(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.catalog.mu.Lock()
	r := f.catalog.roms[2]
	r.Removed = true
	f.catalog.roms[2] = r
	f.catalog.mu.Unlock()

	if _, err := f.downloads.Queue(context.Background(), 2); kindOf(err) != domain.KindNotFound {
		t.Fatalf("queue: %v", err)
	}
}
