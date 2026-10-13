package app

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// fakeCovers is a server's cover images by path. Its gate, when set, holds every fetch until it
// closes.
type fakeCovers struct {
	mu     sync.Mutex
	images map[string][]byte
	err    error
	gate   chan struct{}
	calls  atomic.Int32
}

func (f *fakeCovers) Cover(ctx context.Context, path string) ([]byte, error) {
	f.calls.Add(1)
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	data, ok := f.images[path]
	if !ok {
		return nil, &domain.Error{Kind: domain.KindNotFound, Message: "no cover"}
	}
	return data, nil
}

// memCoverCache is a CoverCache in memory that records what Trim kept.
type memCoverCache struct {
	mu      sync.Mutex
	covers  map[string][]byte
	stale   map[string][]byte
	trimmed []int
}

func coverKey(id int, size domain.CoverSize) string { return string(size) + "/" + string(rune('0'+id)) }

func (m *memCoverCache) Get(id int, size domain.CoverSize, path string) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.covers[coverKey(id, size)+path]
	return data, ok, nil
}

func (m *memCoverCache) Stale(id int, size domain.CoverSize) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.stale[coverKey(id, size)]
	return data, ok, nil
}

func (m *memCoverCache) Put(id int, size domain.CoverSize, path string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.covers[coverKey(id, size)+path] = data
	m.stale[coverKey(id, size)] = data
	return nil
}

func (m *memCoverCache) Trim(_ int64, keep func(int) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trimmed = nil
	for id := 1; id <= 3; id++ {
		if keep(id) {
			m.trimmed = append(m.trimmed, id)
		}
	}
}

type coverFixture struct {
	covers *Covers
	server *fakeCovers
	cache  *memCoverCache
	owned  *memOwnership
}

func newCoverFixture(t *testing.T) *coverFixture {
	t.Helper()
	catalog := &memStore{}
	if err := catalog.Apply(context.Background(), domain.CatalogSync{
		Platforms: []domain.Platform{{ID: 1, Slug: "snes", Name: "Super Nintendo"}},
		ROMs: []domain.ROM{
			{ID: 1, PlatformID: 1, Name: "Chrono Trigger", CoverSmall: "/c/1/small.png?ts=1", CoverLarge: "/c/1/big.png?ts=1"},
			{ID: 2, PlatformID: 1, Name: "Super Metroid", CoverSmall: "/c/2/small.png", CoverLarge: "/c/2/big.png"},
			{ID: 3, PlatformID: 1, Name: "No Cover"},
		},
		Full: true,
	}); err != nil {
		t.Fatal(err)
	}
	f := &coverFixture{
		server: &fakeCovers{images: map[string][]byte{
			"/c/1/small.png?ts=1": []byte("chrono small"), "/c/1/big.png?ts=1": []byte("chrono large"),
			"/c/2/small.png": []byte("metroid small"), "/c/2/big.png": []byte("metroid large"),
		}},
		cache: &memCoverCache{covers: map[string][]byte{}, stale: map[string][]byte{}},
		owned: &memOwnership{roms: map[int]domain.OwnedROM{}},
	}
	conn, _ := newConnectionWithEvents(t, &fakeSettings{in: &domain.SignIn{Server: domain.Server{URL: "https://romm.lan"}, Token: "rmm_x"}}, &fakeNet{})
	dial := func(domain.Server, string) CoverServer { return f.server }
	f.covers = NewCovers(t.Context(), catalog, conn, dial, f.cache, f.owned, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return f
}

func TestCoverFetchesOnceThenCaches(t *testing.T) {
	f := newCoverFixture(t)
	ctx := context.Background()

	for range 2 {
		got, err := f.covers.Cover(ctx, 1, domain.CoverSmall)
		if err != nil || string(got) != "chrono small" {
			t.Fatalf("small: %q, %v", got, err)
		}
	}
	if got, _ := f.covers.Cover(ctx, 1, domain.CoverLarge); string(got) != "chrono large" {
		t.Fatalf("large: %q", got)
	}
	if n := f.server.calls.Load(); n != 2 {
		t.Fatalf("server asked %d times, want 2", n)
	}
}

func TestCoverNoneIsNil(t *testing.T) {
	f := newCoverFixture(t)
	if got, err := f.covers.Cover(context.Background(), 3, domain.CoverSmall); got != nil || err != nil {
		t.Fatalf("got %q, %v", got, err)
	}
	if f.server.calls.Load() != 0 {
		t.Fatal("asked the server for a cover the ROM doesn't have")
	}
	if _, err := f.covers.Cover(context.Background(), 99, domain.CoverSmall); kindOf(err) != domain.KindNotFound {
		t.Fatalf("unknown ROM: %v", err)
	}
}

func TestCoverSharesOneFetch(t *testing.T) {
	f := newCoverFixture(t)
	f.server.gate = make(chan struct{})

	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if got, err := f.covers.Cover(context.Background(), 2, domain.CoverSmall); err != nil || string(got) != "metroid small" {
				t.Errorf("got %q, %v", got, err)
			}
		})
	}
	time.Sleep(20 * time.Millisecond)
	close(f.server.gate)
	wg.Wait()
	if n := f.server.calls.Load(); n != 1 {
		t.Fatalf("server asked %d times, want 1", n)
	}
}

func TestCoverOfflineServesCacheAndBacksOff(t *testing.T) {
	f := newCoverFixture(t)
	ctx := context.Background()
	if _, err := f.covers.Cover(ctx, 1, domain.CoverSmall); err != nil {
		t.Fatal(err)
	}
	f.server.mu.Lock()
	f.server.err = &domain.Error{Kind: domain.KindUnreachable, Message: "down"}
	f.server.mu.Unlock()
	calls := f.server.calls.Load()

	// Cached covers still show.
	if got, _ := f.covers.Cover(ctx, 1, domain.CoverSmall); string(got) != "chrono small" {
		t.Fatalf("cached: %q", got)
	}
	// An uncached one is nil, and the next ones don't wait on the server again.
	for range 3 {
		if got, err := f.covers.Cover(ctx, 2, domain.CoverSmall); got != nil || err != nil {
			t.Fatalf("offline: %q, %v", got, err)
		}
	}
	if n := f.server.calls.Load() - calls; n != 1 {
		t.Fatalf("server asked %d times while down, want 1", n)
	}
}

func TestCoverChangedOnServerFallsBackWhileOffline(t *testing.T) {
	f := newCoverFixture(t)
	f.cache.stale[coverKey(1, domain.CoverSmall)] = []byte("older chrono")
	f.server.mu.Lock()
	f.server.err = &domain.Error{Kind: domain.KindUnreachable, Message: "down"}
	f.server.mu.Unlock()

	if got, _ := f.covers.Cover(context.Background(), 1, domain.CoverSmall); string(got) != "older chrono" {
		t.Fatalf("got %q", got)
	}
}

func TestPrefetchOwnedPinsThem(t *testing.T) {
	f := newCoverFixture(t)
	f.owned.roms[2] = domain.OwnedROM{ROMID: 2}

	f.covers.PrefetchOwned()
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, small, _ := f.cache.Get(2, domain.CoverSmall, "/c/2/small.png")
		_, large, _ := f.cache.Get(2, domain.CoverLarge, "/c/2/big.png")
		if small && large {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("owned covers weren't prefetched")
		}
		time.Sleep(time.Millisecond)
	}
	f.cache.mu.Lock()
	defer f.cache.mu.Unlock()
	if len(f.cache.trimmed) != 1 || f.cache.trimmed[0] != 2 {
		t.Fatalf("trim kept %v, want only the owned game", f.cache.trimmed)
	}
}
