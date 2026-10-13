package app

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // matches the code under test
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

type fixedProber struct{ state domain.InstallState }

func (p fixedProber) Probe() domain.InstallState {
	s := p.state
	s.CheckedAt = time.Now()
	return s
}

// memPrefs keeps Keep archives toggles, unset is on, and Install to folders in memory.
type memPrefs struct {
	mu        sync.Mutex
	keep      map[string]bool
	installTo map[string]string
}

func (m *memPrefs) InstallTo(system string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.installTo[system]
}

func (m *memPrefs) SetInstallTo(system, folder string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.installTo == nil {
		m.installTo = map[string]string{}
	}
	m.installTo[system] = folder
	return nil
}

func (m *memPrefs) KeepArchives(folder string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	keep, ok := m.keep[folder]
	return keep || !ok
}

func (m *memPrefs) SetKeepArchives(folder string, keep bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keep[folder] = keep
	return nil
}

// memGamePrefs keeps the Wait for games to close setting in memory.
type memGamePrefs struct{ wait atomic.Bool }

func (m *memGamePrefs) WaitForGames() bool { return m.wait.Load() }

func (m *memGamePrefs) SetWaitForGames(on bool) error {
	m.wait.Store(on)
	return nil
}

// memOwnership is a DownloadStore in memory.
type memOwnership struct {
	mu      sync.Mutex
	roms    map[int]domain.OwnedROM
	links   map[int]domain.LinkedGame
	queue   []domain.QueuedDownload
	saveErr error
}

func (m *memOwnership) SaveLinked(_ context.Context, g domain.LinkedGame) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.links == nil {
		m.links = map[int]domain.LinkedGame{}
	}
	m.links[g.ROMID] = g
	return nil
}

func (m *memOwnership) Linked(_ context.Context, id int) (domain.LinkedGame, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.links[id]
	return g, ok, nil
}

func (m *memOwnership) DeleteLinked(_ context.Context, id int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.links, id)
	return nil
}

func (m *memOwnership) SaveQueued(_ context.Context, d domain.QueuedDownload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, q := range m.queue {
		if q.ROMID == d.ROMID {
			m.queue[i] = d
			return nil
		}
	}
	m.queue = append(m.queue, d)
	return nil
}

func (m *memOwnership) DeleteQueued(_ context.Context, id int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queue = slices.DeleteFunc(m.queue, func(q domain.QueuedDownload) bool { return q.ROMID == id })
	return nil
}

func (m *memOwnership) Queued(context.Context) ([]domain.QueuedDownload, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.queue), nil
}

func (m *memOwnership) SaveOwned(_ context.Context, rom domain.OwnedROM) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveErr != nil {
		return m.saveErr
	}
	m.roms[rom.ROMID] = rom
	return nil
}

func (m *memOwnership) Owned(_ context.Context, id int) (domain.OwnedROM, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.roms[id]
	return r, ok, nil
}

func (m *memOwnership) DeleteOwned(_ context.Context, id int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.roms, id)
	return nil
}

func (m *memOwnership) OwnedIDs(context.Context) ([]int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var ids []int
	for id := range m.roms {
		ids = append(ids, id)
	}
	return ids, nil
}

// fakeContent is a RomM server's files, which it resumes from any offset. A file with a gate sends
// half of what's asked for, then waits for the gate to close. A file with drops left sends half of
// what's asked for, then cuts the connection.
type fakeContent struct {
	mu      sync.Mutex
	files   map[int][]domain.ROMFile
	content map[int][]byte
	gates   map[int]chan struct{}
	drops   map[int]int
	// full fills the disk partway through each download of the file ids it holds, calling onFull.
	full   map[int]bool
	onFull func()
	// started receives a file id each time its download begins, and froms the offset it asked for.
	started chan int
	froms   []int64
	// converts are the conversions the server makes, by file id; formats and convertedFroms record
	// what it was asked.
	converts       map[int]conversion
	formats        []string
	convertedFroms []int64
	err            error
}

func (c *fakeContent) ROMFiles(_ context.Context, id int) ([]domain.ROMFile, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return nil, c.err
	}
	return c.files[id], nil
}

