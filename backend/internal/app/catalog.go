package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

const (
	// catalogMaxAge is how old the Catalog may be before opening the QAM or the library refreshes
	// it.
	catalogMaxAge = 5 * time.Minute
	// catalogPageSize is how many ROMs each request asks for: about 7 MB of JSON, a few seconds
	// on a home server.
	catalogPageSize = 500
)

// retryDelays is how long the Catalog waits before retrying a refresh that couldn't reach the
// Server, one step longer after each failure in a row.
var retryDelays = []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute}

// CatalogServer is the part of the RomM API a Catalog sync needs.
type CatalogServer interface {
	Platforms(ctx context.Context) ([]domain.Platform, error)
	ROMs(ctx context.Context, since time.Time, offset, limit int) ([]domain.ROM, error)
	ROMIDs(ctx context.Context) ([]int, error)
}

// CatalogDialer returns a CatalogServer for server, authenticated with token.
type CatalogDialer func(server domain.Server, token string) CatalogServer

// CatalogStore keeps the Catalog on disk.
type CatalogStore interface {
	SyncState(ctx context.Context) (cursor, syncedAt time.Time, err error)
	Apply(ctx context.Context, sync domain.CatalogSync) error
	Platforms(ctx context.Context) ([]domain.PlatformSummary, error)
	Platform(ctx context.Context, id int) (domain.Platform, bool, error)
	ROMs(ctx context.Context, platformID, offset, limit int) (domain.ROMPage, error)
	ROM(ctx context.Context, id int) (domain.ROM, bool, error)
	Counts(ctx context.Context) (platforms, roms int, err error)
}

// CatalogListener hears the Catalog's status each time a refresh starts, makes progress or ends.
type CatalogListener func(CatalogStatus)

type CatalogStatus struct {
	Refreshing bool
	// Fetched is how many ROMs the running refresh has received so far.
	Fetched  int
	SyncedAt time.Time
	// Platforms and ROMs count what the Catalog holds.
	Platforms int
	ROMs      int
	// Problem says why the last refresh failed, nil when it worked.
	Problem error
}

type Catalog struct {
	store    CatalogStore
	conn     *Connection
	dial     CatalogDialer
	onChange CatalogListener
	log      *slog.Logger
	now      func() time.Time
	// base outlives single calls: refreshes run under it after the call that started them returns.
	base context.Context

	mu         sync.Mutex
	refreshing bool
	fetched    int
	problem    error
	failures   int
	retry      *time.Timer
}

func NewCatalog(ctx context.Context, store CatalogStore, conn *Connection, dial CatalogDialer, onChange CatalogListener, log *slog.Logger) *Catalog {
	return &Catalog{store: store, conn: conn, dial: dial, onChange: onChange, log: log, now: time.Now, base: ctx}
}

