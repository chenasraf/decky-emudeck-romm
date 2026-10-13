package app

import (
	"context"
	"crypto/md5" //nolint:gosec // RomM identifies save content by its MD5, not for security
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// saveBackups is how many overwritten copies of each game's save are kept.
const saveBackups = 5

// SaveServer is the part of the RomM API save sync needs.
type SaveServer interface {
	NegotiateSync(ctx context.Context, req domain.SyncRequest) (domain.SyncPlan, error)
	UploadSave(ctx context.Context, u domain.SaveUpload) (domain.ServerSave, error)
	DownloadSave(ctx context.Context, id int, deviceID string, sessionID int) ([]byte, error)
	ConfirmSaveDownload(ctx context.Context, id int, deviceID, hash string) error
	CompleteSync(ctx context.Context, sessionID, completed, failed int) error
}

// SaveDialer returns a SaveServer for server, authenticated with token.
type SaveDialer func(server domain.Server, token string) SaveServer

// SaveStore keeps what save sync knows of each game's save, and the activity log of what it did.
type SaveStore interface {
	PutSaveRecord(ctx context.Context, r domain.SaveRecord) error
	SaveRecords(ctx context.Context) ([]domain.SaveRecord, error)
	AddActivity(ctx context.Context, a domain.SaveActivity) error
	// Activity returns the newest limit entries, of the ROM with id or of every ROM when id is
	// zero, newest first.
	Activity(ctx context.Context, id, limit int) ([]domain.SaveActivity, error)
	// ResolveConflicts marks the ROM's conflict entries settled.
	ResolveConflicts(ctx context.Context, id int) error
	// PruneActivity drops entries older than before, and all but the newest keep, except
	// unresolved conflicts.
	PruneActivity(ctx context.Context, before time.Time, keep int) (int64, error)
}

// Launchers finds the command ES-DE launches a game with, rel being its path under its system's
// ROM folder.
type Launchers interface {
	Launcher(system, rel string) (domain.GameLauncher, error)
}

// SaveStatus is where a game's save sync stands.
type SaveStatus struct {
	ROMID int
	// Syncs is unset when the plugin doesn't sync the game's saves; Why says why.
	Syncs bool
	Why   string
	// Path is the save file, and Emulator the RetroArch core it's tagged with on RomM.
	Path     string
	Emulator string
	// State is empty until a sync looks at the game.
	State     domain.SaveState
	Problem   string
	CheckedAt time.Time
	Syncing   bool
	// Conflict is set while the save is in conflict.
	Conflict *SaveConflict
}

// SaveConflict is what the user compares to settle a conflict.
type SaveConflict struct {
	// ServerSaveID is RomM's save the conflict is with, and ServerAt when it was written; zero when
	// RomM didn't say, and a sync shows them.
	ServerSaveID int
	ServerAt     time.Time
	// DeckAt is when the save on this Deck was last written.
	DeckAt time.Time
	// Parked says the Deck's copy is also kept on RomM, outside every slot.
	Parked bool
}

// SaveSummary counts what a sync did, or what save sync stands at overall.
type SaveSummary struct {
	Games      int
	Uploaded   int
	Downloaded int
	Conflicts  int
	Waiting    int
	Failed     int
}

// SaveSyncEvent is a sync starting, with Syncing set, or ending with what it did and, when it
// couldn't run at all, why.
type SaveSyncEvent struct {
	Syncing bool
	ROMIDs  []int
	Summary SaveSummary
	Problem error
	// Auto is set on a sync nobody asked for: a save written, a periodic pull, the QAM opening.
	Auto bool
}

// SaveListener hears syncs start and end.
type SaveListener func(SaveSyncEvent)

// Saves syncs the saves of Plugin-owned games RetroArch plays with RomM, one sync at a time. RomM
// works out what each sync takes; the plugin uploads, downloads, and never resolves a conflict on
// its own.
type Saves struct {
	catalog   CatalogStore
	install   *Install
	conn      *Connection
	dial      SaveDialer
	owned     OwnershipStore
	store     SaveStore
	problem   error
	launchers Launchers
	running   RunningGame
	backups   string
	onChange  SaveListener
	log       *slog.Logger
	now       func() time.Time
	base      context.Context

	pullEvery time.Duration
	settle    time.Duration
	pollEvery time.Duration
	openGap   time.Duration

	// passMu is held while a sync runs.
	passMu sync.Mutex
	mu     sync.Mutex
	// pending are the games the next sync covers, every game when pendingAll is set, and
	// pendingManual says whether the user asked for any of it.
	pending       map[int]bool
	pendingAll    bool
	pendingManual bool
	// current are the games the running sync covers, every game when currentAll is set.
	current    map[int]bool
	currentAll bool
	// working is set while a goroutine runs the queued syncs.
	working bool
	lastEnd time.Time
	// waits is set while a game's download may be waiting for RetroArch to close.
	waits bool
	// wrote is when the plugin last wrote each save, so the watch doesn't take its own writes for
	// the game's.
	wrote map[string]time.Time
}

const (
	// savePullEvery is how often every game's save is synced, to pick up what other devices did.
	savePullEvery = 15 * time.Minute
	// saveSettle is how long a save goes unwritten before it uploads: RetroArch writes one in
	// several steps, and some cores every few seconds while a game runs.
	saveSettle = 10 * time.Second
	// savePoll is how often a download waiting for RetroArch looks whether it closed.
	savePoll = 5 * time.Second
	// saveOpenGap is how long after a sync opening the QAM doesn't sync again.
	saveOpenGap = 2 * time.Minute
)

// NewSaves returns save sync. A nil store turns it off, reporting problem. Overwritten saves are
// backed up under backups.
func NewSaves(ctx context.Context, catalog CatalogStore, install *Install, conn *Connection, dial SaveDialer, owned OwnershipStore,
	store SaveStore, problem error, launchers Launchers, running RunningGame, backups string, onChange SaveListener, log *slog.Logger,
) *Saves {
	return &Saves{
		catalog: catalog, install: install, conn: conn, dial: dial, owned: owned, store: store, problem: problem,
		launchers: launchers, running: running, backups: backups, onChange: onChange, log: log, now: time.Now, base: ctx,
		pullEvery: savePullEvery, settle: saveSettle, pollEvery: savePoll, openGap: saveOpenGap,
		pending: map[int]bool{}, current: map[int]bool{}, wrote: map[string]time.Time{}, waits: true,
	}
}

// saveTarget is a game whose save the plugin syncs.
type saveTarget struct {
	romID    int
	name     string
	path     string
	emulator string
}

// Start syncs the saves of the games with ids, or of every game when ids is empty, in the
// background. It fails right away when save sync can't run at all, or when the one game asked for
// doesn't sync.
func (s *Saves) Start(ctx context.Context, ids []int) error {
	if _, err := s.ready(); err != nil {
		return err
	}
	if len(ids) == 1 {
		if _, why, err := s.target(ctx, ids[0]); err != nil || why != "" {
			if err != nil {
				return err
			}
			return &domain.Error{Kind: domain.KindInvalidInput, Message: why}
		}
	}
	s.request(ids, true)
	return nil
}

// Opened syncs every game's save when the QAM opens, unless a sync ran or runs just now.
func (s *Saves) Opened() {
	s.mu.Lock()
	recent := s.working || s.now().Sub(s.lastEnd) < s.openGap
	s.mu.Unlock()
	if !recent {
		s.request(nil, false)
	}
}

// request queues a sync of the games with ids, every game when ids is empty. Syncs asked for while
// one runs gather into the next.
func (s *Saves) request(ids []int, manual bool) {
	s.mu.Lock()
	if len(ids) == 0 {
		s.pendingAll = true
	}
	for _, id := range ids {
		s.pending[id] = true
	}
	s.pendingManual = s.pendingManual || manual
	start := !s.working
	s.working = true
	s.mu.Unlock()
	if start {
		go s.work()
	}
}

// work runs the queued syncs until none is left.
func (s *Saves) work() {
	for {
		s.mu.Lock()
		if !s.pendingAll && len(s.pending) == 0 {
			s.working = false
			s.mu.Unlock()
			return
		}
		var ids []int
		if !s.pendingAll {
			for id := range s.pending {
				ids = append(ids, id)
			}
			slices.Sort(ids)
		}
		manual := s.pendingManual
		s.current, s.currentAll = s.pending, s.pendingAll
		s.pending, s.pendingAll, s.pendingManual = map[int]bool{}, false, false
		s.mu.Unlock()

		// A sync nobody asked for that can't run, before sign-in say, has nothing to tell anyone.
		if _, err := s.ready(); err != nil && !manual {
			s.log.Debug("save sync skipped", "err", err)
			s.mu.Lock()
			s.current, s.currentAll = map[int]bool{}, false
			s.mu.Unlock()
			continue
		}

		s.onChange(SaveSyncEvent{Syncing: true, ROMIDs: ids, Auto: !manual})
		summary, err := s.Sync(s.base, ids)
		s.mu.Lock()
		s.current, s.currentAll, s.lastEnd = map[int]bool{}, false, s.now()
		if summary.Waiting > 0 {
			s.waits = true
		}
		s.mu.Unlock()
		if err != nil && !manual {
			s.log.Info("save sync skipped", "err", err)
		}
		s.onChange(SaveSyncEvent{ROMIDs: ids, Summary: summary, Problem: err, Auto: !manual})
	}
}

// syncing says whether a queued or running sync covers the game with id.
func (s *Saves) syncing(id int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pendingAll || s.currentAll || s.pending[id] || s.current[id]
}

// SaveWatch reports the files written in the folders it watches.
type SaveWatch interface {
	Watch(dirs ...string) error
	Changes() <-chan string
}

// Run trims the activity log, then syncs saves on their own until ctx ends: every game's at start
// and every pullEvery, a game's once its save settles after RetroArch writes it, and a download that
// waits for RetroArch once it closes. A nil watch leaves out the syncs on writes.
func (s *Saves) Run(ctx context.Context, watch SaveWatch) {
	s.pruneActivity(ctx)
	s.request(nil, false)
	pull := time.NewTicker(s.pullEvery)
	defer pull.Stop()
	poll := time.NewTicker(s.pollEvery)
	defer poll.Stop()
	settled := make(chan string, 16)
	timers := map[string]*time.Timer{}
	defer func() {
		for _, t := range timers {
			t.Stop()
		}
	}()
	var changes <-chan string
	if watch != nil {
		changes = watch.Changes()
	}
	watched := ""
	for {
		if dir, ok := retroArchSaves(s.install.State(false)); watch != nil && ok && dir != watched {
			if err := watch.Watch(dir); err != nil {
				s.log.Warn("watch saves", "dir", dir, "err", err)
			} else {
				watched = dir
				s.log.Info("watching saves", "dir", dir)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-pull.C:
			s.request(nil, false)
		case p, ok := <-changes:
			if !ok {
				changes = nil
				continue
			}
			if t := timers[p]; t != nil {
				t.Reset(s.settle)
				continue
			}
			timers[p] = time.AfterFunc(s.settle, func() {
				select {
				case settled <- p:
				case <-ctx.Done():
				}
			})
		case p := <-settled:
			delete(timers, p)
			s.saveWritten(ctx, p)
		case <-poll.C:
			s.retryWaiting(ctx)
		}
	}
}

// saveWritten syncs the game whose save was just written, unless the plugin wrote it.
func (s *Saves) saveWritten(ctx context.Context, path string) {
	s.mu.Lock()
	at, own := s.wrote[path]
	delete(s.wrote, path)
	s.mu.Unlock()
	if own && s.now().Sub(at) < 2*s.settle {
		return
	}
	if s.store == nil {
		return
	}
	ids, err := s.owned.OwnedIDs(ctx)
	if err != nil {
		s.log.Warn("save written", "path", path, "err", err)
		return
	}
	for _, id := range ids {
		if t, why, err := s.target(ctx, id); err == nil && why == "" && t.path == path {
			s.log.Info("save written", "rom_id", id, "path", path)
			s.request([]int{id}, false)
			return
		}
	}
}

// retryWaiting syncs the games whose downloads wait for RetroArch, once it's closed.
func (s *Saves) retryWaiting(ctx context.Context) {
	s.mu.Lock()
	waits := s.waits
	s.mu.Unlock()
	if !waits || s.store == nil {
		return
	}
	if emu, ok := s.running(); ok && emu.ID == domain.RetroArchID {
		return
	}
	records, err := s.store.SaveRecords(ctx)
	if err != nil {
		return
	}
	var ids []int
	for _, r := range records {
		if r.State == domain.SaveWaiting {
			ids = append(ids, r.ROMID)
		}
	}
	s.mu.Lock()
	s.waits = false
	s.mu.Unlock()
	if len(ids) > 0 {
		s.log.Info("RetroArch closed; syncing waiting saves", "games", len(ids))
		s.request(ids, false)
	}
}

// ready returns the sign-in a sync talks to RomM with, or why it can't run.
func (s *Saves) ready() (*domain.SignIn, error) {
	if s.store == nil {
		return nil, s.problem
	}
	in, err := s.conn.activeSignIn()
	if err != nil {
		return nil, err
	}
	if in == nil {
		return nil, &domain.Error{Kind: domain.KindSignedOut, Message: "Sign in to RomM to sync saves."}
	}
	if in.DeviceID == "" {
		return nil, &domain.Error{
			Kind:    domain.KindMissingPermission,
			Message: "This Deck signed in without pairing as a device, which save sync needs. Sign in again with a pairing.",
		}
	}
	for _, d := range domain.DisabledFeatures(in.Scopes) {
		if d.Feature == domain.FeatureSaveSync {
			return nil, &domain.Error{
				Kind:    domain.KindMissingPermission,
				Message: "RomM didn't grant this Deck the permissions save sync needs: " + strings.Join(d.MissingScopes, ", ") + ".",
			}
		}
	}
	if _, ok := retroArchSaves(s.install.State(false)); !ok {
		return nil, &domain.Error{
			Kind:    domain.KindInvalidInput,
			Message: "RetroArch's saves folder wasn't found. Set RetroArch up in EmuDeck, then check Setup status again.",
		}
	}
	return in, nil
}

// retroArchSaves is the folder RetroArch keeps its saves in, links followed.
func retroArchSaves(state domain.InstallState) (string, bool) {
	i := slices.IndexFunc(state.Emulators, func(e domain.EmulatorState) bool { return e.ID == domain.RetroArchID })
	if i < 0 || !state.Emulators[i].SaveSyncReady() {
		return "", false
	}
	return state.Emulators[i].Saves[0].Resolved, true
}

// Sync syncs the saves of the games with ids, or of every Plugin-owned game when ids is empty, and
// returns what it did. It fails only when it couldn't ask RomM; how each game went is in its
// SaveStatus.
func (s *Saves) Sync(ctx context.Context, ids []int) (SaveSummary, error) {
	s.passMu.Lock()
	defer s.passMu.Unlock()
	in, err := s.ready()
	if err != nil {
		return SaveSummary{}, err
	}
	if len(ids) == 0 {
		if ids, err = s.owned.OwnedIDs(ctx); err != nil {
			return SaveSummary{}, err
		}
	}
	records, err := s.records(ctx)
	if err != nil {
		return SaveSummary{}, err
	}

	var targets []saveTarget
	for _, id := range ids {
		t, why, err := s.target(ctx, id)
		if err != nil {
			return SaveSummary{}, err
		}
		if why == "" {
			targets = append(targets, t)
		}
	}
	targets = s.dropShared(ctx, targets, records)
	if len(targets) == 0 {
		return SaveSummary{}, nil
	}
	p := &savePass{Saves: s, in: *in, server: s.dial(in.Server, in.Token), records: records, local: map[int]localSave{}}
	return p.run(ctx, targets)
}

// records returns what save sync knows of each game, by ROM id.
func (s *Saves) records(ctx context.Context) (map[int]domain.SaveRecord, error) {
	list, err := s.store.SaveRecords(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[int]domain.SaveRecord, len(list))
	for _, r := range list {
		out[r.ROMID] = r
	}
	return out, nil
}

// dropShared leaves out games whose saves would be the same file, marking each failed: syncing
// either would overwrite the other's progress.
func (s *Saves) dropShared(ctx context.Context, targets []saveTarget, records map[int]domain.SaveRecord) []saveTarget {
	byPath := map[string][]saveTarget{}
	for _, t := range targets {
		byPath[t.path] = append(byPath[t.path], t)
	}
	var out []saveTarget
	for _, t := range targets {
		others := byPath[t.path]
		if len(others) == 1 {
			out = append(out, t)
			continue
		}
		var names []string
		for _, o := range others {
			if o.romID != t.romID {
				names = append(names, o.name)
			}
		}
		r := s.record(records, t)
		r.State = domain.SaveFailed
		r.Problem = fmt.Sprintf("Its save would be the same file as %s's, so neither syncs.", strings.Join(names, ", "))
		s.put(ctx, r)
	}
	return out
}

// record returns what save sync knows of t's game, set to where its save is now.
func (s *Saves) record(records map[int]domain.SaveRecord, t saveTarget) domain.SaveRecord {
	r, ok := records[t.romID]
	if !ok || r.Path != t.path {
		// A save that moved is a new file: what was known of the old one says nothing of it.
		r = domain.SaveRecord{ROMID: t.romID}
	}
	r.Path, r.Slot, r.Emulator, r.CheckedAt = t.path, domain.AutosaveSlot, t.emulator, s.now().UTC()
	return r
}

func (s *Saves) put(ctx context.Context, r domain.SaveRecord) {
	if err := s.store.PutSaveRecord(context.WithoutCancel(ctx), r); err != nil {
		s.log.Error("save sync record", "rom_id", r.ROMID, "err", err)
	}
}

// Status returns where the save sync of a game stands.
func (s *Saves) Status(ctx context.Context, id int) (SaveStatus, error) {
	out := SaveStatus{ROMID: id}
	if s.store == nil {
		out.Why = s.problem.Error()
		return out, nil
	}
	t, why, err := s.target(ctx, id)
	if err != nil {
		return out, err
	}
	if why != "" {
		out.Why = why
		return out, nil
	}
	out.Syncs, out.Path, out.Emulator = true, t.path, t.emulator
	records, err := s.records(ctx)
	if err != nil {
		return out, err
	}
	if r, ok := records[id]; ok && r.Path == t.path {
		out.State, out.Problem, out.CheckedAt = r.State, r.Problem, r.CheckedAt
		if r.State == domain.SaveConflict {
			c := &SaveConflict{ServerSaveID: r.ConflictSaveID, ServerAt: r.ConflictServerAt, Parked: r.ParkedHash != ""}
			if info, err := os.Stat(t.path); err == nil {
				c.DeckAt = info.ModTime().UTC()
			}
			out.Conflict = c
		}
	}
	out.Syncing = s.syncing(id)
	return out, nil
}

// Summary counts where save sync stands across every game it looked at.
func (s *Saves) Summary(ctx context.Context) (SaveSummary, time.Time, bool, error) {
	s.mu.Lock()
	syncing := s.working
	s.mu.Unlock()
	if s.store == nil {
		return SaveSummary{}, time.Time{}, syncing, nil
	}
	records, err := s.records(ctx)
	if err != nil {
		return SaveSummary{}, time.Time{}, syncing, err
	}
	var out SaveSummary
	var last time.Time
	for _, r := range records {
		out.Games++
		switch r.State {
		case domain.SaveConflict:
			out.Conflicts++
		case domain.SaveWaiting:
			out.Waiting++
		case domain.SaveFailed:
			out.Failed++
		}
		if r.CheckedAt.After(last) {
			last = r.CheckedAt
		}
	}
	return out, last, syncing, nil
}

// target finds where a Plugin-owned game's save is and how RomM tags it. why says why the plugin
// doesn't sync it, empty when it does.
func (s *Saves) target(ctx context.Context, id int) (saveTarget, string, error) {
	rec, ok, err := s.owned.Owned(ctx, id)
	if err != nil {
		return saveTarget{}, "", err
	}
	if !ok || len(rec.Files) == 0 {
		return saveTarget{}, "Only games the plugin downloaded sync their saves.", nil
	}
	rom, ok, err := s.catalog.ROM(ctx, id)
	if err != nil {
		return saveTarget{}, "", err
	}
	name := "Game " + strconv.Itoa(id)
	if ok {
		name = rom.Name
	}
	platform, _, err := s.catalog.Platform(ctx, rec.PlatformID)
	if err != nil {
		return saveTarget{}, "", err
	}
	system, ok := domain.SystemForSlug(platform.Slug)
	if !ok {
		return saveTarget{}, "EmuDeck has no folder for this game's platform.", nil
	}
	state := s.install.State(false)
	folder, dir := folderHolding(state, system, rec.Files[0].Path)
	if dir == "" {
		return saveTarget{}, "The game isn't in one of EmuDeck's ROM folders.", nil
	}
	entry, why := gameEntry(rec.Files, dir)
	if why != "" {
		return saveTarget{}, why, nil
	}
	if domain.IsArchive(entry) {
		return saveTarget{}, "Saves of games kept as archives don't sync: which name RetroArch gives them isn't known.", nil
	}

	rel, err := filepath.Rel(filepath.Join(state.EmuDeck.ROMsPath, folder), entry)
	if err != nil || !filepath.IsLocal(rel) {
		rel = filepath.Base(entry)
	}
	core, why := s.core(folder, filepath.ToSlash(rel), state)
	if why != "" {
		return saveTarget{}, why, nil
	}
	saves, ok := retroArchSaves(state)
	if !ok {
		return saveTarget{}, "RetroArch's saves folder wasn't found.", nil
	}
	stem := strings.TrimSuffix(filepath.Base(entry), filepath.Ext(entry))
	path := filepath.Join(saves, stem+domain.RetroArchSaveExt)
	return saveTarget{romID: id, name: name, path: path, emulator: core}, "", nil
}

// core is the RetroArch core ES-DE runs the game at rel in folder with: from ES-DE's own files,
// else EmuDeck's default for the folder. why says why the game doesn't sync when RetroArch doesn't
// run it, or which core does can't be told.
func (s *Saves) core(folder, rel string, state domain.InstallState) (string, string) {
	launcher, err := s.launchers.Launcher(folder, rel)
	if err != nil {
		s.log.Debug("ES-DE launcher", "system", folder, "err", err)
	}
	switch {
	case launcher.Known && launcher.RetroArchCore() != "":
		return launcher.RetroArchCore(), ""
	case launcher.Known, launcher.NotRetroArch():
		return "", fmt.Sprintf("ES-DE runs this game with %s, and only RetroArch saves sync.", launcher.Label)
	}
	i := slices.IndexFunc(state.Systems, func(s domain.SystemState) bool { return s.Folder == folder })
	primary := i >= 0 && state.Systems[i].Emulator == domain.RetroArchID
	if core, ok := domain.DefaultRetroArchCore(folder); ok && (primary || i < 0) {
		return core, ""
	}
	return "", "This game doesn't run in RetroArch by default, and only RetroArch saves sync."
}

// folderHolding is the ROM folder of system's that path is in, by its folder name and path.
func folderHolding(state domain.InstallState, system domain.System, path string) (string, string) {
	for _, f := range system.Folders() {
		if dir := folderPath(state, system, f); dir != "" && strings.HasPrefix(path, dir+string(filepath.Separator)) {
			return f, dir
		}
	}
	return "", ""
}

// launchExts are the files RetroArch loads a game of several by, in the order the plugin picks
// them when a game's folder entry holds several files.
var launchExts = []string{".m3u", ".cue", ".gdi", ".ccd", ".chd"}

// gameEntry is the path of what ES-DE lists for a game whose files are in dir: its file, or the
// folder holding them. why says why it can't be told.
func gameEntry(files []domain.PlacedFile, dir string) (string, string) {
	tops := map[string]bool{}
	for _, f := range files {
		rel, err := filepath.Rel(dir, f.Path)
		if err != nil || !filepath.IsLocal(rel) {
			return "", "The game's files aren't where the plugin placed them."
		}
		top, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
		tops[top] = true
	}
	if len(tops) == 1 {
		for top := range tops {
			return filepath.Join(dir, top), ""
		}
	}
	for _, ext := range launchExts {
		var hits []string
		for top := range tops {
			if strings.EqualFold(filepath.Ext(top), ext) {
				hits = append(hits, top)
			}
		}
		if len(hits) == 1 {
			return filepath.Join(dir, hits[0]), ""
		}
	}
	return "", "The game is several files, and which one RetroArch loads, and so names its save after, can't be told."
}

// localSave is a save on the Deck as a sync read it.
type localSave struct {
	exists bool
	data   []byte
	hash   string
	mod    time.Time
}

func readLocal(path string) (localSave, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return localSave{}, nil
	}
	if err != nil {
		return localSave{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return localSave{}, err
	}
	return localSave{exists: true, data: data, hash: md5Hex(data), mod: info.ModTime().UTC()}, nil
}

func md5Hex(data []byte) string {
	sum := md5.Sum(data) //nolint:gosec // see import
	return hex.EncodeToString(sum[:])
}

// savePass is one sync: a negotiation with RomM and the steps it planned.
type savePass struct {
	*Saves
	in      domain.SignIn
	server  SaveServer
	records map[int]domain.SaveRecord
	local   map[int]localSave
	summary SaveSummary
	done    int
	failed  int
}

func (p *savePass) run(ctx context.Context, targets []saveTarget) (SaveSummary, error) {
	plan, err := p.plan(ctx, targets)
	if err != nil {
		return p.summary, err
	}
	acted := map[int]bool{}
	for _, t := range targets {
		if op, ok := ownOperation(plan, t.romID); ok {
			acted[t.romID] = true
			p.step(ctx, t, op, plan.SessionID)
		}
	}
	// Games RomM planned nothing for have no save on either side.
	for _, t := range targets {
		if _, read := p.local[t.romID]; read && !acted[t.romID] {
			r := p.record(p.records, t)
			r.State, r.Problem = domain.SaveSynced, ""
			clearConflict(&r)
			p.put(ctx, r)
		}
	}
	p.complete(ctx, plan.SessionID)
	p.log.Info("save sync done", "session", plan.SessionID, "uploaded", p.summary.Uploaded, "downloaded", p.summary.Downloaded,
		"conflicts", p.summary.Conflicts, "waiting", p.summary.Waiting, "failed", p.summary.Failed)
	return p.summary, nil
}

// ownOperation is the step RomM planned for the save the plugin syncs of the ROM with id. Other
// slots, and saves in none, are other timelines: the plugin leaves them to their owners.
func ownOperation(plan domain.SyncPlan, id int) (domain.SyncOperation, bool) {
	for _, op := range plan.Operations {
		if op.ROMID == id && op.Slot == domain.AutosaveSlot {
			return op, true
		}
	}
	return domain.SyncOperation{}, false
}

func (p *savePass) complete(ctx context.Context, session int) {
	if err := p.server.CompleteSync(context.WithoutCancel(ctx), session, p.done, p.failed); err != nil {
		p.log.Warn("complete save sync", "session", session, "err", p.conn.observe(p.in, err))
	}
}

// plan reads the saves of targets and asks RomM what syncing them takes.
func (p *savePass) plan(ctx context.Context, targets []saveTarget) (domain.SyncPlan, error) {
	req := domain.SyncRequest{DeviceID: p.in.DeviceID, RestoreUnlisted: true}
	for _, t := range targets {
		req.ROMIDs = append(req.ROMIDs, t.romID)
		local, err := readLocal(t.path)
		if err != nil {
			r := p.record(p.records, t)
			r.State, r.Problem = domain.SaveFailed, "Couldn't read the save: "+err.Error()
			p.put(ctx, r)
			p.summary.Failed++
			continue
		}
		p.local[t.romID] = local
		if !local.exists {
			continue
		}
		updated := local.mod
		if r, ok := p.records[t.romID]; ok && r.Path == t.path && r.LocalHash != "" && !r.SyncedAt.IsZero() {
			if r.LocalHash == local.hash {
				// RetroArch writes the save again whenever a game closes, changed or not. A save that
				// still matches what was synced reports when it was synced, so it never reads as a new
				// change.
				updated = r.SyncedAt
			} else if !updated.After(r.SyncedAt) {
				// A save that differs from what was synced changed since, whatever this Deck's clock
				// says against RomM's.
				updated = r.SyncedAt.Add(time.Second)
			}
		}
		req.Saves = append(req.Saves, domain.ClientSave{
			ROMID: t.romID, FileName: filepath.Base(t.path), Slot: domain.AutosaveSlot, Emulator: t.emulator,
			ContentHash: local.hash, UpdatedAt: updated, SizeBytes: int64(len(local.data)),
		})
	}
	p.summary.Games = len(targets)

	plan, err := p.server.NegotiateSync(ctx, req)
	if err != nil {
		return plan, p.conn.observe(p.in, err)
	}
	p.log.Info("save sync planned", "session", plan.SessionID, "games", len(targets), "operations", len(plan.Operations))
	return plan, nil
}

// step carries out what RomM planned for one game, and records where its save stands after.
func (p *savePass) step(ctx context.Context, t saveTarget, op domain.SyncOperation, session int) {
	r := p.record(p.records, t)
	before, problem := r.State, r.Problem
	local := p.local[t.romID]
	var err error
	switch op.Action {
	case domain.SyncUpload:
		err = p.upload(ctx, t, local, op, session, false, &r)
	case domain.SyncDownload:
		err = p.download(ctx, t, op, local, session, &r)
	case domain.SyncConflict:
		p.conflict(ctx, t, op, local, &r)
	case domain.SyncDelete:
		r.State, r.Problem = domain.SaveRemovedOnServer, ""
		p.log.Info("save removed on server", "rom_id", t.romID)
		if before != domain.SaveRemovedOnServer {
			p.logActivity(ctx, t.romID, domain.ActivityRemovedOnServer, "RomM's save was deleted. The save on this Deck stays.")
		}
	default:
		// The two sides match. The first sync that finds them so takes them as where both stand.
		r.State, r.Problem = domain.SaveSynced, ""
		if local.exists && (op.ServerHash == "" || op.ServerHash == local.hash) {
			r.LocalHash, r.ServerSaveID, r.ServerHash = local.hash, op.SaveID, op.ServerHash
			if !op.ServerUpdatedAt.IsZero() {
				r.SyncedAt = op.ServerUpdatedAt
			}
		}
	}
	if err != nil {
		r.State, r.Problem = domain.SaveFailed, err.Error()
		p.summary.Failed++
		p.failed++
		p.log.Warn("save sync failed", "rom_id", t.romID, "action", op.Action, "err", err)
		// A failure that repeats sync after sync is logged once.
		if before != domain.SaveFailed || problem != r.Problem {
			p.logActivity(ctx, t.romID, domain.ActivityFailed, "Sync failed: "+r.Problem)
		}
	}
	if r.State != domain.SaveConflict {
		clearConflict(&r)
	}
	p.put(ctx, r)
}

func clearConflict(r *domain.SaveRecord) {
	r.ConflictSaveID, r.ConflictServerAt, r.ParkedHash = 0, time.Time{}, ""
}

// upload adds the Deck's save to its slot, op being what RomM planned for it. With overwrite it does
// so even when the slot moved on since this Deck last synced: the user chose the Deck's copy.
func (p *savePass) upload(ctx context.Context, t saveTarget, local localSave, op domain.SyncOperation, session int, overwrite bool, r *domain.SaveRecord) error {
	if !local.exists {
		return errors.New("the save to upload is gone")
	}
	saved, err := p.server.UploadSave(ctx, domain.SaveUpload{
		ROMID: t.romID, FileName: filepath.Base(t.path), Slot: domain.AutosaveSlot, Emulator: t.emulator,
		DeviceID: p.in.DeviceID, ContentHash: local.hash, SessionID: session, Data: local.data, Overwrite: overwrite,
	})
	if kindOf(err) == domain.KindSaveConflict {
		// The slot's newest save is one this Deck never synced: another device's. RomM keeps a
		// Device's sync per version, so it plans an upload by time and refuses it here, rather than
		// planning a conflict. The plan names that save.
		p.conflict(ctx, t, op, local, r)
		return nil
	}
	if err != nil {
		return p.conn.observe(p.in, err)
	}
	r.LocalHash, r.ServerSaveID, r.ServerHash, r.SyncedAt = local.hash, saved.ID, saved.ContentHash, saved.UpdatedAt
	r.State, r.Problem = domain.SaveSynced, ""
	p.summary.Uploaded++
	p.done++
	p.log.Info("save uploaded", "rom_id", t.romID, "save_id", saved.ID, "bytes", len(local.data))
	p.logActivity(ctx, t.romID, domain.ActivityUploaded, "Uploaded this Deck's save to RomM.")
	return nil
}

func (p *savePass) download(ctx context.Context, t saveTarget, op domain.SyncOperation, local localSave, session int, r *domain.SaveRecord) error {
	if !domain.RetroArchReads(op.Emulator) {
		return fmt.Errorf("RomM's newest save is from %s, which RetroArch can't load", op.Emulator)
	}
	if emu, ok := p.running(); ok && emu.ID == domain.RetroArchID {
		// RetroArch writes the save it holds when the game closes, which would undo the download.
		r.State, r.Problem = domain.SaveWaiting, ""
		p.summary.Waiting++
		return nil
	}
	// The save changed since the plan was made: the plan no longer applies.
	if now, err := readLocal(t.path); err != nil || now.exists != local.exists || now.hash != local.hash {
		return errors.New("the save changed while syncing; sync again")
	}
	data, err := p.server.DownloadSave(ctx, op.SaveID, p.in.DeviceID, session)
	if err != nil {
		return p.conn.observe(p.in, err)
	}
	hash := md5Hex(data)
	if op.ServerHash != "" && hash != op.ServerHash {
		return errors.New("the save from RomM didn't match its hash")
	}
	if local.exists {
		if err := p.backupSave(t.romID, t.path, local.data); err != nil {
			return fmt.Errorf("back up the save: %w", err)
		}
	}
	p.mu.Lock()
	p.wrote[t.path] = p.now()
	p.mu.Unlock()
	if err := writeAtomic(t.path, data); err != nil {
		return fmt.Errorf("write the save: %w", err)
	}
	if err := p.server.ConfirmSaveDownload(ctx, op.SaveID, p.in.DeviceID, hash); err != nil {
		// The bytes are in place; RomM just doesn't know yet. The next sync finds them equal.
		p.log.Warn("confirm save download", "rom_id", t.romID, "err", p.conn.observe(p.in, err))
	}
	r.LocalHash, r.ServerSaveID, r.ServerHash, r.SyncedAt = hash, op.SaveID, hash, op.ServerUpdatedAt
	r.State, r.Problem = domain.SaveSynced, ""
	p.summary.Downloaded++
	p.done++
	p.log.Info("save downloaded", "rom_id", t.romID, "save_id", op.SaveID, "bytes", len(data), "replaced", local.exists)
	msg := "Downloaded RomM's save."
	if local.exists {
		msg = "Downloaded RomM's save. The one it replaced is in this game's backups."
	}
	p.logActivity(ctx, t.romID, domain.ActivityDownloaded, msg)
	return nil
}

// backupTime is how a backup's name starts: when it was taken, so names sort oldest first. The
// save's extension follows.
const backupTime = "20060102-150405.000"

// backupSave sets data, the save at path of the ROM with id, aside before it's replaced, keeping
// the newest saveBackups of the game.
func (s *Saves) backupSave(id int, path string, data []byte) error {
	dir := s.backupDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := s.now().UTC().Format(backupTime) + filepath.Ext(path)
	if err := writeAtomic(filepath.Join(dir, name), data); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	// Names start with their time, so they sort oldest first.
	sort.Strings(names)
	for _, old := range names[:max(len(names)-saveBackups, 0)] {
		if err := os.Remove(filepath.Join(dir, old)); err != nil {
			s.log.Warn("prune save backup", "path", old, "err", err)
		}
	}
	s.log.Info("save backed up", "rom_id", id, "path", filepath.Join(dir, name))
	return nil
}

func (s *Saves) backupDir(id int) string {
	return filepath.Join(s.backups, domain.RetroArchID, strconv.Itoa(id))
}

// writeAtomic writes data to path through a file beside it, so path is never half-written.
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".romm-part"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
