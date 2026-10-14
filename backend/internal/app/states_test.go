package app

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
	"github.com/chenasraf/saveporter"
	"github.com/chenasraf/saveporter/retroarch"
)

type fakeState struct {
	domain.ServerState
	data, shot []byte
}

func (f *fakeRomMSaves) States(_ context.Context, id int) ([]domain.ServerState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stateFail != nil {
		return nil, f.stateFail
	}
	f.stateLists++
	var out []domain.ServerState
	for _, s := range f.states {
		if id == 0 || s.ROMID == id {
			out = append(out, s.ServerState)
		}
	}
	return out, nil
}

func (f *fakeRomMSaves) UploadState(_ context.Context, u domain.StateUpload) (domain.ServerState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stateFail != nil {
		return domain.ServerState{}, f.stateFail
	}
	return f.addState(u.ROMID, u.Emulator, f.tick(), u.Data, u.Screenshot), nil
}

func (f *fakeRomMSaves) addState(romID int, emulator string, at time.Time, data, shot []byte) domain.ServerState {
	s := domain.ServerState{ID: f.nextID, ROMID: romID, Emulator: emulator, CreatedAt: at, SizeBytes: int64(len(data))}
	f.nextID++
	if shot != nil {
		s.Screenshot = "/api/screenshots/" + string(rune('0'+s.ID%10)) + "/content"
	}
	f.states = append(f.states, fakeState{ServerState: s, data: data, shot: shot})
	return s
}

// otherState is a state of mGBA's another device or the web player put on RomM at at.
func (f *fakeRomMSaves) otherState(romID int, at time.Time, data, shot []byte) domain.ServerState {
	return f.otherStateOf(romID, "mgba", at, data, shot)
}

// otherStateOf is a state of core's another device or the web player put on RomM at at.
func (f *fakeRomMSaves) otherStateOf(romID int, core string, at time.Time, data, shot []byte) domain.ServerState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.addState(romID, core, at, data, shot)
}

func (f *fakeRomMSaves) DownloadState(_ context.Context, id int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.states {
		if s.ID == id {
			return s.data, nil
		}
	}
	return nil, &domain.Error{Kind: domain.KindNotFound, Message: "no such state"}
}

func (f *fakeRomMSaves) StateScreenshot(_ context.Context, path string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.states {
		if s.Screenshot == path {
			return s.shot, nil
		}
	}
	return nil, &domain.Error{Kind: domain.KindNotFound, Message: "no such screenshot"}
}

func (f *fakeRomMSaves) DeleteStates(_ context.Context, ids []int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletedStates = append(f.deletedStates, ids...)
	f.states = slices.DeleteFunc(f.states, func(s fakeState) bool { return slices.Contains(ids, s.ID) })
	return nil
}

// ours are the states on RomM the plugin uploaded, by the fake's own count.
func (f *fakeRomMSaves) stateCount(romID int) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, s := range f.states {
		if s.ROMID == romID {
			n++
		}
	}
	return n
}

func (f *fakeRomMSaves) newestState(romID int) fakeState {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out fakeState
	for _, s := range f.states {
		if s.ROMID == romID && (out.ID == 0 || s.CreatedAt.After(out.CreatedAt)) {
			out = s
		}
	}
	return out
}

func (m *memSaveStore) StateFiles(_ context.Context, id int) ([]domain.StateFile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.StateFile
	for path, hash := range m.stateFiles[id] {
		out = append(out, domain.StateFile{ROMID: id, Path: path, Hash: hash})
	}
	return out, nil
}

func (m *memSaveStore) PutStateFile(_ context.Context, f domain.StateFile) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stateFiles == nil {
		m.stateFiles = map[int]map[string]string{}
	}
	if m.stateFiles[f.ROMID] == nil {
		m.stateFiles[f.ROMID] = map[string]string{}
	}
	m.stateFiles[f.ROMID][f.Path] = f.Hash
	return nil
}

func (m *memSaveStore) UploadedStates(_ context.Context, id int) ([]domain.UploadedState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.UploadedState
	for _, u := range m.uploaded {
		if u.ROMID == id {
			out = append(out, u)
		}
	}
	slices.SortStableFunc(out, func(a, b domain.UploadedState) int { return a.At.Compare(b.At) })
	return out, nil
}