func (c *fakeContent) DownloadFile(ctx context.Context, id int, _ string, from int64, open func(int64) (io.Writer, error)) error {
	c.mu.Lock()
	data, gate := c.content[id], c.gates[id]
	drop := c.drops[id] > 0
	full := c.full[id]
	if drop {
		c.drops[id]--
	}
	c.froms = append(c.froms, from)
	c.mu.Unlock()
	c.started <- id
	w, err := open(from)
	if err != nil {
		return err
	}
	data = data[min(from, int64(len(data))):]
	if full {
		if _, err := w.Write(data[:len(data)/2]); err != nil {
			return err
		}
		c.onFull()
		return &os.PathError{Op: "write", Path: "staging", Err: syscall.ENOSPC}
	}
	if drop {
		if _, err := w.Write(data[:len(data)/2]); err != nil {
			return err
		}
		return &domain.Error{Kind: domain.KindUnreachable, Message: "The download from RomM was interrupted."}
	}
	if gate != nil {
		if _, err := w.Write(data[:len(data)/2]); err != nil {
			return err
		}
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
		data = data[len(data)/2:]
	}
	_, err = w.Write(data)
	return err
}

// conversion is what the fake server does when asked to convert a file: serve data as name, or
// refuse.
type conversion struct {
	name   string
	data   []byte
	refuse bool
}

// Convert answers from c.converts; a file with none is served as stored, the way a server that
// predates conversion answers.
func (c *fakeContent) Convert(_ context.Context, _, fileID int, name, format string) (domain.ServedFile, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.formats = append(c.formats, format)
	conv, ok := c.converts[fileID]
	switch {
	case !ok:
		return domain.ServedFile{Name: name, Size: int64(len(c.content[fileID]))}, nil
	case conv.refuse:
		return domain.ServedFile{}, domain.ErrNoConversion
	}
	return domain.ServedFile{Name: conv.name, Size: int64(len(conv.data))}, nil
}

func (c *fakeContent) DownloadConverted(_ context.Context, _, fileID int, _, _ string, from int64, open func(int64) (io.Writer, error)) error {
	c.mu.Lock()
	data := c.converts[fileID].data
	c.convertedFroms = append(c.convertedFroms, from)
	c.mu.Unlock()
	w, err := open(from)
	if err != nil {
		return err
	}
	_, err = w.Write(data[min(from, int64(len(data))):])
	return err
}

type downloadFixture struct {
	downloads *Downloads
	catalog   *memStore
	owned     *memOwnership
	server    *fakeContent
	settings  *fakeSettings
	// free is what the Emulation drive has free.
	free   atomic.Int64
	drives sync.Map
	holds  chan error
	prefs  *memPrefs
	games  *Games
	// playing is set while an emulator runs a game.
	playing atomic.Bool
	gaming  *memGamePrefs
	updates chan Download
	deletes chan DeleteUpdate
	// stop stops the backend the downloads run under.
	stop context.CancelFunc
	root string
	snes string
}

var (
	chrono  = []byte("chrono trigger rom bytes")
	metroid = []byte("super metroid rom bytes")
)

