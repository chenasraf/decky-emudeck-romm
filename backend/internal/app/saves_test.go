package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// fakeRomMSaves keeps saves the way RomM does for one user and this Deck's Device: versions per
// slot, and the Device's record of the version it last synced. Its negotiation follows RomM's.
type fakeRomMSaves struct {
	mu       sync.Mutex
	clock    time.Time
	nextID   int
	versions []fakeVersion
	// synced is this Device's record of each save it synced, by save id.
	synced    map[int]fakeSync
	uploads   int
	downloads int
	sessions  []int
	completed map[int][2]int
	requests  []domain.SyncRequest
	failWith  error
	// noBaselines is RomM before 5.4.0, which keeps no hashes of what a Device synced and goes by
	// times alone.
	noBaselines bool
}

type fakeVersion struct {
	domain.ServerSave
	data []byte
}

type fakeSync struct {
	at               time.Time
	hash, serverHash string
}

func newFakeRomMSaves() *fakeRomMSaves {
	return &fakeRomMSaves{clock: time.Now().UTC(), nextID: 1, synced: map[int]fakeSync{}, completed: map[int][2]int{}}
}

func (f *fakeRomMSaves) tick() time.Time {
	f.clock = f.clock.Add(time.Second)
	return f.clock
}

// newest is the head of each (ROM, slot), slotted saves only.
func (f *fakeRomMSaves) heads() map[[2]any]fakeVersion {
	out := map[[2]any]fakeVersion{}
	for _, v := range f.versions {
		if v.Slot == "" {
			continue
		}
		k := [2]any{v.ROMID, v.Slot}
		if cur, ok := out[k]; !ok || v.UpdatedAt.After(cur.UpdatedAt) {
			out[k] = v
		}
	}
	return out
}

// otherDevice stores a save as another device would.
func (f *fakeRomMSaves) otherDevice(romID int, emulator string, data []byte) fakeVersion {
	f.mu.Lock()
	defer f.mu.Unlock()
	v := fakeVersion{ServerSave: domain.ServerSave{
		ID: f.nextID, ROMID: romID, FileName: "save.srm", Slot: domain.AutosaveSlot, Emulator: emulator,
		ContentHash: md5Hex(data), UpdatedAt: f.tick(),
	}, data: data}
	f.nextID++
	f.versions = append(f.versions, v)
	return v
}

func (f *fakeRomMSaves) NegotiateSync(_ context.Context, req domain.SyncRequest) (domain.SyncPlan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	if f.failWith != nil {
		return domain.SyncPlan{}, f.failWith
	}
	inScope := map[int]bool{}
	for _, id := range req.ROMIDs {
		inScope[id] = true
	}
	heads := f.heads()
	plan := domain.SyncPlan{SessionID: len(f.sessions) + 1}
	f.sessions = append(f.sessions, plan.SessionID)
	matched := map[int]bool{}
	for _, c := range req.Saves {
		head, ok := heads[[2]any{c.ROMID, c.Slot}]
		if !ok {
			plan.Operations = append(plan.Operations, domain.SyncOperation{Action: domain.SyncUpload, ROMID: c.ROMID, Slot: c.Slot})
			continue
		}
		matched[head.ID] = true
		sync := f.synced[head.ID]
		if f.noBaselines {
			sync.hash, sync.serverHash = "", ""
		}
		plan.Operations = append(plan.Operations, domain.SyncOperation{
			Action: compareSaves(c, head.ServerSave, sync), ROMID: c.ROMID, SaveID: head.ID, Slot: head.Slot,
			Emulator: head.Emulator, ServerUpdatedAt: head.UpdatedAt, ServerHash: head.ContentHash,
		})
	}
	for _, head := range heads {
		if matched[head.ID] || !inScope[head.ROMID] {
			continue
		}
		if s, ok := f.synced[head.ID]; ok && !head.UpdatedAt.After(s.at) && !req.RestoreUnlisted {
			continue
		}
		plan.Operations = append(plan.Operations, domain.SyncOperation{
			Action: domain.SyncDownload, ROMID: head.ROMID, SaveID: head.ID, Slot: head.Slot, Emulator: head.Emulator,
			ServerUpdatedAt: head.UpdatedAt, ServerHash: head.ContentHash,
		})
	}
	return plan, nil
}

