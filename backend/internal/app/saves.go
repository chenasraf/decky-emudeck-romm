package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
	"github.com/chenasraf/saveporter"
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
	SaveSlots(ctx context.Context, id int) ([]domain.SaveSlot, error)

	// States lists the ROM with id's save states, every ROM's when id is 0.
	States(ctx context.Context, id int) ([]domain.ServerState, error)
	UploadState(ctx context.Context, u domain.StateUpload) (domain.ServerState, error)
	DownloadState(ctx context.Context, id int) ([]byte, error)
	StateScreenshot(ctx context.Context, path string) ([]byte, error)
	DeleteStates(ctx context.Context, ids []int) error
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
	// SaveSlot is the RomM slot the user picked for the ROM with id, empty when they picked none.
	SaveSlot(ctx context.Context, id int) (string, error)
	// SetSaveSlot picks the ROM's slot; domain.AutosaveSlot clears the pick.
	SetSaveSlot(ctx context.Context, id int, slot string) error

	StateFiles(ctx context.Context, id int) ([]domain.StateFile, error)
	PutStateFile(ctx context.Context, f domain.StateFile) error
	// UploadedStates are the ROM's states this Deck put on RomM, oldest first.
	UploadedStates(ctx context.Context, id int) ([]domain.UploadedState, error)
	AddUploadedState(ctx context.Context, u domain.UploadedState) error
	ForgetUploadedStates(ctx context.Context, ids []int) error
	StateGame(ctx context.Context, id int) (domain.StateGame, error)
	StateGames(ctx context.Context) ([]domain.StateGame, error)
	PutStateGame(ctx context.Context, g domain.StateGame) error
	StatePrefs(ctx context.Context, id int) (domain.StatePrefs, error)
	SetStatePrefs(ctx context.Context, id int, p domain.StatePrefs) error
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
	// Path is the save file, Emulator the tag RomM keeps it under (a RetroArch core's name, or the
	// standalone emulator's), and Saver the name of the emulator that writes it. Slot is the RomM
	// slot it syncs in.
	Path     string
	Emulator string
	Saver    string
	Slot     string
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
	// StatesUploaded and StatesDownloaded count save states.
	StatesUploaded   int
	StatesDownloaded int
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

// SaveGameStore keeps the games whose saves sync: Plugin-owned ROMs and Linked games.
type SaveGameStore interface {
	OwnershipStore
	LinkStore
}

// Saves syncs the saves of Plugin-owned and Linked games with RomM, for the emulators
// domain.SaveRules lists, one sync at a time. RomM works out what each sync takes; the plugin
// uploads, downloads, and never resolves a conflict on its own.
type Saves struct {
	catalog     CatalogStore
	install     *Install
	conn        *Connection
	dial        SaveDialer
	owned       SaveGameStore
	store       SaveStore
	problem     error
	launchers   Launchers
	configs     SaveConfigs
	states      StateSettingsStore
	n64db       N64Database
	psx         PSXDiscs
	duckstation DuckStationGames
	running     RunningGame
	backups     string
	onChange    SaveListener
	log         *slog.Logger
	now         func() time.Time
	base        context.Context

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
	// waits is set while a game's download may be waiting for its emulator to close.
	waits bool
	// wrote is when the plugin last wrote each save, so the watch doesn't take its own writes for
	// the game's.
	wrote map[string]time.Time
	// reads are what games' files gave when last read, by path.
	reads map[string]cachedRead
}

const (
	// savePullEvery is how often every game's save is synced, to pick up what other devices did.
	savePullEvery = 15 * time.Minute
	// saveSettle is how long a save goes unwritten before it uploads: emulators write one in
	// several steps, and some every few seconds while a game runs.
	saveSettle = 10 * time.Second
	// savePoll is how often a download waiting for its emulator looks whether it closed.
	savePoll = 5 * time.Second
	// saveOpenGap is how long after a sync opening the QAM doesn't sync again.
	saveOpenGap = 2 * time.Minute
)

