package emudeck

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/appimage"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

const (
	// duckStationData is where DuckStation keeps its settings, under home.
	duckStationData = ".local/share/duckstation"
	// duckStationResources is where DuckStation's AppImage bundles its game database. The database
	// changes between versions in ways that rename cards, so the installed copy is the one to read.
	duckStationResources = "usr/bin/resources"
)

// DuckStation reads what DuckStation names a game's memory card after: its game database, bundled
// in the AppImage EmuDeck installs, and the titles and settings the user gave games.
type DuckStation struct {
	home string

	mu    sync.Mutex
	image string
	size  int64
	mod   time.Time
	db    *duckDB
}

func NewDuckStation(home string) *DuckStation { return &DuckStation{home: home} }

// Game returns how DuckStation names the memory card of the game it boots from path, whose disc
// is disc. why says what in DuckStation's settings for the game stops the plugin from naming it.
func (d *DuckStation) Game(path string, disc domain.PSXDisc) (domain.DuckStationGame, string, error) {
	d.mu.Lock()
	db, err := d.load()
	d.mu.Unlock()
	if err != nil {
		return domain.DuckStationGame{}, "", err
	}
	data := filepath.Join(d.home, duckStationData)
	custom := readINI(filepath.Join(data, "custom_properties.ini"))[path]
	var e *duckEntry
	serial := custom["Serial"]
	switch {
	case serial != "":
		e = db.lookup(serial)
	case disc.Read:
		if e = db.lookup(disc.Serial); e == nil {
			e = db.lookup(disc.HashID)
		}
		serial = disc.Serial
		if serial == "" {
			serial = disc.HashID
		}
	}
	if e != nil && custom["Serial"] == "" {
		serial = e.serial
	}
	if serial != "" {
		if cards := readINI(filepath.Join(data, "gamesettings", serial+".ini"))["MemoryCards"]; len(cards) > 0 {
			return domain.DuckStationGame{}, "This game has its own memory card settings in DuckStation, so which card it uses can't be told. Clear them in the game's properties in DuckStation.", nil
		}
	}
	g := domain.DuckStationGame{Title: domain.FileTitle(path)}
	if e != nil {
		g.Title, g.DiscSet = e.save, e.set
	}
	if t := custom["Title"]; t != "" {
		g.Title = t
	}
	return g, "", nil
}

// load reads the game database out of the newest DuckStation AppImage, again whenever it changes.
func (d *DuckStation) load() (*duckDB, error) {
	image := d.appImage()
	if image == "" {
		return nil, errors.New("DuckStation's AppImage wasn't found in ~/Applications; is DuckStation installed?")
	}
	info, err := os.Stat(image)
	if err != nil {
		return nil, err
	}
	if image == d.image && info.Size() == d.size && info.ModTime().Equal(d.mod) {
		return d.db, nil
	}
	r, err := appimage.Open(image)
	if err != nil {
		return nil, fmt.Errorf("read DuckStation's AppImage: %w", err)
	}
	defer func() { _ = r.Close() }()
	games, err := fs.ReadFile(r, duckStationResources+"/gamedb.yaml")
	if err != nil {
		return nil, fmt.Errorf("read DuckStation's game database: %w", err)
	}
	sets, err := fs.ReadFile(r, duckStationResources+"/discsets.yaml")
	if err != nil {
		return nil, fmt.Errorf("read DuckStation's disc sets: %w", err)
	}
	db := parseDuckDB(games, sets)
	d.image, d.size, d.mod, d.db = image, info.Size(), info.ModTime(), db
	return db, nil
}

// appImage is the AppImage EmuDeck's launcher runs: the last DuckStation*.AppImage by name in
// ~/Applications, matched in any case.
func (d *DuckStation) appImage() string {
	dir := filepath.Join(d.home, "Applications")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var found []string
	for _, e := range entries {
		name := strings.ToLower(e.Name())
		if !e.IsDir() && strings.HasPrefix(name, "duckstation") && strings.HasSuffix(name, ".appimage") {
			found = append(found, filepath.Join(dir, e.Name()))
		}
	}
	if len(found) == 0 {
		return ""
	}
	slices.Sort(found)
	return found[len(found)-1]
}