func newDownloadFixture(t *testing.T, limit int) *downloadFixture {
	t.Helper()
	root := t.TempDir()
	f := &downloadFixture{
		owned: &memOwnership{roms: map[int]domain.OwnedROM{}},
		server: &fakeContent{
			files: map[int][]domain.ROMFile{
				1: {{ID: 10, Name: "Chrono Trigger (USA).sfc", SizeBytes: int64(len(chrono)), Category: "game", SHA1: "server-sha1"},
					{ID: 11, Name: "Manual.pdf", SizeBytes: 5, Category: "manual"}},
				2: {{ID: 20, Name: "Super Metroid (USA).sfc", SizeBytes: int64(len(metroid)), Category: "game", MD5: "server-md5"}},
			},
			content:  map[int][]byte{10: chrono, 20: metroid},
			gates:    map[int]chan struct{}{},
			drops:    map[int]int{},
			full:     map[int]bool{},
			converts: map[int]conversion{},
			started:  make(chan int, 64),
		},
		settings: &fakeSettings{in: &domain.SignIn{Server: domain.Server{URL: "https://romm.lan"}, Token: "rmm_x"}},
		updates:  make(chan Download, 256),
		deletes:  make(chan DeleteUpdate, 256),
		root:     root,
		snes:     filepath.Join(root, "roms", "snes"),
	}

	catalog := &memStore{}
	f.catalog = catalog
	if err := catalog.Apply(context.Background(), domain.CatalogSync{
		Platforms: []domain.Platform{{ID: 1, Slug: "snes", Name: "Super Nintendo"}, {ID: 2, Slug: "unknown-console", Name: "Mystery"},
			{ID: 3, Slug: "psx", Name: "PlayStation"}, {ID: 4, Slug: "arcade", Name: "Arcade"},
			{ID: 5, Slug: "dc", Name: "Dreamcast"}, {ID: 6, Slug: "dos", Name: "DOS"}, {ID: 7, Slug: "ps3", Name: "PlayStation 3"},
			{ID: 8, Slug: "3ds", Name: "Nintendo 3DS"}},
		ROMs: []domain.ROM{
			{ID: 1, PlatformID: 1, Name: "Chrono Trigger", SizeBytes: int64(len(chrono))},
			{ID: 2, PlatformID: 1, Name: "Super Metroid", SizeBytes: int64(len(metroid))},
			{ID: 3, PlatformID: 1, Name: "Disc Game", MultiFile: true},
			{ID: 4, PlatformID: 2, Name: "Mystery Game"},
			{ID: 5, PlatformID: 3, Name: "Ridge Racer", FileName: "Ridge Racer.zip", SizeBytes: 1000},
			{ID: 6, PlatformID: 4, Name: "Street Fighter II", FileName: "sf2.zip"},
			{ID: 7, PlatformID: 1, Name: "Zipped", FileName: "Zipped.zip"},
			{ID: 8, PlatformID: 3, Name: "Final Fantasy VII", FileName: "Final Fantasy VII (USA)", MultiFile: true},
			{ID: 9, PlatformID: 5, Name: "Shenmue", FileName: "Shenmue (USA)", MultiFile: true},
			{ID: 10, PlatformID: 6, Name: "Doom", FileName: "Doom", MultiFile: true},
			{ID: 11, PlatformID: 7, Name: "Demon's Souls", FileName: "Demon's Souls", MultiFile: true},
			{ID: 12, PlatformID: 8, Name: "Zelda", FileName: "Zelda.cia"},
		},
		Full: true,
	}); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	install := NewInstall(fixedProber{domain.InstallState{
		EmuDeck: domain.EmuDeckState{Root: root, ROMsPath: filepath.Join(root, "roms")},
		Systems: []domain.SystemState{{Folder: "snes", ROMPath: f.snes}, {Folder: "psx", ROMPath: filepath.Join(root, "roms", "psx")},
			{Folder: "arcade", ROMPath: filepath.Join(root, "roms", "arcade")},
			{Folder: "dreamcast", ROMPath: filepath.Join(root, "roms", "dreamcast")},
			{Folder: "dos", ROMPath: filepath.Join(root, "roms", "dos")},
			{Folder: "ps3", ROMPath: filepath.Join(root, "roms", "ps3")},
			{Folder: "n3ds", ROMPath: filepath.Join(root, "roms", "n3ds")}},
	}}, log)
	conn, _ := newConnectionWithEvents(t, f.settings, &fakeNet{})
	dial := func(domain.Server, string) DownloadServer { return f.server }
	base, stop := context.WithCancel(t.Context())
	f.stop = stop
	f.free.Store(1 << 40)
	f.holds = make(chan error, 16)
	f.prefs = &memPrefs{keep: map[string]bool{}}
	f.gaming = &memGamePrefs{}
	f.gaming.wait.Store(true)
	f.games = NewGames(func() (domain.Emulator, bool) {
		if f.playing.Load() {
			return domain.EmulatorByID("pcsx2")
		}
		return domain.Emulator{}, false
	}, f.gaming, log)
	f.games.poll = time.Millisecond
	f.downloads = NewDownloads(base, catalog, install, conn, dial, f.owned, nil, f.measure, f.prefs, f.games, limit, f.listeners(), log)
	f.downloads.retries = testRetries
	f.downloads.spacePoll = time.Millisecond
	return f
}