func (m *memSaveStore) AddUploadedState(_ context.Context, u domain.UploadedState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.uploaded = append(m.uploaded, u)
	return nil
}

func (m *memSaveStore) ForgetUploadedStates(_ context.Context, ids []int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.uploaded = slices.DeleteFunc(m.uploaded, func(u domain.UploadedState) bool { return slices.Contains(ids, u.StateID) })
	return nil
}

func (m *memSaveStore) StateGame(_ context.Context, id int) (domain.StateGame, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if g, ok := m.stateGames[id]; ok {
		return g, nil
	}
	return domain.StateGame{ROMID: id}, nil
}

func (m *memSaveStore) StateGames(context.Context) ([]domain.StateGame, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.StateGame
	for _, g := range m.stateGames {
		out = append(out, g)
	}
	return out, nil
}

func (m *memSaveStore) PutStateGame(_ context.Context, g domain.StateGame) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stateGames == nil {
		m.stateGames = map[int]domain.StateGame{}
	}
	m.stateGames[g.ROMID] = g
	return nil
}

func (m *memSaveStore) StatePrefs(_ context.Context, id int) (domain.StatePrefs, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statePrefs[id], nil
}

func (m *memSaveStore) SetStatePrefs(_ context.Context, id int, p domain.StatePrefs) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.statePrefs == nil {
		m.statePrefs = map[int]domain.StatePrefs{}
	}
	m.statePrefs[id] = p
	return nil
}

type memStateSettings struct {
	mu  sync.Mutex
	set domain.StateSettings
}

func (m *memStateSettings) StateSettings() domain.StateSettings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.set
}

func (m *memStateSettings) SetStateSettings(p domain.StatePrefs) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.set = p.Apply(m.set)
	return nil
}

// gbaState is a RetroArch state of the mGBA core at version, its content marked with mark, as
// RetroArch writes it.
func gbaState(version uint32, mark byte) []byte {
	core := make([]byte, 0x61000+16)
	binary.LittleEndian.PutUint32(core, 0x01000000+version)
	core[0x1000] = mark
	return retroarch.Wrap(core)
}

// stateMark is the mark gbaState gave a state, whatever form it's in now.
func stateMark(t *testing.T, data []byte) byte {
	t.Helper()
	core, err := retroarch.CoreState(data)
	if err != nil {
		t.Fatal(err)
	}
	return core[0x1000]
}

