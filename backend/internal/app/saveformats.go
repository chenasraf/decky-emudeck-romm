package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// saveFormat is how a game's save is kept on the Deck, read and written as the one save RomM keeps
// of it.
type saveFormat interface {
	read() (localSave, error)
	// write puts a save from RomM in place of the game's.
	write(data []byte) error
	// files are the files the save is kept in.
	files() []string
}

// localSave is a save on the Deck as a sync read it: as RomM would keep it, and when it was last
// written.
type localSave struct {
	exists bool
	data   []byte
	hash   string
	mod    time.Time
}

// rawFile is a save kept in one file, on RomM as is.
type rawFile struct{ path string }

func (f rawFile) files() []string { return []string{f.path} }

func (f rawFile) read() (localSave, error) {
	info, err := os.Stat(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return localSave{}, nil
	}
	if err != nil {
		return localSave{}, err
	}
	data, err := os.ReadFile(f.path)
	if err != nil {
		return localSave{}, err
	}
	return localSave{exists: true, data: data, hash: domain.ContentHash(data), mod: info.ModTime().UTC()}, nil
}

func (f rawFile) write(data []byte) error { return writeAtomic(f.path, data) }

// psxCard is a PlayStation memory card kept in one file, on RomM as is. Any emulator's raw card
// will do: RetroArch's PlayStation cores keep the same.
type psxCard struct{ rawFile }

func (f psxCard) write(data []byte) error {
	if !domain.IsPSXCard(data) {
		return errors.New("RomM's save isn't a PlayStation memory card, so DuckStation can't use it")
	}
	return f.rawFile.write(data)
}

// n64Files is an N64 save in mupen64plus's files, base being their path without the extension. On
// RomM it's the unit RomM's clients share.
type n64Files struct{ base string }

func (f n64Files) files() []string {
	out := make([]string, 0, len(domain.N64SaveExts))
	for _, ext := range domain.N64SaveExts {
		out = append(out, f.base+ext)
	}
	return out
}

// load reads the game's files into a save, with the paks' file as it is and when the newest file
// was written.
func (f n64Files) load() (*domain.N64Save, []byte, time.Time, error) {
	s := &domain.N64Save{}
	var paks []byte
	var mod time.Time
	for _, ext := range domain.N64SaveExts {
		path := f.base + ext
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, nil, mod, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, mod, err
		}
		s.TakeFile(ext, data)
		if ext == domain.N64PakFileExt {
			paks = data
		}
		if info.ModTime().After(mod) {
			mod = info.ModTime().UTC()
		}
	}
	return s, paks, mod, nil
}

func (f n64Files) read() (localSave, error) {
	s, _, mod, err := f.load()
	if err != nil || s.Empty() {
		// Files the game never wrote to hold no save.
		return localSave{}, err
	}
	unit := s.Unit()
	return localSave{exists: true, data: unit, hash: domain.ContentHash(unit), mod: mod}, nil
}

func (f n64Files) write(data []byte) error {
	s, err := domain.ParseN64Unit(data)
	if err != nil {
		return fmt.Errorf("%w, so RMG can't use it", err)
	}
	old, oldPaks, _, err := f.load()
	if err != nil {
		return err
	}
	for _, file := range s.Files(old, oldPaks) {
		path := f.base + file.Ext
		if file.Data == nil {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			continue
		}
		if err := writeAtomic(path, file.Data); err != nil {
			return err
		}
	}
	return nil
}

// n64Base is the path, without the extension, of the files RMG keeps the save of the ROM at entry
// in, in dir.
func (s *Saves) n64Base(entry, dir string) (string, string) {
	rom, err := s.n64ROM(entry)
	if err != nil {
		return "", "The game couldn't be read as an N64 ROM to tell its save's name: " + err.Error()
	}
	// A save already there is found by the ROM's hash alone, whatever name it was created under.
	suffix := "-" + rom.MD5[:8]
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			ext := filepath.Ext(e.Name())
			stem := strings.TrimSuffix(e.Name(), ext)
			if !e.IsDir() && slices.Contains(domain.N64SaveExts, strings.ToLower(ext)) && strings.HasSuffix(stem, suffix) {
				return filepath.Join(dir, stem), ""
			}
		}
	}
	good, known, err := s.n64db.GoodName(rom)
	if err != nil {
		return "", "RMG's ROM database couldn't be read, so this game's save name can't be told: " + err.Error()
	}
	return filepath.Join(dir, domain.N64SaveName(rom, good, known)), ""
}

