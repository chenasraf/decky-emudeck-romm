package app

import (
	"archive/zip"
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
	"strings"
	"sync"
	"syscall"
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

// spaceMargin is the room a download leaves free on the Emulation drive, for saves, logs and the
// filesystem itself.
const spaceMargin = 256 << 20

// spacePoll is how often a queue on hold for space looks at the drive again. Deletes and dismissed
// downloads look right away.
const spacePoll = 30 * time.Second

// FreeSpace returns how many bytes may still be written to the drive holding path.
type FreeSpace func(path string) (int64, error)

// DownloadPrefs keeps the Keep archives toggle and the Install to folder of each EmuDeck system,
// by its folder.
type DownloadPrefs interface {
	KeepArchives(folder string) bool
	SetKeepArchives(folder string, keep bool) error
	// InstallTo is empty for the system's own folder.
	InstallTo(system string) string
	SetInstallTo(system, folder string) error
}

// DownloadServer is the part of the RomM API downloading needs.
type DownloadServer interface {
	ROMFiles(ctx context.Context, id int) ([]domain.ROMFile, error)
	// PlatformFiles returns the files of up to limit of a platform's ROMs from offset, ordered by id.
	PlatformFiles(ctx context.Context, platformID, offset, limit int) ([]domain.ROMWithFiles, error)
	// DownloadFile fetches a file from byte offset from onward, calling open with the offset the
	// server's bytes start at before writing them to the writer it returns.
	DownloadFile(ctx context.Context, fileID int, name string, from int64, open func(start int64) (io.Writer, error)) error
	// Convert asks for a ROM's file in format, waiting while the Server converts it, and returns
	// the file the Server will serve. It fails with domain.ErrNoConversion when the Server won't.
	Convert(ctx context.Context, romID, fileID int, name, format string) (domain.ServedFile, error)
	// DownloadConverted is DownloadFile for the file Convert found ready.
	DownloadConverted(ctx context.Context, romID, fileID int, name, format string, from int64, open func(start int64) (io.Writer, error)) error
}

// DownloadDialer returns a DownloadServer for server, authenticated with token.
type DownloadDialer func(server domain.Server, token string) DownloadServer

// OwnershipStore keeps the records of Plugin-owned ROMs.
type OwnershipStore interface {
	SaveOwned(ctx context.Context, rom domain.OwnedROM) error
	Owned(ctx context.Context, id int) (domain.OwnedROM, bool, error)
	OwnedIDs(ctx context.Context) ([]int, error)
	DeleteOwned(ctx context.Context, id int) error
	// MoveOwned replaces the record of the ROM with id from by rom, at once.
	MoveOwned(ctx context.Context, from int, rom domain.OwnedROM) error
	// SetUpdateAvailable records whether the Server has an update of the ROM with id. Saving its
	// record anew clears it.
	SetUpdateAvailable(ctx context.Context, id int, available bool) error
	UpdateIDs(ctx context.Context) ([]int, error)
	// IgnoreUpdate records files as the update of the ROM with id the user ignores, clearing its
	// update.
	IgnoreUpdate(ctx context.Context, id int, files []domain.FileSnapshot) error
}

// DownloadStore keeps the records of Plugin-owned ROMs and Linked games, and the download queue.
type DownloadStore interface {
	OwnershipStore
	LinkStore
	SaveQueued(ctx context.Context, d domain.QueuedDownload) error
	DeleteQueued(ctx context.Context, id int) error
	Queued(ctx context.Context) ([]domain.QueuedDownload, error)
	// ForgetServer drops every record tied to the Server, leaving the files.
	ForgetServer(ctx context.Context) error
	// LastSweep and SetLastSweep keep when the last sweep for updates finished.
	LastSweep(ctx context.Context) (time.Time, error)
	SetLastSweep(ctx context.Context, at time.Time) error
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
	// Converting is set while the Server converts the game to a format its emulator plays.
	Converting bool
	// WaitingForGame is set while the download, transferred, waits for a running game to close
	// before it's extracted or zipped.
	WaitingForGame bool
}

// DownloadListener hears each change to a download. A download that leaves the queue is
// StateInstalled, or StateOnRomM when it was cancelled.
type DownloadListener func(Download)

// HoldListener hears the queue going on hold, with why, and coming off it, with nil.
type HoldListener func(error)

// DownloadListeners hear what the download queue does.
type DownloadListeners struct {
	Change DownloadListener
	Delete DeleteListener
	Hold   HoldListener
	// Queued, when set, hears each game newly added to the queue; a retry isn't one.
	Queued DownloadListener
	// Ownership, when set, hears each game adopted or linked.
	Ownership OwnershipListener
	// Find, when set, hears Find my games start, make progress and end.
	Find FindListener
	// Folder, when set, hears each download or delete end, with what it changed in a ROM folder.
	Folder FolderListener
}

type Downloads struct {
	catalog CatalogStore
	install *Install
	conn    *Connection
	dial    DownloadDialer
	// store is nil when state.db couldn't be opened, and problem says why.
	store   DownloadStore
	problem error
	free    FreeSpace
	prefs   DownloadPrefs
	games   *Games
	// media is nil when no art is placed for ES-DE.
	media    *Media
	limit    int
	onChange DownloadListener
	onDelete DeleteListener
	onHold   HoldListener
	onQueued DownloadListener
	// onOwnership and onFind are nil when nothing listens.
	onOwnership OwnershipListener
	onFind      FindListener
	onFolder    FolderListener
	log         *slog.Logger
	now         func() time.Time
	retries     []time.Duration
	spacePoll   time.Duration
	// base outlives single calls: downloads run under it after the call that queued them returns.
	// It ends when the backend stops.
	base context.Context

	mu sync.Mutex
	// jobs are the queued, running and failed downloads, in the order they were queued.
	jobs    []*job
	running int
	// held is set while the drive is too full to start the next download.
	held bool
	// deleting are the games queued for deletion or being deleted.
	deleting map[int]bool
	// checks are the comparisons of User-managed files with RomM's games, by ROM id.
	checks map[int]*onDeckJob
	// finds are how Find my games stands, by platform id, and found what it found, by ROM id.
	finds map[int]*FindStatus
	found map[int]foundGame

	// scanMu guards scans and sums: what sits where each platform's games would go, by platform
	// id, and the hashes of files compared with RomM's, by path.
	scanMu sync.Mutex
	scans  map[int]folderScan
	sums   map[string]cachedSums

	// deleteMu is held while deletes run, so they run one at a time.
	deleteMu sync.Mutex
}

type job struct {
	Download
	// cancel stops the download while it runs; nil otherwise.
	cancel context.CancelFunc
	// partial is the Server's file whose bytes are in staging.
	partial domain.FileSnapshot
	// update is set when the download replaces the files of a Plugin-owned game with its update.
	update bool
}

// NewDownloads returns the download queue, running up to limit downloads at once. A nil store
// turns downloading and deleting off, reporting problem. Extracting and zipping wait for games to
// close; a nil games never waits. Installs place their art for ES-DE through media, unless it's nil.
func NewDownloads(ctx context.Context, catalog CatalogStore, install *Install, conn *Connection, dial DownloadDialer,
	store DownloadStore, problem error, free FreeSpace, prefs DownloadPrefs, games *Games, media *Media, limit int, listen DownloadListeners,
	log *slog.Logger,
) *Downloads {
	return &Downloads{
		catalog: catalog, install: install, conn: conn, dial: dial, store: store, problem: problem, free: free, prefs: prefs, games: games, media: media,
		limit: max(limit, 1), onChange: listen.Change, onDelete: listen.Delete, onHold: listen.Hold, onQueued: listen.Queued,
		onOwnership: listen.Ownership, onFind: listen.Find, onFolder: listen.Folder, log: log, now: time.Now, retries: downloadRetryDelays, spacePoll: spacePoll, base: ctx,
		deleting: map[int]bool{}, checks: map[int]*onDeckJob{}, finds: map[int]*FindStatus{}, found: map[int]foundGame{},
		scans: map[int]folderScan{}, sums: map[string]cachedSums{},
	}
}

// Problem says why downloading is off, nil when it works.
func (d *Downloads) Problem() error { return d.problem }

// Held says why the queue is on hold, nil when it isn't.
func (d *Downloads) Held() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.held {
		return errHeld
	}
	return nil
}

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
		if d.recorded(ctx, q) {
			d.dequeue(q.ROMID)
			continue
		}
		j := &job{Download: Download{
			ROMID: q.ROMID, PlatformID: q.PlatformID, PlatformName: q.PlatformName, Name: q.Name,
			State: domain.StateQueued, Total: q.TotalBytes,
		}, partial: q.Partial, update: q.Update}
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
	if rom.Removed {
		return Download{}, errRemoved
	}
	system, dir, err := d.targetDir(rom, platform)
	if err != nil {
		return Download{}, err
	}
	// A Plugin-owned game downloads again only as its update, which replaces its files.
	update := false
	if _, owned, err := d.store.Owned(ctx, id); err != nil || owned {
		if err != nil {
			return Download{}, err
		}
		updates, err := d.store.UpdateIDs(ctx)
		if err != nil {
			return Download{}, err
		}
		if !slices.Contains(updates, id) {
			return Download{}, &domain.Error{Kind: domain.KindInvalidInput, Message: "That game is already on this Deck."}
		}
		update = true
	}
	if path, ok, err := d.occupant(ctx, rom, platform); !update && (err != nil || ok) {
		if err != nil {
			return Download{}, err
		}
		return Download{}, &domain.Error{
			Kind:    domain.KindTargetExists,
			Message: fmt.Sprintf("%s is already in %s, and the plugin never downloads over it. Adopt it or link it to RomM on the game's page.", filepath.Base(path), filepath.Dir(path)),
		}
	}

	d.mu.Lock()
	j := d.find(id)
	if j != nil && j.State != domain.StateFailed {
		d.mu.Unlock()
		return j.Download, nil
	}
	need := roomFor(system, rom)
	if j != nil {
		// A retry resumes, so only what's left of it needs room.
		need += j.Total - j.Received - rom.SizeBytes
	}
	ahead := d.pending()
	d.mu.Unlock()
	if err := d.checkSpace(need, ahead, dir); err != nil {
		return Download{}, err
	}

	d.mu.Lock()
	j = d.find(id)
	added := j == nil
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
		}, update: update}
		d.jobs = append(d.jobs, j)
		d.log.Info("download queued", "rom_id", id, "name", rom.Name, "update", update)
	}
	queued, record := j.Download, j.record()
	d.mu.Unlock()

	d.save(record)
	d.onChange(queued)
	if added && d.onQueued != nil {
		d.onQueued(queued)
	}
	d.pump()
	return queued, nil
}