func stateInfo(t *testing.T, data []byte) saveporter.Info {
	t.Helper()
	info, err := saveporter.Detect(data)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

var png1x1 = append([]byte("\x89PNG\r\n\x1a\n"), "thumbnail"...)

const goldenSun = 60

// ownGoldenSun installs a GBA game, which EmuDeck runs in RetroArch's mGBA core, and returns where
// its states are.
func (f *saveFixture) ownGoldenSun(t *testing.T) string {
	t.Helper()
	f.ownGame(t, goldenSun, 10, "gba", "Golden Sun (USA).gba", "")
	return filepath.Join(f.root, "saves", "retroarch", "states")
}

func writeAt(t *testing.T, path string, data []byte, at time.Time) {
	t.Helper()
	writeFile(t, path, data)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func readState(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStatesUploadNewDeckStates(t *testing.T) {
	f := newSaveFixture(t)
	dir := f.ownGoldenSun(t)
	now := time.Now()
	writeAt(t, filepath.Join(dir, "Golden Sun (USA).state.auto"), gbaState(9, 1), now)
	writeAt(t, filepath.Join(dir, "Golden Sun (USA).state1"), gbaState(9, 2), now)
	writeFile(t, filepath.Join(dir, "Golden Sun (USA).state1.png"), png1x1)

	sum := f.syncGame(t, goldenSun)
	if sum.StatesUploaded != 2 || sum.StatesDownloaded != 0 || f.romm.stateCount(goldenSun) != 2 {
		t.Fatalf("summary %+v, %d on RomM", sum, f.romm.stateCount(goldenSun))
	}
	for _, s := range f.romm.states {
		info := stateInfo(t, s.data)
		if s.Emulator != "mgba" || info.StateVersion != 7 || info.Container != saveporter.ContainerRetroArch {
			t.Errorf("uploaded %+v as v%d %s", s.ServerState, info.StateVersion, info.Container)
		}
		if mark := stateMark(t, s.data); mark == 2 && !bytes.Equal(s.shot, png1x1) || mark == 1 && s.shot != nil {
			t.Errorf("state %d screenshot %q", mark, s.shot)
		}
	}
	if kinds := f.store.kinds(goldenSun); slices.Index(kinds, domain.ActivityStateUploaded) < 0 {
		t.Errorf("activity %v", kinds)
	}

	// Nothing changed: nothing goes up again, and RomM's newest are this Deck's own.
	if sum := f.syncGame(t, goldenSun); sum.StatesUploaded != 0 || sum.StatesDownloaded != 0 || f.romm.stateCount(goldenSun) != 2 {
		t.Fatalf("second sync %+v", sum)
	}
	// RetroArch writes the auto-state again on quit: that one goes up.
	writeAt(t, filepath.Join(dir, "Golden Sun (USA).state.auto"), gbaState(9, 3), now.Add(time.Minute))
	if sum := f.syncGame(t, goldenSun); sum.StatesUploaded != 1 || f.romm.stateCount(goldenSun) != 3 {
		t.Fatalf("after a new auto-state %+v", sum)
	}
}

func TestStatesDownloadTheNewestFromElsewhere(t *testing.T) {
	f := newSaveFixture(t)
	dir := f.ownGoldenSun(t)
	auto := filepath.Join(dir, "Golden Sun (USA).state.auto")
	f.romm.otherState(goldenSun, time.Now().Add(-time.Hour), gbaState(7, 4), nil)
	web := f.romm.otherState(goldenSun, time.Now().Add(-time.Minute), gbaState(7, 5), png1x1)

	sum := f.syncGame(t, goldenSun)
	if sum.StatesDownloaded != 1 || sum.StatesUploaded != 0 {
		t.Fatalf("summary %+v", sum)
	}
	got := readState(t, auto)
	if stateMark(t, got) != 5 || stateInfo(t, got).Container != saveporter.ContainerRetroArch {
		t.Fatalf("auto-state holds %d", stateMark(t, got))
	}
	if !bytes.Equal(readState(t, auto+".png"), png1x1) {
		t.Fatal("RetroArch's thumbnail isn't RomM's screenshot")
	}
	if g, _ := f.store.StateGame(context.Background(), goldenSun); g.Placed != web.ID {
		t.Fatalf("placed %d, want %d", g.Placed, web.ID)
	}
	// It came from RomM, so it doesn't go back, and it isn't placed again.
	if sum := f.syncGame(t, goldenSun); sum.StatesUploaded != 0 || sum.StatesDownloaded != 0 {
		t.Fatalf("second sync %+v", sum)
	}
}

func TestStatesKeepTheDecksNewerState(t *testing.T) {
	f := newSaveFixture(t)
	dir := f.ownGoldenSun(t)
	auto := filepath.Join(dir, "Golden Sun (USA).state.auto")
	f.romm.otherState(goldenSun, time.Now().Add(-time.Hour), gbaState(7, 4), nil)
	writeAt(t, auto, gbaState(9, 1), time.Now())

	sum := f.syncGame(t, goldenSun)
	if sum.StatesDownloaded != 0 || sum.StatesUploaded != 1 || stateMark(t, readState(t, auto)) != 1 {
		t.Fatalf("summary %+v, auto-state %d", sum, stateMark(t, readState(t, auto)))
	}
}

func TestStatesUploadBeforeReplacing(t *testing.T) {
	f := newSaveFixture(t)
	dir := f.ownGoldenSun(t)
	auto := filepath.Join(dir, "Golden Sun (USA).state.auto")
	writeAt(t, auto, gbaState(9, 1), time.Now().Add(-2*time.Hour))
	f.romm.otherState(goldenSun, time.Now().Add(-time.Minute), gbaState(7, 5), nil)

	sum := f.syncGame(t, goldenSun)
	if sum.StatesUploaded != 1 || sum.StatesDownloaded != 1 || stateMark(t, readState(t, auto)) != 5 {
		t.Fatalf("summary %+v", sum)
	}
	found := false
	for _, s := range f.romm.states {
		found = found || stateMark(t, s.data) == 1
	}
	if !found {
		t.Fatal("the Deck's state was replaced before it reached RomM")
	}

	// When it can't reach RomM, it isn't replaced at all.
	f2 := newSaveFixture(t)
	dir = f2.ownGoldenSun(t)
	auto = filepath.Join(dir, "Golden Sun (USA).state.auto")
	writeAt(t, auto, gbaState(9, 1), time.Now().Add(-2*time.Hour))
	f2.romm.otherState(goldenSun, time.Now().Add(-time.Minute), gbaState(7, 5), nil)
	f2.romm.stateFail = &domain.Error{Kind: domain.KindUnreachable, Message: "down"}
	if sum := f2.syncGame(t, goldenSun); sum.StatesDownloaded != 0 || stateMark(t, readState(t, auto)) != 1 {
		t.Fatalf("offline: %+v", sum)
	}
}

func TestStatesWaitForTheEmulator(t *testing.T) {
	f := newSaveFixture(t)
	dir := f.ownGoldenSun(t)
	auto := filepath.Join(dir, "Golden Sun (USA).state.auto")
	f.romm.otherState(goldenSun, time.Now(), gbaState(7, 5), nil)
	f.retroarch.Store(true)
	if sum := f.syncGame(t, goldenSun); sum.Waiting != 1 || sum.StatesDownloaded != 0 {
		t.Fatalf("while running %+v", sum)
	}
	if _, err := os.Stat(auto); err == nil {
		t.Fatal("wrote the auto-state while RetroArch ran")
	}
	if g, _ := f.store.StateGame(context.Background(), goldenSun); !g.Waiting {
		t.Fatal("not marked waiting")
	}
	f.retroarch.Store(false)
	if sum := f.syncGame(t, goldenSun); sum.StatesDownloaded != 1 || stateMark(t, readState(t, auto)) != 5 {
		t.Fatalf("after closing %+v", sum)
	}
}

func TestStatesTargetAndExtraSlots(t *testing.T) {
	f := newSaveFixture(t)
	dir := f.ownGoldenSun(t)
	three, extra := "3", "1"
	if err := f.store.SetStatePrefs(context.Background(), goldenSun, domain.StatePrefs{Target: &three, Extra: &extra}); err != nil {
		t.Fatal(err)
	}
	f.romm.otherState(goldenSun, time.Now(), gbaState(7, 5), png1x1)
	if sum := f.syncGame(t, goldenSun); sum.StatesDownloaded != 1 {
		t.Fatalf("summary %+v", sum)
	}
	for _, name := range []string{"Golden Sun (USA).state3", "Golden Sun (USA).state1"} {
		if stateMark(t, readState(t, filepath.Join(dir, name))) != 5 || !bytes.Equal(readState(t, filepath.Join(dir, name+".png")), png1x1) {
			t.Errorf("%s not placed", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "Golden Sun (USA).state.auto")); err == nil {
		t.Error("the auto-state was written")
	}
	// Neither copy goes back up.
	if sum := f.syncGame(t, goldenSun); sum.StatesUploaded != 0 {
		t.Fatalf("copies uploaded %+v", sum)
	}
}

func TestStatesTrimThisDecksUploads(t *testing.T) {
	f := newSaveFixture(t)
	dir := f.ownGoldenSun(t)
	five := 5
	if err := f.store.SetStatePrefs(context.Background(), goldenSun, domain.StatePrefs{Keep: &five}); err != nil {
		t.Fatal(err)
	}
	web := f.romm.otherState(goldenSun, time.Now().Add(-48*time.Hour), gbaState(7, 9), nil)
	auto := filepath.Join(dir, "Golden Sun (USA).state.auto")
	for i := range 7 {
		writeAt(t, auto, gbaState(9, byte(10+i)), time.Now().Add(time.Duration(i)*time.Minute))
		f.syncGame(t, goldenSun)
	}
	ups, _ := f.store.UploadedStates(context.Background(), goldenSun)
	if len(ups) != 5 || f.romm.stateCount(goldenSun) != 6 {
		t.Fatalf("%d uploads recorded, %d on RomM", len(ups), f.romm.stateCount(goldenSun))
	}
	if slices.Contains(f.romm.deletedStates, web.ID) {
		t.Fatal("deleted the web player's state")
	}
	if newest := f.romm.newestState(goldenSun); stateMark(t, newest.data) != 16 {
		t.Fatalf("newest on RomM is %d", stateMark(t, newest.data))
	}
}

func TestStatesInTheMGBAApp(t *testing.T) {
	f := newSaveFixture(t)
	f.launchers["gba"] = mGBA
	f.saves = f.newSaves()
	f.ownGame(t, goldenSun, 10, "gba", "Golden Sun (USA).gba", "")
	dir := filepath.Join(f.root, "saves", "mgba", "states")
	ss3 := filepath.Join(dir, "Golden Sun (USA).ss3")
	raw, err := saveporter.Convert(gbaState(9, 2), saveporter.Target{Emulator: saveporter.MGBA})
	if err != nil {
		t.Fatal(err)
	}
	writeAt(t, ss3, raw, time.Now().Add(-time.Hour))
	f.romm.otherState(goldenSun, time.Now(), gbaState(7, 5), nil)

	sum := f.syncGame(t, goldenSun)
	if sum.StatesUploaded != 1 || sum.StatesDownloaded != 1 {
		t.Fatalf("summary %+v", sum)
	}
	ss0 := readState(t, filepath.Join(dir, "Golden Sun (USA).ss0"))
	if info := stateInfo(t, ss0); info.Container != saveporter.ContainerRaw || info.StateVersion != 7 {
		t.Fatalf("ss0 is %+v", info)
	}
}

func TestStatesOffWhenTurnedOff(t *testing.T) {
	f := newSaveFixture(t)
	dir := f.ownGoldenSun(t)
	writeAt(t, filepath.Join(dir, "Golden Sun (USA).state.auto"), gbaState(9, 1), time.Now())
	off := false
	if err := f.stateSet.SetStateSettings(domain.StatePrefs{Sync: &off}); err != nil {
		t.Fatal(err)
	}
	if sum := f.syncGame(t, goldenSun); sum.StatesUploaded != 0 {
		t.Fatalf("synced while off %+v", sum)
	}
	// A game's own setting turns it back on.
	on := true
	if err := f.store.SetStatePrefs(context.Background(), goldenSun, domain.StatePrefs{Sync: &on}); err != nil {
		t.Fatal(err)
	}
	if sum := f.syncGame(t, goldenSun); sum.StatesUploaded != 1 {
		t.Fatalf("game turned on %+v", sum)
	}
}

func TestStatesOnlyForGameBoys(t *testing.T) {
	f := newSaveFixture(t)
	states := filepath.Join(f.root, "saves", "retroarch", "states")
	writeFile(t, filepath.Join(states, "Chrono Trigger (USA).state"), gbaState(9, 1))
	if sum := f.sync(t); sum.StatesUploaded != 0 || f.romm.stateCount(1) != 0 {
		t.Fatalf("an SNES game's state synced %+v", sum)
	}
	st, err := f.saves.StateStatus(context.Background(), 1)
	if err != nil || st.Syncs || st.Why == "" {
		t.Fatalf("status %+v, %v", st, err)
	}
}

func TestStateStatusAndList(t *testing.T) {
	f := newSaveFixture(t)
	dir := f.ownGoldenSun(t)
	st, err := f.saves.StateStatus(context.Background(), goldenSun)
	if err != nil || !st.Syncs || st.Emulator != "RetroArch" || st.Dir != dir || st.Settings != domain.DefaultStateSettings {
		t.Fatalf("status %+v, %v", st, err)
	}
	web := f.romm.otherState(goldenSun, time.Now().Add(-time.Hour), gbaState(7, 5), nil)
	writeAt(t, filepath.Join(dir, "Golden Sun (USA).state2"), gbaState(9, 1), time.Now())
	f.syncGame(t, goldenSun)

	list, err := f.saves.States(context.Background(), goldenSun)
	if err != nil || len(list) != 2 {
		t.Fatalf("list %+v, %v", list, err)
	}
	if !list[0].FromDeck || list[0].Slot != "2" || list[1].ID != web.ID || list[1].FromDeck || !list[1].Placed {
		t.Fatalf("list %+v", list)
	}
}

func TestLoadState(t *testing.T) {
	f := newSaveFixture(t)
	dir := f.ownGoldenSun(t)
	auto := filepath.Join(dir, "Golden Sun (USA).state.auto")
	old := f.romm.otherState(goldenSun, time.Now().Add(-48*time.Hour), gbaState(7, 4), nil)
	writeAt(t, auto, gbaState(9, 1), time.Now())

	f.retroarch.Store(true)
	if err := f.saves.LoadState(context.Background(), goldenSun, old.ID); err == nil {
		t.Fatal("loaded a state while RetroArch ran")
	}
	f.retroarch.Store(false)
	if err := f.saves.LoadState(context.Background(), goldenSun, old.ID); err != nil {
		t.Fatal(err)
	}
	if stateMark(t, readState(t, auto)) != 4 {
		t.Fatal("not loaded")
	}
	// The Deck's state it replaced went to RomM first.
	found := false
	for _, s := range f.romm.states {
		found = found || stateMark(t, s.data) == 1
	}
	if !found {
		t.Fatal("the replaced state isn't on RomM")
	}
	// The next sync leaves the picked state, older as it is, in place.
	if sum := f.syncGame(t, goldenSun); sum.StatesDownloaded != 0 || sum.StatesUploaded != 0 || stateMark(t, readState(t, auto)) != 4 {
		t.Fatalf("sync after loading %+v", sum)
	}
	if err := f.saves.LoadState(context.Background(), goldenSun, 999); err == nil {
		t.Fatal("loaded a state RomM doesn't have")
	}
}

func TestStateWriteSyncsTheGame(t *testing.T) {
	f := newSaveFixture(t)
	dir := f.ownGoldenSun(t)
	auto := filepath.Join(dir, "Golden Sun (USA).state.auto")
	writeFile(t, auto, gbaState(9, 1))
	f.saves.saveWritten(context.Background(), auto)
	f.awaitSync(t, goldenSun)
}

func TestStateSettingsAreChecked(t *testing.T) {
	f := newSaveFixture(t)
	bad, keep := "12", 7
	if err := f.saves.SetStatePrefs(context.Background(), goldenSun, domain.StatePrefs{Target: &bad}); err == nil {
		t.Error("took slot 12")
	}
	if err := f.saves.SetGlobalStateSettings(domain.StatePrefs{Keep: &keep}); err == nil {
		t.Error("took keeping 7")
	}
}

// Changing how a game's states sync syncs them with the change.
func TestStateSettingsSyncTheGame(t *testing.T) {
	f := newSaveFixture(t)
	f.ownGoldenSun(t)
	one := "1"
	if err := f.saves.SetStatePrefs(context.Background(), goldenSun, domain.StatePrefs{Extra: &one}); err != nil {
		t.Fatal(err)
	}
	f.awaitSync(t, goldenSun)
	got, _ := f.store.StatePrefs(context.Background(), goldenSun)
	if got.Extra == nil || *got.Extra != "1" || got.Target != nil {
		t.Fatalf("kept %+v", got)
	}
}

// awaitSync waits for a sync of the game with id to start.
func (f *saveFixture) awaitSync(t *testing.T, id int) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case e := <-f.events:
			if e.Syncing && slices.Contains(e.ROMIDs, id) {
				return
			}
		case <-timeout:
			t.Fatalf("no sync of game %d started", id)
		}
	}
}

// A sync of several games lists RomM's states once, and keeps track of what it uploads meanwhile.
func TestStatesOfManyGamesListOnce(t *testing.T) {
	f := newSaveFixture(t)
	dir := f.ownGoldenSun(t)
	f.ownGame(t, 61, 10, "gba", "Advance Wars (USA).gba", "")
	writeAt(t, filepath.Join(dir, "Golden Sun (USA).state.auto"), gbaState(9, 1), time.Now())
	writeAt(t, filepath.Join(dir, "Advance Wars (USA).state"), gbaState(9, 2), time.Now())
	web := f.romm.otherState(61, time.Now().Add(-48*time.Hour), gbaState(7, 3), nil)

	sum, err := f.saves.Sync(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if sum.StatesUploaded != 2 || f.romm.stateLists != 1 {
		t.Fatalf("summary %+v, %d listings", sum, f.romm.stateLists)
	}
	for _, id := range []int{goldenSun, 61} {
		if ups, _ := f.store.UploadedStates(context.Background(), id); len(ups) != 1 {
			t.Errorf("game %d: uploads recorded %+v", id, ups)
		}
	}
	// The web player's older state was considered, not placed over the Deck's newer one.
	if g, _ := f.store.StateGame(context.Background(), 61); g.Placed != web.ID {
		t.Errorf("game 61 placed %d", g.Placed)
	}
}
