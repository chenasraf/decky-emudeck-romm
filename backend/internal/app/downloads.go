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
// in roms/ half-written. A download's bytes stay there between attempts, so a retry resumes them.
const stagingDir = ".romm-staging"

// downloadRetryDelays are the waits before each retry of a download the network cut off. An
// attempt that gets further than any before it starts the count over, so a download fails only
// once the link stays down for good.
var downloadRetryDelays = []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute}

// DownloadServer is the part of the RomM API downloading needs.
type DownloadServer interface {
	ROMFiles(ctx context.Context, id int) ([]domain.ROMFile, error)
	// DownloadFile fetches a file from byte offset from onward, calling open with the offset the
	// server's bytes start at before writing them to the writer it returns.
	DownloadFile(ctx context.Context, fileID int, name string, from int64, open func(start int64) (io.Writer, error)) error
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

// DownloadStore keeps the records of Plugin-owned ROMs and the download queue.
type DownloadStore interface {
	OwnershipStore
	SaveQueued(ctx context.Context, d domain.QueuedDownload) error
	DeleteQueued(ctx context.Context, id int) error
	Queued(ctx context.Context) ([]domain.QueuedDownload, error)
}

// Download is a game in the download queue.
type Download struct {
	ROMID        int
	PlatformID   int
	PlatformName string
	Name         string
	// State is StateQueued, StateDownloading or StateFailed.
	State    domain.GameState
	Received int64
	Total    int64
	// Problem says why the download failed; nil unless it is StateFailed.
	Problem error
}

// DownloadListener hears each change to a download. A download that leaves the queue is
// StateInstalled, or StateOnRomM when it was cancelled.
type DownloadListener func(Download)

type Downloads struct {
	catalog CatalogStore
	install *Install
	conn    *Connection
	dial    DownloadDialer
	// store is nil when state.db couldn't be opened, and problem says why.
	store    DownloadStore
	problem  error
	limit    int
	onChange DownloadListener
	onDelete DeleteListener
	log      *slog.Logger
	now      func() time.Time
	retries  []time.Duration
	// base outlives single calls: downloads run under it after the call that queued them returns.
	// It ends when the backend stops.
	base context.Context

	mu sync.Mutex
	// jobs are the queued, running and failed downloads, in the order they were queued.
	jobs    []*job
	running int
	// deleting are the games queued for deletion or being deleted.
	deleting map[int]bool

	// deleteMu is held while deletes run, so they run one at a time.
	deleteMu sync.Mutex
}

type job struct {
	Download
	// cancel stops the download while it runs; nil otherwise.
	cancel context.CancelFunc
	// partial is the Server's file whose bytes are in staging.
	partial domain.FileSnapshot
}

// NewDownloads returns the download queue, running up to limit downloads at once. A nil store
// turns downloading and deleting off, reporting problem.
func NewDownloads(ctx context.Context, catalog CatalogStore, install *Install, conn *Connection, dial DownloadDialer,
	store DownloadStore, problem error, limit int, onChange DownloadListener, onDelete DeleteListener, log *slog.Logger,
) *Downloads {
	return &Downloads{
		catalog: catalog, install: install, conn: conn, dial: dial, store: store, problem: problem,
		limit: max(limit, 1), onChange: onChange, onDelete: onDelete, log: log, now: time.Now,
		retries: downloadRetryDelays, base: ctx, deleting: map[int]bool{},
	}
}

// Problem says why downloading is off, nil when it works.
func (d *Downloads) Problem() error { return d.problem }

// Resume puts back the queue state.db kept from earlier runs and starts it, then deletes what
// staging holds for downloads no longer in the queue.
func (d *Downloads) Resume(ctx context.Context) {
	if d.store == nil {
		return
	}
	saved, err := d.store.Queued(ctx)
	if err != nil {
		// Staging is left alone: it may hold the bytes of the downloads that couldn't be read.
		d.log.Error("read download queue", "err", err)
		return
	}
	var restored []*job
	for _, q := range saved {
		// The backend stopped between recording the game and taking it out of the queue.
		if _, owned, err := d.store.Owned(ctx, q.ROMID); err == nil && owned {
			d.dequeue(q.ROMID)
			continue
		}
		j := &job{Download: Download{
			ROMID: q.ROMID, PlatformID: q.PlatformID, PlatformName: q.PlatformName, Name: q.Name,
			State: domain.StateQueued, Total: q.TotalBytes,
		}, partial: q.Partial}
		if q.Problem != nil {
			j.State, j.Problem = domain.StateFailed, q.Problem
		}
		restored = append(restored, j)
	}

	d.mu.Lock()
	// Games queued since the backend started go after the ones queued before.
	restored = slices.DeleteFunc(restored, func(j *job) bool { return d.find(j.ROMID) != nil })
	d.jobs = append(restored, d.jobs...)
	keep := map[string]bool{}
	for _, j := range d.jobs {
		keep[strconv.Itoa(j.ROMID)] = true
	}
	d.mu.Unlock()

	d.cleanStaging(keep)
	for _, j := range restored {
		d.log.Info("download restored", "rom_id", j.ROMID, "state", j.State)
		d.onChange(j.Download)
	}
	d.pump()
}

// cleanStaging deletes everything in staging but the folders named in keep.
func (d *Downloads) cleanStaging(keep map[string]bool) {
	root := d.install.State(false).EmuDeck.Root
	if root == "" {
		return
	}
	dir := filepath.Join(root, stagingDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			d.log.Warn("read staging", "err", err)
		}
		return
	}
	for _, e := range entries {
		if keep[e.Name()] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			d.log.Warn("clean staging", "path", e.Name(), "err", err)
		}
	}
}