// roomFor is how much room a download of rom needs on its drive.
func roomFor(system domain.System, rom domain.ROM) int64 {
	need := rom.SizeBytes
	if domain.IsArchive(rom.FileName) && system.ArchivePolicy() != domain.ArchivesKeep {
		// Extracting needs room for the contents while the archive is still there. Whether a
		// Keep archives system keeps this one isn't known until it's opened.
		need += rom.SizeBytes
	}
	return need
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
	if j.update {
		ended.State = domain.StateUpdate
	}
	d.log.Info("download cancelled", "rom_id", id)
	d.onChange(ended)
	d.folderChanged(0, 0)
	d.release()
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
		onDeck, err := d.onDeck(ctx)
		if err != nil {
			return nil, err
		}
		for id := range onDeck {
			out[id] = domain.StateOnDeck
		}
		// A Plugin-owned game's own file is where it would go, so owning comes after.
		ids, err := d.store.OwnedIDs(ctx)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			out[id] = domain.StateInstalled
		}
		updates, err := d.store.UpdateIDs(ctx)
		if err != nil {
			return nil, err
		}
		for _, id := range updates {
			out[id] = domain.StateUpdate
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
	if d.held {
		return
	}
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
		if j.update {
			// The old version is still in place, its update still to take.
			ended.State = domain.StateUpdate
		}
		d.log.Info("download cancelled", "rom_id", j.ROMID)
	default:
		d.save(record)
		d.log.Warn("download failed", "rom_id", j.ROMID, "err", err)
	}
	d.onChange(ended)
	// An update keeps the game's place in the ROM folder, so there's nothing new to find there.
	added := 0
	if err == nil && !j.update {
		added = 1
	}
	d.folderChanged(added, 0)
	if kindOf(err) == domain.KindNoSpace {
		d.hold()
	} else {
		d.release()
	}
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
		if errors.Is(err, syscall.ENOSPC) {
			return &domain.Error{Kind: domain.KindNoSpace, Message: "Out of space on the Emulation drive.", Err: err}
		}
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

