package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
	"github.com/chenasraf/saveporter/gb"
)

// gbSave is a Game Boy or Game Boy Color battery save, kept in an emulator's files: a .srm with the
// clock in a .rtc beside it for RetroArch's cores, one .sav for mGBA. On RomM it's the cartridge's
// RAM as the web player keeps it, followed by its clock.
type gbSave struct {
	// path is the RAM's file (.srm or .sav); a .rtc beside it holds RetroArch's clock.
	path   string
	format gb.Format
	cart   gb.Cart
	// tag is what RomM keeps the save under, which names the emulator a save from elsewhere came
	// from.
	tag string
	now func() time.Time
}

func (f gbSave) rtcPath() string { return strings.TrimSuffix(f.path, gb.ExtSRM) + gb.ExtRTC }

func (f gbSave) files() []string {
	if f.format == gb.MGBAApp {
		return []string{f.path}
	}
	return []string{f.path, f.rtcPath()}
}

// load reads the emulator's files of the save, and when the newest of them was written.
func (f gbSave) load() (gb.Files, time.Time, error) {
	files := gb.Files{}
	var mod time.Time
	for _, path := range f.files() {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, mod, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, mod, err
		}
		ext := gb.ExtRTC
		if path == f.path {
			ext = gb.ExtSRM
			if f.format == gb.MGBAApp {
				ext = gb.ExtSav
			}
		}
		files[ext] = data
		if info.ModTime().After(mod) {
			mod = info.ModTime().UTC()
		}
	}
	return files, mod, nil
}

func (f gbSave) read() (localSave, error) {
	files, mod, err := f.load()
	if err != nil {
		return localSave{}, err
	}
	if _, ok := files[gb.ExtSRM]; !ok {
		if _, ok := files[gb.ExtSav]; !ok {
			// A clock alone, or nothing: the game never saved.
			return localSave{}, nil
		}
	}
	// The clock is read and written as of the files' last write: the same files always make the
	// same save, though the clock runs on.
	s, err := gb.Read(f.format, f.cart, files, mod)
	if err != nil {
		return localSave{}, err
	}
	data, err := domain.GBServerSave(f.cart, s, mod)
	if err != nil {
		return localSave{}, err
	}
	return localSave{exists: true, data: data, hash: domain.ContentHash(data), mod: mod}, nil
}

func (f gbSave) write(data []byte) error {
	now := f.now()
	s, err := domain.ParseGBServerSave(data, f.tag, f.cart, now)
	if err != nil {
		return err
	}
	if s.Clock == nil && s.HuC3 == nil {
		// A save without a clock (the web player keeps none) leaves the Deck's running.
		if files, _, err := f.load(); err == nil {
			if local, err := gb.Read(f.format, f.cart, files, now); err == nil {
				s.Clock, s.HuC3 = local.Clock, local.HuC3
			}
		}
	}
	out, err := gb.Write(f.format, f.cart, s, now)
	if err != nil {
		return err
	}
	for ext, b := range out {
		path := f.path
		if ext == gb.ExtRTC {
			path = f.rtcPath()
		}
		if err := writeAtomic(path, b); err != nil {
			return err
		}
	}
	return nil
}

// gbCart reads the cartridge header of the Game Boy ROM at path, from inside it when it's an archive
// of one file.
func (s *Saves) gbCart(path string) (gb.Cart, error) {
	return readCached(s, path, func() (gb.Cart, error) {
		header, err := romHeader(path, gb.HeaderSize)
		if err != nil {
			return gb.Cart{}, err
		}
		return gb.ParseCart(header)
	})
}

// romHeader reads the first n bytes of the ROM at path, from inside it when it's an archive of one
// file.
func romHeader(path string, n int) ([]byte, error) {
	var r io.ReadCloser
	if !domain.IsArchive(path) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		r = f
	} else {
		members, closeArchive, err := openArchive(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = closeArchive() }()
		if len(members) != 1 {
			return nil, fmt.Errorf("the archive holds %d files", len(members))
		}
		if r, err = members[0].open(); err != nil {
			return nil, err
		}
	}
	defer func() { _ = r.Close() }()
	header := make([]byte, n)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}
	return header, nil
}
