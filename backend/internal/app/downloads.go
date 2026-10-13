package app

import (
	"context"
	"crypto/sha1" //nolint:gosec // matches files against RomM's SHA-1, not for security
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// progressEvery is how often a running download reports its progress.
const progressEvery = 250 * time.Millisecond

// stagingDir is where downloads land under the Emulation folder before one rename moves them into
// roms/. It sits on the same drive as roms/, so the rename never copies, and a game never shows up
// in roms/ half-written.
const stagingDir = ".romm-staging"

// DownloadServer is the part of the RomM API downloading needs.
type DownloadServer interface {
	ROMFiles(ctx context.Context, id int) ([]domain.ROMFile, error)
	DownloadFile(ctx context.Context, fileID int, name string, w io.Writer) error
}

// DownloadDialer returns a DownloadServer for server, authenticated with token.
type DownloadDialer func(server domain.Server, token string) DownloadServer

// OwnershipStore keeps the records of Plugin-owned ROMs.
type OwnershipStore interface {
	SaveOwned(ctx context.Context, rom domain.OwnedROM) error
	Owned(ctx context.Context, id int) (domain.OwnedROM, bool, error)
	OwnedIDs(ctx context.Context) ([]int, error)
	DeleteOwned(ctx context.Context, id int) error
}

// Download is a game in the download queue.
type Download struct {
	ROMID        int
	PlatformID   int
	PlatformName string
	Name         string
	// State is StateQueued or StateDownloading.
	State    domain.GameState
	Received int64
	Total    int64
}

// DownloadUpdate is a change to a download. A download that ends is StateInstalled, or
// StateOnRomM when it failed or was cancelled.
type DownloadUpdate struct {
	Download
	// Problem says why a download ended without installing; nil when it was cancelled.
	Problem error
}

// DownloadListener hears each change to a download.
type DownloadListener func(DownloadUpdate)

type Downloads struct {
	catalog CatalogStore
	install *Install
	conn    *Connection
	dial    DownloadDialer
	// store is nil when state.db couldn't be opened, and problem says why.
	store    OwnershipStore
	problem  error
	limit    int
	onChange DownloadListener
	onDelete DeleteListener
	log      *slog.Logger
	now      func() time.Time
	// base outlives single calls: downloads run under it after the call that queued them returns.
	base context.Context

	mu sync.Mutex
	// jobs are the queued and running downloads, in the order they were queued.
	jobs    []*job
	running int
	// deleting are the games queued for deletion or being deleted.
	deleting map[int]bool

	// deleteMu is held while deletes run, so they run one at a time.
	deleteMu sync.Mutex
}

type job struct {
	Download
	// cancel stops the download once it runs.
	cancel context.CancelFunc
}

// NewDownloads returns the download queue, running up to limit downloads at once. A nil store
// turns downloading and deleting off, reporting problem.
func NewDownloads(ctx context.Context, catalog CatalogStore, install *Install, conn *Connection, dial DownloadDialer,
	store OwnershipStore, problem error, limit int, onChange DownloadListener, onDelete DeleteListener, log *slog.Logger,
) *Downloads {
	return &Downloads{
		catalog: catalog, install: install, conn: conn, dial: dial, store: store, problem: problem,
		limit: max(limit, 1), onChange: onChange, onDelete: onDelete, log: log, now: time.Now, base: ctx,
		deleting: map[int]bool{},
	}
}

// Problem says why downloading is off, nil when it works.
func (d *Downloads) Problem() error { return d.problem }

// CleanStaging deletes what earlier runs left in the staging folder. Nothing resumes from it, so
// whatever is there is a download that a crash or shutdown cut short.
func (d *Downloads) CleanStaging() {
	root := d.install.State(false).EmuDeck.Root
	if root == "" {
		return
	}
	if err := os.RemoveAll(filepath.Join(root, stagingDir)); err != nil {
		d.log.Warn("clean staging", "err", err)
	}
}

// Queue adds a game to the download queue, or returns the download already queued for it.
func (d *Downloads) Queue(ctx context.Context, id int) (Download, error) {
	if d.store == nil {
		return Download{}, d.problem
	}
	if in, err := d.conn.activeSignIn(); err != nil || in == nil {
		if err != nil {
			return Download{}, err
		}
		return Download{}, errNotSignedIn
	}
	rom, platform, err := d.lookup(ctx, id)
	if err != nil {
		return Download{}, err
	}
	if _, err := d.targetDir(rom, platform); err != nil {
		return Download{}, err
	}
	if _, owned, err := d.store.Owned(ctx, id); err != nil || owned {
		if err != nil {
			return Download{}, err
		}
		return Download{}, &domain.Error{Kind: domain.KindInvalidInput, Message: "That game is already on this Deck."}
	}

	d.mu.Lock()
	if j := d.find(id); j != nil {
		d.mu.Unlock()
		return j.Download, nil
	}
	j := &job{Download: Download{
		ROMID: id, PlatformID: rom.PlatformID, PlatformName: platform.Name, Name: rom.Name,
		State: domain.StateQueued, Total: rom.SizeBytes,
	}}
	d.jobs = append(d.jobs, j)
	queued := j.Download
	d.mu.Unlock()

	d.log.Info("download queued", "rom_id", id, "name", rom.Name)
	d.onChange(DownloadUpdate{Download: queued})
	d.pump()
	return queued, nil
}

// Cancel stops a game's download, or takes it out of the queue, deleting what it downloaded. It
// reports whether the game was in the queue.
func (d *Downloads) Cancel(id int) bool {
	d.mu.Lock()
	j := d.find(id)
	if j == nil {
		d.mu.Unlock()
		return false
	}
	if j.cancel != nil {
		// run takes it out of the queue once it stops.
		j.cancel()
		d.mu.Unlock()
		return true
	}
	d.remove(j)
	ended := j.Download
	d.mu.Unlock()

	ended.State = domain.StateOnRomM
	d.log.Info("download cancelled", "rom_id", id)
	d.onChange(DownloadUpdate{Download: ended})
	return true
}

// List returns the queued and running downloads, in queue order.
func (d *Downloads) List() []Download {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Download, 0, len(d.jobs))
	for _, j := range d.jobs {
		out = append(out, j.Download)
	}
	return out
}