// duckEntry is a game in DuckStation's database: the serial it's filed under, the title its
// memory card takes, and that of the disc set it belongs to, if any.
type duckEntry struct {
	serial, save, set string
}

type duckDB struct {
	// codes are the disc serials each entry answers to.
	codes   map[string]string
	entries map[string]*duckEntry
}

func (db *duckDB) lookup(code string) *duckEntry {
	if code == "" {
		return nil
	}
	return db.entries[db.codes[code]]
}

// parseDuckDB reads gamedb.yaml and discsets.yaml. They're generated with a fixed shape, so a line
// scanner reads the few fields naming cards: an entry per top-level serial with its name, saveName
// and the codes it answers to (its own serial when it lists none), and a list of disc sets, each
// with its name, saveName and serials.
func parseDuckDB(games, sets []byte) *duckDB {
	db := &duckDB{codes: map[string]string{}, entries: map[string]*duckEntry{}}
	var cur *duckEntry
	var name string
	var codes []string
	inCodes := false
	end := func() {
		if cur == nil {
			return
		}
		if cur.save == "" {
			cur.save = name
		}
		if len(codes) == 0 {
			codes = []string{cur.serial}
		}
		// The first entry to claim a code keeps it.
		for _, c := range codes {
			if _, taken := db.codes[c]; !taken {
				db.codes[c] = cur.serial
			}
		}
		db.entries[cur.serial] = cur
		cur, name, codes, inCodes = nil, "", nil, false
	}
	for line := range lines(games) {
		switch {
		case line == "" || strings.HasPrefix(strings.TrimSpace(line), "#"):
		case line[0] != ' ' && line[0] != '-' && strings.HasSuffix(line, ":"):
			end()
			cur = &duckEntry{serial: strings.TrimSuffix(line, ":")}
		case cur == nil:
		case strings.HasPrefix(line, "  name: "):
			name, inCodes = yamlString(line[len("  name: "):]), false
		case strings.HasPrefix(line, "  saveName: "):
			cur.save, inCodes = yamlString(line[len("  saveName: "):]), false
		case line == "  codes:":
			inCodes = true
		case inCodes && strings.HasPrefix(line, "    - "):
			codes = append(codes, yamlString(line[len("    - "):]))
		case strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   "):
			inCodes = false
		}
	}
	end()

	var save string
	var serials []string
	inSerials := false
	endSet := func() {
		if save == "" {
			save = name
		}
		for _, s := range serials {
			if e := db.entries[s]; e != nil {
				e.set = save
			}
		}
		name, save, serials, inSerials = "", "", nil, false
	}
	for line := range lines(sets) {
		if strings.HasPrefix(line, "- ") {
			endSet()
			line = "  " + line[2:]
		}
		switch {
		case strings.HasPrefix(line, "  name: "):
			name, inSerials = yamlString(line[len("  name: "):]), false
		case strings.HasPrefix(line, "  saveName: "):
			save, inSerials = yamlString(line[len("  saveName: "):]), false
		case line == "  serials:":
			inSerials = true
		case inSerials && strings.HasPrefix(line, "    - "):
			serials = append(serials, yamlString(line[len("    - "):]))
		case strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   "):
			inSerials = false
		}
	}
	endSet()
	return db
}

func lines(data []byte) func(func(string) bool) {
	return func(yield func(string) bool) {
		sc := bufio.NewScanner(bytes.NewReader(data))
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for sc.Scan() {
			if !yield(strings.TrimRight(sc.Text(), " \t\r")) {
				return
			}
		}
	}
}

// yamlString reads a scalar: double-quoted with escapes, single-quoted, or plain with an optional
// trailing comment.
func yamlString(s string) string {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasPrefix(s, `"`):
		if end := closingQuote(s); end > 0 {
			if v, err := strconv.Unquote(s[:end+1]); err == nil {
				return v
			}
			return s[1:end]
		}
	case strings.HasPrefix(s, "'"):
		if end := strings.LastIndexByte(s, '\''); end > 0 {
			return strings.ReplaceAll(s[1:end], "''", "'")
		}
	}
	if i := strings.Index(s, " #"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// closingQuote is the index of the quote ending the double-quoted string s starts with.
func closingQuote(s string) int {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i
		}
	}
	return -1
}