// Status reports what the Catalog holds and whether a refresh is running.
func (c *Catalog) Status(ctx context.Context) (CatalogStatus, error) {
	_, syncedAt, err := c.store.SyncState(ctx)
	if err != nil {
		return CatalogStatus{}, err
	}
	platforms, roms, err := c.store.Counts(ctx)
	if err != nil {
		return CatalogStatus{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return CatalogStatus{
		Refreshing: c.refreshing,
		Fetched:    c.fetched,
		SyncedAt:   syncedAt,
		Platforms:  platforms,
		ROMs:       roms,
		Problem:    c.problem,
	}, nil
}

// Refresh starts a refresh in the background when the Catalog is over five minutes old, or always
// when force is set, as Refresh library asks. It does nothing while one already runs. Cached data
// stays readable throughout.
func (c *Catalog) Refresh(force bool) {
	// Not set up or Signed out: there is nothing to sync with, and signing in refreshes.
	if in, err := c.conn.activeSignIn(); err != nil || in == nil {
		return
	}
	if !force {
		_, syncedAt, err := c.store.SyncState(c.base)
		if err == nil && c.now().Sub(syncedAt) < catalogMaxAge {
			return
		}
	}
	c.mu.Lock()
	if c.refreshing {
		c.mu.Unlock()
		return
	}
	c.refreshing, c.fetched = true, 0
	if c.retry != nil {
		c.retry.Stop()
		c.retry = nil
	}
	c.mu.Unlock()

	c.announce()
	go c.run()
}

func (c *Catalog) run() {
	start := c.now()
	err := c.sync(c.base)

	c.mu.Lock()
	c.refreshing = false
	c.problem = err
	switch {
	case err == nil:
		c.failures = 0
	case kindOf(err) == domain.KindUnreachable && c.base.Err() == nil:
		delay := retryDelays[min(c.failures, len(retryDelays)-1)]
		c.failures++
		c.retry = time.AfterFunc(delay, func() { c.Refresh(true) })
		c.log.Info("catalog refresh will retry", "in", delay)
	}
	c.mu.Unlock()

	if err != nil {
		c.log.Warn("catalog refresh failed", "err", err, "duration", c.now().Sub(start).Round(time.Millisecond))
	}
	c.announce()
}

func (c *Catalog) announce() {
	status, err := c.Status(c.base)
	if err != nil {
		c.log.Error("read catalog status", "err", err)
		return
	}
	c.onChange(status)
}

// sync brings the Catalog up to date: everything on the first sync, then only what changed, with
// the Server's full id list to find what was deleted. Not being signed in isn't a failure; there
// is just nothing to sync with.
func (c *Catalog) sync(ctx context.Context) error {
	in, err := c.conn.activeSignIn()
	if err != nil || in == nil {
		return err
	}
	server := c.dial(in.Server, in.Token)
	cursor, syncedAt, err := c.store.SyncState(ctx)
	if err != nil {
		return err
	}
	full := syncedAt.IsZero()
	if full {
		cursor = time.Time{}
	}

	platforms, err := server.Platforms(ctx)
	if err != nil {
		return c.conn.observe(*in, err)
	}
	var roms []domain.ROM
	for offset := 0; ; offset += catalogPageSize {
		page, err := server.ROMs(ctx, cursor, offset, catalogPageSize)
		if err != nil {
			return c.conn.observe(*in, err)
		}
		roms = append(roms, page...)
		c.mu.Lock()
		c.fetched = len(roms)
		c.mu.Unlock()
		c.announce()
		if len(page) < catalogPageSize {
			break
		}
	}

	update := domain.CatalogSync{Platforms: platforms, ROMs: roms, Full: full, Cursor: cursor, At: c.now()}
	for _, r := range roms {
		if r.UpdatedAt.After(update.Cursor) {
			update.Cursor = r.UpdatedAt
		}
	}
	if !full {
		ids, err := server.ROMIDs(ctx)
		if err != nil {
			return c.conn.observe(*in, err)
		}
		update.KeepIDs = ids
	}
	if err := c.store.Apply(ctx, update); err != nil {
		return fmt.Errorf("save catalog: %w", err)
	}
	c.log.Info("catalog refreshed", "full", full, "platforms", len(platforms), "roms_received", len(roms),
		"server_roms", len(update.KeepIDs))
	return nil
}

// Platforms lists the platforms the Catalog holds games for.
func (c *Catalog) Platforms(ctx context.Context) ([]domain.PlatformSummary, error) {
	return c.store.Platforms(ctx)
}

// Games returns a page of a platform's games that match filter.
func (c *Catalog) Games(ctx context.Context, platformID int, filter domain.ROMFilter, offset, limit int) (domain.Platform, domain.ROMPage, error) {
	platform, ok, err := c.store.Platform(ctx, platformID)
	if err != nil {
		return platform, domain.ROMPage{}, err
	}
	if !ok {
		return platform, domain.ROMPage{}, errNotInCatalog
	}
	switch filter {
	case domain.FilterInstalled, domain.FilterUpdates:
		// Nothing is on this Deck until the plugin downloads it.
		return platform, domain.ROMPage{}, nil
	default:
		page, err := c.store.ROMs(ctx, platformID, offset, limit)
		return platform, page, err
	}
}

// Game returns a game and its platform.
func (c *Catalog) Game(ctx context.Context, id int) (domain.ROM, domain.Platform, error) {
	rom, ok, err := c.store.ROM(ctx, id)
	if err != nil {
		return rom, domain.Platform{}, err
	}
	if !ok {
		return rom, domain.Platform{}, errNotInCatalog
	}
	platform, _, err := c.store.Platform(ctx, rom.PlatformID)
	return rom, platform, err
}

var errNotInCatalog = &domain.Error{Kind: domain.KindNotFound, Message: "That's no longer in the library. Refresh the library and try again."}
