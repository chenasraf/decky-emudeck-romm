package app

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // matches the code under test
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// fakeFirmware is a RomM server's firmware files, by platform id, with their content by firmware
// id.
type fakeFirmware struct {
	mu       sync.Mutex
	files    map[int][]domain.Firmware
	content  map[int][]byte
	fetched  []int
	listings int
}

func (s *fakeFirmware) Firmware(_ context.Context, platformID int) ([]domain.Firmware, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listings++
	return s.files[platformID], nil
}

func (s *fakeFirmware) DownloadFirmware(_ context.Context, id int, _ string, open func(int64) (io.Writer, error)) error {
	s.mu.Lock()
	data := s.content[id]
	s.fetched = append(s.fetched, id)
	s.mu.Unlock()
	w, err := open(0)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func (s *fakeFirmware) fetches() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.fetched...)
}

// memBIOS is a BIOSStore in memory.
type memBIOS struct {
	mu    sync.Mutex
	owned map[int]domain.OwnedBIOS
}

func (m *memBIOS) SaveOwnedBIOS(_ context.Context, b domain.OwnedBIOS) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.owned[b.FirmwareID] = b
	return nil
}

func (m *memBIOS) OwnedBIOS(_ context.Context, platformID int) ([]domain.OwnedBIOS, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.OwnedBIOS
	for _, b := range m.owned {
		if b.PlatformID == platformID {
			out = append(out, b)
		}
	}
	return out, nil
}

func (m *memBIOS) DeleteOwnedBIOS(_ context.Context, id int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.owned, id)
	return nil
}

func (m *memBIOS) record(id int) (domain.OwnedBIOS, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.owned[id]
	return b, ok
}

type autoPref struct {
	mu sync.Mutex
	on bool
}

func (p *autoPref) AutoBIOS() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.on
}

func (p *autoPref) SetAutoBIOS(on bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.on = on
	return nil
}

type biosFixture struct {
	bios      *BIOS
	downloads *Downloads
	server    *fakeFirmware
	store     *memBIOS
	pref      *autoPref
	updates   chan BIOSUpdate
	biosDir   string
}

var (
	scph5501 = []byte("psx bios us")
	scph5502 = []byte("psx bios eu")
	dcBoot   = []byte("dreamcast boot rom")
)

func sha1Of(b []byte) string {
	sum := sha1.Sum(b) //nolint:gosec // matches the code under test
	return hex.EncodeToString(sum[:])
}

func newBIOSFixture(t *testing.T) *biosFixture {
	t.Helper()
	root := t.TempDir()
	f := &biosFixture{
		server: &fakeFirmware{
			files: map[int][]domain.Firmware{
				3: {{ID: 31, PlatformID: 3, FileName: "scph5501.bin", SizeBytes: int64(len(scph5501)), SHA1: sha1Of(scph5501)},
					{ID: 32, PlatformID: 3, FileName: "scph5502.bin", SizeBytes: int64(len(scph5502)), SHA1: sha1Of(scph5502)}},
				5: {{ID: 51, PlatformID: 5, FileName: "dc_boot.bin", SizeBytes: int64(len(dcBoot)), SHA1: sha1Of(dcBoot)}},
				9: {{ID: 91, PlatformID: 9, FileName: "prod.keys", SizeBytes: 10}},
			},
			content: map[int][]byte{31: scph5501, 32: scph5502, 51: dcBoot},
		},
		store:   &memBIOS{owned: map[int]domain.OwnedBIOS{}},
		pref:    &autoPref{on: true},
		updates: make(chan BIOSUpdate, 16),
		biosDir: filepath.Join(root, "bios"),
	}
	catalog := &memStore{}
	if err := catalog.Apply(context.Background(), domain.CatalogSync{
		Platforms: []domain.Platform{{ID: 3, Slug: "psx", Name: "PlayStation"}, {ID: 5, Slug: "dc", Name: "Dreamcast"},
			{ID: 9, Slug: "switch", Name: "Switch"}},
		ROMs: []domain.ROM{{ID: 1, PlatformID: 3, Name: "Ridge Racer", FileName: "Ridge Racer.chd", SizeBytes: 4},
			{ID: 2, PlatformID: 3, Name: "Tekken", FileName: "Tekken.chd", SizeBytes: 4}},
		Full: true,
	}); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	install := NewInstall(fixedProber{domain.InstallState{
		EmuDeck: domain.EmuDeckState{Root: root, ROMsPath: filepath.Join(root, "roms"), BIOSPath: f.biosDir},
		Systems: []domain.SystemState{{Folder: "psx", ROMPath: filepath.Join(root, "roms", "psx")}},
	}}, log)
	settings := &fakeSettings{in: &domain.SignIn{Server: domain.Server{URL: "https://romm.lan"}, Token: "rmm_x"}}
	conn, _ := newConnectionWithEvents(t, settings, &fakeNet{})
	f.bios = NewBIOS(t.Context(), catalog, install, conn, func(domain.Server, string) FirmwareServer { return f.server },
		f.store, nil, f.pref, func(u BIOSUpdate) { f.updates <- u }, log)

	// Games never finish downloading here: the queue is what starts the BIOS download.
	gate := &fakeContent{files: map[int][]domain.ROMFile{}, gates: map[int]chan struct{}{}, started: make(chan int, 16)}
	listeners := DownloadListeners{
		Change: func(Download) {}, Delete: func(DeleteUpdate) {}, Hold: func(error) {},
		Queued: func(d Download) { f.bios.GameQueued(d.PlatformID) },
	}
	f.downloads = NewDownloads(t.Context(), catalog, install, conn, func(domain.Server, string) DownloadServer { return gate },
		&memOwnership{roms: map[int]domain.OwnedROM{}}, nil, func(string) (int64, error) { return 1 << 40, nil },
		&memPrefs{keep: map[string]bool{}}, nil, nil, 0, listeners, log)
	f.downloads.limit = 0
	return f
}

