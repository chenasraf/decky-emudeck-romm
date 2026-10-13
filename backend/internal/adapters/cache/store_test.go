package cache

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func openStore(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(context.Background(), path, discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func rom(id, platform int, name string) domain.ROM {
	return domain.ROM{ID: id, PlatformID: platform, Name: name, FileName: name + ".gba", Extension: "gba",
		SizeBytes: 1024, Regions: []string{"USA"}, UpdatedAt: time.Unix(int64(1000+id), 0).UTC()}
}

var platforms = []domain.Platform{{ID: 1, Slug: "gba", Name: "Game Boy Advance", ROMCount: 3}, {ID: 2, Slug: "nes", Name: "NES", ROMCount: 0}}

func TestFullThenDeltaSync(t *testing.T) {
	ctx := context.Background()
	s := openStore(t, filepath.Join(t.TempDir(), "cache.db"))

	if c, at, err := s.SyncState(ctx); err != nil || !c.IsZero() || !at.IsZero() {
		t.Fatalf("empty store: %v %v %v", c, at, err)
	}

	at := time.Unix(5000, 0).UTC()
	if err := s.Apply(ctx, domain.CatalogSync{
		Platforms: platforms,
		ROMs:      []domain.ROM{rom(1, 1, "zelda"), rom(2, 1, "Advance Wars"), rom(3, 1, "metroid")},
		Full:      true,
		Cursor:    time.Unix(1003, 0),
		At:        at,
	}); err != nil {
		t.Fatal(err)
	}

	// Platforms without games are left out.
	ps, err := s.Platforms(ctx)
	if err != nil || len(ps) != 1 || ps[0].Games != 3 || ps[0].Name != "Game Boy Advance" {
		t.Fatalf("platforms %+v, %v", ps, err)
	}
	page, err := s.ROMs(ctx, 1, nil, domain.SortName, 0, 2)
	if err != nil || page.Total != 3 || len(page.ROMs) != 2 || page.ROMs[0].Name != "Advance Wars" || page.ROMs[1].Name != "metroid" {
		t.Fatalf("first page %+v, %v", page, err)
	}
	if page, _ := s.ROMs(ctx, 1, nil, domain.SortName, 2, 2); len(page.ROMs) != 1 || page.ROMs[0].Name != "zelda" {
		t.Fatalf("second page %+v", page)
	}
	if page, _ := s.ROMs(ctx, 1, &domain.IDFilter{IDs: []int{3, 99}}, domain.SortName, 0, 10); page.Total != 1 || page.ROMs[0].Name != "metroid" {
		t.Fatalf("only ids %+v", page)
	}
	if page, _ := s.ROMs(ctx, 1, &domain.IDFilter{IDs: []int{3}, Exclude: true}, domain.SortName, 0, 10); page.Total != 2 {
		t.Fatalf("excluding ids %+v", page)
	}
	if page, _ := s.ROMs(ctx, 1, &domain.IDFilter{Exclude: true}, domain.SortName, 0, 10); page.Total != 3 {
		t.Fatalf("excluding no ids %+v", page)
	}
	if page, _ := s.ROMs(ctx, 1, &domain.IDFilter{}, domain.SortName, 0, 10); page.Total != 0 {
		t.Fatalf("only no ids %+v", page)
	}
	if counts, err := s.CountByPlatform(ctx, []int{1, 3, 99}); err != nil || counts[1] != 2 || len(counts) != 1 {
		t.Fatalf("count by platform %v, %v", counts, err)
	}

	// A delta renames one ROM, adds one, and the id list drops metroid.
	renamed := rom(1, 1, "The Legend of Zelda")
	if err := s.Apply(ctx, domain.CatalogSync{
		Platforms: platforms,
		ROMs:      []domain.ROM{renamed, rom(4, 1, "Golden Sun")},
		KeepIDs:   []int{1, 2, 4},
		Cursor:    time.Unix(1004, 0),
		At:        at.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.ROM(ctx, 3); ok {
		t.Fatal("deleted ROM still there")
	}
	got, ok, err := s.ROM(ctx, 1)
	if err != nil || !ok || got.Name != "The Legend of Zelda" || got.Regions[0] != "USA" || !got.UpdatedAt.Equal(renamed.UpdatedAt) {
		t.Fatalf("renamed %+v %v %v", got, ok, err)
	}
	if p, r, _ := s.Counts(ctx); p != 1 || r != 3 {
		t.Fatalf("counts %d %d", p, r)
	}
	if c, a, _ := s.SyncState(ctx); !c.Equal(time.Unix(1004, 0)) || !a.Equal(at.Add(time.Minute)) {
		t.Fatalf("sync state %v %v", c, a)
	}
}

func TestRetainedROMsStayMarkedRemoved(t *testing.T) {
	ctx := context.Background()
	s := openStore(t, filepath.Join(t.TempDir(), "cache.db"))
	nes := rom(5, 2, "Contra")
	if err := s.Apply(ctx, domain.CatalogSync{
		Platforms: platforms, ROMs: []domain.ROM{rom(1, 1, "zelda"), rom(2, 1, "metroid"), nes}, Full: true,
	}); err != nil {
		t.Fatal(err)
	}

	// The server drops metroid, Contra and the NES; this Deck holds records of both games.
	if err := s.Apply(ctx, domain.CatalogSync{Platforms: platforms[:1], KeepIDs: []int{1}, Retain: []int{2, 5}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{2, 5} {
		if r, ok, err := s.ROM(ctx, id); err != nil || !ok || !r.Removed {
			t.Fatalf("retained %d: %+v %v %v", id, r, ok, err)
		}
	}
	if r, _, _ := s.ROM(ctx, 1); r.Removed {
		t.Fatal("kept ROM marked removed")
	}
	if p, ok, _ := s.Platform(ctx, 2); !ok || p.Name != "NES" {
		t.Fatalf("platform of a removed game %+v", p)
	}

	// A full sync keeps them too, and a game back on the server is no longer removed.
	if err := s.Apply(ctx, domain.CatalogSync{Platforms: platforms, ROMs: []domain.ROM{rom(1, 1, "zelda"), nes}, Full: true, Retain: []int{2}}); err != nil {
		t.Fatal(err)
	}
	if r, ok, _ := s.ROM(ctx, 2); !ok || !r.Removed {
		t.Fatalf("after full sync %+v", r)
	}
	if r, _, _ := s.ROM(ctx, 5); r.Removed {
		t.Fatal("ROM back on the server still removed")
	}

	// Once nothing holds a record of it, it goes.
	if err := s.Apply(ctx, domain.CatalogSync{Platforms: platforms, KeepIDs: []int{1, 5}}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.ROM(ctx, 2); ok {
		t.Fatal("unrecorded removed ROM kept")
	}
}

func TestCollections(t *testing.T) {
	ctx := context.Background()
	s := openStore(t, filepath.Join(t.TempDir(), "cache.db"))
	fav := domain.CollectionRef{Kind: domain.CollectionRegular, ID: 1}
	smart := domain.CollectionRef{Kind: domain.CollectionSmart, ID: 1}
	sync := domain.CatalogSync{
		Platforms: platforms,
		ROMs:      []domain.ROM{rom(1, 1, "zelda"), rom(2, 2, "Advance Wars"), rom(3, 1, "metroid")},
		Collections: []domain.Collection{
			{CollectionRef: smart, Name: "Adventure", ROMIDs: []int{1}},
			// 99 isn't in the library.
			{CollectionRef: fav, Name: "Favorites", Favorite: true, ROMIDs: []int{1, 2, 99}},
		},
		Full: true,
	}
	if err := s.Apply(ctx, sync); err != nil {
		t.Fatal(err)
	}

	cols, err := s.Collections(ctx)
	if err != nil || len(cols) != 2 {
		t.Fatalf("collections %+v, %v", cols, err)
	}
	// Favorites first; ROMs by name, across platforms, only those the library holds.
	if cols[0].CollectionRef != fav || !cols[0].Favorite || len(cols[0].ROMIDs) != 2 || cols[0].ROMIDs[0] != 2 || cols[0].ROMIDs[1] != 1 {
		t.Fatalf("favorites %+v", cols[0])
	}
	if c, ok, err := s.Collection(ctx, smart); err != nil || !ok || c.Name != "Adventure" || len(c.ROMIDs) != 1 {
		t.Fatalf("smart %+v %v %v", c, ok, err)
	}
	// Platform zero lists every platform.
	if page, _ := s.ROMs(ctx, 0, &domain.IDFilter{IDs: cols[0].ROMIDs}, domain.SortName, 0, 10); page.Total != 2 || page.ROMs[0].Name != "Advance Wars" {
		t.Fatalf("collection page %+v", page)
	}

	// Each sync replaces the collections.
	sync.Collections, sync.Full = nil, false
	if err := s.Apply(ctx, sync); err != nil {
		t.Fatal(err)
	}
	if cols, _ := s.Collections(ctx); len(cols) != 0 {
		t.Fatalf("after replace %+v", cols)
	}
	if _, ok, _ := s.Collection(ctx, fav); ok {
		t.Fatal("collection kept")
	}
}

func TestSearch(t *testing.T) {
	ctx := context.Background()
	s := openStore(t, filepath.Join(t.TempDir(), "cache.db"))
	odd := rom(5, 2, "100% Pure_Fun")
	odd.FileName = "pure.nes"
	if err := s.Apply(ctx, domain.CatalogSync{
		Platforms: platforms,
		ROMs: []domain.ROM{
			rom(1, 1, "Golden Sun: The Lost Age"), rom(2, 1, "Metroid Fusion"), rom(3, 1, "Golden Sun"),
			rom(4, 2, "Sunset Riders"), odd,
		},
		Full: true,
	}); err != nil {
		t.Fatal(err)
	}

	names := func(term string, limit int) []string {
		t.Helper()
		roms, err := s.Search(ctx, domain.SearchQuery{Term: term, Limit: limit})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range roms {
			out = append(out, r.Name)
		}
		return out
	}
	for _, tc := range []struct {
		term  string
		limit int
		want  []string
	}{
		// Across platforms; names that start with the term first.
		{"sun", 10, []string{"Sunset Riders", "Golden Sun", "Golden Sun: The Lost Age"}},
		{"golden SUN", 10, []string{"Golden Sun", "Golden Sun: The Lost Age"}},
		// Every word must match, in any order.
		{"lost golden", 10, []string{"Golden Sun: The Lost Age"}},
		{"fusion.gba", 10, []string{"Metroid Fusion"}},
		{"sun", 1, []string{"Sunset Riders"}},
		// LIKE's wildcards are plain text.
		{"100%", 10, []string{"100% Pure_Fun"}},
		{"100_", 10, nil},
		{"   ", 10, nil},
	} {
		if got := names(tc.term, tc.limit); !slices.Equal(got, tc.want) {
			t.Errorf("search %q: got %q, want %q", tc.term, got, tc.want)
		}
	}

	// Narrowed to one platform, Sunset Riders (platform 2) drops out.
	inGBA, err := s.Search(ctx, domain.SearchQuery{Term: "sun", PlatformID: 1, Limit: 10})
	if err != nil || len(inGBA) != 2 || inGBA[0].Name != "Golden Sun" {
		t.Fatalf("one platform: %+v, %v", inGBA, err)
	}

	got, err := s.ROMsByID(ctx, []int{3, 99, 1})
	if err != nil || len(got) != 2 || got[0].ID != 3 || got[1].ID != 1 {
		t.Fatalf("by id: %+v, %v", got, err)
	}
}

func TestSorts(t *testing.T) {
	ctx := context.Background()
	s := openStore(t, filepath.Join(t.TempDir(), "cache.db"))
	roms := []domain.ROM{rom(1, 1, "d"), rom(2, 1, "b"), rom(3, 1, "C"), rom(4, 1, "a")}
	// 2 was added last; 3 and 4 have no time and come after, highest id first.
	roms[0].CreatedAt = time.Unix(100, 0).UTC()
	roms[1].CreatedAt = time.Unix(200, 0).UTC()
	// 3 is largest; 1 and 2 tie and fall back to name.
	roms[2].SizeBytes = 4096
	if err := s.Apply(ctx, domain.CatalogSync{Platforms: platforms, ROMs: roms, Full: true, At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		sort domain.ROMSort
		want []int
	}{
		{"", []int{4, 2, 3, 1}},
		{domain.SortName, []int{4, 2, 3, 1}},
		{domain.SortAdded, []int{2, 1, 4, 3}},
		{domain.SortSize, []int{3, 4, 2, 1}},
	} {
		page, err := s.ROMs(ctx, 1, nil, tc.sort, 0, 10)
		if err != nil {
			t.Fatal(err)
		}
		var ids []int
		for _, r := range page.ROMs {
			ids = append(ids, r.ID)
		}
		if !slices.Equal(ids, tc.want) {
			t.Errorf("sort %q: %v, want %v", tc.sort, ids, tc.want)
		}
	}
	// Paging a sort with ties never repeats a game.
	first, _ := s.ROMs(ctx, 1, nil, domain.SortSize, 0, 2)
	second, _ := s.ROMs(ctx, 1, nil, domain.SortSize, 2, 2)
	if first.ROMs[1].ID != 4 || second.ROMs[0].ID != 2 {
		t.Fatalf("pages: %+v %+v", first.ROMs, second.ROMs)
	}
	if !first.ROMs[0].CreatedAt.IsZero() || !second.ROMs[0].CreatedAt.Equal(roms[1].CreatedAt) {
		t.Fatal("created_at didn't round-trip")
	}
}

func TestDamagedFileIsRebuilt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	if err := os.WriteFile(path, []byte("not a database, just junk bytes that are long enough to matter"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := openStore(t, path)
	if p, r, err := s.Counts(context.Background()); err != nil || p != 0 || r != 0 {
		t.Fatalf("rebuilt store: %d %d %v", p, r, err)
	}
}

func TestOtherSchemaIsRebuilt(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "cache.db")
	s := openStore(t, path)
	if err := s.Apply(ctx, domain.CatalogSync{Platforms: platforms, ROMs: []domain.ROM{rom(1, 1, "zelda")}, Full: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	s = openStore(t, path)
	if _, r, err := s.Counts(ctx); err != nil || r != 0 {
		t.Fatalf("want an empty rebuilt store, got %d ROMs, %v", r, err)
	}
}

func TestDeletedFileIsRecreated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	s := openStore(t, path)
	_ = s.Close()
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		_ = os.Remove(p)
	}
	openStore(t, path)
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestClearEmptiesTheCatalog(t *testing.T) {
	ctx := context.Background()
	s := openStore(t, filepath.Join(t.TempDir(), "cache.db"))
	if err := s.Apply(ctx, domain.CatalogSync{Platforms: platforms, ROMs: []domain.ROM{rom(1, 1, "zelda")}, Full: true, Cursor: time.Unix(1001, 0), At: time.Unix(5000, 0)}); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	if p, r, _ := s.Counts(ctx); p != 0 || r != 0 {
		t.Fatalf("counts %d %d", p, r)
	}
	if c, at, _ := s.SyncState(ctx); !c.IsZero() || !at.IsZero() {
		t.Fatalf("sync state %v %v", c, at)
	}
}
