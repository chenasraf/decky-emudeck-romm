package app

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

const (
	// coverCacheLimit is how much room covers take on disk before the least recently used go.
	// Covers of games on this Deck don't count against it being freed: they are never evicted.
	coverCacheLimit = 500 << 20
	// coverFetches is how many covers download at once; a list opens dozens together.
	coverFetches = 4
	// coverTimeout bounds one cover download.
	coverTimeout = 15 * time.Second
	// coverBackoff is how long covers come only from the cache after the Server couldn't be
	// reached, so a list opened offline doesn't wait on a timeout per row.
	coverBackoff = 30 * time.Second
)

// CoverServer fetches cover images.
type CoverServer interface {
	// Cover fetches the image at a path the Server gave for it.
	Cover(ctx context.Context, path string) ([]byte, error)
}

// CoverDialer returns a CoverServer for server, authenticated with token.
type CoverDialer func(server domain.Server, token string) CoverServer

// CoverCache keeps cover images on disk.
type CoverCache interface {
	// Get returns the cover of a ROM in size that came from path.
	Get(romID int, size domain.CoverSize, path string) ([]byte, bool, error)
	// Stale returns any cover it holds of a ROM in size.
	Stale(romID int, size domain.CoverSize) ([]byte, bool, error)
	Put(romID int, size domain.CoverSize, path string, data []byte) error
	// Trim evicts the least recently used covers down to limit bytes, never those of ROMs keep
	// holds on to.
	Trim(limit int64, keep func(romID int) bool)
}

// Covers fetches covers from the Server on first display and serves them from a disk cache after,
// so they show whatever the Server's certificate or scheme, and offline. Covers of Plugin-owned
// ROMs are fetched ahead of display and kept.
type Covers struct {
	catalog CatalogStore
	conn    *Connection
	dial    CoverDialer
	cache   CoverCache
	// owned is nil when state.db couldn't be opened; then no cover is kept.
	owned   OwnershipStore
	limit   int64
	backoff time.Duration
	now     func() time.Time
	log     *slog.Logger
	// base outlives single calls: a download shared by several callers runs under it.
	base    context.Context
	fetches chan struct{}
	group   singleflight.Group

	mu           sync.Mutex
	offlineUntil time.Time
}

func NewCovers(ctx context.Context, catalog CatalogStore, conn *Connection, dial CoverDialer, cache CoverCache, owned OwnershipStore, log *slog.Logger) *Covers {
	return &Covers{
		catalog: catalog, conn: conn, dial: dial, cache: cache, owned: owned, limit: coverCacheLimit,
		backoff: coverBackoff, now: time.Now, log: log, base: ctx, fetches: make(chan struct{}, coverFetches),
	}
}

// Cover returns a ROM's cover in size. It is nil when the ROM has none, or when it isn't cached
// and the Server can't send it right now; a cover the Server has since changed is served from the
// cache until the new one can be fetched.
func (c *Covers) Cover(ctx context.Context, id int, size domain.CoverSize) ([]byte, error) {
	rom, ok, err := c.catalog.ROM(ctx, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errNotInCatalog
	}
	path := rom.Cover(size)
	if path == "" {
		return nil, nil
	}
	if data, ok, err := c.cache.Get(id, size, path); err != nil || ok {
		return data, err
	}
	data, err := c.fetch(ctx, id, size, path)
	if err == nil {
		return data, nil
	}
	c.log.Debug("cover unavailable", "rom_id", id, "size", size, "err", err)
	if stale, ok, err := c.cache.Stale(id, size); err == nil && ok {
		return stale, nil
	}
	return nil, nil
}

// fetch downloads a cover into the cache, once for every caller asking for it at the same time.
func (c *Covers) fetch(ctx context.Context, id int, size domain.CoverSize, path string) ([]byte, error) {
	c.mu.Lock()
	offline := c.now().Before(c.offlineUntil)
	c.mu.Unlock()
	if offline {
		return nil, errCoversOffline
	}
	key := strconv.Itoa(id) + "-" + string(size) + "-" + path
	ch := c.group.DoChan(key, func() (any, error) {
		return c.download(id, size, path)
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		if r.Err != nil {
			return nil, r.Err
		}
		return r.Val.([]byte), nil
	}
}

func (c *Covers) download(id int, size domain.CoverSize, path string) ([]byte, error) {
	select {
	case c.fetches <- struct{}{}:
	case <-c.base.Done():
		return nil, c.base.Err()
	}
	defer func() { <-c.fetches }()

	in, err := c.conn.activeSignIn()
	if err != nil {
		return nil, err
	}
	if in == nil {
		return nil, errNotSignedIn
	}
	ctx, cancel := context.WithTimeout(c.base, coverTimeout)
	defer cancel()
	data, err := c.dial(in.Server, in.Token).Cover(ctx, path)
	if err != nil {
		if kindOf(err) == domain.KindUnreachable || kindOf(err) == domain.KindUntrustedCert {
			c.mu.Lock()
			c.offlineUntil = c.now().Add(c.backoff)
			c.mu.Unlock()
		}
		return nil, err
	}
	if err := c.cache.Put(id, size, path, data); err != nil {
		// The cover still shows; it just isn't kept.
		c.log.Warn("cache cover", "rom_id", id, "err", err)
		return data, nil
	}
	c.trim()
	return data, nil
}

// trim keeps the cache under its limit, holding on to the covers of Plugin-owned ROMs.
func (c *Covers) trim() {
	var owned map[int]bool
	c.cache.Trim(c.limit, func(id int) bool {
		if owned == nil {
			owned = c.ownedSet()
		}
		return owned[id]
	})
}

func (c *Covers) ownedSet() map[int]bool {
	out := map[int]bool{}
	if c.owned == nil {
		return out
	}
	ids, err := c.owned.OwnedIDs(c.base)
	if err != nil {
		c.log.Warn("read owned games for covers", "err", err)
		return out
	}
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// Prefetch fetches both covers of each ROM in ids in the background, so they show offline.
func (c *Covers) Prefetch(ids ...int) {
	if len(ids) == 0 {
		return
	}
	go func() {
		var wg sync.WaitGroup
		for _, id := range ids {
			for _, size := range []domain.CoverSize{domain.CoverSmall, domain.CoverLarge} {
				wg.Go(func() {
					if _, err := c.Cover(c.base, id, size); err != nil {
						c.log.Debug("prefetch cover", "rom_id", id, "size", size, "err", err)
					}
				})
			}
		}
		wg.Wait()
	}()
}

// PrefetchOwned fetches the covers of every Plugin-owned ROM the cache lacks.
func (c *Covers) PrefetchOwned() {
	if c.owned == nil {
		return
	}
	ids, err := c.owned.OwnedIDs(c.base)
	if err != nil {
		c.log.Warn("read owned games for covers", "err", err)
		return
	}
	c.Prefetch(ids...)
}

var errCoversOffline = errors.New("the server couldn't be reached moments ago")