// NewSaves returns save sync. A nil store turns it off, reporting problem. Overwritten saves are
// backed up under backups.
func NewSaves(ctx context.Context, catalog CatalogStore, install *Install, conn *Connection, dial SaveDialer, owned SaveGameStore,
	store SaveStore, problem error, sources SaveSources, running RunningGame, backups string, onChange SaveListener, log *slog.Logger,
) *Saves {
	return &Saves{
		catalog: catalog, install: install, conn: conn, dial: dial, owned: owned, store: store, problem: problem,
		launchers: sources.Launchers, configs: sources.Configs, states: sources.States, n64db: sources.N64, psx: sources.PSX, duckstation: sources.DuckStation,
		running: running, backups: backups, onChange: onChange, log: log, now: time.Now, base: ctx, reads: map[string]cachedRead{},
		pullEvery: savePullEvery, settle: saveSettle, pollEvery: savePoll, openGap: saveOpenGap,
		pending: map[int]bool{}, current: map[int]bool{}, wrote: map[string]time.Time{}, waits: true,
	}
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
// and every pullEvery, a game's once its save settles after its emulator writes it, every game's
// once an emulator's settings move its saves or stop them syncing, and a download that waits for
// an emulator once it closes. A nil watch leaves out the syncs on writes and settings changes.
func (s *Saves) Run(ctx context.Context, watch SaveWatch) {
	s.pruneActivity(ctx)
	configs := s.configs.Files()
	seen := s.saveConfigs()
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
		if dirs := s.watchDirs(configs); watch != nil && len(dirs) > 0 && strings.Join(dirs, "\n") != watched {
			if err := watch.Watch(dirs...); err != nil {
				s.log.Warn("watch saves", "dirs", dirs, "err", err)
			} else {
				watched = strings.Join(dirs, "\n")
				s.log.Info("watching saves", "dirs", dirs)
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
			if !slices.Contains(configs, p) {
				s.saveWritten(ctx, p)
				continue
			}
			if now := s.saveConfigs(); !maps.Equal(now, seen) {
				seen = now
				s.log.Info("emulator save settings changed", "path", p)
				s.request(nil, false)
			}
		case <-poll.C:
			s.retryWaiting(ctx)
		}
	}
}

// saveConfigs is what each emulator's settings say of its saves, by its id.
func (s *Saves) saveConfigs() map[string]domain.SaveConfig {
	out := map[string]domain.SaveConfig{}
	for _, r := range domain.SaveRules() {
		out[r.Emulator.ID] = s.configs.SaveConfig(r.Emulator.ID)
	}
	return out
}

// watchDirs are the folders the watch covers: those saves are in, and those holding the emulators'
// settings files, of which only the files among configs count.
func (s *Saves) watchDirs(configs []string) []string {
	state := s.install.State(false)
	dirs := s.saveDirs(state)
	for _, r := range domain.SaveRules() {
		if dir, why := s.stateDir(state, r.Emulator); why == "" && !slices.Contains(dirs, dir) {
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				dirs = append(dirs, dir)
			}
		}
	}
	for _, f := range configs {
		if dir := filepath.Dir(f); !slices.Contains(dirs, dir) {
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				dirs = append(dirs, dir)
			}
		}
	}
	return dirs
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
	ids, err := s.gameIDs(ctx)
	if err != nil {
		s.log.Warn("save written", "path", path, "err", err)
		return
	}
	records, err := s.records(ctx)
	if err != nil {
		s.log.Warn("save written", "path", path, "err", err)
		return
	}
	if _, ok := saveporter.ParseStateName(path); ok {
		s.stateWritten(ctx, ids, path)
		return
	}
	for _, id := range ids {
		if t, why, err := s.target(ctx, id); err == nil && why == "" && slices.Contains(t.format.files(), path) {
			// A write that leaves the save as it last synced, the plugin's own download included
			// however late the watch reports it, has nothing to upload.
			if r, ok := records[id]; ok && r.Path == t.path && r.Slot == t.slot && r.LocalHash != "" {
				if local, err := t.format.read(); err == nil && local.exists && local.hash == r.LocalHash {
					return
				}
			}
			s.log.Info("save written", "rom_id", id, "path", path)
			s.request([]int{id}, false)
			return
		}
	}
}