// download fetches a game's files into staging, resuming bytes an earlier attempt left there, and
// lays them out the way its system's ROM folder needs: one file as it is, or extracted when it's
// an archive its system extracts; several files in the system's layout. It then moves the result
// into the ROM folder and records it as Plugin-owned. It never replaces anything already there.
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
	system, dir, err := d.targetDir(rom, platform)
	if err != nil {
		return err
	}
	// An update replaces the files the plugin placed, in the folder they're in.
	var old, oldMedia []domain.PlacedFile
	if j.update {
		rec, ok, err := d.store.Owned(ctx, j.ROMID)
		if err != nil {
			return err
		}
		if !ok {
			return &domain.Error{Kind: domain.KindInvalidInput, Message: "The plugin no longer manages that game, so it can't update it."}
		}
		old, oldMedia = rec.Files, rec.Media
		if len(old) > 0 {
			if held := d.dirHolding(ctx, rom.PlatformID, old[0].Path); held != "" {
				dir = held
			}
		}
	}
	server := d.dial(in.Server, in.Token)
	files, err := server.ROMFiles(ctx, j.ROMID)
	if err != nil {
		return d.conn.observe(*in, err)
	}
	game := domain.GameFiles(files)
	name := rom.FileName
	if name == "" {
		name = rom.Name
	}
	layout, err := domain.PlanLayout(system, name, game)
	if err != nil {
		return err
	}
	whole := len(layout.Files) == 1 && !layout.Zip
	_, convertible := system.ConversionFor(layout.Root)
	if !whole || (!domain.IsArchive(layout.Root) || system.ArchivePolicy() == domain.ArchivesKeep) && !convertible {
		// The game's entry in the ROM folder is known before it downloads, so one already there
		// fails it now rather than after the download. A file the Server may convert is checked
		// once its name is known.
		if err := checkTarget(filepath.Join(dir, layout.Root), old); err != nil {
			return err
		}
	}

	staging := d.staging(j.ROMID)
	snapshot := domain.SnapshotOfSet(game)
	d.mu.Lock()
	same := samePartial(j.partial, snapshot)
	j.partial = snapshot
	record := j.record()
	d.mu.Unlock()
	if !same {
		// What staging holds, if anything, is of files the Server no longer has.
		if err := os.RemoveAll(staging); err != nil {
			return err
		}
		d.save(record)
	}

	// A game its emulator can't play as stored is asked for in a format it can. stored is the file
	// as the Server keeps it, which the conversion is asked for by name.
	var conversion *domain.Conversion
	var stored domain.ROMFile
	if c, ok := system.ConversionFor(layout.Root); ok && whole {
		stored = layout.Files[0].File
		served, ok, err := d.convert(ctx, server, j, stored, c)
		if err != nil {
			return d.conn.observe(*in, err)
		}
		if ok {
			conversion = &c
			file := domain.ROMFile{ID: stored.ID, Name: served.Name, Path: served.Name, SizeBytes: served.Size}
			layout = domain.Layout{Root: served.Name, Files: []domain.PlannedFile{{File: file, Rel: served.Name}}}
			if err := checkTarget(filepath.Join(dir, layout.Root), old); err != nil {
				return err
			}
		}
	}

	var total int64
	for _, f := range layout.Files {
		total += f.File.SizeBytes
	}
	d.mu.Lock()
	j.State, j.Total = domain.StateDownloading, total
	started := j.Download
	d.mu.Unlock()
	d.onChange(started)

	// Each file lands in staging where it sits inside the game, and resumes on its own. A converted
	// file has a staging folder of its own, so its bytes never mix with the stored file's.
	sums, paths := make([]string, len(layout.Files)), make([]string, len(layout.Files))
	var done int64
	for i, f := range layout.Files {
		path := filepath.Join(staging, filepath.FromSlash(f.File.Path))
		get := func(from int64, open func(int64) (io.Writer, error)) error {
			return server.DownloadFile(ctx, f.File.ID, f.File.Name, from, open)
		}
		if conversion != nil {
			path = filepath.Join(staging, convertedDir, conversion.To, f.File.Name)
			get = func(from int64, open func(int64) (io.Writer, error)) error {
				return server.DownloadConverted(ctx, j.ROMID, f.File.ID, stored.Name, conversion.To, from, open)
			}
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create staging folder: %w", err)
		}
		sum, err := d.fetch(get, f.File, path, j, done)
		if err != nil {
			return d.conn.observe(*in, err)
		}
		sums[i], paths[i], done = sum, path, done+f.File.SizeBytes
	}

	var staged []stagedFile
	switch {
	case layout.Zip:
		if err = d.waitForGame(ctx, j); err == nil {
			staged, err = zipGame(layout, paths, staging, dir, d.checkSpace)
		}
	case whole:
		staged, err = d.unpack(ctx, j, system, paths[0], sums[0], dir)
	default:
		staged, err = stageSet(layout, paths, staging, sums)
	}
	if err != nil {
		return err
	}
	place := d.place
	if j.update {
		place = func(staged []stagedFile, dir string) ([]domain.PlacedFile, error) { return d.swap(staged, dir, old) }
	}
	placed, err := place(staged, dir)
	if err != nil {
		return err
	}
	// The snapshot is of the files as the Server keeps them, which later checks for updates
	// compare against, even when the plugin placed a converted copy.
	snapshots := make([]domain.FileSnapshot, 0, len(game))
	for _, f := range layout.Files {
		if conversion != nil {
			f.File = stored
		}
		snapshots = append(snapshots, domain.SnapshotOf(f.File))
	}
	// The files are in place, so the rest runs even if the download is cancelled now. An update's
	// art goes the way of its old files, then comes again from RomM, except where something replaced
	// it.
	ctx = context.WithoutCancel(ctx)
	d.removeMedia(oldMedia)
	media := d.placeMedia(ctx, j.ROMID, dir, layout.Root)
	owned := domain.OwnedROM{
		ROMID:       j.ROMID,
		PlatformID:  rom.PlatformID,
		Files:       placed,
		Snapshot:    snapshots,
		InstalledAt: d.now().UTC(),
		Media:       media,
	}
	if err := d.store.SaveOwned(ctx, owned); err != nil {
		// Without a record the files would be User-managed, and the plugin could never update or
		// delete them. They are the plugin's own download, so they go.
		d.removeFiles(placed, dir)
		d.removeMedia(media)
		return fmt.Errorf("record download: %w", err)
	}
	return nil
}

