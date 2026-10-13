package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// memStore is a CatalogStore in memory.
type memStore struct {
	mu          sync.Mutex
	platforms   []domain.Platform
	collections []domain.Collection
	roms        map[int]domain.ROM
	cursor      time.Time
	syncedAt    time.Time
	syncs       []domain.CatalogSync
}

func (s *memStore) SyncState(context.Context) (time.Time, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cursor, s.syncedAt, nil
}

func (s *memStore) Apply(_ context.Context, sync domain.CatalogSync) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncs = append(s.syncs, sync)
	s.platforms = sync.Platforms
	s.collections = sync.Collections
	if sync.Full || s.roms == nil {
		s.roms = map[int]domain.ROM{}
	}
	for _, r := range sync.ROMs {
		s.roms[r.ID] = r
	}
	if sync.KeepIDs != nil {
		for id := range s.roms {
			if !slices.Contains(sync.KeepIDs, id) {
				delete(s.roms, id)
			}
		}
	}
	s.cursor, s.syncedAt = sync.Cursor, sync.At
	return nil
}

func (s *memStore) Platforms(context.Context) ([]domain.PlatformSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.PlatformSummary
	for _, p := range s.platforms {
		out = append(out, domain.PlatformSummary{Platform: p, Games: len(s.roms)})
	}
	return out, nil
}

func (s *memStore) Platform(_ context.Context, id int) (domain.Platform, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.platforms {
		if p.ID == id {
			return p, true, nil
		}
	}
	return domain.Platform{}, false, nil
}