// compareSaves is RomM's compare_save_state.
func compareSaves(c domain.ClientSave, s domain.ServerSave, sync fakeSync) domain.SyncAction {
	if c.ContentHash != "" && c.ContentHash == s.ContentHash {
		return domain.SyncNoOp
	}
	if !sync.at.IsZero() {
		clientSame := c.ContentHash != "" && c.ContentHash == sync.hash
		serverSame := s.ContentHash != "" && s.ContentHash == sync.serverHash
		clientChanged := c.UpdatedAt.After(sync.at) && !clientSame
		serverChanged := s.UpdatedAt.After(sync.at) && !serverSame
		switch {
		case clientChanged && serverChanged:
			return domain.SyncConflict
		case clientChanged:
			return domain.SyncUpload
		case serverChanged:
			return domain.SyncDownload
		}
		return domain.SyncNoOp
	}
	switch {
	case c.UpdatedAt.After(s.UpdatedAt):
		return domain.SyncUpload
	case s.UpdatedAt.After(c.UpdatedAt):
		return domain.SyncDownload
	}
	return domain.SyncConflict
}

func (f *fakeRomMSaves) UploadSave(_ context.Context, u domain.SaveUpload) (domain.ServerSave, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if head, ok := f.heads()[[2]any{u.ROMID, u.Slot}]; ok {
		if s, ok := f.synced[head.ID]; !ok || s.at.Before(head.UpdatedAt) {
			return domain.ServerSave{}, &domain.Error{Kind: domain.KindSaveConflict, Message: "newer"}
		}
	}
	v := fakeVersion{ServerSave: domain.ServerSave{
		ID: f.nextID, ROMID: u.ROMID, FileName: u.FileName, Slot: u.Slot, Emulator: u.Emulator,
		ContentHash: md5Hex(u.Data), UpdatedAt: f.tick(),
	}, data: append([]byte{}, u.Data...)}
	f.nextID++
	f.versions = append(f.versions, v)
	f.synced[v.ID] = fakeSync{at: v.UpdatedAt, hash: u.ContentHash, serverHash: v.ContentHash}
	f.uploads++
	return v.ServerSave, nil
}

func (f *fakeRomMSaves) version(id int) (fakeVersion, bool) {
	for _, v := range f.versions {
		if v.ID == id {
			return v, true
		}
	}
	return fakeVersion{}, false
}

func (f *fakeRomMSaves) DownloadSave(_ context.Context, id int, _ string, _ int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.version(id)
	if !ok {
		return nil, &domain.Error{Kind: domain.KindNotFound, Message: "gone"}
	}
	f.downloads++
	return append([]byte{}, v.data...), nil
}

func (f *fakeRomMSaves) ConfirmSaveDownload(_ context.Context, id int, _ string, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.version(id)
	if !ok {
		return &domain.Error{Kind: domain.KindNotFound, Message: "gone"}
	}
	f.synced[id] = fakeSync{at: v.UpdatedAt, hash: hash, serverHash: v.ContentHash}
	return nil
}

func (f *fakeRomMSaves) CompleteSync(_ context.Context, session, completed, failed int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completed[session] = [2]int{completed, failed}
	return nil
}

// head returns the newest autosave of a ROM.
func (f *fakeRomMSaves) head(romID int) (fakeVersion, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.heads()[[2]any{romID, domain.AutosaveSlot}]
	return v, ok
}

// memSaveStore is a SaveStore in memory.
type memSaveStore struct {
	mu      sync.Mutex
	records map[int]domain.SaveRecord
}

func (m *memSaveStore) PutSaveRecord(_ context.Context, r domain.SaveRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records[r.ROMID] = r
	return nil
}

func (m *memSaveStore) SaveRecords(context.Context) ([]domain.SaveRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.SaveRecord
	for _, r := range m.records {
		out = append(out, r)
	}
	return out, nil
}

// fakeLaunchers answers with the launcher of each system, or with nothing known.
type fakeLaunchers map[string]domain.GameLauncher

func (f fakeLaunchers) Launcher(system, _ string) (domain.GameLauncher, error) {
	if l, ok := f[system]; ok {
		return l, nil
	}
	return domain.GameLauncher{}, errors.New("no ES-DE here")
}

type saveFixture struct {
	*downloadFixture
	romm      *fakeRomMSaves
	store     *memSaveStore
	launchers fakeLaunchers
	saves     *Saves
	retroarch atomic.Bool
	backups   string
	events    chan SaveSyncEvent
}

