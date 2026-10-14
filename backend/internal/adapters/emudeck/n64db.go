package emudeck

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// rmgDatabase is where RMG's Flatpak keeps mupen64plus's ROM database, user installs first. The
// database changes between RMG versions in ways that change save names, so the installed copy is
// the one to read.
var rmgDatabase = []string{
	".local/share/flatpak/app/com.github.Rosalie241.RMG/current/active/files/share/RMG/mupen64plus.ini",
	"/var/lib/flatpak/app/com.github.Rosalie241.RMG/current/active/files/share/RMG/mupen64plus.ini",
}

// N64Database reads the ROM database RMG names saves from, again whenever it changes.
type N64Database struct {
	home string

	mu    sync.Mutex
	path  string
	size  int64
	mod   time.Time
	byMD5 map[string]string
	byCRC map[string][]string
}

func NewN64Database(home string) *N64Database { return &N64Database{home: home} }

// GoodName returns the database's name of a ROM: by its MD5, else by its header CRCs when exactly
// one entry has them. known is false when the database lacks the ROM.
func (d *N64Database) GoodName(rom domain.N64ROM) (string, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.load(); err != nil {
		return "", false, err
	}
	if name, ok := d.byMD5[rom.MD5]; ok {
		return name, true, nil
	}
	if names := d.byCRC[fmt.Sprintf("%08X %08X", rom.CRC1, rom.CRC2)]; len(names) == 1 && names[0] != "" {
		return names[0], true, nil
	}
	return "", false, nil
}

func (d *N64Database) load() error {
	for _, p := range rmgDatabase {
		if !filepath.IsAbs(p) {
			p = filepath.Join(d.home, p)
		}
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if p == d.path && info.Size() == d.size && info.ModTime().Equal(d.mod) {
			return nil
		}
		byMD5, byCRC, err := parseN64Database(p)
		if err != nil {
			return err
		}
		d.path, d.size, d.mod, d.byMD5, d.byCRC = p, info.Size(), info.ModTime(), byMD5, byCRC
		return nil
	}
	return errors.New("RMG's ROM database wasn't found; is RMG installed?")
}

// parseN64Database reads mupen64plus.ini: a section per ROM MD5, with its GoodName, its header CRCs,
// and RefMD5 naming the entry it takes what it lacks from.
func parseN64Database(path string) (map[string]string, map[string][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()
	type entry struct{ good, crc, ref string }
	entries := map[string]*entry{}
	var cur *entry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "", strings.HasPrefix(line, ";"), strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			cur = &entry{}
			entries[strings.ToUpper(line[1:len(line)-1])] = cur
		case cur != nil:
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			switch strings.TrimSpace(key) {
			case "GoodName":
				cur.good = strings.TrimSpace(value)
			case "CRC":
				cur.crc = strings.ToUpper(strings.TrimSpace(value))
			case "RefMD5":
				cur.ref = strings.ToUpper(strings.TrimSpace(value))
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, nil, err
	}
	byMD5, byCRC := map[string]string{}, map[string][]string{}
	for md5, e := range entries {
		good := e.good
		if ref := entries[e.ref]; ref != nil && good == "" {
			good = ref.good
		}
		// mupen64plus indexes CRCs as it reads each entry's own, before it resolves RefMD5s: a CRC an
		// entry only inherits is never looked up.
		if e.crc != "" {
			byCRC[e.crc] = append(byCRC[e.crc], good)
		}
		if good != "" {
			byMD5[md5] = good
		}
	}
	return byMD5, byCRC, nil
}