func (s *memStore) ROMs(_ context.Context, platformID int, only *domain.IDFilter, offset, limit int) (domain.ROMPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var all []domain.ROM
	for _, r := range s.roms {
		if platformID != 0 && r.PlatformID != platformID {
			continue
		}
		if only != nil && slices.Contains(only.IDs, r.ID) == only.Exclude {
			continue
		}
		all = append(all, r)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	page := domain.ROMPage{Total: len(all)}
	if offset < len(all) {
		page.ROMs = all[offset:min(offset+limit, len(all))]
	}
	return page, nil
}

// held narrows c's ROMIDs to those the store holds. s.mu must be held.
func (s *memStore) held(c domain.Collection) domain.Collection {
	ids := []int{}
	for _, id := range c.ROMIDs {
		if _, ok := s.roms[id]; ok {
			ids = append(ids, id)
		}
	}
	c.ROMIDs = ids
	return c
}

func (s *memStore) Collections(context.Context) ([]domain.Collection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Collection
	for _, c := range s.collections {
		out = append(out, s.held(c))
	}
	return out, nil
}

func (s *memStore) Collection(_ context.Context, ref domain.CollectionRef) (domain.Collection, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.collections {
		if c.CollectionRef == ref {
			return s.held(c), true, nil
		}
	}
	return domain.Collection{}, false, nil
}

func (s *memStore) ROM(_ context.Context, id int) (domain.ROM, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.roms[id]
	return r, ok, nil
}

func (s *memStore) Search(_ context.Context, q domain.SearchQuery) ([]domain.ROM, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.ROM
	for _, r := range s.roms {
		if q.PlatformID > 0 && r.PlatformID != q.PlatformID {
			continue
		}
		if strings.Contains(strings.ToLower(r.Name), strings.ToLower(q.Term)) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out[:min(q.Limit, len(out))], nil
}

func (s *memStore) ROMsByID(_ context.Context, ids []int) ([]domain.ROM, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.ROM
	for _, id := range ids {
		if r, ok := s.roms[id]; ok {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *memStore) Recent(_ context.Context, limit int) ([]domain.ROM, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.ROM
	for _, r := range s.roms {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	return out[:min(limit, len(out))], nil
}

func (s *memStore) CountByPlatform(_ context.Context, ids []int) (map[int]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[int]int{}
	for _, id := range ids {
		if r, ok := s.roms[id]; ok {
			out[r.PlatformID]++
		}
	}
	return out, nil
}

// fixedStates is GameStates with states set by the test.
type fixedStates map[int]domain.GameState

func (f fixedStates) States(context.Context) (map[int]domain.GameState, error) {
	return maps.Clone(f), nil
}

func (s *memStore) Counts(context.Context) (int, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.platforms), len(s.roms), nil
}

// fakeLibrary is a RomM server's library.
type fakeLibrary struct {
	mu          sync.Mutex
	roms        map[int]domain.ROM
	collections []domain.Collection
	// collectionsErr is what listing collections fails with.
	collectionsErr error
	err            error
	since          []time.Time
}

func (l *fakeLibrary) Collections(context.Context) ([]domain.Collection, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.collections, l.collectionsErr
}

func newLibrary(n int) *fakeLibrary {
	l := &fakeLibrary{roms: map[int]domain.ROM{}}
	for id := 1; id <= n; id++ {
		l.roms[id] = domain.ROM{ID: id, PlatformID: 1, Name: "game", UpdatedAt: time.Unix(int64(id), 0).UTC()}
	}
	return l
}

func (l *fakeLibrary) Platforms(context.Context) ([]domain.Platform, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return nil, l.err
	}
	return []domain.Platform{{ID: 1, Slug: "gba", Name: "Game Boy Advance", ROMCount: len(l.roms)}}, nil
}

func (l *fakeLibrary) ROMs(_ context.Context, since time.Time, offset, limit int) ([]domain.ROM, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if offset == 0 {
		l.since = append(l.since, since)
	}
	var match []domain.ROM
	for _, r := range l.roms {
		if r.UpdatedAt.After(since) {
			match = append(match, r)
		}
	}
	sort.Slice(match, func(i, j int) bool { return match[i].ID < match[j].ID })
	if offset >= len(match) {
		return nil, nil
	}
	return match[offset:min(offset+limit, len(match))], nil
}

func (l *fakeLibrary) ROMIDs(context.Context) ([]int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var ids []int
	for id := range l.roms {
		ids = append(ids, id)
	}
	return ids, nil
}

// SearchROMs matches term against names and against "md5:<id>", newest ROM first, as a stand-in
// for RomM's relevance and hash matching.
func (l *fakeLibrary) SearchROMs(_ context.Context, q domain.SearchQuery) ([]int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return nil, l.err
	}
	var ids []int
	for id, r := range l.roms {
		if q.PlatformID > 0 && r.PlatformID != q.PlatformID {
			continue
		}
		if strings.Contains(r.Name, q.Term) || q.Term == fmt.Sprintf("md5:%d", id) {
			ids = append(ids, id)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(ids)))
	return ids[:min(q.Limit, len(ids))], nil
}

type catalogFixture struct {
	catalog  *Catalog
	store    *memStore
	states   fixedStates
	library  *fakeLibrary
	settings *fakeSettings
	statuses chan CatalogStatus
	now      time.Time
}

func newCatalogFixture(t *testing.T, library *fakeLibrary) *catalogFixture {
	t.Helper()
	f := &catalogFixture{
		store:    &memStore{},
		states:   fixedStates{},
		library:  library,
		settings: &fakeSettings{in: &domain.SignIn{Server: domain.Server{URL: "https://romm.lan"}, Token: "rmm_x"}},
		statuses: make(chan CatalogStatus, 64),
		now:      time.Unix(100000, 0),
	}
	conn, _ := newConnectionWithEvents(t, f.settings, &fakeNet{})
	dial := func(domain.Server, string) CatalogServer { return library }
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	f.catalog = NewCatalog(t.Context(), f.store, f.states, conn, dial, func(s CatalogStatus) { f.statuses <- s }, log)
	f.catalog.now = func() time.Time { return f.now }
	return f
}

// refresh runs a refresh and waits for it to end.
func (f *catalogFixture) refresh(t *testing.T, force bool) CatalogStatus {
	t.Helper()
	f.catalog.Refresh(force)
	for {
		select {
		case s := <-f.statuses:
			if !s.Refreshing {
				return s
			}
		case <-time.After(3 * time.Second):
			t.Fatal("refresh didn't end")
			return CatalogStatus{}
		}
	}
}

func TestCatalogFullThenDelta(t *testing.T) {
	library := newLibrary(1201)
	f := newCatalogFixture(t, library)

	s := f.refresh(t, false)
	if s.Problem != nil || s.ROMs != 1201 || s.Platforms != 1 || !s.SyncedAt.Equal(f.now) {
		t.Fatalf("after first sync: %+v", s)
	}
	if first := f.store.syncs[0]; !first.Full || first.KeepIDs != nil || !first.Cursor.Equal(time.Unix(1201, 0)) {
		t.Fatalf("first sync %+v", first)
	}

	// On RomM: one ROM deleted, one renamed, one added.
	library.mu.Lock()
	delete(library.roms, 5)
	library.roms[7] = domain.ROM{ID: 7, PlatformID: 1, Name: "renamed", UpdatedAt: time.Unix(5000, 0).UTC()}
	library.roms[2000] = domain.ROM{ID: 2000, PlatformID: 1, Name: "new", UpdatedAt: time.Unix(5001, 0).UTC()}
	library.mu.Unlock()

	f.now = f.now.Add(time.Minute)
	s = f.refresh(t, true)
	if s.ROMs != 1201 {
		t.Fatalf("after delta: %+v", s)
	}
	delta := f.store.syncs[1]
	if delta.Full || len(delta.ROMs) != 2 || len(delta.KeepIDs) != 1201 || !delta.Cursor.Equal(time.Unix(5001, 0)) {
		t.Fatalf("delta %+v", delta)
	}
	if !library.since[1].Equal(time.Unix(1201, 0)) {
		t.Fatalf("delta asked for changes since %v", library.since[1])
	}
	if r, _, _, _ := f.catalog.Game(context.Background(), 7); r.Name != "renamed" {
		t.Fatalf("renamed ROM %+v", r)
	}
	if _, _, _, err := f.catalog.Game(context.Background(), 5); kindOf(err) != domain.KindNotFound {
		t.Fatalf("deleted ROM: %v", err)
	}
}

func TestCatalogSkipsFreshRefresh(t *testing.T) {
	f := newCatalogFixture(t, newLibrary(3))
	f.refresh(t, false)

	f.now = f.now.Add(catalogMaxAge - time.Second)
	f.catalog.Refresh(false)
	select {
	case s := <-f.statuses:
		t.Fatalf("a fresh catalog refreshed: %+v", s)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestCatalogRetriesWhenUnreachable(t *testing.T) {
	saved := retryDelays
	retryDelays = []time.Duration{10 * time.Millisecond}
	t.Cleanup(func() { retryDelays = saved })

	library := newLibrary(3)
	library.err = &domain.Error{Kind: domain.KindUnreachable, Message: "unreachable"}
	f := newCatalogFixture(t, library)

	if s := f.refresh(t, false); kindOf(s.Problem) != domain.KindUnreachable || s.ROMs != 0 {
		t.Fatalf("offline: %+v", s)
	}
	library.mu.Lock()
	library.err = nil
	library.mu.Unlock()

	// The retry runs on its own and fills the catalog.
	deadline := time.After(3 * time.Second)
	for {
		select {
		case s := <-f.statuses:
			if !s.Refreshing && s.Problem == nil && s.ROMs == 3 {
				return
			}
		case <-deadline:
			t.Fatal("no retry")
		}
	}
}

func TestCatalogRejectedTokenSignsOut(t *testing.T) {
	library := newLibrary(3)
	library.err = &domain.Error{Kind: domain.KindSignedOut, Message: "signed out"}
	f := newCatalogFixture(t, library)

	if s := f.refresh(t, false); kindOf(s.Problem) != domain.KindSignedOut {
		t.Fatalf("got %+v", s)
	}
	if !f.settings.in.SignedOut {
		t.Fatal("a 401 during sync didn't sign out")
	}
	// Signed out, there's nothing to refresh with.
	f.catalog.Refresh(true)
	select {
	case s := <-f.statuses:
		t.Fatalf("refreshed while signed out: %+v", s)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestCatalogCollections(t *testing.T) {
	library := newLibrary(4)
	fav := domain.CollectionRef{Kind: domain.CollectionRegular, ID: 2}
	smart := domain.CollectionRef{Kind: domain.CollectionSmart, ID: 2}
	library.collections = []domain.Collection{
		// 99 is a game RomM lists in the collection that the library doesn't hold.
		{CollectionRef: fav, Name: "Favorites", Favorite: true, ROMIDs: []int{1, 2, 99}},
		{CollectionRef: smart, Name: "Fire Emblem", ROMIDs: []int{3, 4}},
	}
	f := newCatalogFixture(t, library)
	f.refresh(t, false)
	ctx := context.Background()
	f.states[2] = domain.StateInstalled

	cols, err := f.catalog.Collections(ctx)
	if err != nil || len(cols) != 2 {
		t.Fatalf("collections %+v, %v", cols, err)
	}
	// The same id names a different collection in each kind.
	if cols[0].CollectionRef != fav || cols[0].Games != 2 || cols[0].Installed != 1 || cols[1].CollectionRef != smart || cols[1].Games != 2 {
		t.Fatalf("collections %+v", cols)
	}

	col, page, err := f.catalog.CollectionGames(ctx, fav, domain.FilterNotInstalled, 0, 10)
	if err != nil || col.Name != "Favorites" || page.Total != 1 || page.ROMs[0].ID != 1 {
		t.Fatalf("not installed: %+v %+v, %v", col, page, err)
	}
	if _, page, _ := f.catalog.CollectionGames(ctx, fav, domain.FilterInstalled, 0, 10); page.Total != 1 || page.ROMs[0].ID != 2 {
		t.Fatalf("installed: %+v", page)
	}
	if _, _, err := f.catalog.CollectionGames(ctx, domain.CollectionRef{Kind: domain.CollectionSmart, ID: 9}, domain.FilterAll, 0, 10); kindOf(err) != domain.KindNotFound {
		t.Fatalf("unknown collection: %v", err)
	}
}

func TestCatalogSyncsWithoutCollectionsPermission(t *testing.T) {
	library := newLibrary(2)
	library.collectionsErr = &domain.Error{Kind: domain.KindMissingPermission, Message: "no collections.read"}
	f := newCatalogFixture(t, library)

	if s := f.refresh(t, false); s.Problem != nil || s.ROMs != 2 {
		t.Fatalf("refresh %+v", s)
	}
	if cols, _ := f.catalog.Collections(context.Background()); len(cols) != 0 {
		t.Fatalf("collections %+v", cols)
	}
}

func TestCatalogFilters(t *testing.T) {
	f := newCatalogFixture(t, newLibrary(3))
	f.refresh(t, false)
	ctx := context.Background()

	if _, page, err := f.catalog.Games(ctx, 1, domain.FilterAll, 0, 2); err != nil || page.Total != 3 || len(page.ROMs) != 2 {
		t.Fatalf("all: %+v, %v", page, err)
	}
	if _, page, _ := f.catalog.Games(ctx, 1, domain.FilterInstalled, 0, 2); page.Total != 0 {
		t.Fatalf("installed: %+v", page)
	}

	f.states[2] = domain.StateInstalled
	f.states[3] = domain.StateDownloading
	if _, page, _ := f.catalog.Games(ctx, 1, domain.FilterInstalled, 0, 10); page.Total != 1 || page.ROMs[0].ID != 2 {
		t.Fatalf("installed: %+v", page)
	}
	// A game still downloading isn't installed yet.
	_, page, _ := f.catalog.Games(ctx, 1, domain.FilterNotInstalled, 0, 10)
	if page.Total != 2 || domain.StateOf(page.States, 3) != domain.StateDownloading || domain.StateOf(page.States, 1) != domain.StateOnRomM {
		t.Fatalf("not installed: %+v", page)
	}
	if ps, _ := f.catalog.Platforms(ctx); ps[0].Installed != 1 {
		t.Fatalf("platforms: %+v", ps)
	}
	if _, _, state, _ := f.catalog.Game(ctx, 2); state != domain.StateInstalled {
		t.Fatalf("game state %s", state)
	}
	if _, _, err := f.catalog.Games(ctx, 99, domain.FilterAll, 0, 2); kindOf(err) != domain.KindNotFound {
		t.Fatalf("unknown platform: %v", err)
	}
}

func searchLibrary() *fakeLibrary {
	l := &fakeLibrary{roms: map[int]domain.ROM{}}
	for id, name := range map[int]string{1: "Golden Sun", 2: "Metroid Fusion", 3: "Golden Sun: The Lost Age"} {
		l.roms[id] = domain.ROM{ID: id, PlatformID: 1, Name: name, UpdatedAt: time.Unix(int64(id), 0).UTC()}
	}
	l.roms[5] = domain.ROM{ID: 5, PlatformID: 2, Name: "Golden Axe", UpdatedAt: time.Unix(5, 0).UTC()}
	return l
}

// anywhere searches every platform for term.
func anywhere(term string) domain.SearchQuery { return domain.SearchQuery{Term: term, Limit: 50} }

func TestCatalogSearchAsksServer(t *testing.T) {
	library := searchLibrary()
	f := newCatalogFixture(t, library)
	f.refresh(t, false)
	ctx := context.Background()

	got, err := f.catalog.Search(ctx, anywhere(" Golden Sun "))
	if err != nil || got.Local || len(got.ROMs) != 2 || got.ROMs[0].ID != 3 || got.PlatformNames[1] != "Game Boy Advance" {
		t.Fatalf("by name: %+v, %v", got, err)
	}
	if got, _ := f.catalog.Search(ctx, anywhere("md5:2")); len(got.ROMs) != 1 || got.ROMs[0].Name != "Metroid Fusion" {
		t.Fatalf("by hash: %+v", got)
	}
	if got, _ := f.catalog.Search(ctx, anywhere("Golden")); len(got.ROMs) != 3 {
		t.Fatalf("every platform: %+v", got)
	}
	if got, _ := f.catalog.Search(ctx, domain.SearchQuery{Term: "Golden", PlatformID: 2, Limit: 50}); len(got.ROMs) != 1 || got.ROMs[0].ID != 5 {
		t.Fatalf("one platform: %+v", got)
	}

	// A game on RomM that the Catalog doesn't hold yet can't be opened, so it isn't listed.
	library.mu.Lock()
	library.roms[4] = domain.ROM{ID: 4, PlatformID: 1, Name: "Golden Sun 3"}
	library.mu.Unlock()
	if got, _ := f.catalog.Search(ctx, anywhere("Golden Sun")); len(got.ROMs) != 2 {
		t.Fatalf("not in catalog: %+v", got)
	}
	if got, _ := f.catalog.Search(ctx, anywhere("  ")); len(got.ROMs) != 0 {
		t.Fatalf("blank: %+v", got)
	}
}

func TestCatalogSearchFallsBackToCatalog(t *testing.T) {
	library := searchLibrary()
	f := newCatalogFixture(t, library)
	f.refresh(t, false)
	ctx := context.Background()

	library.mu.Lock()
	library.err = &domain.Error{Kind: domain.KindUnreachable, Message: "unreachable"}
	library.mu.Unlock()
	got, err := f.catalog.Search(ctx, domain.SearchQuery{Term: "golden", PlatformID: 1, Limit: 50})
	if err != nil || !got.Local || len(got.ROMs) != 2 || got.PlatformNames[1] != "Game Boy Advance" {
		t.Fatalf("offline: %+v, %v", got, err)
	}

	library.mu.Lock()
	library.err = &domain.Error{Kind: domain.KindSignedOut, Message: "signed out"}
	library.mu.Unlock()
	if got, _ := f.catalog.Search(ctx, anywhere("metroid")); !got.Local || len(got.ROMs) != 1 {
		t.Fatalf("token rejected: %+v", got)
	}
	if !f.settings.in.SignedOut {
		t.Fatal("a 401 during search didn't sign out")
	}
	// Signed out, the Catalog answers without asking the Server.
	if got, _ := f.catalog.Search(ctx, anywhere("golden")); !got.Local || len(got.ROMs) != 3 {
		t.Fatalf("signed out: %+v", got)
	}
}

func TestCatalogShelves(t *testing.T) {
	library := &fakeLibrary{roms: map[int]domain.ROM{}}
	for id, name := range map[int]string{1: "Advance Wars", 2: "Boktai", 3: "Castlevania", 4: "Drill Dozer"} {
		library.roms[id] = domain.ROM{ID: id, PlatformID: 1, Name: name, UpdatedAt: time.Unix(int64(id), 0).UTC()}
	}
	// RomM added 1 last; 4 has no time and sorts after every game that has one.
	for id, at := range map[int]int64{1: 300, 2: 100, 3: 200} {
		r := library.roms[id]
		r.CreatedAt = time.Unix(at, 0).UTC()
		library.roms[id] = r
	}
	f := newCatalogFixture(t, library)
	f.refresh(t, false)
	f.states[3] = domain.StateInstalled

	got, err := f.catalog.Shelves(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	var recent []int
	for _, r := range got.Recent {
		recent = append(recent, r.ID)
	}
	if !slices.Equal(recent, []int{1, 3, 2}) {
		t.Fatalf("recent: %v", recent)
	}
	if got.Installed.Total != 1 || got.Installed.ROMs[0].ID != 3 {
		t.Fatalf("installed: %+v", got.Installed)
	}
	if len(got.Platforms) != 1 || len(got.Platforms[0].ROMs) != 3 || got.Platforms[0].Platform.Installed != 1 {
		t.Fatalf("platforms: %+v", got.Platforms)
	}
	if got.PlatformNames[1] != "Game Boy Advance" || domain.StateOf(got.States, 3) != domain.StateInstalled {
		t.Fatalf("names %v, states %v", got.PlatformNames, got.States)
	}
}
