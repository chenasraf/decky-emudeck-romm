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

// SaveStore keeps what save sync knows of each game's save.
type SaveStore interface {
	PutSaveRecord(ctx context.Context, r domain.SaveRecord) error
	SaveRecords(ctx context.Context) ([]domain.SaveRecord, error)
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

	// passMu is held while a sync runs.
	passMu sync.Mutex
	mu     sync.Mutex
	// syncing are the games a running or waiting sync covers; nil ids means every game.
	syncing map[int]bool
	all     bool
}

// NewSaves returns save sync. A nil store turns it off, reporting problem. Overwritten saves are
// backed up under backups.
func NewSaves(ctx context.Context, catalog CatalogStore, install *Install, conn *Connection, dial SaveDialer, owned OwnershipStore,
	store SaveStore, problem error, launchers Launchers, running RunningGame, backups string, onChange SaveListener, log *slog.Logger,
) *Saves {
	return &Saves{
		catalog: catalog, install: install, conn: conn, dial: dial, owned: owned, store: store, problem: problem,
		launchers: launchers, running: running, backups: backups, onChange: onChange, log: log, now: time.Now, base: ctx,
		syncing: map[int]bool{},
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
	s.mu.Lock()
	if len(ids) == 0 {
		s.all = true
	}
	for _, id := range ids {
		s.syncing[id] = true
	}
	s.mu.Unlock()
	s.onChange(SaveSyncEvent{Syncing: true, ROMIDs: ids})
	go func() {
		summary, err := s.Sync(s.base, ids)
		s.mu.Lock()
		if len(ids) == 0 {
			s.all = false
		}
		for _, id := range ids {
			delete(s.syncing, id)
		}
		s.mu.Unlock()
		s.onChange(SaveSyncEvent{ROMIDs: ids, Summary: summary, Problem: err})
	}()
	return nil
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
	}
	s.mu.Lock()
	out.Syncing = s.all || s.syncing[id]
	s.mu.Unlock()
	return out, nil
}

// Summary counts where save sync stands across every game it looked at.
func (s *Saves) Summary(ctx context.Context) (SaveSummary, time.Time, bool, error) {
	s.mu.Lock()
	syncing := s.all || len(s.syncing) > 0
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
	req := domain.SyncRequest{DeviceID: p.in.DeviceID, RestoreUnlisted: true}
	byID := map[int]saveTarget{}
	for _, t := range targets {
		byID[t.romID] = t
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
		// RetroArch writes the save again whenever a game closes, changed or not. A save that still
		// matches what was synced reports when it was synced, so it never reads as a new change.
		if r, ok := p.records[t.romID]; ok && r.Path == t.path && r.LocalHash == local.hash && !r.SyncedAt.IsZero() {
			updated = r.SyncedAt
		}
		req.Saves = append(req.Saves, domain.ClientSave{
			ROMID: t.romID, FileName: filepath.Base(t.path), Slot: domain.AutosaveSlot, Emulator: t.emulator,
			ContentHash: local.hash, UpdatedAt: updated, SizeBytes: int64(len(local.data)),
		})
	}
	p.summary.Games = len(targets)

	plan, err := p.server.NegotiateSync(ctx, req)
	if err != nil {
		return p.summary, p.conn.observe(p.in, err)
	}
	p.log.Info("save sync planned", "session", plan.SessionID, "games", len(targets), "operations", len(plan.Operations))

	acted := map[int]bool{}
	for _, op := range plan.Operations {
		t, ok := byID[op.ROMID]
		// Other slots are other timelines: the plugin syncs one, and leaves the rest to their owners.
		if !ok || (op.Slot != "" && op.Slot != domain.AutosaveSlot) || acted[op.ROMID] {
			continue
		}
		acted[op.ROMID] = true
		p.step(ctx, t, op, plan.SessionID)
	}
	// Games RomM planned nothing for have no save on either side.
	for _, t := range targets {
		if _, read := p.local[t.romID]; read && !acted[t.romID] {
			r := p.record(p.records, t)
			r.State, r.Problem = domain.SaveSynced, ""
			p.put(ctx, r)
		}
	}
	if err := p.server.CompleteSync(context.WithoutCancel(ctx), plan.SessionID, p.done, p.failed); err != nil {
		p.log.Warn("complete save sync", "session", plan.SessionID, "err", p.conn.observe(p.in, err))
	}
	p.log.Info("save sync done", "session", plan.SessionID, "uploaded", p.summary.Uploaded, "downloaded", p.summary.Downloaded,
		"conflicts", p.summary.Conflicts, "waiting", p.summary.Waiting, "failed", p.summary.Failed)
	return p.summary, nil
}

// step carries out what RomM planned for one game, and records where its save stands after.
func (p *savePass) step(ctx context.Context, t saveTarget, op domain.SyncOperation, session int) {
	r := p.record(p.records, t)
	local := p.local[t.romID]
	var err error
	switch op.Action {
	case domain.SyncUpload:
		err = p.upload(ctx, t, local, session, &r)
	case domain.SyncDownload:
		err = p.download(ctx, t, op, local, session, &r)
	case domain.SyncConflict:
		r.State, r.Problem = domain.SaveConflict, ""
		p.summary.Conflicts++
		p.log.Info("save conflict", "rom_id", t.romID, "save_id", op.SaveID, "reason", op.Reason)
	case domain.SyncDelete:
		r.State, r.Problem = domain.SaveRemovedOnServer, ""
		p.log.Info("save removed on server", "rom_id", t.romID)
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
	}
	p.put(ctx, r)
}

func (p *savePass) upload(ctx context.Context, t saveTarget, local localSave, session int, r *domain.SaveRecord) error {
	if !local.exists {
		return errors.New("the save to upload is gone")
	}
	saved, err := p.server.UploadSave(ctx, domain.SaveUpload{
		ROMID: t.romID, FileName: filepath.Base(t.path), Slot: domain.AutosaveSlot, Emulator: t.emulator,
		DeviceID: p.in.DeviceID, ContentHash: local.hash, SessionID: session, Data: local.data,
	})
	if kindOf(err) == domain.KindSaveConflict {
		// Another device uploaded between the plan and the upload.
		r.State, r.Problem = domain.SaveConflict, ""
		p.summary.Conflicts++
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
		if err := p.backup(t, local); err != nil {
			return fmt.Errorf("back up the save: %w", err)
		}
	}
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
	return nil
}

// backup copies the save a download replaces aside, keeping the newest saveBackups of the game.
func (p *savePass) backup(t saveTarget, local localSave) error {
	dir := filepath.Join(p.backups, domain.RetroArchID, strconv.Itoa(t.romID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := p.now().UTC().Format("20060102-150405.000") + filepath.Ext(t.path)
	if err := writeAtomic(filepath.Join(dir, name), local.data); err != nil {
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
			p.log.Warn("prune save backup", "path", old, "err", err)
		}
	}
	p.log.Info("save backed up", "rom_id", t.romID, "path", filepath.Join(dir, name))
	return nil
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