// States returns the state of every game that isn't just On RomM.
func (d *Downloads) States(ctx context.Context) (map[int]domain.GameState, error) {
	out := map[int]domain.GameState{}
	if d.store != nil {
		ids, err := d.store.OwnedIDs(ctx)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			out[id] = domain.StateInstalled
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, j := range d.jobs {
		out[j.ROMID] = j.State
	}
	for id := range d.deleting {
		out[id] = domain.StateDeleting
	}
	return out, nil
}

// Owned returns the record of a Plugin-owned ROM.
func (d *Downloads) Owned(ctx context.Context, id int) (domain.OwnedROM, bool, error) {
	if d.store == nil {
		return domain.OwnedROM{}, false, nil
	}
	return d.store.Owned(ctx, id)
}

// pump starts queued downloads while fewer than limit run.
func (d *Downloads) pump() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, j := range d.jobs {
		if d.running >= d.limit {
			return
		}
		if j.cancel != nil {
			continue
		}
		ctx, cancel := context.WithCancel(d.base)
		j.cancel = cancel
		d.running++
		go d.run(ctx, j)
	}
}

func (d *Downloads) run(ctx context.Context, j *job) {
	start := d.now()
	err := d.download(ctx, j)

	d.mu.Lock()
	j.cancel()
	d.remove(j)
	d.running--
	ended := DownloadUpdate{Download: j.Download}
	d.mu.Unlock()

	switch {
	case err == nil:
		ended.State = domain.StateInstalled
		d.log.Info("download installed", "rom_id", j.ROMID, "bytes", ended.Received, "duration", d.now().Sub(start).Round(time.Millisecond))
	case errors.Is(err, context.Canceled):
		ended.State = domain.StateOnRomM
		d.log.Info("download cancelled", "rom_id", j.ROMID)
	default:
		ended.State = domain.StateOnRomM
		ended.Problem = err
		d.log.Warn("download failed", "rom_id", j.ROMID, "err", err)
	}
	d.onChange(ended)
	d.pump()
}

