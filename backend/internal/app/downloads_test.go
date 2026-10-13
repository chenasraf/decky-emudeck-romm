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
	"sync"
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

// memOwnership is an OwnershipStore in memory.
type memOwnership struct {
	mu      sync.Mutex
	roms    map[int]domain.OwnedROM
	saveErr error
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

// fakeContent is a RomM server's files. A file with a gate sends half its bytes, then waits for
// the gate to close.
type fakeContent struct {
	mu      sync.Mutex
	files   map[int][]domain.ROMFile
	content map[int][]byte
	gates   map[int]chan struct{}
	// started receives a file id each time its download begins.
	started chan int
	err     error
}

func (c *fakeContent) ROMFiles(_ context.Context, id int) ([]domain.ROMFile, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return nil, c.err
	}
	return c.files[id], nil
}

func (c *fakeContent) DownloadFile(ctx context.Context, id int, _ string, w io.Writer) error {
	c.mu.Lock()
	data, gate := c.content[id], c.gates[id]
	c.mu.Unlock()
	c.started <- id
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
	_, err := w.Write(data)
	return err
}

type downloadFixture struct {
	downloads *Downloads
	owned     *memOwnership
	server    *fakeContent
	settings  *fakeSettings
	updates   chan DownloadUpdate
	deletes   chan DeleteUpdate
	root      string
	snes      string
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
			content: map[int][]byte{10: chrono, 20: metroid},
			gates:   map[int]chan struct{}{},
			started: make(chan int, 16),
		},
		settings: &fakeSettings{in: &domain.SignIn{Server: domain.Server{URL: "https://romm.lan"}, Token: "rmm_x"}},
		updates:  make(chan DownloadUpdate, 256),
		deletes:  make(chan DeleteUpdate, 256),
		root:     root,
		snes:     filepath.Join(root, "roms", "snes"),
	}

	catalog := &memStore{}
	if err := catalog.Apply(context.Background(), domain.CatalogSync{
		Platforms: []domain.Platform{{ID: 1, Slug: "snes", Name: "Super Nintendo"}, {ID: 2, Slug: "unknown-console", Name: "Mystery"}},
		ROMs: []domain.ROM{
			{ID: 1, PlatformID: 1, Name: "Chrono Trigger", SizeBytes: int64(len(chrono))},
			{ID: 2, PlatformID: 1, Name: "Super Metroid", SizeBytes: int64(len(metroid))},
			{ID: 3, PlatformID: 1, Name: "Disc Game", MultiFile: true},
			{ID: 4, PlatformID: 2, Name: "Mystery Game"},
		},
		Full: true,
	}); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	install := NewInstall(fixedProber{domain.InstallState{
		EmuDeck: domain.EmuDeckState{Root: root, ROMsPath: filepath.Join(root, "roms")},
		Systems: []domain.SystemState{{Folder: "snes", ROMPath: f.snes}},
	}}, log)
	conn, _ := newConnectionWithEvents(t, f.settings, &fakeNet{})
	dial := func(domain.Server, string) DownloadServer { return f.server }
	f.downloads = NewDownloads(t.Context(), catalog, install, conn, dial, f.owned, nil, limit,
		func(u DownloadUpdate) { f.updates <- u }, func(u DeleteUpdate) { f.deletes <- u }, log)
	return f
}

// end waits for the download of id to end and returns its last update.
func (f *downloadFixture) end(t *testing.T, id int) DownloadUpdate {
	t.Helper()
	for {
		select {
		case u := <-f.updates:
			if u.ROMID == id && (u.State == domain.StateInstalled || u.State == domain.StateOnRomM) {
				return u
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("download %d didn't end", id)
			return DownloadUpdate{}
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
	if u := f.end(t, 1); u.State != domain.StateOnRomM || kindOf(u.Problem) != domain.KindTargetExists {
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
		nil, problem, 2, func(DownloadUpdate) {}, func(DeleteUpdate) {}, f.downloads.log)

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
		if u := f.end(t, 1); kindOf(u.Problem) != domain.KindUnreachable {
			t.Fatalf("end: %+v", u)
		}
		if entries, _ := os.ReadDir(f.snes); len(entries) != 0 {
			t.Fatalf("placed a short file: %v", entries)
		}
		f.stagingEmpty(t)
	})

	t.Run("record not saved", func(t *testing.T) {
		f := newDownloadFixture(t, 2)
		f.owned.saveErr = errors.New("disk I/O error")
		if _, err := f.downloads.Queue(context.Background(), 1); err != nil {
			t.Fatal(err)
		}
		if u := f.end(t, 1); u.State != domain.StateOnRomM || u.Problem == nil {
			t.Fatalf("end: %+v", u)
		}
		// An unrecorded file would be User-managed forever, so the plugin takes it back out.
		if entries, _ := os.ReadDir(f.snes); len(entries) != 0 {
			t.Fatalf("left an unrecorded file: %v", entries)
		}
	})

	t.Run("token rejected", func(t *testing.T) {
		f := newDownloadFixture(t, 2)
		f.server.err = &domain.Error{Kind: domain.KindSignedOut, Message: "signed out"}
		if _, err := f.downloads.Queue(context.Background(), 1); err != nil {
			t.Fatal(err)
		}
		if u := f.end(t, 1); kindOf(u.Problem) != domain.KindSignedOut {
			t.Fatalf("end: %+v", u)
		}
		if !f.settings.in.SignedOut {
			t.Fatal("a 401 during a download didn't sign out")
		}
	})
}

func TestCleanStaging(t *testing.T) {
	f := newDownloadFixture(t, 2)
	leftover := filepath.Join(f.root, stagingDir, "7", "half.sfc")
	if err := os.MkdirAll(filepath.Dir(leftover), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(leftover, []byte("half"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.downloads.CleanStaging()
	if _, err := os.Stat(filepath.Join(f.root, stagingDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging still there: %v", err)
	}
}