// retryWaiting syncs the games whose downloads wait for their emulator, once it's closed.
func (s *Saves) retryWaiting(ctx context.Context) {
	s.mu.Lock()
	waits := s.waits
	s.mu.Unlock()
	if !waits || s.store == nil {
		return
	}
	records, err := s.store.SaveRecords(ctx)
	if err != nil {
		return
	}
	var ids []int
	still := false
	waiting := func(id int) {
		if t, why, err := s.target(ctx, id); err == nil && why == "" && s.runs(t.rule.Emulator.ID) {
			still = true
			return
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	for _, r := range records {
		if r.State == domain.SaveWaiting {
			waiting(r.ROMID)
		}
	}
	if games, err := s.store.StateGames(ctx); err == nil {
		for _, g := range games {
			if g.Waiting {
				waiting(g.ROMID)
			}
		}
	}
	s.mu.Lock()
	s.waits = still
	s.mu.Unlock()
	if len(ids) > 0 {
		s.log.Info("emulator closed; syncing waiting saves", "games", len(ids))
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
	if len(s.saveDirs(s.install.State(false))) == 0 {
		return nil, &domain.Error{
			Kind:    domain.KindInvalidInput,
			Message: "No emulator's saves folder was found. Set your emulators up in EmuDeck, then check Setup status again.",
		}
	}
	return in, nil
}

// saveDirs are the folders the emulators whose saves sync keep them in.
func (s *Saves) saveDirs(state domain.InstallState) []string {
	var out []string
	for _, r := range domain.SaveRules() {
		if dir, why := s.saveDir(state, r.Emulator); why == "" {
			out = append(out, dir)
		}
	}
	return out
}

// PausedEmulator is an installed emulator whose saves don't sync because of its own settings.
type PausedEmulator struct {
	Emulator domain.Emulator
	Why      string
}

// Paused lists the installed emulators whose own settings stop their saves from syncing.
func (s *Saves) Paused() []PausedEmulator {
	out := []PausedEmulator{}
	state := s.install.State(false)
	for _, r := range domain.SaveRules() {
		i := slices.IndexFunc(state.Emulators, func(e domain.EmulatorState) bool { return e.ID == r.Emulator.ID })
		if i < 0 || !state.Emulators[i].Installed {
			continue
		}
		if cfg := s.configs.SaveConfig(r.Emulator.ID); cfg.Problem != "" {
			out = append(out, PausedEmulator{Emulator: r.Emulator, Why: cfg.Problem})
		}
	}
	return out
}

// Sync syncs the saves of the games with ids, or of every game whose saves sync when ids is empty, and
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
		if ids, err = s.gameIDs(ctx); err != nil {
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
	server := s.dial(in.Server, in.Token)
	var total SaveSummary
	for batch := range slices.Chunk(targets, domain.SyncBatch) {
		p := &savePass{Saves: s, in: *in, server: server, records: records, local: map[int]localSave{}}
		summary, err := p.run(ctx, batch)
		total.add(summary)
		if err != nil {
			return total, err
		}
	}
	total.add(s.syncStates(ctx, *in, server, targets))
	return total, nil
}

// add counts what another sync did into s.
func (s *SaveSummary) add(o SaveSummary) {
	s.Games += o.Games
	s.Uploaded += o.Uploaded
	s.Downloaded += o.Downloaded
	s.Conflicts += o.Conflicts
	s.Waiting += o.Waiting
	s.Failed += o.Failed
	s.StatesUploaded += o.StatesUploaded
	s.StatesDownloaded += o.StatesDownloaded
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
	if !ok || r.Path != t.path || r.Slot != t.slot {
		// A save that moved is a new file, and another slot another timeline: what was known of the
		// old one says nothing of it.
		r = domain.SaveRecord{ROMID: t.romID}
	}
	r.Path, r.Slot, r.Emulator, r.CheckedAt = t.path, t.slot, t.emulator, s.now().UTC()
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
	out.Syncs, out.Path, out.Emulator, out.Saver, out.Slot = true, t.path, t.emulator, t.rule.Emulator.Name, t.slot
	records, err := s.records(ctx)
	if err != nil {
		return out, err
	}
	if r, ok := records[id]; ok && r.Path == t.path && r.Slot == t.slot {
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
		if op, ok := ownOperation(plan, t); ok {
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

// ownOperation is the step RomM planned for t's save, in its slot. Other slots, and saves in none,
// are other timelines: the plugin leaves them to their owners.
func ownOperation(plan domain.SyncPlan, t saveTarget) (domain.SyncOperation, bool) {
	for _, op := range plan.Operations {
		if op.ROMID == t.romID && op.Slot == t.slot {
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
		local, err := t.format.read()
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
		if r, ok := p.records[t.romID]; ok && r.Path == t.path && r.Slot == t.slot && r.LocalHash != "" && !r.SyncedAt.IsZero() {
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
			ROMID: t.romID, FileName: filepath.Base(t.path), Slot: t.slot, Emulator: t.emulator,
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
		ROMID: t.romID, FileName: filepath.Base(t.path), Slot: t.slot, Emulator: t.emulator,
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
	// An N64 save is checked as it's read: any N64 emulator's unit, files or .srm will do.
	if t.rule.Kind == domain.SaveRawFile && !domain.RawBatterySave(op.Emulator) {
		return fmt.Errorf("RomM's newest save is from %s, which %s can't load", op.Emulator, t.rule.Emulator.Name)
	}
	if p.runs(t.rule.Emulator.ID) {
		// The emulator writes the save it holds when the game closes, which would undo the download.
		r.State, r.Problem = domain.SaveWaiting, ""
		p.summary.Waiting++
		return nil
	}
	// The save changed since the plan was made: the plan no longer applies.
	if now, err := t.format.read(); err != nil || now.exists != local.exists || now.hash != local.hash {
		return errors.New("the save changed while syncing; sync again")
	}
	data, err := p.server.DownloadSave(ctx, op.SaveID, p.in.DeviceID, session)
	if err != nil {
		return p.conn.observe(p.in, err)
	}
	hash := domain.ContentHash(data)
	if op.ServerHash != "" && hash != op.ServerHash {
		return errors.New("the save from RomM didn't match its hash")
	}
	if local.exists {
		if err := p.backupSave(t.romID, t.path, local.data); err != nil {
			return fmt.Errorf("back up the save: %w", err)
		}
	}
	p.mu.Lock()
	for _, f := range t.format.files() {
		p.wrote[f] = p.now()
	}
	p.mu.Unlock()
	if err := t.format.write(data); err != nil {
		return fmt.Errorf("write the save: %w", err)
	}
	if err := p.server.ConfirmSaveDownload(ctx, op.SaveID, p.in.DeviceID, hash); err != nil {
		// The bytes are in place; RomM just doesn't know yet. The next sync finds them equal.
		p.log.Warn("confirm save download", "rom_id", t.romID, "err", p.conn.observe(p.in, err))
	}
	// The save as the Deck now holds it can hash differently from RomM's: an N64 save from another
	// emulator's files reads back as the shared unit. Each side keeps its own hash.
	written := hash
	if now, err := t.format.read(); err == nil && now.exists {
		written = now.hash
	}
	r.LocalHash, r.ServerSaveID, r.ServerHash, r.SyncedAt = written, op.SaveID, hash, op.ServerUpdatedAt
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
	// Two backups within a millisecond would share a name; the later one takes the next free
	// millisecond so neither is lost and names still sort in the order they were taken.
	at := s.now().UTC()
	name := at.Format(backupTime) + filepath.Ext(path)
	for {
		if _, err := os.Lstat(filepath.Join(dir, name)); errors.Is(err, os.ErrNotExist) {
			break
		} else if err != nil {
			return err
		}
		at = at.Add(time.Millisecond)
		name = at.Format(backupTime) + filepath.Ext(path)
	}
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

// backupDir holds a game's backups. They're the game's whichever emulator writes its save, which
// ES-DE may change.
func (s *Saves) backupDir(id int) string {
	return filepath.Join(s.backups, strconv.Itoa(id))
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