// measure is what the drive at path has free: what drives says for a path on its list, the
// fixture's free otherwise.
func (f *downloadFixture) measure(path string) (int64, error) {
	if n, ok := f.drives.Load(path); ok {
		return n.(int64), nil
	}
	return f.free.Load(), nil
}

func (f *downloadFixture) listeners() DownloadListeners {
	return DownloadListeners{
		Change: func(u Download) { f.updates <- u },
		Delete: func(u DeleteUpdate) { f.deletes <- u },
		Hold:   func(err error) { f.holds <- err },
	}
}

// testRetries keep retries quick: three, a millisecond apart.
var testRetries = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}

// end waits for the download of id to end, installed, failed or out of the queue, and returns its
// last update.
func (f *downloadFixture) end(t *testing.T, id int) Download {
	t.Helper()
	for {
		select {
		case u := <-f.updates:
			if u.ROMID == id && (u.State == domain.StateInstalled || u.State == domain.StateOnRomM || u.State == domain.StateFailed) {
				return u
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("download %d didn't end", id)
			return Download{}
		}
	}
}

func (f *downloadFixture) started(t *testing.T) int {
	t.Helper()
	select {
	case id := <-f.server.started:
		return id
	case <-time.After(3 * time.Second):
		t.Fatal("no download started")
		return 0
	}
}

func (f *downloadFixture) stagingEmpty(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(f.root, stagingDir))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("staging left behind: %v", entries)
	}
}

func TestDownloadInstalls(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()

	d, err := f.downloads.Queue(ctx, 1)
	if err != nil || d.State != domain.StateQueued || d.Name != "Chrono Trigger" || d.PlatformName != "Super Nintendo" {
		t.Fatalf("queue: %+v, %v", d, err)
	}
	if u := f.end(t, 1); u.State != domain.StateInstalled || u.Problem != nil || u.Received != int64(len(chrono)) {
		t.Fatalf("end: %+v", u)
	}

	// The ROM folder didn't exist; the plugin made it with EmuDeck's name.
	target := filepath.Join(f.snes, "Chrono Trigger (USA).sfc")
	if data, err := os.ReadFile(target); err != nil || !bytes.Equal(data, chrono) {
		t.Fatalf("placed file: %q, %v", data, err)
	}
	rec, ok, _ := f.owned.Owned(ctx, 1)
	sum := sha1.Sum(chrono) //nolint:gosec // matches the code under test
	if !ok || rec.PlatformID != 1 || len(rec.Files) != 1 || rec.Files[0].Path != target ||
		rec.Files[0].SizeBytes != int64(len(chrono)) || rec.Files[0].SHA1 != hex.EncodeToString(sum[:]) {
		t.Fatalf("record %+v", rec)
	}
	// Only the game file counts, and the strongest hash RomM had is kept.
	if len(rec.Snapshot) != 1 || rec.Snapshot[0] != (domain.FileSnapshot{FileID: 10, SizeBytes: int64(len(chrono)), HashKind: "sha1", Hash: "server-sha1"}) {
		t.Fatalf("snapshot %+v", rec.Snapshot)
	}
	f.stagingEmpty(t)

	if states, _ := f.downloads.States(ctx); states[1] != domain.StateInstalled {
		t.Fatalf("states %v", states)
	}
	if _, err := f.downloads.Queue(ctx, 1); kindOf(err) != domain.KindInvalidInput {
		t.Fatalf("queueing an installed game: %v", err)
	}
}