// psxCardName is the file DuckStation keeps the memory card of the game at entry in, in dir, and
// whether the discs of a game on several share it.
func (s *Saves) psxCardName(entry, dir string) (string, bool, string) {
	path, err := psxBoot(entry)
	if err != nil {
		return "", false, "The game's disc couldn't be read to tell its memory card's name: " + err.Error()
	}
	disc, err := readCached(s, path, func() (domain.PSXDisc, error) { return s.psx.Disc(path) })
	if err != nil {
		return "", false, "The game's disc couldn't be read to tell its memory card's name: " + err.Error()
	}
	game, why, err := s.duckstation.Game(path, disc)
	if err != nil {
		return "", false, "DuckStation's game database couldn't be read, so this game's memory card name can't be told: " + err.Error()
	}
	if why != "" {
		return "", false, why
	}
	card := game.Card(func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	})
	return card, game.DiscSet != "" && card == domain.DuckStationCardName(game.DiscSet), ""
}

// psxBootExts are the files a PlayStation game in a folder may boot from, the likeliest first.
var psxBootExts = []string{".m3u", ".cue", ".chd", ".iso", ".bin", ".img"}

// psxBoot is the file the PlayStation game at entry boots from. ES-DE launches a folder named like a
// file by the file in it of the same name; a plain folder is browsed into, and its playlist or
// disc launched, the first by name when there are several, which is the first disc.
func psxBoot(entry string) (string, error) {
	info, err := os.Stat(entry)
	if err != nil || !info.IsDir() {
		return entry, nil //nolint:nilerr // a file, or one gone, is read as it is
	}
	same := filepath.Join(entry, filepath.Base(entry))
	if info, err := os.Stat(same); err == nil && !info.IsDir() {
		return same, nil
	}
	entries, err := os.ReadDir(entry)
	if err != nil {
		return "", err
	}
	for _, ext := range psxBootExts {
		for _, e := range entries {
			// A hidden file is no disc: macOS leaves a ._ file beside each one it copies.
			if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") && strings.EqualFold(filepath.Ext(e.Name()), ext) {
				return filepath.Join(entry, e.Name()), nil
			}
		}
	}
	return "", fmt.Errorf("%s holds no disc", filepath.Base(entry))
}

// cardOwner is the name of the game with the lowest ID, below id, that syncs the memory card at
// card: the discs of a game RomM keeps as one game each share the card DuckStation gives them all.
func (s *Saves) cardOwner(ctx context.Context, id, platformID int, card string) (string, error) {
	ids, err := s.gameIDs(ctx)
	if err != nil {
		return "", err
	}
	for _, other := range ids {
		if other >= id {
			break
		}
		otherPlatform, _, _, why, err := s.placed(ctx, other)
		if err != nil {
			return "", err
		}
		if why != "" || otherPlatform != platformID {
			continue
		}
		t, why, err := s.resolve(ctx, other, false)
		if err != nil {
			return "", err
		}
		if why == "" && t.path == card {
			return t.name, nil
		}
	}
	return "", nil
}

// cachedRead is what a game's file gave when it was last read.
type cachedRead struct {
	size  int64
	mod   time.Time
	value any
}

// readCached returns what read gives for the file at path, reading it again only when it changed.
func readCached[T any](s *Saves, path string, read func() (T, error)) (T, error) {
	var zero T
	info, err := os.Stat(path)
	if err != nil {
		return zero, err
	}
	s.mu.Lock()
	c, ok := s.reads[path]
	s.mu.Unlock()
	if v, isT := c.value.(T); ok && isT && c.size == info.Size() && c.mod.Equal(info.ModTime()) {
		return v, nil
	}
	v, err := read()
	if err != nil {
		return zero, err
	}
	s.mu.Lock()
	s.reads[path] = cachedRead{size: info.Size(), mod: info.ModTime(), value: v}
	s.mu.Unlock()
	return v, nil
}

// maxN64ROM bounds an N64 ROM as read; the largest carts are 64 MB.
const maxN64ROM = 128 << 20

// n64ROM reads the N64 ROM at path, from inside it when it's an archive of one file.
func (s *Saves) n64ROM(path string) (domain.N64ROM, error) {
	return readCached(s, path, func() (domain.N64ROM, error) {
		data, err := romBytes(path)
		if err != nil {
			return domain.N64ROM{}, err
		}
		return domain.ParseN64ROM(data)
	})
}

func romBytes(path string) ([]byte, error) {
	if !domain.IsArchive(path) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		return io.ReadAll(io.LimitReader(f, maxN64ROM))
	}
	members, closeArchive, err := openArchive(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeArchive() }()
	if len(members) != 1 {
		return nil, fmt.Errorf("the archive holds %d files", len(members))
	}
	rc, err := members[0].open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(io.LimitReader(rc, maxN64ROM))
}