var snes9x = domain.GameLauncher{ESDECommand: domain.ESDECommand{
	Label: "Snes9x - Current", Command: "%EMULATOR_RETROARCH% -L %CORE_RETROARCH%/snes9x_libretro.so %ROM%",
}, Known: true}

// newSaveFixture is the download fixture with Chrono Trigger installed, signed in as a Device.
func newSaveFixture(t *testing.T) *saveFixture {
	t.Helper()
	f := &saveFixture{
		downloadFixture: newDownloadFixture(t, 2),
		romm:            newFakeRomMSaves(),
		store:           &memSaveStore{records: map[int]domain.SaveRecord{}},
		launchers:       fakeLaunchers{"snes": snes9x},
		events:          make(chan SaveSyncEvent, 16),
	}
	f.settings.mu.Lock()
	f.settings.in.DeviceID = "deck-1"
	f.settings.mu.Unlock()
	f.backups = filepath.Join(f.root, "backups")
	f.install(t, 1)
	f.saves = f.newSaves()
	return f
}

// newSaves makes save sync over the fixture's stores, the way the backend starts it.
func (f *saveFixture) newSaves() *Saves {
	d := f.downloads
	s := NewSaves(context.Background(), d.catalog, d.install, d.conn, func(domain.Server, string) SaveServer { return f.romm },
		f.owned, f.store, nil, f.launchers, func() (domain.Emulator, bool) {
			if f.retroarch.Load() {
				return domain.EmulatorByID(domain.RetroArchID)
			}
			return domain.Emulator{}, false
		}, f.backups, func(e SaveSyncEvent) { f.events <- e }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return s
}

func (f *saveFixture) chronoSave() string {
	return filepath.Join(f.raSaves, "Chrono Trigger (USA).srm")
}

// writeSave writes the local save with its time set to at, as RetroArch writing it then would.
func (f *saveFixture) writeSave(t *testing.T, data string, at time.Time) {
	t.Helper()
	writeFile(t, f.chronoSave(), []byte(data))
	if err := os.Chtimes(f.chronoSave(), at, at); err != nil {
		t.Fatal(err)
	}
}

func (f *saveFixture) sync(t *testing.T) SaveSummary {
	t.Helper()
	summary, err := f.saves.Sync(context.Background(), []int{1})
	if err != nil {
		t.Fatal(err)
	}
	return summary
}

func (f *saveFixture) status(t *testing.T) SaveStatus {
	t.Helper()
	s, err := f.saves.Status(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSaveUploads(t *testing.T) {
	f := newSaveFixture(t)
	f.writeSave(t, "deck progress", time.Now())

	if s := f.sync(t); s.Uploaded != 1 || s.Games != 1 {
		t.Fatalf("summary %+v", s)
	}
	head, ok := f.romm.head(1)
	if !ok || string(head.data) != "deck progress" || head.Emulator != "snes9x" || head.FileName != "Chrono Trigger (USA).srm" {
		t.Fatalf("server holds %+v", head)
	}
	if st := f.status(t); !st.Syncs || st.State != domain.SaveSynced || st.Path != f.chronoSave() || st.Emulator != "snes9x" {
		t.Fatalf("status %+v", st)
	}
	if got := f.romm.completed[1]; got != [2]int{1, 0} {
		t.Fatalf("session completed with %v", got)
	}
	if req := f.romm.requests[0]; !req.RestoreUnlisted || req.DeviceID != "deck-1" || len(req.ROMIDs) != 1 {
		t.Fatalf("request %+v", req)
	}
}

func TestSaveDownloadsWhenTheDeckHasNone(t *testing.T) {
	f := newSaveFixture(t)
	f.romm.otherDevice(1, "snes9x", []byte("handheld progress"))

	if s := f.sync(t); s.Downloaded != 1 {
		t.Fatalf("summary %+v", s)
	}
	if got := readFile(t, f.chronoSave()); got != "handheld progress" {
		t.Fatalf("save holds %q", got)
	}
	// RomM now counts this Deck as holding its save: syncing again does nothing.
	if s := f.sync(t); s.Downloaded+s.Uploaded != 0 || f.romm.downloads != 1 {
		t.Fatalf("second sync %+v, %d downloads", s, f.romm.downloads)
	}
}

// RetroArch writes the save again when a game closes, changed or not. That alone never makes a
// conflict, even on a RomM that goes by times alone.
func TestNewerServerSaveReplacesTheDecksWithABackup(t *testing.T) {
	for name, noBaselines := range map[string]bool{"RomM 5.4.0": false, "older RomM": true} {
		t.Run(name, func(t *testing.T) {
			f := newSaveFixture(t)
			f.romm.noBaselines = noBaselines
			f.writeSave(t, "old progress", time.Now().Add(-time.Hour))
			f.sync(t)
			f.romm.otherDevice(1, "snes9x", []byte("newer progress"))
			f.writeSave(t, "old progress", time.Now().Add(time.Hour))

			if s := f.sync(t); s.Downloaded != 1 || s.Conflicts != 0 {
				t.Fatalf("summary %+v", s)
			}
			if got := readFile(t, f.chronoSave()); got != "newer progress" {
				t.Fatalf("save holds %q", got)
			}
			backups, _ := os.ReadDir(filepath.Join(f.backups, "retroarch", "1"))
			if len(backups) != 1 || readFile(t, filepath.Join(f.backups, "retroarch", "1", backups[0].Name())) != "old progress" {
				t.Fatalf("backups %v", backups)
			}
		})
	}
}

func TestBothChangedIsAConflict(t *testing.T) {
	f := newSaveFixture(t)
	f.writeSave(t, "shared start", time.Now().Add(-time.Hour))
	f.sync(t)
	f.romm.otherDevice(1, "snes9x", []byte("handheld went on"))
	f.writeSave(t, "deck went on", time.Now().Add(time.Hour))
	before := len(f.romm.versions)

	if s := f.sync(t); s.Conflicts != 1 || s.Uploaded+s.Downloaded != 0 {
		t.Fatalf("summary %+v", s)
	}
	if got := readFile(t, f.chronoSave()); got != "deck went on" {
		t.Fatalf("local save touched: %q", got)
	}
	if len(f.romm.versions) != before {
		t.Fatal("server save touched")
	}
	if st := f.status(t); st.State != domain.SaveConflict {
		t.Fatalf("status %+v", st)
	}
	// It stays a conflict until the user resolves it.
	if s := f.sync(t); s.Conflicts != 1 {
		t.Fatalf("second sync %+v", s)
	}
}

// What a sync learned outlives the backend: a save RetroArch only rewrote isn't uploaded again.
func TestBaselinesOutliveARestart(t *testing.T) {
	f := newSaveFixture(t)
	f.writeSave(t, "progress", time.Now().Add(-time.Hour))
	f.sync(t)
	f.saves = f.newSaves()
	f.writeSave(t, "progress", time.Now().Add(time.Hour))

	if s := f.sync(t); s.Uploaded+s.Downloaded+s.Conflicts != 0 {
		t.Fatalf("summary %+v", s)
	}
	if f.romm.uploads != 1 {
		t.Fatalf("%d uploads", f.romm.uploads)
	}
	req := f.romm.requests[len(f.romm.requests)-1]
	if head, _ := f.romm.head(1); !req.Saves[0].UpdatedAt.Equal(head.UpdatedAt) {
		t.Fatalf("an unchanged save reported %v, synced at %v", req.Saves[0].UpdatedAt, head.UpdatedAt)
	}
}

// A changed local save after a sync uploads as the slot's new head.
func TestChangedSaveUploadsAgain(t *testing.T) {
	f := newSaveFixture(t)
	f.writeSave(t, "level 1", time.Now().Add(-time.Hour))
	f.sync(t)
	f.writeSave(t, "level 2", time.Now().Add(time.Hour))

	if s := f.sync(t); s.Uploaded != 1 {
		t.Fatalf("summary %+v", s)
	}
	if head, _ := f.romm.head(1); string(head.data) != "level 2" {
		t.Fatalf("head %q", head.data)
	}
}

func TestDownloadWaitsForRetroArchToClose(t *testing.T) {
	f := newSaveFixture(t)
	f.romm.otherDevice(1, "snes9x", []byte("handheld progress"))
	f.retroarch.Store(true)

	if s := f.sync(t); s.Waiting != 1 || s.Downloaded != 0 {
		t.Fatalf("summary %+v", s)
	}
	gone(t, f.chronoSave())
	if st := f.status(t); st.State != domain.SaveWaiting {
		t.Fatalf("status %+v", st)
	}

	f.retroarch.Store(false)
	if s := f.sync(t); s.Downloaded != 1 {
		t.Fatalf("after closing %+v", s)
	}
}

func TestSaveFromAnotherEmulatorIsNotDownloaded(t *testing.T) {
	f := newSaveFixture(t)
	f.romm.otherDevice(1, "duckstation", []byte("a memory card"))

	if s := f.sync(t); s.Failed != 1 || s.Downloaded != 0 {
		t.Fatalf("summary %+v", s)
	}
	gone(t, f.chronoSave())
	if st := f.status(t); st.State != domain.SaveFailed || st.Problem == "" {
		t.Fatalf("status %+v", st)
	}
}

func TestSavesOutsideRetroArchDontSync(t *testing.T) {
	cases := map[string]fakeLaunchers{
		"a standalone emulator":   {"snes": {ESDECommand: domain.ESDECommand{Label: "Snes9x (Standalone)", Command: "%EMULATOR_SNES9X% %ROM%"}, Known: true}},
		"a standalone label only": {"snes": {ESDECommand: domain.ESDECommand{Label: "bsnes (Standalone)"}}},
	}
	for name, launchers := range cases {
		t.Run(name, func(t *testing.T) {
			f := newSaveFixture(t)
			f.launchers = launchers
			f.saves = f.newSaves()
			if st := f.status(t); st.Syncs || st.Why == "" {
				t.Fatalf("status %+v", st)
			}
			if err := f.saves.Start(context.Background(), []int{1}); kindOf(err) != domain.KindInvalidInput {
				t.Fatalf("start: %v", err)
			}
		})
	}
}

// Without ES-DE's files, EmuDeck's default core for the system stands in.
func TestDefaultCoreWithoutESDE(t *testing.T) {
	f := newSaveFixture(t)
	f.launchers = fakeLaunchers{}
	f.saves = f.newSaves()
	if st := f.status(t); !st.Syncs || st.Emulator != "snes9x" {
		t.Fatalf("status %+v", st)
	}
}

func TestSyncNeedsADevice(t *testing.T) {
	f := newSaveFixture(t)
	f.settings.mu.Lock()
	f.settings.in.DeviceID = ""
	f.settings.mu.Unlock()
	if _, err := f.saves.Sync(context.Background(), nil); kindOf(err) != domain.KindMissingPermission {
		t.Fatalf("sync: %v", err)
	}
}

func TestOfflineSyncLeavesSavesAlone(t *testing.T) {
	f := newSaveFixture(t)
	f.writeSave(t, "progress", time.Now())
	f.romm.failWith = &domain.Error{Kind: domain.KindUnreachable, Message: "offline"}
	if _, err := f.saves.Sync(context.Background(), nil); kindOf(err) != domain.KindUnreachable {
		t.Fatalf("sync: %v", err)
	}
	if st := f.status(t); st.State != "" {
		t.Fatalf("status %+v", st)
	}
}

func TestStartReportsTheSync(t *testing.T) {
	f := newSaveFixture(t)
	f.writeSave(t, "progress", time.Now())
	if err := f.saves.Start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if e := <-f.events; !e.Syncing {
		t.Fatalf("first event %+v", e)
	}
	select {
	case e := <-f.events:
		if e.Syncing || e.Problem != nil || e.Summary.Uploaded != 1 {
			t.Fatalf("end %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("sync didn't end")
	}
}

func TestGameEntry(t *testing.T) {
	dir := filepath.FromSlash("/e/roms/psx")
	placed := func(names ...string) []domain.PlacedFile {
		var out []domain.PlacedFile
		for _, n := range names {
			out = append(out, domain.PlacedFile{Path: filepath.Join(dir, filepath.FromSlash(n))})
		}
		return out
	}
	cases := map[string]struct {
		files []domain.PlacedFile
		want  string
	}{
		"one file":           {placed("Ridge Racer.chd"), "Ridge Racer.chd"},
		"a folder":           {placed("Doom/doom.wad", "Doom/doom.exe"), "Doom"},
		"a cue and its bins": {placed("FF7.cue", "FF7 (Track 1).bin", "FF7 (Track 2).bin"), "FF7.cue"},
		"a playlist":         {placed("FF7.m3u", "FF7 (Disc 1).chd", "FF7 (Disc 2).chd"), "FF7.m3u"},
		"no telling":         {placed("a.bin", "b.bin"), ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, why := gameEntry(c.files, dir)
			want := ""
			if c.want != "" {
				want = filepath.Join(dir, c.want)
			}
			if got != want || (want == "") != (why != "") {
				t.Fatalf("got %q, %q", got, why)
			}
		})
	}
}
