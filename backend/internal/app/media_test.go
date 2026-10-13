package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// fakeArt is the cover cache and the Server's screenshots: covers by ROM id, screenshot paths by
// ROM id, and the image at each path.
type fakeArt struct {
	mu          sync.Mutex
	covers      map[int][]byte
	screenshots map[int][]string
	images      map[string][]byte
}

func (a *fakeArt) Cover(_ context.Context, id int, size domain.CoverSize) ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if size != domain.CoverLarge {
		return nil, errors.New("media wants the large cover")
	}
	return a.covers[id], nil
}

func (a *fakeArt) Screenshots(_ context.Context, id int) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.screenshots[id], nil
}

func (a *fakeArt) image(_ context.Context, path string) ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	data, ok := a.images[path]
	if !ok {
		return nil, &domain.Error{Kind: domain.KindNotFound, Message: "no image"}
	}
	return data, nil
}

// serverArt is fakeArt as a MediaServer, whose Cover fetches by path.
type serverArt struct{ *fakeArt }

func (s serverArt) Cover(ctx context.Context, path string) ([]byte, error) { return s.image(ctx, path) }

// memMediaPrefs keeps the ES-DE media setting in memory.
type memMediaPrefs struct{ on atomic.Bool }

func (m *memMediaPrefs) ESDEMedia() bool { return m.on.Load() }

func (m *memMediaPrefs) SetESDEMedia(on bool) error {
	m.on.Store(on)
	return nil
}

var (
	coverPNG      = []byte("\x89PNG\r\n\x1a\nromm cover")
	coverPNGv2    = []byte("\x89PNG\r\n\x1a\nromm cover, new art")
	screenshotJPG = []byte("\xff\xd8\xff\xe0romm screenshot")
	scrapedPNG    = []byte("\x89PNG\r\n\x1a\nscraped by es-de")
)

// withChronoArt gives Chrono Trigger a cover and a screenshot on RomM.
func (f *downloadFixture) withChronoArt() {
	f.art.mu.Lock()
	defer f.art.mu.Unlock()
	f.art.covers[1] = coverPNG
	f.art.screenshots[1] = []string{"/shots/1/0.jpg", "/shots/1/1.jpg"}
	f.art.images["/shots/1/0.jpg"] = screenshotJPG
}

func (f *downloadFixture) chronoCover() string {
	return filepath.Join(f.mediaDir, "snes", "covers", "Chrono Trigger (USA).png")
}

func (f *downloadFixture) chronoScreenshot() string {
	return filepath.Join(f.mediaDir, "snes", "screenshots", "Chrono Trigger (USA).jpg")
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func gone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s still there: %v", path, err)
	}
}

func TestDownloadPlacesMedia(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.withChronoArt()
	f.install(t, 1)

	if got := readFile(t, f.chronoCover()); got != string(coverPNG) {
		t.Fatalf("cover holds %q", got)
	}
	if got := readFile(t, f.chronoScreenshot()); got != string(screenshotJPG) {
		t.Fatalf("screenshot holds %q", got)
	}
	rec, _, _ := f.owned.Owned(context.Background(), 1)
	if len(rec.Media) != 2 || len(rec.Files) != 1 {
		t.Fatalf("record %+v", rec)
	}
	for _, m := range rec.Media {
		if same, err := unchanged(m); err != nil || !same || m.SHA1 == "" {
			t.Fatalf("media %+v recorded wrong: %v", m, err)
		}
	}
}

func TestDownloadNeverReplacesMedia(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.withChronoArt()
	// ES-DE looks for a cover under either extension, so a scraped JPEG is the game's cover.
	scraped := filepath.Join(f.mediaDir, "snes", "covers", "Chrono Trigger (USA).jpg")
	writeFile(t, scraped, scrapedPNG)
	f.install(t, 1)

	gone(t, f.chronoCover())
	if got := readFile(t, scraped); got != string(scrapedPNG) {
		t.Fatalf("scraped cover holds %q", got)
	}
	if rec, _, _ := f.owned.Owned(context.Background(), 1); len(rec.Media) != 1 || rec.Media[0].Path != f.chronoScreenshot() {
		t.Fatalf("record %+v", rec)
	}
}