// extractedDir is where an archive's contents go in its staging folder.
const extractedDir = ".extracted"

// convertedDir is where files the Server converted go in a game's staging folder, one folder per
// format.
const convertedDir = ".converted"

// convert asks the Server for file in the format c names, and returns the file it serves. ok is
// false when the Server can't convert it, because it predates conversion, has it off or fails at
// it, and the file is downloaded as stored. Its game page then says what's left to do.
func (d *Downloads) convert(ctx context.Context, server DownloadServer, j *job, file domain.ROMFile, c domain.Conversion) (domain.ServedFile, bool, error) {
	d.setConverting(j, true)
	defer d.setConverting(j, false)
	served, err := server.Convert(ctx, j.ROMID, file.ID, file.Name, c.To)
	switch {
	case errors.Is(err, domain.ErrNoConversion):
		d.log.Info("conversion unavailable", "rom_id", j.ROMID, "file", file.Name, "format", c.To)
		return served, false, nil
	case err != nil:
		return served, false, err
	case !strings.EqualFold(filepath.Ext(served.Name), "."+c.To):
		// A server that predates conversion serves the file as stored.
		d.log.Info("conversion unavailable", "rom_id", j.ROMID, "file", file.Name, "served", served.Name)
		return served, false, nil
	}
	if served.Size < 0 {
		return served, false, errors.New("the server didn't say how big the converted file is")
	}
	d.log.Info("converted", "rom_id", j.ROMID, "file", served.Name, "bytes", served.Size)
	return served, true, nil
}

