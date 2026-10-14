package app

import (
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

// cachedROM is what a ROM's file gave when it was last read.
type cachedROM struct {
	size int64
	mod  time.Time
	rom  domain.N64ROM
}

// maxN64ROM bounds an N64 ROM as read; the largest carts are 64 MB.
const maxN64ROM = 128 << 20

// n64ROM reads the N64 ROM at path, from inside it when it's an archive of one file, reading it
// again only when it changed.
func (s *Saves) n64ROM(path string) (domain.N64ROM, error) {
	info, err := os.Stat(path)
	if err != nil {
		return domain.N64ROM{}, err
	}
	s.mu.Lock()
	c, ok := s.roms[path]
	s.mu.Unlock()
	if ok && c.size == info.Size() && c.mod.Equal(info.ModTime()) {
		return c.rom, nil
	}
	data, err := romBytes(path)
	if err != nil {
		return domain.N64ROM{}, err
	}
	rom, err := domain.ParseN64ROM(data)
	if err != nil {
		return domain.N64ROM{}, err
	}
	s.mu.Lock()
	s.roms[path] = cachedROM{size: info.Size(), mod: info.ModTime(), rom: rom}
	s.mu.Unlock()
	return rom, nil
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