func TestDownloadWithoutMedia(t *testing.T) {
	cases := map[string]func(f *downloadFixture){
		"setting off": func(f *downloadFixture) { f.mediaPrefs.on.Store(false) },
		"no art on RomM": func(f *downloadFixture) {
			f.art.mu.Lock()
			defer f.art.mu.Unlock()
			f.art.covers, f.art.screenshots = map[int][]byte{}, map[int][]string{}
		},
		"art ES-DE can't show": func(f *downloadFixture) {
			f.art.mu.Lock()
			defer f.art.mu.Unlock()
			f.art.covers[1] = []byte("GIF89a cover")
			f.art.images["/shots/1/0.jpg"] = []byte("RIFF....WEBPVP8 screenshot")
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newDownloadFixture(t, 2)
			f.withChronoArt()
			setup(f)
			f.install(t, 1)
			if _, err := os.Stat(f.mediaDir); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("media folder made: %v", err)
			}
			if rec, _, _ := f.owned.Owned(context.Background(), 1); rec.Media != nil {
				t.Fatalf("record %+v", rec)
			}
		})
	}
}

func TestDeleteRemovesMedia(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.withChronoArt()
	f.install(t, 1)

	if _, err := f.downloads.Delete(context.Background(), []int{1}, 0); err != nil {
		t.Fatal(err)
	}
	if u := f.deleted(t, 1); u.Problem != nil {
		t.Fatalf("end %+v", u)
	}
	gone(t, f.chronoCover())
	gone(t, f.chronoScreenshot())
}

// ES-DE's scraper replaced the cover after the download: the scraped one stays.
func TestDeleteKeepsReplacedMedia(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.withChronoArt()
	f.install(t, 1)
	writeFile(t, f.chronoCover(), scrapedPNG)

	if _, err := f.downloads.Delete(context.Background(), []int{1}, 0); err != nil {
		t.Fatal(err)
	}
	if u := f.deleted(t, 1); u.Problem != nil {
		t.Fatalf("a replaced cover refused the delete: %+v", u)
	}
	if got := readFile(t, f.chronoCover()); got != string(scrapedPNG) {
		t.Fatalf("cover holds %q", got)
	}
	gone(t, f.chronoScreenshot())
	gone(t, filepath.Join(f.snes, "Chrono Trigger (USA).sfc"))
}

func TestUpdateReplacesMedia(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.withChronoArt()
	f.chronoWithUpdate(t, "Chrono Trigger (USA).sfc", []byte("chrono trigger, revised"))
	// The screenshot was scraped over since; the cover is still the plugin's.
	writeFile(t, f.chronoScreenshot(), scrapedPNG)
	f.art.mu.Lock()
	f.art.covers[1] = coverPNGv2
	f.art.mu.Unlock()

	if _, err := f.downloads.Queue(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if u := f.end(t, 1); u.State != domain.StateInstalled {
		t.Fatalf("end %+v", u)
	}
	if got := readFile(t, f.chronoCover()); got != string(coverPNGv2) {
		t.Fatalf("cover holds %q", got)
	}
	if got := readFile(t, f.chronoScreenshot()); got != string(scrapedPNG) {
		t.Fatalf("screenshot holds %q", got)
	}
	if rec, _, _ := f.owned.Owned(context.Background(), 1); len(rec.Media) != 1 || rec.Media[0].Path != f.chronoCover() {
		t.Fatalf("record %+v", rec)
	}
}

// Systems whose ROMs sit in a folder inside the system's have their media in the same folder
// inside each media type's.
func TestMediaMirrorsFoldersInsideTheSystem(t *testing.T) {
	f := newDownloadFixture(t, 2)
	f.withChronoArt()
	placed := f.media.Place(context.Background(), 1, f.mediaDir, filepath.Join("wiiu", "roms", "Zelda.wua"))
	if len(placed) != 2 || placed[0].Path != filepath.Join(f.mediaDir, "wiiu", "covers", "roms", "Zelda.png") ||
		placed[1].Path != filepath.Join(f.mediaDir, "wiiu", "screenshots", "roms", "Zelda.jpg") {
		t.Fatalf("placed %+v", placed)
	}
}