func TestDownloadNeverOverwrites(t *testing.T) {
	f := newDownloadFixture(t, 2)
	if err := os.MkdirAll(f.snes, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(f.snes, "Chrono Trigger (USA).sfc")
	if err := os.WriteFile(target, []byte("the user's own dump"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := f.downloads.Queue(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if u := f.end(t, 1); u.State != domain.StateFailed || kindOf(u.Problem) != domain.KindTargetExists {
		t.Fatalf("end: %+v", u)
	}
	if data, _ := os.ReadFile(target); string(data) != "the user's own dump" {
		t.Fatalf("file replaced: %q", data)
	}
	if _, ok, _ := f.owned.Owned(context.Background(), 1); ok {
		t.Fatal("recorded a file the plugin didn't place")
	}
}

func TestDownloadCancel(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.server.gates[10] = make(chan struct{})

	if _, err := f.downloads.Queue(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	f.started(t)
	// Half the file is in staging now; none of it is in roms/.
	if _, err := os.Stat(filepath.Join(f.snes, "Chrono Trigger (USA).sfc")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial file in roms/: %v", err)
	}
	if !f.downloads.Cancel(1) {
		t.Fatal("cancel found no download")
	}
	if u := f.end(t, 1); u.State != domain.StateOnRomM || u.Problem != nil {
		t.Fatalf("end: %+v", u)
	}
	f.stagingEmpty(t)
	if len(f.downloads.List()) != 0 {
		t.Fatalf("still listed: %+v", f.downloads.List())
	}
	if f.downloads.Cancel(1) {
		t.Fatal("cancelled twice")
	}
}

func TestDownloadQueueRespectsLimit(t *testing.T) {
	f := newDownloadFixture(t, 1)
	gate := make(chan struct{})
	f.server.gates[10] = gate
	ctx := context.Background()

	for _, id := range []int{1, 2} {
		if _, err := f.downloads.Queue(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if id := f.started(t); id != 10 {
		t.Fatalf("started file %d first", id)
	}
	list := f.downloads.List()
	if len(list) != 2 || list[0].State != domain.StateDownloading || list[1].State != domain.StateQueued {
		t.Fatalf("list %+v", list)
	}

	// Cancelling a queued game takes it out without it ever starting.
	f.downloads.Cancel(2)
	if u := f.end(t, 2); u.State != domain.StateOnRomM {
		t.Fatalf("queued cancel: %+v", u)
	}
	if _, err := f.downloads.Queue(ctx, 2); err != nil {
		t.Fatal(err)
	}
	close(gate)
	f.end(t, 1)
	if id := f.started(t); id != 20 {
		t.Fatalf("then started file %d", id)
	}
	f.end(t, 2)
	if ids, _ := f.owned.OwnedIDs(ctx); len(ids) != 2 {
		t.Fatalf("owned %v", ids)
	}
}

func TestDownloadRefusals(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()

	for id, kind := range map[int]domain.Kind{
		3:  domain.KindCantDownload, // several files
		4:  domain.KindCantDownload, // no EmuDeck folder for the platform
		99: domain.KindNotFound,
	} {
		if _, err := f.downloads.Queue(ctx, id); kindOf(err) != kind {
			t.Errorf("game %d: want %s, got %v", id, kind, err)
		}
	}

	f.settings.in.SignedOut = true
	if _, err := f.downloads.Queue(ctx, 1); kindOf(err) != domain.KindSignedOut {
		t.Fatalf("signed out: %v", err)
	}
}

func TestDownloadWithoutStateDB(t *testing.T) {
	f := newDownloadFixture(t, 2)
	problem := &domain.Error{Kind: domain.KindNewerData, Message: "newer"}
	d := NewDownloads(t.Context(), f.downloads.catalog, f.downloads.install, f.downloads.conn, f.downloads.dial,
		nil, problem, f.measure, f.prefs, f.games, 2, DownloadListeners{}, f.downloads.log)

	if _, err := d.Queue(context.Background(), 1); kindOf(err) != domain.KindNewerData {
		t.Fatalf("queue: %v", err)
	}
	if _, err := d.Delete(context.Background(), []int{1}, 0); kindOf(err) != domain.KindNewerData {
		t.Fatalf("delete: %v", err)
	}
	if states, err := d.States(context.Background()); err != nil || len(states) != 0 {
		t.Fatalf("states %v, %v", states, err)
	}
	if !errors.Is(d.Problem(), problem) {
		t.Fatalf("problem %v", d.Problem())
	}
}

func TestDownloadFailures(t *testing.T) {
	t.Run("wrong size", func(t *testing.T) {
		f := newDownloadFixture(t, 2)
		f.server.content[10] = chrono[:5]
		if _, err := f.downloads.Queue(context.Background(), 1); err != nil {
			t.Fatal(err)
		}
		if u := f.end(t, 1); u.State != domain.StateFailed || kindOf(u.Problem) != domain.KindUnreachable {
			t.Fatalf("end: %+v", u)
		}
		if entries, _ := os.ReadDir(f.snes); len(entries) != 0 {
			t.Fatalf("placed a short file: %v", entries)
		}
		// The first attempt and every retry.
		if n := len(f.server.froms); n != 1+len(testRetries) {
			t.Fatalf("%d attempts", n)
		}
	})

	t.Run("record not saved", func(t *testing.T) {
		f := newDownloadFixture(t, 2)
		f.owned.saveErr = errors.New("disk I/O error")
		if _, err := f.downloads.Queue(context.Background(), 1); err != nil {
			t.Fatal(err)
		}
		if u := f.end(t, 1); u.State != domain.StateFailed || u.Problem == nil {
			t.Fatalf("end: %+v", u)
		}
		// An unrecorded file would be User-managed forever, so the plugin takes it back out.
		if entries, _ := os.ReadDir(f.snes); len(entries) != 0 {
			t.Fatalf("left an unrecorded file: %v", entries)
		}
		// Only network errors are retried.
		if n := len(f.server.froms); n != 1 {
			t.Fatalf("%d attempts", n)
		}
	})
}

func TestDownloadWaitsForSignIn(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.server.mu.Lock()
	f.server.err = &domain.Error{Kind: domain.KindSignedOut, Message: "signed out"}
	f.server.mu.Unlock()
	if _, err := f.downloads.Queue(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for in, _ := f.settings.SignIn(); !in.SignedOut; in, _ = f.settings.SignIn() {
		if time.Now().After(deadline) {
			t.Fatal("a 401 during a download didn't sign out")
		}
		time.Sleep(time.Millisecond)
	}
	// Signed out pauses the download rather than failing it.
	if list := f.downloads.List(); len(list) != 1 || list[0].State == domain.StateFailed {
		t.Fatalf("list %+v", list)
	}

	f.server.mu.Lock()
	f.server.err = nil
	f.server.mu.Unlock()
	if err := f.downloads.conn.saveSignIn(domain.SignIn{Server: domain.Server{URL: "https://romm.lan"}, Token: "rmm_y"}); err != nil {
		t.Fatal(err)
	}
	if u := f.end(t, 1); u.State != domain.StateInstalled {
		t.Fatalf("end: %+v", u)
	}
}

func TestDownloadRetriesDroppedConnection(t *testing.T) {
	f := newDownloadFixture(t, 2)
	// More drops than retries: each drop gets further, which starts the count over.
	f.server.drops[10] = len(testRetries) + 2
	if _, err := f.downloads.Queue(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if u := f.end(t, 1); u.State != domain.StateInstalled || u.Received != int64(len(chrono)) {
		t.Fatalf("end: %+v", u)
	}
	// Each retry resumed where the last attempt stopped.
	froms := f.server.froms
	for i := 1; i < len(froms); i++ {
		if froms[i] <= froms[i-1] {
			t.Fatalf("froms %v", froms)
		}
	}
	if data, err := os.ReadFile(filepath.Join(f.snes, "Chrono Trigger (USA).sfc")); err != nil || !bytes.Equal(data, chrono) {
		t.Fatalf("placed file: %q, %v", data, err)
	}
	rec, _, _ := f.owned.Owned(context.Background(), 1)
	if sum := sha1.Sum(chrono); rec.Files[0].SHA1 != hex.EncodeToString(sum[:]) { //nolint:gosec // matches the code under test
		t.Fatalf("hash of a resumed file %s", rec.Files[0].SHA1)
	}
	f.stagingEmpty(t)
	if q, _ := f.owned.Queued(context.Background()); len(q) != 0 {
		t.Fatalf("still in the saved queue: %+v", q)
	}
}

func TestDownloadFailedThenRetried(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	// The link stays down: the first attempt gets halfway, and the retries get no further.
	f.server.content[10] = chrono[:len(chrono)/2]
	if _, err := f.downloads.Queue(ctx, 1); err != nil {
		t.Fatal(err)
	}
	u := f.end(t, 1)
	if u.State != domain.StateFailed || kindOf(u.Problem) != domain.KindUnreachable {
		t.Fatalf("end: %+v", u)
	}
	// Failed stays listed, and in state.db, with its error.
	if list := f.downloads.List(); len(list) != 1 || list[0].State != domain.StateFailed || list[0].Problem == nil {
		t.Fatalf("list %+v", list)
	}
	if states, _ := f.downloads.States(ctx); states[1] != domain.StateFailed {
		t.Fatalf("states %v", states)
	}
	if q, _ := f.owned.Queued(ctx); len(q) != 1 || q[0].Problem == nil || q[0].Problem.Kind != domain.KindUnreachable {
		t.Fatalf("saved queue %+v", q)
	}

	// Retry resumes from the partial once the server has the rest again.
	f.server.mu.Lock()
	f.server.content[10] = chrono
	f.server.froms = nil
	f.server.mu.Unlock()
	if d, err := f.downloads.Queue(ctx, 1); err != nil || d.State != domain.StateQueued {
		t.Fatalf("retry: %+v, %v", d, err)
	}
	if u := f.end(t, 1); u.State != domain.StateInstalled {
		t.Fatalf("end: %+v", u)
	}
	if froms := f.server.froms; len(froms) != 1 || froms[0] != int64(len(chrono)/2) {
		t.Fatalf("retry asked from %v", froms)
	}
	if data, _ := os.ReadFile(filepath.Join(f.snes, "Chrono Trigger (USA).sfc")); !bytes.Equal(data, chrono) {
		t.Fatalf("placed file %q", data)
	}
}

func TestDismissFailedDeletesPartial(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.server.content[10] = chrono[:len(chrono)/2]
	if _, err := f.downloads.Queue(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	f.end(t, 1)
	if !f.downloads.Cancel(1) {
		t.Fatal("dismiss found no download")
	}
	if u := f.end(t, 1); u.State != domain.StateOnRomM || u.Problem != nil {
		t.Fatalf("end: %+v", u)
	}
	f.stagingEmpty(t)
	if q, _ := f.owned.Queued(context.Background()); len(q) != 0 {
		t.Fatalf("still in the saved queue: %+v", q)
	}
}

// restart makes a second Downloads over the same state.db, Emulation folder and server, the way
// the backend comes back up.
func (f *downloadFixture) restart(t *testing.T) *Downloads {
	t.Helper()
	old := f.downloads
	d := NewDownloads(t.Context(), old.catalog, old.install, old.conn, old.dial, f.owned, nil, f.measure, f.prefs, f.games, 2, f.listeners(), old.log)
	d.retries = testRetries
	f.downloads = d
	return d
}

func TestResumeAfterRestart(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	f.server.gates[10] = make(chan struct{})
	if _, err := f.downloads.Queue(ctx, 1); err != nil {
		t.Fatal(err)
	}
	f.started(t)
	// A failed download comes back failed.
	if err := f.owned.SaveQueued(ctx, domain.QueuedDownload{ROMID: 2, PlatformID: 1, Name: "Super Metroid",
		Problem: &domain.Error{Kind: domain.KindUnreachable, Message: "interrupted"}}); err != nil {
		t.Fatal(err)
	}
	// What a cancelled download left in staging, and a game recorded just before the backend stopped.
	orphan := filepath.Join(f.root, stagingDir, "7", "half.sfc")
	if err := os.MkdirAll(filepath.Dir(orphan), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphan, []byte("half"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.owned.SaveQueued(ctx, domain.QueuedDownload{ROMID: 5, PlatformID: 1, Name: "Done"}); err != nil {
		t.Fatal(err)
	}
	if err := f.owned.SaveOwned(ctx, domain.OwnedROM{ROMID: 5, PlatformID: 1}); err != nil {
		t.Fatal(err)
	}

	// The backend stops mid-download, with half of Chrono Trigger in staging. Nothing reports
	// that the download stopped, so give it a moment to do anything it shouldn't.
	f.stop()
	time.Sleep(20 * time.Millisecond)
	if q, _ := f.owned.Queued(ctx); len(q) != 3 {
		t.Fatalf("stopping changed the saved queue: %+v", q)
	}

	d := f.restart(t)
	f.server.mu.Lock()
	delete(f.server.gates, 10)
	f.server.froms = nil
	f.server.mu.Unlock()
	d.Resume(ctx)

	if u := f.end(t, 1); u.State != domain.StateInstalled {
		t.Fatalf("end: %+v", u)
	}
	if froms := f.server.froms; len(froms) != 1 || froms[0] != int64(len(chrono)/2) {
		t.Fatalf("resumed from %v", froms)
	}
	list := d.List()
	if len(list) != 1 || list[0].ROMID != 2 || list[0].State != domain.StateFailed || list[0].Problem == nil {
		t.Fatalf("list %+v", list)
	}
	if _, err := os.Stat(filepath.Dir(orphan)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphaned staging kept: %v", err)
	}
	if q, _ := f.owned.Queued(ctx); len(q) != 1 || q[0].ROMID != 2 {
		t.Fatalf("saved queue %+v", q)
	}
}

func TestQueueChecksSpace(t *testing.T) {
	f := newDownloadFixture(t, 1)
	ctx := context.Background()
	// Room for Chrono Trigger and the margin, but not Super Metroid on top.
	f.free.Store(int64(len(chrono)) + spaceMargin)
	f.server.gates[10] = make(chan struct{})
	if _, err := f.downloads.Queue(ctx, 1); err != nil {
		t.Fatal(err)
	}
	f.started(t)

	_, err := f.downloads.Queue(ctx, 2)
	var de *domain.Error
	if !errors.As(err, &de) || de.Kind != domain.KindNoSpace {
		t.Fatalf("want no_space, got %v", err)
	}
	// The download ahead of it counts: half of Chrono Trigger is still to come.
	if want := "Needs 256 MB with the downloads ahead of it, 256 MB free in " + f.root + "."; de.Message != want {
		t.Fatalf("message %q, want %q", de.Message, want)
	}
	if len(f.downloads.List()) != 1 {
		t.Fatalf("queued anyway: %+v", f.downloads.List())
	}
	close(f.server.gates[10])
	f.end(t, 1)

	f.free.Store(10)
	if _, err := f.downloads.Queue(ctx, 2); kindOf(err) != domain.KindNoSpace || !strings.HasPrefix(err.Error(), "Needs ") {
		t.Fatalf("alone: %v", err)
	}
}

func TestOutOfSpaceHoldsQueue(t *testing.T) {
	f := newDownloadFixture(t, 1)
	ctx := context.Background()
	// The disk fills, and stays full until the user makes room.
	f.server.full[10] = true
	f.server.onFull = func() { f.free.Store(0) }
	for _, id := range []int{1, 2} {
		if _, err := f.downloads.Queue(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	u := f.end(t, 1)
	if u.State != domain.StateFailed || kindOf(u.Problem) != domain.KindNoSpace {
		t.Fatalf("end: %+v", u)
	}
	// Running out of space isn't retried.
	if n := len(f.server.froms); n != 1 {
		t.Fatalf("%d attempts", n)
	}
	select {
	case err := <-f.holds:
		if kindOf(err) != domain.KindNoSpace {
			t.Fatalf("hold %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("queue didn't go on hold")
	}
	if kindOf(f.downloads.Held()) != domain.KindNoSpace {
		t.Fatalf("held %v", f.downloads.Held())
	}
	// Super Metroid waits rather than failing too.
	time.Sleep(20 * time.Millisecond)
	if list := f.downloads.List(); len(list) != 2 || list[1].State != domain.StateQueued {
		t.Fatalf("list %+v", list)
	}

	f.free.Store(1 << 40)
	select {
	case err := <-f.holds:
		if err != nil {
			t.Fatalf("release %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("queue stayed on hold")
	}
	if u := f.end(t, 2); u.State != domain.StateInstalled {
		t.Fatalf("end: %+v", u)
	}
	if f.downloads.Held() != nil {
		t.Fatal("still held")
	}
}
