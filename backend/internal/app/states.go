package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
	"github.com/chenasraf/saveporter"
)

// Save states sync in the same passes as saves, for the games whose saves sync. Every state on the
// Deck that changed goes to RomM as a new entry. The newest state RomM has from elsewhere comes to the
// Deck's target slot, and a copy to the extra slot, unless the Deck's state there is newer; a state
// the Deck holds that isn't on RomM yet is uploaded before anything replaces it.

// stateTarget is where a game's save states are on the Deck.
type stateTarget struct {
	romID int
	name  string
	// emulator plays the game, and kind is the format its states take.
	emulator domain.Emulator
	kind     saveporter.Emulator
	// system is the game's EmuDeck folder, core the emulator core it plays in, which is also the tag
	// its states are kept under on RomM.
	system string
	core   string
	dir    string
	// base is the states' name without the slot's extension, as the game's save is named.
	base string
}

// path is the file of a state slot.
func (t stateTarget) path(slot string) string {
	return filepath.Join(t.dir, saveporter.StateFileName(t.base, t.kind, toSlot(slot)))
}

// files are the game's state files, one per slot the emulator has.
func (t stateTarget) files() []string {
	var out []string
	for _, slot := range domain.StateSlots {
		if p := t.path(slot); !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// slotOf names the slot of one of the target's state files.
func slotOf(path string) string {
	n, ok := saveporter.ParseStateName(path)
	if !ok {
		return filepath.Base(path)
	}
	if n.Slot == saveporter.Auto {
		return domain.StateSlotAuto
	}
	return n.Slot.String()
}

func toSlot(slot string) saveporter.Slot {
	if slot == domain.StateSlotAuto {
		return saveporter.Auto
	}
	n, err := strconv.Atoi(slot)
	if err != nil {
		return saveporter.Auto
	}
	return saveporter.Slot(n)
}

// stateTarget finds where the save states of the game t syncs the save of are. why says why they
// don't sync.
func (s *Saves) stateTarget(state domain.InstallState, t saveTarget) (stateTarget, string) {
	gb := slices.Contains(domain.GBSystems, t.system)
	if !gb && t.system != "gba" {
		return stateTarget{}, "Save states sync for Game Boy, Game Boy Color and Game Boy Advance games."
	}
	core, ok := domain.StateCore(t.system, t.rule.Emulator.ID, t.emulator)
	if !ok {
		if gb {
			return stateTarget{}, "Save states sync for games played in RetroArch's Gambatte or mGBA core, or in mGBA."
		}
		return stateTarget{}, "Save states sync for games played in RetroArch's mGBA core or in mGBA."
	}
	kind := saveporter.RetroArch
	if t.rule.Emulator.ID == "mgba" {
		kind = saveporter.MGBA
	}
	dir, why := s.stateDir(state, t.rule.Emulator)
	if why != "" {
		return stateTarget{}, why
	}
	return stateTarget{
		romID: t.romID, name: t.name, emulator: t.rule.Emulator, kind: kind, system: t.system, core: core, dir: dir,
		base: strings.TrimSuffix(filepath.Base(t.path), t.rule.Ext),
	}, ""
}

// stateDir is the folder the emulator keeps its save states in: the one its own settings name,
// else EmuDeck's. why says why they don't sync.
func (s *Saves) stateDir(state domain.InstallState, e domain.Emulator) (dir, why string) {
	cfg := s.configs.StateConfig(e.ID)
	switch {
	case cfg.Problem != "":
		return "", cfg.Problem
	case cfg.Dir != "":
		return cfg.Dir, ""
	}
	i := slices.IndexFunc(state.Emulators, func(es domain.EmulatorState) bool { return es.ID == e.ID })
	if i < 0 || len(state.Emulators[i].Saves) < 2 || state.Emulators[i].Saves[1].Resolved == "" {
		return "", e.Name + "'s save states folder wasn't found. Set it up in EmuDeck, then check Setup status again."
	}
	return state.Emulators[i].Saves[1].Resolved, ""
}

// stateSettings is how the game with id's states sync: its own settings over the global ones.
func (s *Saves) stateSettings(ctx context.Context, id int) (domain.StateSettings, domain.StatePrefs, error) {
	global := domain.DefaultStateSettings
	if s.states != nil {
		global = s.states.StateSettings()
	}
	prefs, err := s.store.StatePrefs(ctx, id)
	if err != nil {
		return global, prefs, err
	}
	return prefs.Apply(global), prefs, nil
}

// syncStates syncs the save states of the games whose saves the pass synced.
func (s *Saves) syncStates(ctx context.Context, in domain.SignIn, server SaveServer, targets []saveTarget) SaveSummary {
	var sum SaveSummary
	state := s.install.State(false)
	type game struct {
		st  stateTarget
		set domain.StateSettings
	}
	var games []game
	for _, t := range targets {
		st, why := s.stateTarget(state, t)
		if why != "" {
			continue
		}
		set, _, err := s.stateSettings(ctx, t.romID)
		if err != nil {
			s.log.Warn("state settings", "rom_id", t.romID, "err", err)
			continue
		}
		if set.Sync {
			games = append(games, game{st, set})
		}
	}
	if len(games) == 0 {
		return sum
	}
	// One game's states, or every game's in one request rather than one per game.
	id := 0
	if len(games) == 1 {
		id = games[0].st.romID
	}
	all, listErr := server.States(ctx, id)
	if listErr != nil {
		listErr = s.conn.observe(in, listErr)
	}
	byROM := map[int][]domain.ServerState{}
	for _, x := range all {
		byROM[x.ROMID] = append(byROM[x.ROMID], x)
	}
	for _, g := range games {
		sum.add(s.syncGameStates(ctx, in, server, g.st, g.set, byROM[g.st.romID], listErr))
	}
	s.log.Info("save states synced", "games", len(games), "uploaded", sum.StatesUploaded, "downloaded", sum.StatesDownloaded,
		"waiting", sum.Waiting, "failed", sum.Failed)
	return sum
}

// syncGameStates syncs one game's save states: uploads what changed, puts RomM's newest from
// elsewhere in the target slot, and trims this Deck's uploads to what the game keeps.
func (s *Saves) syncGameStates(ctx context.Context, in domain.SignIn, server SaveServer, st stateTarget, set domain.StateSettings,
	list []domain.ServerState, listErr error,
) SaveSummary {
	var sum SaveSummary
	g, err := s.store.StateGame(ctx, st.romID)
	if err != nil {
		s.log.Warn("state game", "rom_id", st.romID, "err", err)
		return sum
	}
	before := g.Problem
	g.CheckedAt, g.Waiting, g.Problem = s.now().UTC(), false, ""
	defer func() {
		if err := s.store.PutStateGame(context.WithoutCancel(ctx), g); err != nil {
			s.log.Error("state game", "rom_id", st.romID, "err", err)
		}
	}()
	fail := func(what string, err error) {
		err = s.conn.observe(in, err)
		s.log.Warn("state sync", "rom_id", st.romID, "what", what, "err", err)
		if g.Problem != "" {
			return
		}
		g.Problem = fmt.Sprintf("Couldn't %s: %s", what, err)
		// A failure that repeats sync after sync is logged once.
		if g.Problem != before {
			s.logActivity(ctx, st.romID, domain.ActivityFailed, g.Problem)
		}
		sum.Failed++
	}

	// The slots RomM's state goes in must hold nothing that isn't on RomM yet.
	placing := []string{st.path(set.Target)}
	if set.Extra != "" && st.path(set.Extra) != placing[0] {
		placing = append(placing, st.path(set.Extra))
	}
	blocked := false
	seen, err := s.seenStates(ctx, st.romID)
	if err != nil {
		fail("read what was synced", err)
		return sum
	}
	for _, path := range st.files() {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err == nil && seen[path] == domain.ContentHash(data) {
			continue
		}
		var up domain.ServerState
		if err == nil {
			up, err = s.uploadState(ctx, server, st, path, data)
		}
		if err != nil {
			fail("upload the save state in "+domain.StateSlotName(slotOf(path)), err)
			blocked = blocked || slices.Contains(placing, path)
			continue
		}
		// The list was made before this upload.
		list = append(list, up)
		sum.StatesUploaded++
	}

	if listErr != nil {
		fail("list RomM's save states", listErr)
		return sum
	}
	uploaded, err := s.store.UploadedStates(ctx, st.romID)
	if err != nil {
		fail("read this Deck's uploads", err)
		return sum
	}
	ours := map[int]bool{}
	for _, u := range uploaded {
		ours[u.StateID] = true
	}
	var newest *domain.ServerState
	for i, x := range list {
		if x.Emulator == st.core && !ours[x.ID] && (newest == nil || x.CreatedAt.After(newest.CreatedAt)) {
			newest = &list[i]
		}
	}
	if newest != nil && newest.ID != g.Placed && !blocked {
		info, err := os.Stat(placing[0])
		switch {
		case err == nil && !newest.CreatedAt.After(info.ModTime()):
			// The Deck's state is newer; this one is in RomM's list to pick from.
			g.Placed = newest.ID
		case s.runs(st.emulator.ID):
			// The emulator writes its auto-state when the game closes, which would undo this.
			g.Waiting = true
			sum.Waiting++
		default:
			if err := s.placeState(ctx, server, st, *newest, placing); err != nil {
				fail("put RomM's save state on the Deck", err)
				break
			}
			g.Placed = newest.ID
			sum.StatesDownloaded++
			s.logActivity(ctx, st.romID, domain.ActivityStateDownloaded,
				fmt.Sprintf("Put RomM's save state from %s in %s.", newest.CreatedAt.Local().Format("Jan 2 15:04"), domain.StateSlotName(set.Target)))
		}
	}

	if err := s.trimStates(ctx, server, st.romID, uploaded, list, set.Keep); err != nil {
		fail("remove this Deck's older save states from RomM", err)
	}
	return sum
}

// stateWritten syncs the game, among ids, whose state file at path was just written, unless it holds
// what state sync last saw there.
func (s *Saves) stateWritten(ctx context.Context, ids []int, path string) {
	state := s.install.State(false)
	for _, id := range ids {
		t, why, err := s.target(ctx, id)
		if err != nil || why != "" {
			continue
		}
		st, why := s.stateTarget(state, t)
		if why != "" || !slices.Contains(st.files(), path) {
			continue
		}
		if seen, err := s.seenStates(ctx, id); err == nil {
			if data, err := os.ReadFile(path); err == nil && seen[path] == domain.ContentHash(data) {
				return
			}
		}
		s.log.Info("save state written", "rom_id", id, "path", path)
		s.request([]int{id}, false)
		return
	}
}

// seenStates is the content state sync last saw of each of a game's state files, by path.
func (s *Saves) seenStates(ctx context.Context, id int) (map[string]string, error) {
	files, err := s.store.StateFiles(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(files))
	for _, f := range files {
		out[f.Path] = f.Hash
	}
	return out, nil
}

// uploadState puts the state at path on RomM as the newest of its game's, as the web player keeps
// states, with the emulator's screenshot of it.
func (s *Saves) uploadState(ctx context.Context, server SaveServer, st stateTarget, path string, data []byte) (domain.ServerState, error) {
	hash := domain.ContentHash(data)
	web, err := domain.StateFor(data, st.system, st.core, saveporter.EmulatorJS)
	if err != nil {
		// Unreadable as it is, it stays so until the emulator writes it again.
		_ = s.store.PutStateFile(context.WithoutCancel(ctx), domain.StateFile{ROMID: st.romID, Path: path, Hash: hash})
		return domain.ServerState{}, fmt.Errorf("it isn't a state the plugin can read: %w", err)
	}
	// Named, as the web player names its states, for when the Deck wrote it: a state from long ago
	// reads as one.
	written := s.now()
	if info, err := os.Stat(path); err == nil {
		written = info.ModTime()
	}
	up := domain.StateUpload{
		ROMID: st.romID, Emulator: st.core, Data: web, Screenshot: stateScreenshot(st, path, data),
		FileName: fmt.Sprintf("%s [%s].state", st.base, written.Local().Format("2006-01-02 15-04-05-000")),
	}
	got, err := server.UploadState(ctx, up)
	if err != nil {
		return got, err
	}
	if got.CreatedAt.IsZero() {
		got.CreatedAt = s.now().UTC()
	}
	ctx = context.WithoutCancel(ctx)
	if err := s.store.AddUploadedState(ctx, domain.UploadedState{StateID: got.ID, ROMID: st.romID, Path: path, At: got.CreatedAt}); err != nil {
		return got, err
	}
	if err := s.store.PutStateFile(ctx, domain.StateFile{ROMID: st.romID, Path: path, Hash: hash}); err != nil {
		return got, err
	}
	s.log.Info("save state uploaded", "rom_id", st.romID, "state_id", got.ID, "path", path)
	s.logActivity(ctx, st.romID, domain.ActivityStateUploaded, "Uploaded the save state in "+domain.StateSlotName(slotOf(path))+".")
	return got, nil
}

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

// stateScreenshot is the picture of the state at path the emulator keeps: RetroArch's thumbnail
// beside it, or the mGBA app's state itself, a PNG of the screen. Nil when there's none.
func stateScreenshot(st stateTarget, path string, data []byte) []byte {
	if st.kind == saveporter.MGBA {
		if bytes.HasPrefix(data, pngSignature) {
			return data
		}
		return nil
	}
	shot, err := os.ReadFile(path + ".png")
	if err != nil || !bytes.HasPrefix(shot, pngSignature) {
		return nil
	}
	return shot
}

// placeState writes RomM's state x in the slots at paths, converted for the game's emulator, with
// RomM's screenshot as RetroArch's thumbnail.
func (s *Saves) placeState(ctx context.Context, server SaveServer, st stateTarget, x domain.ServerState, paths []string) error {
	data, err := server.DownloadState(ctx, x.ID)
	if err != nil {
		return err
	}
	out, err := domain.StateFor(data, st.system, st.core, st.kind)
	if err != nil {
		return fmt.Errorf("RomM's state isn't one %s can load: %w", st.emulator.Name, err)
	}
	var shot []byte
	if st.kind == saveporter.RetroArch && x.Screenshot != "" {
		if shot, err = server.StateScreenshot(ctx, x.Screenshot); err != nil || !bytes.HasPrefix(shot, pngSignature) {
			s.log.Warn("state screenshot", "rom_id", st.romID, "state_id", x.ID, "err", err)
			shot = nil
		}
	}
	hash := domain.ContentHash(out)
	for _, path := range paths {
		s.mu.Lock()
		s.wrote[path] = s.now()
		s.mu.Unlock()
		if err := writeAtomic(path, out); err != nil {
			return err
		}
		if err := s.store.PutStateFile(context.WithoutCancel(ctx), domain.StateFile{ROMID: st.romID, Path: path, Hash: hash}); err != nil {
			return err
		}
		if st.kind != saveporter.RetroArch {
			continue
		}
		// The thumbnail RetroArch shows for the slot is the old state's otherwise.
		if shot != nil {
			if err := writeAtomic(path+".png", shot); err != nil {
				s.log.Warn("state thumbnail", "path", path, "err", err)
			}
		} else if err := os.Remove(path + ".png"); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.log.Warn("state thumbnail", "path", path, "err", err)
		}
	}
	s.log.Info("save state placed", "rom_id", st.romID, "state_id", x.ID, "slots", len(paths))
	return nil
}

// trimStates removes this Deck's uploads of a game from RomM past the newest keep, keep being 0 for
// every one, and forgets those RomM no longer has.
func (s *Saves) trimStates(ctx context.Context, server SaveServer, id int, uploaded []domain.UploadedState, list []domain.ServerState, keep int) error {
	on := map[int]bool{}
	for _, x := range list {
		on[x.ID] = true
	}
	var gone []int
	var live []domain.UploadedState
	for _, u := range uploaded {
		if on[u.StateID] {
			live = append(live, u)
		} else {
			gone = append(gone, u.StateID)
		}
	}
	var err error
	if keep > 0 && len(live) > keep {
		var drop []int
		for _, u := range live[:len(live)-keep] {
			drop = append(drop, u.StateID)
		}
		if err = server.DeleteStates(ctx, drop); err == nil {
			gone = append(gone, drop...)
			s.log.Info("older save states removed", "rom_id", id, "states", len(drop))
		}
	}
	if len(gone) > 0 {
		if ferr := s.store.ForgetUploadedStates(context.WithoutCancel(ctx), gone); ferr != nil && err == nil {
			err = ferr
		}
	}
	return err
}

// StateStatus is where a game's state sync stands, and how it syncs.
type StateStatus struct {
	ROMID int
	// Syncs is unset when the game's states don't sync at all; Why says why.
	Syncs bool
	Why   string
	// Emulator writes the states, Dir is where.
	Emulator string
	Dir      string
	// Settings are how the game's states sync, Own the ones the game sets itself.
	Settings domain.StateSettings
	Own      domain.StatePrefs
	Waiting  bool
	Problem  string
	// Placed is RomM's state last put in the target slot.
	Placed int
}

// StateStatus returns where the game with id's state sync stands.
func (s *Saves) StateStatus(ctx context.Context, id int) (StateStatus, error) {
	out := StateStatus{ROMID: id}
	if s.store == nil {
		out.Why = s.problem.Error()
		return out, nil
	}
	t, why, err := s.target(ctx, id)
	if err != nil {
		return out, err
	}
	if why != "" {
		out.Why = "The game's saves don't sync, and its save states sync with them."
		return out, nil
	}
	st, why := s.stateTarget(s.install.State(false), t)
	if why != "" {
		out.Why = why
		return out, nil
	}
	if out.Settings, out.Own, err = s.stateSettings(ctx, id); err != nil {
		return out, err
	}
	g, err := s.store.StateGame(ctx, id)
	if err != nil {
		return out, err
	}
	out.Syncs, out.Emulator, out.Dir = true, st.emulator.Name, st.dir
	out.Waiting, out.Problem, out.Placed = g.Waiting, g.Problem, g.Placed
	return out, nil
}

// SetStatePrefs sets the game with id's own state settings, and syncs its states with them.
func (s *Saves) SetStatePrefs(ctx context.Context, id int, p domain.StatePrefs) error {
	if err := validStatePrefs(p); err != nil {
		return err
	}
	if s.store == nil {
		return s.problem
	}
	if err := s.store.SetStatePrefs(ctx, id, p); err != nil {
		return err
	}
	s.request([]int{id}, false)
	return nil
}

// SetGlobalStateSettings changes how states sync for games that don't set their own, and syncs
// every game's states with it.
func (s *Saves) SetGlobalStateSettings(p domain.StatePrefs) error {
	if err := validStatePrefs(p); err != nil {
		return err
	}
	if s.states == nil {
		return errors.New("state settings aren't kept")
	}
	if err := s.states.SetStateSettings(p); err != nil {
		return err
	}
	s.request(nil, false)
	return nil
}

// GlobalStateSettings is how states sync for games that don't set their own.
func (s *Saves) GlobalStateSettings() domain.StateSettings {
	if s.states == nil {
		return domain.DefaultStateSettings
	}
	return s.states.StateSettings()
}

func validStatePrefs(p domain.StatePrefs) error {
	invalid := func(what string) error {
		return &domain.Error{Kind: domain.KindInvalidInput, Message: what}
	}
	if p.Target != nil && !domain.ValidStateSlot(*p.Target) {
		return invalid("No such state slot: " + *p.Target + ".")
	}
	if p.Extra != nil && *p.Extra != "" && !domain.ValidStateSlot(*p.Extra) {
		return invalid("No such state slot: " + *p.Extra + ".")
	}
	if p.Keep != nil && !domain.ValidStateKeep(*p.Keep) {
		return invalid(fmt.Sprintf("Keeping %d states isn't one of the choices.", *p.Keep))
	}
	return nil
}

// StateEntry is a save state of a game's on RomM, as its list shows it.
type StateEntry struct {
	ID int
	At time.Time
	// FromDeck says this Deck uploaded it, and Slot from which slot.
	FromDeck bool
	Slot     string
	// Placed says it's RomM's state last put in the target slot.
	Placed     bool
	Screenshot string
	SizeBytes  int64
}

// stateListLength is how many of a game's states its list shows, newest first.
const stateListLength = 20

// States lists the newest of the game with id's save states on RomM.
func (s *Saves) States(ctx context.Context, id int) ([]StateEntry, error) {
	in, err := s.ready()
	if err != nil {
		return nil, err
	}
	t, why, err := s.target(ctx, id)
	if err != nil {
		return nil, err
	}
	if why != "" {
		return nil, &domain.Error{Kind: domain.KindInvalidInput, Message: why}
	}
	st, why := s.stateTarget(s.install.State(false), t)
	if why != "" {
		return nil, &domain.Error{Kind: domain.KindInvalidInput, Message: why}
	}
	list, err := s.dial(in.Server, in.Token).States(ctx, id)
	if err != nil {
		return nil, s.conn.observe(*in, err)
	}
	uploaded, err := s.store.UploadedStates(ctx, id)
	if err != nil {
		return nil, err
	}
	g, err := s.store.StateGame(ctx, id)
	if err != nil {
		return nil, err
	}
	from := map[int]string{}
	for _, u := range uploaded {
		from[u.StateID] = slotOf(u.Path)
	}
	out := []StateEntry{}
	for _, x := range list {
		// A state of another core than the Deck plays the game in doesn't load on the Deck.
		if x.Emulator != st.core {
			continue
		}
		slot, ours := from[x.ID]
		out = append(out, StateEntry{
			ID: x.ID, At: x.CreatedAt, FromDeck: ours, Slot: slot, Placed: x.ID == g.Placed, Screenshot: x.Screenshot, SizeBytes: x.SizeBytes,
		})
	}
	slices.SortStableFunc(out, func(a, b StateEntry) int { return b.At.Compare(a.At) })
	return out[:min(len(out), stateListLength)], nil
}

// StateScreenshot fetches the screenshot RomM keeps of a state, at the path its list gave.
func (s *Saves) StateScreenshot(ctx context.Context, path string) ([]byte, error) {
	in, err := s.ready()
	if err != nil {
		return nil, err
	}
	data, err := s.dial(in.Server, in.Token).StateScreenshot(ctx, path)
	return data, s.conn.observe(*in, err)
}

// LoadState puts RomM's state with stateID in the game with id's target slot, and the extra slot,
// after uploading what those hold that isn't on RomM yet.
func (s *Saves) LoadState(ctx context.Context, id, stateID int) error {
	s.passMu.Lock()
	defer s.passMu.Unlock()
	in, err := s.ready()
	if err != nil {
		return err
	}
	t, why, err := s.target(ctx, id)
	if err != nil {
		return err
	}
	if why != "" {
		return &domain.Error{Kind: domain.KindInvalidInput, Message: why}
	}
	st, why := s.stateTarget(s.install.State(false), t)
	if why != "" {
		return &domain.Error{Kind: domain.KindInvalidInput, Message: why}
	}
	if s.runs(st.emulator.ID) {
		return closeFirst(st.emulator)
	}
	set, _, err := s.stateSettings(ctx, id)
	if err != nil {
		return err
	}
	server := s.dial(in.Server, in.Token)
	list, err := server.States(ctx, id)
	if err != nil {
		return s.conn.observe(*in, err)
	}
	i := slices.IndexFunc(list, func(x domain.ServerState) bool { return x.ID == stateID })
	if i < 0 {
		return &domain.Error{Kind: domain.KindNotFound, Message: "RomM no longer has that save state."}
	}
	paths := []string{st.path(set.Target)}
	if set.Extra != "" && st.path(set.Extra) != paths[0] {
		paths = append(paths, st.path(set.Extra))
	}
	seen, err := s.seenStates(ctx, id)
	if err != nil {
		return err
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) || err == nil && seen[path] == domain.ContentHash(data) {
			continue
		}
		if err == nil {
			_, err = s.uploadState(ctx, server, st, path, data)
		}
		if err != nil {
			return fmt.Errorf("upload the save state in %s before replacing it: %w", domain.StateSlotName(slotOf(path)), s.conn.observe(*in, err))
		}
	}
	if err := s.placeState(ctx, server, st, list[i], paths); err != nil {
		return s.conn.observe(*in, err)
	}
	g, err := s.store.StateGame(ctx, id)
	if err != nil {
		return err
	}
	g.Placed, g.Waiting, g.Problem, g.CheckedAt = stateID, false, "", s.now().UTC()
	if err := s.store.PutStateGame(context.WithoutCancel(ctx), g); err != nil {
		return err
	}
	s.logActivity(ctx, id, domain.ActivityStateDownloaded,
		fmt.Sprintf("Put RomM's save state from %s in %s.", list[i].CreatedAt.Local().Format("Jan 2 15:04"), domain.StateSlotName(set.Target)))
	return nil
}