// download fetches a single-file game into staging and moves it into its ROM folder with one
// rename, then records it as Plugin-owned. It never replaces a file already at the target.
func (d *Downloads) download(ctx context.Context, j *job) error {
	in, err := d.conn.activeSignIn()
	if err != nil {
		return err
	}
	if in == nil {
		return errNotSignedIn
	}
	rom, platform, err := d.lookup(ctx, j.ROMID)
	if err != nil {
		return err
	}
	dir, err := d.targetDir(rom, platform)
	if err != nil {
		return err
	}
	server := d.dial(in.Server, in.Token)
	files, err := server.ROMFiles(ctx, j.ROMID)
	if err != nil {
		return d.conn.observe(*in, err)
	}
	game := domain.GameFiles(files)
	if len(game) != 1 {
		return &domain.Error{Kind: domain.KindCantDownload, Message: fmt.Sprintf("RomM lists %d game files for this game; the plugin places games that are a single file.", len(game))}
	}
	file := game[0]
	target := filepath.Join(dir, file.Name)
	if err := checkFree(target); err != nil {
		return err
	}

	staging := filepath.Join(d.install.State(false).EmuDeck.Root, stagingDir, strconv.Itoa(j.ROMID))
	if err := os.RemoveAll(staging); err != nil {
		return err
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return fmt.Errorf("create staging folder: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(staging); err != nil {
			d.log.Warn("remove staging", "path", staging, "err", err)
		}
	}()
	partial := filepath.Join(staging, file.Name)

	d.mu.Lock()
	j.State, j.Total = domain.StateDownloading, file.SizeBytes
	started := j.Download
	d.mu.Unlock()
	d.onChange(DownloadUpdate{Download: started})

	sum, err := d.fetch(ctx, server, file, partial, j)
	if err != nil {
		return d.conn.observe(*in, err)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	// Checked again: a file may have appeared while the download ran.
	if err := checkFree(target); err != nil {
		return err
	}
	if err := os.Rename(partial, target); err != nil {
		return fmt.Errorf("move into %s: %w", dir, err)
	}
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	record := domain.OwnedROM{
		ROMID:       j.ROMID,
		PlatformID:  rom.PlatformID,
		Files:       []domain.PlacedFile{{Path: target, SizeBytes: info.Size(), ModTime: info.ModTime().UTC(), SHA1: sum}},
		Snapshot:    []domain.FileSnapshot{domain.SnapshotOf(file)},
		InstalledAt: d.now().UTC(),
	}
	// The file is in place, so the record is written even if the download is cancelled now.
	if err := d.store.SaveOwned(context.WithoutCancel(ctx), record); err != nil {
		// Without a record the file would be User-managed, and the plugin could never update or
		// delete it. It is the plugin's own download, so it goes.
		if rmErr := os.Remove(target); rmErr != nil {
			d.log.Error("remove unrecorded download", "path", target, "err", rmErr)
		}
		return fmt.Errorf("record download: %w", err)
	}
	return nil
}

// fetch downloads file to path, reporting progress on j, and returns the SHA-1 of what it wrote.
func (d *Downloads) fetch(ctx context.Context, server DownloadServer, file domain.ROMFile, path string, j *job) (string, error) {
	out, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("create download: %w", err)
	}
	w := &progressWriter{w: out, hash: sha1.New(), report: func(n int64) { d.progress(j, n) }, every: progressEvery, now: d.now}
	err = server.DownloadFile(ctx, file.ID, file.Name, w)
	if err == nil {
		err = out.Sync()
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	w.flush()
	if file.SizeBytes > 0 && w.n != file.SizeBytes {
		return "", &domain.Error{
			Kind:    domain.KindUnreachable,
			Message: "The download from RomM was the wrong size.",
			Err:     fmt.Errorf("got %d bytes, RomM lists %d", w.n, file.SizeBytes),
		}
	}
	return hex.EncodeToString(w.hash.Sum(nil)), nil
}

func (d *Downloads) progress(j *job, n int64) {
	d.mu.Lock()
	j.Received = n
	update := j.Download
	d.mu.Unlock()
	d.onChange(DownloadUpdate{Download: update})
}

func (d *Downloads) lookup(ctx context.Context, id int) (domain.ROM, domain.Platform, error) {
	rom, ok, err := d.catalog.ROM(ctx, id)
	if err != nil {
		return rom, domain.Platform{}, err
	}
	if !ok {
		return rom, domain.Platform{}, errNotInCatalog
	}
	platform, _, err := d.catalog.Platform(ctx, rom.PlatformID)
	return rom, platform, err
}

// targetDir is the ROM folder a game goes in.
func (d *Downloads) targetDir(rom domain.ROM, platform domain.Platform) (string, error) {
	state := d.install.State(false)
	if !state.EmuDeckFound() {
		return "", &domain.Error{Kind: domain.KindCantDownload, Message: "EmuDeck wasn't found, so there's nowhere to put the game. See Setup status."}
	}
	system, ok := domain.SystemForSlug(platform.Slug)
	if !ok {
		return "", &domain.Error{Kind: domain.KindCantDownload, Message: "EmuDeck has no folder for " + platform.Name + "."}
	}
	if rom.MultiFile {
		return "", &domain.Error{Kind: domain.KindCantDownload, Message: "This game is a folder of several files; the plugin places games that are a single file."}
	}
	i := slices.IndexFunc(state.Systems, func(s domain.SystemState) bool { return s.Folder == system.Folder })
	if i < 0 {
		return "", fmt.Errorf("no install state for %s", system.Folder)
	}
	return state.Systems[i].ROMPath, nil
}

// checkFree fails when something is at path. The plugin never replaces a file it didn't download.
func checkFree(path string) error {
	_, err := os.Lstat(path)
	switch {
	case err == nil:
		return &domain.Error{
			Kind:    domain.KindTargetExists,
			Message: fmt.Sprintf("%s is already in %s. The plugin doesn't replace files it didn't download.", filepath.Base(path), filepath.Dir(path)),
		}
	case errors.Is(err, os.ErrNotExist):
		return nil
	default:
		return err
	}
}

func (d *Downloads) find(id int) *job {
	for _, j := range d.jobs {
		if j.ROMID == id {
			return j
		}
	}
	return nil
}

func (d *Downloads) remove(j *job) {
	d.jobs = slices.DeleteFunc(d.jobs, func(o *job) bool { return o == j })
}

var errNotSignedIn = &domain.Error{Kind: domain.KindSignedOut, Message: "Sign in to RomM to download games."}

// progressWriter writes to w, hashing what it writes and reporting the running total at most once
// every interval.
type progressWriter struct {
	w      io.Writer
	hash   hash.Hash
	n      int64
	report func(n int64)
	every  time.Duration
	now    func() time.Time
	last   time.Time
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.hash.Write(b[:n])
	p.n += int64(n)
	if t := p.now(); t.Sub(p.last) >= p.every {
		p.last = t
		p.report(p.n)
	}
	return n, err
}

// flush reports the final total.
func (p *progressWriter) flush() { p.report(p.n) }