func (d *Downloads) setConverting(j *job, on bool) {
	d.mu.Lock()
	j.Converting = on
	if on {
		j.State = domain.StateDownloading
	}
	update := j.Download
	d.mu.Unlock()
	d.onChange(update)
}

// waitForGame holds j while a game runs. It gives up j's slot meanwhile, so the downloads behind it
// keep transferring.
func (d *Downloads) waitForGame(ctx context.Context, j *job) error {
	return d.games.Wait(ctx, func(on bool) {
		d.mu.Lock()
		j.WaitingForGame = on
		if on {
			d.running--
		} else {
			d.running++
		}
		update := j.Download
		d.mu.Unlock()
		d.onChange(update)
		if on {
			d.pump()
		}
	})
}

// madeDir is where the files the plugin makes for a game, a playlist or a zip, go in its staging
// folder.
const madeDir = ".made"

// stageSet returns the downloaded files of a game of several files, at paths with their sums,
// and the playlist its layout calls for, written to staging.
func stageSet(layout domain.Layout, paths []string, staging string, sums []string) ([]stagedFile, error) {
	out := make([]stagedFile, 0, len(layout.Files)+1)
	for i, f := range layout.Files {
		out = append(out, stagedFile{From: paths[i], Rel: f.Rel, SHA1: sums[i]})
	}
	if layout.Playlist == nil {
		return out, nil
	}
	content := []byte(strings.Join(layout.Playlist.Lines, "\n") + "\n")
	path := filepath.Join(staging, madeDir, filepath.Base(layout.Playlist.Rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return nil, fmt.Errorf("write playlist: %w", err)
	}
	sum := sha1.Sum(content) //nolint:gosec // see import
	return append(out, stagedFile{From: path, Rel: layout.Playlist.Rel, SHA1: hex.EncodeToString(sum[:])}), nil
}

// zipGame zips the downloaded files of a game, at paths, into one archive in staging, checking
// dir's drive has room for it, and returns it to place.
func zipGame(layout domain.Layout, paths []string, staging, dir string, checkSpace func(need, ahead int64, dir string) error) ([]stagedFile, error) {
	var size int64
	for _, f := range layout.Files {
		size += f.File.SizeBytes
	}
	if err := checkSpace(size, 0, dir); err != nil {
		return nil, err
	}
	path := filepath.Join(staging, madeDir, layout.Root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	sum, err := writeZip(path, layout, paths)
	if err != nil {
		return nil, fmt.Errorf("zip %s: %w", layout.Root, err)
	}
	return []stagedFile{{From: path, Rel: layout.Root, SHA1: sum}}, nil
}

func writeZip(path string, layout domain.Layout, paths []string) (string, error) {
	out, err := os.Create(path)
	if err != nil {
		return "", err
	}
	h := sha1.New() //nolint:gosec // see import
	zw := zip.NewWriter(io.MultiWriter(out, h))
	err = func() error {
		for i, f := range layout.Files {
			w, err := zw.Create(filepath.ToSlash(f.Rel))
			if err != nil {
				return err
			}
			in, err := os.Open(paths[i])
			if err != nil {
				return err
			}
			_, err = io.Copy(w, in)
			_ = in.Close()
			if err != nil {
				return err
			}
		}
		return zw.Close()
	}()
	if err == nil {
		err = out.Sync()
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// stagedFile is a file in staging waiting to move into its ROM folder.
type stagedFile struct {
	From string
	// Rel is where it goes under the ROM folder.
	Rel  string
	SHA1 string
}

// unpack decides whether the downloaded file at path is placed as it is or extracted into dir,
// and returns what to place: the file itself, or the archive's contents.
func (d *Downloads) unpack(ctx context.Context, j *job, system domain.System, path, sum, dir string) ([]stagedFile, error) {
	whole := []stagedFile{{From: path, Rel: filepath.Base(path), SHA1: sum}}
	if !domain.IsArchive(path) || system.ArchivePolicy() == domain.ArchivesKeep {
		return whole, nil
	}
	members, closeArchive, err := openArchive(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeArchive() }()
	if len(members) == 0 {
		return nil, &domain.Error{Kind: domain.KindCantDownload, Message: "The archive from RomM is empty."}
	}
	if !system.Extracts(len(members), d.prefs.KeepArchives(system.Folder)) {
		return whole, nil
	}
	if err := d.waitForGame(ctx, j); err != nil {
		return nil, err
	}

	var size int64
	for _, m := range members {
		size += m.Size
	}
	if err := d.checkSpace(size, 0, dir); err != nil {
		return nil, err
	}
	dest := filepath.Join(filepath.Dir(path), extractedDir)
	// What an earlier attempt extracted may be incomplete.
	if err := os.RemoveAll(dest); err != nil {
		return nil, err
	}
	files, err := extract(members, dest)
	if err != nil {
		return nil, err
	}
	d.log.Info("extracted", "archive", filepath.Base(path), "files", len(files), "bytes", size)
	out := make([]stagedFile, 0, len(files))
	for _, f := range files {
		out = append(out, stagedFile{From: filepath.Join(dest, f.Rel), Rel: f.Rel, SHA1: f.SHA1})
	}
	return out, nil
}

// place moves staged files into dir and returns them as placed. Nothing is moved when any target
// is taken, and what was moved goes back out when a move fails.
func (d *Downloads) place(staged []stagedFile, dir string) ([]domain.PlacedFile, error) {
	for _, f := range staged {
		if err := checkFree(filepath.Join(dir, f.Rel)); err != nil {
			return nil, err
		}
	}
	placed := make([]domain.PlacedFile, 0, len(staged))
	for _, f := range staged {
		target := filepath.Join(dir, f.Rel)
		err := os.MkdirAll(filepath.Dir(target), 0o755)
		if err == nil {
			err = moveFile(f.From, target)
		}
		var info os.FileInfo
		if err == nil {
			info, err = os.Stat(target)
		}
		if err != nil {
			d.removeFiles(placed, dir)
			return nil, fmt.Errorf("move into %s: %w", dir, err)
		}
		placed = append(placed, domain.PlacedFile{Path: target, SizeBytes: info.Size(), ModTime: info.ModTime().UTC(), SHA1: f.SHA1})
	}
	return placed, nil
}

// rename is os.Rename, replaced in tests to stand in for a move across drives.
var rename = os.Rename

// moveFile moves from to to, which must not exist. Across drives (a ROM folder linked to an SD
// card, say) a rename can't, so it copies to a temporary name beside to and renames that: to
// never shows up half-written.
func moveFile(from, to string) error {
	err := rename(from, to)
	if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	tmp := to + ".romm-part"
	if err := copyFile(from, tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, to); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Remove(from)
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	return err
}

// removeFiles deletes placed files, then the folders under dir they leave empty.
func (d *Downloads) removeFiles(files []domain.PlacedFile, dir string) {
	for _, f := range files {
		if err := os.Remove(f.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			d.log.Error("remove placed file", "path", f.Path, "err", err)
		}
		removeEmptyDirs(filepath.Dir(f.Path), dir)
	}
}

// removeEmptyDirs removes path and its parents while they are empty, stopping at dir, which stays.
func removeEmptyDirs(path, dir string) {
	if dir == "" {
		return
	}
	for path != dir && strings.HasPrefix(path, dir+string(filepath.Separator)) {
		if os.Remove(path) != nil {
			return
		}
		path = filepath.Dir(path)
	}
}

// pending is how many bytes the queue has still to download, failed downloads aside. d.mu must
// be held.
func (d *Downloads) pending() int64 {
	var n int64
	for _, j := range d.jobs {
		if j.State != domain.StateFailed {
			n += max(j.Total-j.Received, 0)
		}
	}
	return n
}

// checkSpace fails when staging's drive or dir's hasn't room for need bytes after the ahead bytes
// the queue has still to download, keeping spaceMargin free. The two are often one drive, but a
// ROM folder can be a link to another. A drive it can't measure doesn't block: a download that
// runs out of space still stops cleanly.
func (d *Downloads) checkSpace(need, ahead int64, dir string) error {
	total := need + ahead + spaceMargin
	for _, path := range []string{d.install.State(false).EmuDeck.Root, dir} {
		free, err := d.freeAt(path)
		if err != nil {
			d.log.Warn("measure free space", "path", path, "err", err)
			continue
		}
		if free >= total {
			continue
		}
		msg := fmt.Sprintf("Needs %s, %s free in %s.", domain.FormatSize(total), domain.FormatSize(free), path)
		if ahead > 0 {
			msg = fmt.Sprintf("Needs %s with the downloads ahead of it, %s free in %s.", domain.FormatSize(total), domain.FormatSize(free), path)
		}
		return &domain.Error{Kind: domain.KindNoSpace, Message: msg}
	}
	return nil
}

// freeAt measures the drive path is on, or would be on once created: its nearest folder that
// exists. Links are followed, so a folder linked to another drive measures that drive.
func (d *Downloads) freeAt(path string) (int64, error) {
	if path == "" {
		return 0, errors.New("no path to measure")
	}
	for {
		if _, err := os.Stat(path); err == nil {
			return d.free(path)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return d.free(path)
		}
		path = parent
	}
}

// hold stops the queue starting downloads until the drive has room for the next one, so a full
// drive fails the downloads running when it filled rather than every one queued.
func (d *Downloads) hold() {
	d.mu.Lock()
	if d.held {
		d.mu.Unlock()
		return
	}
	d.held = true
	d.mu.Unlock()
	d.log.Warn("download queue on hold", "reason", "out of space")
	d.onHold(errHeld)
	go d.watchSpace()
}

// watchSpace releases the hold once the drive has room again.
func (d *Downloads) watchSpace() {
	t := time.NewTicker(d.spacePoll)
	defer t.Stop()
	for !d.release() {
		select {
		case <-d.base.Done():
			return
		case <-t.C:
		}
	}
}

// release takes the queue off hold once the drive has room for the next queued download, and
// reports whether the queue is off hold.
func (d *Downloads) release() bool {
	d.mu.Lock()
	if !d.held {
		d.mu.Unlock()
		return true
	}
	next, platform := int64(-1), 0
	for _, j := range d.jobs {
		if j.State == domain.StateQueued && j.cancel == nil {
			next, platform = max(j.Total-j.Received, 0), j.PlatformID
			break
		}
	}
	d.mu.Unlock()
	if next >= 0 && d.checkSpace(next, 0, d.platformDir(d.base, platform)) != nil {
		return false
	}

	d.mu.Lock()
	d.held = false
	d.mu.Unlock()
	d.log.Info("download queue off hold")
	d.onHold(nil)
	d.pump()
	return true
}

// samePartial says whether bytes kept for the file kept describes belong to the file now
// describes. Only the fields state.db keeps are compared.
func samePartial(kept, now domain.FileSnapshot) bool {
	return kept.FileID == now.FileID && kept.SizeBytes == now.SizeBytes && kept.LastModified.Equal(now.LastModified)
}

// fetch downloads file to path, resuming from the bytes already there, and returns the SHA-1 of
// the whole file. It reports progress on j, counting base bytes of the game's other files before
// this one.
func (d *Downloads) fetch(get func(from int64, open func(int64) (io.Writer, error)) error, file domain.ROMFile, path string, j *job, base int64) (string, error) {
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
		w = &progressWriter{w: f, hash: h, n: start, report: func(n int64) { d.progress(j, base+n) }, every: progressEvery, now: d.now}
		d.progress(j, base+start)
		return w, nil
	}

	var err error
	if file.SizeBytes > 0 && from == file.SizeBytes {
		// An earlier attempt received every byte and stopped before placing the file.
		_, err = open(from)
	} else {
		err = get(from, open)
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

// targetDir is the System a game belongs to and the ROM folder it goes in.
func (d *Downloads) targetDir(rom domain.ROM, platform domain.Platform) (domain.System, string, error) {
	state := d.install.State(false)
	if !state.EmuDeckFound() {
		return domain.System{}, "", &domain.Error{Kind: domain.KindCantDownload, Message: "EmuDeck wasn't found, so there's nowhere to put the game. See Setup status."}
	}
	system, ok := domain.SystemForSlug(platform.Slug)
	if !ok {
		return system, "", &domain.Error{Kind: domain.KindCantDownload, Message: "EmuDeck has no folder for " + platform.Name + "."}
	}
	if rom.MultiFile && system.MultiFile == "" {
		return system, "", &domain.Error{Kind: domain.KindCantDownload, Message: "This game is several files, and the plugin has no way to lay those out in EmuDeck's " + system.Folder + " folder."}
	}
	dir := folderPath(state, system, d.installFolder(system))
	if dir == "" {
		return system, "", fmt.Errorf("no install state for %s", system.Folder)
	}
	return system, dir, nil
}

// installFolder is the ROM folder Install to picks for a system's new downloads: its own folder
// unless it's set to one of its alt folders.
func (d *Downloads) installFolder(system domain.System) string {
	if f := d.prefs.InstallTo(system.Folder); slices.Contains(system.AltFolders, f) {
		return f
	}
	return system.Folder
}

// folderPath is the path of folder, system's own or one of its alt folders, empty when the install
// state has no ROMs folder for it.
func folderPath(state domain.InstallState, system domain.System, folder string) string {
	if folder != system.Folder {
		if state.EmuDeck.ROMsPath == "" {
			return ""
		}
		return filepath.Join(state.EmuDeck.ROMsPath, folder)
	}
	i := slices.IndexFunc(state.Systems, func(s domain.SystemState) bool { return s.Folder == system.Folder })
	if i < 0 {
		return ""
	}
	return state.Systems[i].ROMPath
}

// platformDir is the ROM folder a platform's new downloads go in, empty when it has none.
func (d *Downloads) platformDir(ctx context.Context, platformID int) string {
	system, ok := d.platformSystem(ctx, platformID)
	if !ok {
		return ""
	}
	return folderPath(d.install.State(false), system, d.installFolder(system))
}

// dirHolding is the ROM folder of a platform's that path is in: the one Install to picked when the
// game was placed, which may not be the one it picks now. Empty when path is in none of them.
func (d *Downloads) dirHolding(ctx context.Context, platformID int, path string) string {
	system, ok := d.platformSystem(ctx, platformID)
	if !ok {
		return ""
	}
	state := d.install.State(false)
	for _, f := range system.Folders() {
		if dir := folderPath(state, system, f); dir != "" && strings.HasPrefix(path, dir+string(filepath.Separator)) {
			return dir
		}
	}
	return ""
}

// InstallFolder is a ROM folder a platform's games can go in.
type InstallFolder struct {
	Folder string
	Path   string
	// Primary is set on the system's own folder.
	Primary bool
	// SRMSkips is set when Steam ROM Manager, as EmuDeck sets it up, never adds the folder's games to
	// Steam.
	SRMSkips bool
}

// InstallTo returns the ROM folder a platform's new downloads go in, and the folders Install to
// can pick from: the system's own first, then its alt folders. Both are empty for a platform
// EmuDeck has no folder for.
func (d *Downloads) InstallTo(ctx context.Context, platformID int) (string, []InstallFolder) {
	system, ok := d.platformSystem(ctx, platformID)
	if !ok {
		return "", nil
	}
	state := d.install.State(false)
	var out []InstallFolder
	for _, f := range system.Folders() {
		out = append(out, InstallFolder{
			Folder:   f,
			Path:     folderPath(state, system, f),
			Primary:  f == system.Folder,
			SRMSkips: domain.SRMSkips(f),
		})
	}
	return d.installFolder(system), out
}

// SetInstallTo picks the ROM folder a platform's new downloads go in. Games already placed stay
// where they are.
func (d *Downloads) SetInstallTo(ctx context.Context, platformID int, folder string) error {
	system, ok := d.platformSystem(ctx, platformID)
	if !ok {
		return &domain.Error{Kind: domain.KindInvalidInput, Message: "EmuDeck has no folder for that platform."}
	}
	if !slices.Contains(system.Folders(), folder) {
		return &domain.Error{Kind: domain.KindInvalidInput, Message: folder + " isn't one of EmuDeck's folders for " + system.Folder + "."}
	}
	if err := d.prefs.SetInstallTo(system.Folder, folder); err != nil {
		return err
	}
	d.log.Info("install to set", "system", system.Folder, "folder", folder)
	return nil
}

// platformSystem is the System a platform's games go in.
func (d *Downloads) platformSystem(ctx context.Context, platformID int) (domain.System, bool) {
	platform, ok, err := d.catalog.Platform(ctx, platformID)
	if err != nil || !ok {
		return domain.System{}, false
	}
	return domain.SystemForSlug(platform.Slug)
}

// Archives returns what the plugin does with a platform's archives, and whether its Keep archives
// toggle is on. The policy is empty for a platform EmuDeck has no folder for.
func (d *Downloads) Archives(ctx context.Context, platformID int) (domain.ArchivePolicy, bool) {
	system, ok := d.platformSystem(ctx, platformID)
	if !ok {
		return "", false
	}
	return system.ArchivePolicy(), d.prefs.KeepArchives(system.Folder)
}

// SetKeepArchives sets the Keep archives toggle of a platform's system. It applies to downloads
// from then on; games already placed stay as they are.
func (d *Downloads) SetKeepArchives(ctx context.Context, platformID int, keep bool) error {
	system, ok := d.platformSystem(ctx, platformID)
	if !ok {
		return &domain.Error{Kind: domain.KindInvalidInput, Message: "EmuDeck has no folder for that platform."}
	}
	if system.ArchivePolicy() != domain.ArchivesOptional {
		return &domain.Error{Kind: domain.KindInvalidInput, Message: "Keep archives doesn't apply to " + system.Folder + "."}
	}
	return d.prefs.SetKeepArchives(system.Folder, keep)
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
		TotalBytes: j.Total, Partial: j.partial, Update: j.update,
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

var errHeld = &domain.Error{Kind: domain.KindNoSpace, Message: "The Emulation drive is full. Downloads wait until there's room for the next one."}

var errRemoved = &domain.Error{Kind: domain.KindNotFound, Message: "That game is no longer on RomM, so it can't be downloaded."}

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