// Queue adds a game to the download queue and returns its download. A game already queued or
// running returns the download as it is; a failed one is retried, resuming what it downloaded.
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
	j := d.find(id)
	switch {
	case j != nil && j.State != domain.StateFailed:
		d.mu.Unlock()
		return j.Download, nil
	case j != nil:
		j.State, j.Problem = domain.StateQueued, nil
		d.log.Info("download retried", "rom_id", id)
	default:
		j = &job{Download: Download{
			ROMID: id, PlatformID: rom.PlatformID, PlatformName: platform.Name, Name: rom.Name,
			State: domain.StateQueued, Total: rom.SizeBytes,
		}}
		d.jobs = append(d.jobs, j)
		d.log.Info("download queued", "rom_id", id, "name", rom.Name)
	}
	queued, record := j.Download, j.record()
	d.mu.Unlock()

	d.save(record)
	d.onChange(queued)
	d.pump()
	return queued, nil
}

// Cancel stops a game's download, takes it out of the queue, or dismisses its failure, deleting
// what it downloaded. It reports whether the game was in the queue.
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

	d.discard(id)
	ended.State, ended.Problem = domain.StateOnRomM, nil
	d.log.Info("download cancelled", "rom_id", id)
	d.onChange(ended)
	return true
}

// List returns the queued, running and failed downloads, in queue order.
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
		if j.cancel != nil || j.State == domain.StateFailed {
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
	err := d.attempts(ctx, j)
	if err != nil && d.base.Err() != nil {
		// The backend is stopping. The download stays in state.db's queue with its bytes in
		// staging, and the next start resumes it.
		return
	}

	d.mu.Lock()
	j.cancel()
	j.cancel = nil
	d.running--
	if err != nil && !errors.Is(err, context.Canceled) {
		j.State, j.Problem = domain.StateFailed, err
	} else {
		d.remove(j)
	}
	ended, record := j.Download, j.record()
	d.mu.Unlock()

	switch {
	case err == nil:
		d.discard(j.ROMID)
		ended.State = domain.StateInstalled
		d.log.Info("download installed", "rom_id", j.ROMID, "bytes", ended.Received, "duration", d.now().Sub(start).Round(time.Millisecond))
	case errors.Is(err, context.Canceled):
		d.discard(j.ROMID)
		ended.State = domain.StateOnRomM
		d.log.Info("download cancelled", "rom_id", j.ROMID)
	default:
		d.save(record)
		d.log.Warn("download failed", "rom_id", j.ROMID, "err", err)
	}
	d.onChange(ended)
	d.pump()
}

