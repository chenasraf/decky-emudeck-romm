package app

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// memStore is a CatalogStore in memory.
type memStore struct {
	mu        sync.Mutex
	platforms []domain.Platform
	roms      map[int]domain.ROM
	cursor    time.Time
	syncedAt  time.Time
	syncs     []domain.CatalogSync
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

func (s *memStore) ROMs(_ context.Context, platformID, offset, limit int) (domain.ROMPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var all []domain.ROM
	for _, r := range s.roms {
		if r.PlatformID == platformID {
			all = append(all, r)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	page := domain.ROMPage{Total: len(all)}
	if offset < len(all) {
		page.ROMs = all[offset:min(offset+limit, len(all))]
	}
	return page, nil
}

func (s *memStore) ROM(_ context.Context, id int) (domain.ROM, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.roms[id]
	return r, ok, nil
}

func (s *memStore) Counts(context.Context) (int, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.platforms), len(s.roms), nil
}

// fakeLibrary is a RomM server's library.
type fakeLibrary struct {
	mu    sync.Mutex
	roms  map[int]domain.ROM
	err   error
	since []time.Time
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

type catalogFixture struct {
	catalog  *Catalog
	store    *memStore
	library  *fakeLibrary
	settings *fakeSettings
	statuses chan CatalogStatus
	now      time.Time
}

func newCatalogFixture(t *testing.T, library *fakeLibrary) *catalogFixture {
	t.Helper()
	f := &catalogFixture{
		store:    &memStore{},
		library:  library,
		settings: &fakeSettings{in: &domain.SignIn{Server: domain.Server{URL: "https://romm.lan"}, Token: "rmm_x"}},
		statuses: make(chan CatalogStatus, 64),
		now:      time.Unix(100000, 0),
	}
	conn, _ := newConnectionWithEvents(t, f.settings, &fakeNet{})
	dial := func(domain.Server, string) CatalogServer { return library }
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	f.catalog = NewCatalog(t.Context(), f.store, conn, dial, func(s CatalogStatus) { f.statuses <- s }, log)
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
	if r, _, _ := f.catalog.Game(context.Background(), 7); r.Name != "renamed" {
		t.Fatalf("renamed ROM %+v", r)
	}
	if _, _, err := f.catalog.Game(context.Background(), 5); kindOf(err) != domain.KindNotFound {
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
	if _, _, err := f.catalog.Games(ctx, 99, domain.FilterAll, 0, 2); kindOf(err) != domain.KindNotFound {
		t.Fatalf("unknown platform: %v", err)
	}
}