// done waits for a platform's BIOS download to end and returns its last update.
func (f *biosFixture) done(t *testing.T) BIOSUpdate {
	t.Helper()
	for {
		select {
		case u := <-f.updates:
			if u.Done {
				return u
			}
		case <-time.After(3 * time.Second):
			t.Fatal("BIOS download didn't end")
			return BIOSUpdate{}
		}
	}
}

func TestBIOSDownloadPlacesMissingFiles(t *testing.T) {
	f := newBIOSFixture(t)
	ctx := context.Background()

	status, err := f.bios.Status(ctx, 3)
	if err != nil || len(status.Missing()) != 2 || status.InEmulator {
		t.Fatalf("status %+v, %v", status, err)
	}
	if n, err := f.bios.Download(ctx, 3); err != nil || n != 2 {
		t.Fatalf("download: %d, %v", n, err)
	}
	if start := <-f.updates; start.Done || start.Files != 2 || start.Auto || start.PlatformName != "PlayStation" {
		t.Fatalf("start %+v", start)
	}
	if u := f.done(t); u.Downloaded != 2 || u.Problem != nil {
		t.Fatalf("end %+v", u)
	}

	// PlayStation BIOS goes flat in bios/, where DuckStation and RetroArch look.
	path := filepath.Join(f.biosDir, "scph5501.bin")
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, scph5501) {
		t.Fatalf("placed %q, %v", data, err)
	}
	rec, ok := f.store.record(31)
	if !ok || rec.PlatformID != 3 || rec.File.Path != path || rec.File.SHA1 != sha1Of(scph5501) {
		t.Fatalf("record %+v", rec)
	}
	status, _ = f.bios.Status(ctx, 3)
	for _, file := range status.Files {
		if file.Status != domain.BIOSPresent || !file.Owned {
			t.Fatalf("after download %+v", file)
		}
	}
	if _, err := os.Stat(filepath.Join(f.biosDir, "..", stagingDir, biosStaging)); err == nil {
		entries, _ := os.ReadDir(filepath.Join(f.biosDir, "..", stagingDir, biosStaging))
		if len(entries) > 0 {
			t.Fatalf("staging left behind: %v", entries)
		}
	}
	// Nothing left to fetch.
	if n, err := f.bios.Download(ctx, 3); err != nil || n != 0 {
		t.Fatalf("second download: %d, %v", n, err)
	}
}