// attempts runs a download until it installs, is cancelled, fails on an error that retrying can't
// fix, or the network stays down through every retry. While the plugin is Signed out it waits for
// the user to sign in again.
func (d *Downloads) attempts(ctx context.Context, j *job) error {
	tries := 0
	var furthest int64
	for {
		if err := d.conn.WaitSignedIn(ctx); err != nil {
			return err
		}
		err := d.download(ctx, j)
		switch {
		case err == nil:
			return nil
		case ctx.Err() != nil:
			return ctx.Err()
		case kindOf(err) == domain.KindSignedOut:
			// RomM rejected the token, which signed the plugin out; the next wait holds until the
			// user signs in again.
			continue
		case kindOf(err) != domain.KindUnreachable:
			return err
		}

		if got := d.received(j); got > furthest {
			furthest, tries = got, 0
		}
		if tries == len(d.retries) {
			return err
		}
		wait := d.retries[tries]
		tries++
		d.log.Info("download retrying", "rom_id", j.ROMID, "try", tries, "in", wait, "err", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// download fetches a single-file game into staging, resuming bytes an earlier attempt left there,
// and moves it into its ROM folder with one rename, then records it as Plugin-owned. It never
// replaces a file already at the target.
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

	staging := d.staging(j.ROMID)
	snapshot := domain.SnapshotOf(file)
	d.mu.Lock()
	same := samePartial(j.partial, snapshot)
	j.partial = snapshot
	record := j.record()
	d.mu.Unlock()
	if !same {
		// What staging holds, if anything, is of a file the Server no longer has.
		if err := os.RemoveAll(staging); err != nil {
			return err
		}
		d.save(record)
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return fmt.Errorf("create staging folder: %w", err)
	}
	partial := filepath.Join(staging, file.Name)

	d.mu.Lock()
	j.State, j.Total = domain.StateDownloading, file.SizeBytes
	started := j.Download
	d.mu.Unlock()
	d.onChange(started)

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
	owned := domain.OwnedROM{
		ROMID:       j.ROMID,
		PlatformID:  rom.PlatformID,
		Files:       []domain.PlacedFile{{Path: target, SizeBytes: info.Size(), ModTime: info.ModTime().UTC(), SHA1: sum}},
		Snapshot:    []domain.FileSnapshot{snapshot},
		InstalledAt: d.now().UTC(),
	}
	// The file is in place, so the record is written even if the download is cancelled now.
	if err := d.store.SaveOwned(context.WithoutCancel(ctx), owned); err != nil {
		// Without a record the file would be User-managed, and the plugin could never update or
		// delete it. It is the plugin's own download, so it goes.
		if rmErr := os.Remove(target); rmErr != nil {
			d.log.Error("remove unrecorded download", "path", target, "err", rmErr)
		}
		return fmt.Errorf("record download: %w", err)
	}
	return nil
}

// samePartial says whether bytes kept for the file kept describes belong to the file now
// describes. Only the fields state.db keeps are compared.
func samePartial(kept, now domain.FileSnapshot) bool {
	return kept.FileID == now.FileID && kept.SizeBytes == now.SizeBytes && kept.LastModified.Equal(now.LastModified)
}

// fetch downloads file to path, resuming from the bytes already there, reporting progress on j,
// and returns the SHA-1 of the whole file.
func (d *Downloads) fetch(ctx context.Context, server DownloadServer, file domain.ROMFile, path string, j *job) (string, error) {
	var from int64
	if info, err := os.Stat(path); err == nil {
		from = info.Size()
	}
	if file.SizeBytes > 0 && from > file.SizeBytes {
		from = 0
	}

	var out *os.File
	var w *progressWriter
	open := func(start int64) (io.Writer, error) {
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
		if err != nil {
			return nil, fmt.Errorf("open download: %w", err)
		}
		out = f
		if err := f.Truncate(start); err != nil {
			return nil, fmt.Errorf("truncate download: %w", err)
		}
		// The kept bytes are part of the file, so they go into its hash first. Reading them leaves
		// the file positioned where new bytes go.
		h := sha1.New() //nolint:gosec // see import
		if _, err := io.CopyN(h, f, start); err != nil {
			return nil, fmt.Errorf("read kept download: %w", err)
		}
		w = &progressWriter{w: f, hash: h, n: start, report: func(n int64) { d.progress(j, n) }, every: progressEvery, now: d.now}
		d.progress(j, start)
		return w, nil
	}

	var err error
	if file.SizeBytes > 0 && from == file.SizeBytes {
		// An earlier attempt received every byte and stopped before placing the file.
		_, err = open(from)
	} else {
		err = server.DownloadFile(ctx, file.ID, file.Name, from, open)
	}
	if out != nil {
		if err == nil {
			err = out.Sync()
		}
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
	}
	if err != nil {
		return "", err
	}
	if w == nil {
		return "", errors.New("the server sent nothing to write")
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
	d.onChange(update)
}

func (d *Downloads) received(j *job) int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return j.Received
}

// staging is the folder a game's download lands in.
func (d *Downloads) staging(id int) string {
	return filepath.Join(d.install.State(false).EmuDeck.Root, stagingDir, strconv.Itoa(id))
}

// save writes a download to state.db's queue. A download whose write fails still runs; it just
// won't outlive the backend.
func (d *Downloads) save(q domain.QueuedDownload) {
	if err := d.store.SaveQueued(context.WithoutCancel(d.base), q); err != nil {
		d.log.Warn("save download queue", "rom_id", q.ROMID, "err", err)
	}
}

// dequeue takes a download out of state.db's queue.
func (d *Downloads) dequeue(id int) {
	if err := d.store.DeleteQueued(context.WithoutCancel(d.base), id); err != nil {
		d.log.Warn("save download queue", "rom_id", id, "err", err)
	}
}

// discard takes a download that left the queue out of state.db and deletes its staging.
func (d *Downloads) discard(id int) {
	d.dequeue(id)
	if d.install.State(false).EmuDeck.Root == "" {
		return
	}
	if err := os.RemoveAll(d.staging(id)); err != nil {
		d.log.Warn("remove staging", "rom_id", id, "err", err)
	}
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

// record is the job as state.db's queue keeps it.
func (j *job) record() domain.QueuedDownload {
	q := domain.QueuedDownload{
		ROMID: j.ROMID, PlatformID: j.PlatformID, PlatformName: j.PlatformName, Name: j.Name,
		TotalBytes: j.Total, Partial: j.partial,
	}
	if j.Problem != nil {
		var de *domain.Error
		if !errors.As(j.Problem, &de) {
			de = &domain.Error{Message: j.Problem.Error()}
		}
		q.Problem = &domain.Error{Kind: de.Kind, Message: de.Message}
	}
	return q
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
