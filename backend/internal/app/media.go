package app

import (
	"context"
	"crypto/sha1" //nolint:gosec // recorded to tell the plugin's art from art that replaced it, not for security
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// MediaServer fetches a game's screenshots.
type MediaServer interface {
	// Screenshots returns the paths the Server gives for a ROM's screenshots.
	Screenshots(ctx context.Context, id int) ([]string, error)
	// Cover fetches the image at a path the Server gave for it.
	Cover(ctx context.Context, path string) ([]byte, error)
}

// MediaDialer returns a MediaServer for server, authenticated with token.
type MediaDialer func(server domain.Server, token string) MediaServer

// CoverSource returns a ROM's cover, nil when it has none or it can't be had right now.
type CoverSource interface {
	Cover(ctx context.Context, id int, size domain.CoverSize) ([]byte, error)
}

// MediaPrefs keeps the ES-DE media setting.
type MediaPrefs interface {
	ESDEMedia() bool
	SetESDEMedia(on bool) error
}

// mediaExts are the image types ES-DE shows, each by the extension it looks for. ES-DE looks for
// every one of them, so a file under any of them is the game's art already.
var mediaExts = map[string]string{"image/png": ".png", "image/jpeg": ".jpg"}

// Media copies RomM's art for a downloaded game into ES-DE's media folder, so the game shows its
// cover in ES-DE without scraping. It never replaces a file there: whatever was there first, scraped
// or the user's, wins.
type Media struct {
	covers CoverSource
	conn   *Connection
	dial   MediaDialer
	prefs  MediaPrefs
	log    *slog.Logger
}

func NewMedia(covers CoverSource, conn *Connection, dial MediaDialer, prefs MediaPrefs, log *slog.Logger) *Media {
	return &Media{covers: covers, conn: conn, dial: dial, prefs: prefs, log: log}
}

// Enabled says whether downloads copy their art into ES-DE's media folder.
func (m *Media) Enabled() bool { return m.prefs.ESDEMedia() }

// SetEnabled turns copying art into ES-DE's media folder on or off. Art already placed stays.
func (m *Media) SetEnabled(on bool) error { return m.prefs.SetESDEMedia(on) }

// Place copies a ROM's cover and first screenshot into dir, ES-DE's media folder, for the game at
// entry: its path under the ROMs folder, starting with its system's folder. ES-DE keeps one
// screenshot per game. Art is a nicety, so what can't be fetched or written is logged and skipped,
// and a nil Media places nothing.
func (m *Media) Place(ctx context.Context, romID int, dir, entry string) []domain.PlacedFile {
	if m == nil || !m.prefs.ESDEMedia() {
		return nil
	}
	system, game, ok := strings.Cut(filepath.ToSlash(entry), "/")
	if !ok || game == "" {
		return nil
	}
	// ES-DE names a game's media after its file or folder, without the extension, mirroring any
	// folders the game sits in under its system's.
	stem := filepath.FromSlash(strings.TrimSuffix(game, filepath.Ext(game)))

	var out []domain.PlacedFile
	place := func(kind string, data []byte) {
		if data == nil {
			return
		}
		f, err := writeMedia(filepath.Join(dir, system, kind, stem), data)
		switch {
		case err != nil:
			m.log.Warn("place media", "rom_id", romID, "kind", kind, "err", err)
		case f != nil:
			m.log.Info("media placed", "rom_id", romID, "path", f.Path)
			out = append(out, *f)
		}
	}
	cover, err := m.covers.Cover(ctx, romID, domain.CoverLarge)
	if err != nil {
		m.log.Warn("cover for media", "rom_id", romID, "err", err)
	}
	place("covers", cover)
	place("screenshots", m.screenshot(ctx, romID))
	return out
}

// screenshot fetches a ROM's first screenshot, nil when it has none or the Server can't send it.
func (m *Media) screenshot(ctx context.Context, romID int) []byte {
	in, err := m.conn.activeSignIn()
	if err != nil || in == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, coverTimeout)
	defer cancel()
	server := m.dial(in.Server, in.Token)
	paths, err := server.Screenshots(ctx, romID)
	if err != nil {
		m.log.Warn("screenshots for media", "rom_id", romID, "err", m.conn.observe(*in, err))
		return nil
	}
	if len(paths) == 0 {
		return nil
	}
	data, err := server.Cover(ctx, paths[0])
	if err != nil {
		m.log.Warn("screenshot for media", "rom_id", romID, "err", m.conn.observe(*in, err))
		return nil
	}
	return data
}

// writeMedia writes data to base with the extension of its image type and returns it as placed. It
// writes nothing, returning nil, when data isn't a type ES-DE shows or the game already has art at
// base under any of them.
func writeMedia(base string, data []byte) (*domain.PlacedFile, error) {
	ext, ok := mediaExts[http.DetectContentType(data)]
	if !ok {
		return nil, nil
	}
	for _, e := range mediaExts {
		if _, err := os.Lstat(base + e); !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	path := base + ext
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, err
	}
	_, err = out.Write(data)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	var info os.FileInfo
	if err == nil {
		info, err = os.Stat(path)
	}
	if err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("write %s: %w", path, err)
	}
	sum := sha1.Sum(data) //nolint:gosec // see import
	return &domain.PlacedFile{Path: path, SizeBytes: info.Size(), ModTime: info.ModTime().UTC(), SHA1: hex.EncodeToString(sum[:])}, nil
}

// placeMedia places a game's art for ES-DE, the game being root in dir, a ROM folder.
func (d *Downloads) placeMedia(ctx context.Context, id int, dir, root string) []domain.PlacedFile {
	state := d.install.State(false)
	// Without its settings ES-DE isn't set up, and when it reads another ROMs folder it never shows
	// the game: either way art placed for it is only clutter.
	if state.ESDE.SettingsPath == "" || state.ESDE.MediaDirectory == "" || !state.ESDE.ROMDirectoryMatches {
		return nil
	}
	rel, err := filepath.Rel(state.EmuDeck.ROMsPath, filepath.Join(dir, root))
	if err != nil || !filepath.IsLocal(rel) {
		return nil
	}
	return d.media.Place(ctx, id, state.ESDE.MediaDirectory, rel)
}

// removeMedia deletes the art the plugin placed for a game, leaving any that ES-DE's scraper or the
// user replaced since.
func (d *Downloads) removeMedia(files []domain.PlacedFile) {
	for _, f := range files {
		same, err := unchanged(f)
		if err != nil || !same {
			d.log.Info("media kept", "path", f.Path, "err", err)
			continue
		}
		if err := os.Remove(f.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			d.log.Warn("remove media", "path", f.Path, "err", err)
		}
	}
}