func TestBIOSNeverReplacesADifferentFile(t *testing.T) {
	f := newBIOSFixture(t)
	ctx := context.Background()
	mine := []byte("a bios the user put here")
	path := filepath.Join(f.biosDir, "scph5501.bin")
	if err := os.MkdirAll(f.biosDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, mine, 0o644); err != nil {
		t.Fatal(err)
	}
	// A user-placed file that matches counts as present.
	if err := os.WriteFile(filepath.Join(f.biosDir, "scph5502.bin"), scph5502, 0o644); err != nil {
		t.Fatal(err)
	}

	status, err := f.bios.Status(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]BIOSFile{}
	for _, file := range status.Files {
		got[file.FileName] = file
	}
	if got["scph5501.bin"].Status != domain.BIOSDifferent || got["scph5502.bin"].Status != domain.BIOSPresent || got["scph5502.bin"].Owned {
		t.Fatalf("status %+v", got)
	}
	if n, err := f.bios.Download(ctx, 3); err != nil || n != 0 {
		t.Fatalf("download: %d, %v", n, err)
	}
	if data, _ := os.ReadFile(path); !bytes.Equal(data, mine) {
		t.Fatalf("user's file replaced: %q", data)
	}
	if fetched := f.server.fetches(); len(fetched) != 0 {
		t.Fatalf("fetched %v", fetched)
	}
}

func TestBIOSHashMismatchPlacesNothing(t *testing.T) {
	f := newBIOSFixture(t)
	f.server.content[51] = []byte("corrupt")

	if _, err := f.bios.Download(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	if u := f.done(t); u.Downloaded != 0 || u.Problem == nil {
		t.Fatalf("end %+v", u)
	}
	// Dreamcast BIOS goes in bios/dc/.
	if _, err := os.Stat(filepath.Join(f.biosDir, "dc", "dc_boot.bin")); !os.IsNotExist(err) {
		t.Fatalf("placed a corrupt file: %v", err)
	}
	if _, ok := f.store.record(51); ok {
		t.Fatal("recorded a corrupt file")
	}
}

func TestBIOSInEmulatorIsNotPlaced(t *testing.T) {
	f := newBIOSFixture(t)
	status, err := f.bios.Status(context.Background(), 9)
	if err != nil || !status.InEmulator || len(status.Files) != 1 || status.Files[0].Path != "" {
		t.Fatalf("status %+v, %v", status, err)
	}
	if _, err := f.bios.Download(context.Background(), 9); kindOf(err) != domain.KindCantDownload {
		t.Fatalf("download: %v", err)
	}
}

func TestBIOSChangedFileIsNoLongerOwned(t *testing.T) {
	f := newBIOSFixture(t)
	ctx := context.Background()
	if _, err := f.bios.Download(ctx, 5); err != nil {
		t.Fatal(err)
	}
	f.done(t)
	path := filepath.Join(f.biosDir, "dc", "dc_boot.bin")
	if err := os.WriteFile(path, []byte("patched by the user"), 0o644); err != nil {
		t.Fatal(err)
	}

	status, err := f.bios.Status(ctx, 5)
	if err != nil || status.Files[0].Status != domain.BIOSDifferent || status.Files[0].Owned {
		t.Fatalf("status %+v, %v", status, err)
	}
	if _, ok := f.store.record(51); ok {
		t.Fatal("record kept for a changed file")
	}
}

func TestQueueingAGameDownloadsItsBIOS(t *testing.T) {
	f := newBIOSFixture(t)
	ctx := context.Background()

	if _, err := f.downloads.Queue(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if start := <-f.updates; !start.Auto || start.Files != 2 || start.PlatformID != 3 {
		t.Fatalf("start %+v", start)
	}
	if u := f.done(t); u.Downloaded != 2 {
		t.Fatalf("end %+v", u)
	}

	// The platform's next game doesn't ask the server again.
	if _, err := f.downloads.Queue(ctx, 2); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	f.server.mu.Lock()
	listings := f.server.listings
	f.server.mu.Unlock()
	if listings != 1 {
		t.Fatalf("listed firmware %d times", listings)
	}
}

func TestAutoBIOSCanBeTurnedOff(t *testing.T) {
	f := newBIOSFixture(t)
	if err := f.bios.SetAutoDownload(false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.downloads.Queue(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	select {
	case u := <-f.updates:
		t.Fatalf("BIOS download with auto off: %+v", u)
	case <-time.After(100 * time.Millisecond):
	}
	if fetched := f.server.fetches(); len(fetched) != 0 {
		t.Fatalf("fetched %v", fetched)
	}
}

func TestAutoBIOSNeedsFirmwareScope(t *testing.T) {
	if biosAllowed([]string{"roms.read", "platforms.read"}) {
		t.Fatal("allowed without firmware.read")
	}
	if !biosAllowed([]string{"firmware.read"}) || !biosAllowed(nil) {
		t.Fatal("refused with firmware.read, or with unknown scopes")
	}
}
