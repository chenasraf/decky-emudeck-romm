// Package esde reads how ES-DE launches games: the commands of its system definitions, and the
// alternative emulators its gamelists pick.
package esde

import (
	"debug/elf"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/CalebQ42/squashfs"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// bundledSystems is where ES-DE's AppImage keeps the system definitions it ships with.
const bundledSystems = "usr/share/es-de/resources/systems/linux/es_systems.xml"

// Launchers reads ES-DE's files under home, caching each until it changes on disk.
type Launchers struct {
	home string

	mu        sync.Mutex
	systems   map[string][]domain.ESDECommand
	systemsAt []fileStamp
	gamelists map[string]cachedGamelist
}

func New(home string) *Launchers {
	return &Launchers{home: home, gamelists: map[string]cachedGamelist{}}
}

// Launcher returns the command ES-DE launches a game of system with, rel being the game's path
// under the system's ROM folder. A gamelist's alternative emulator for the game, then for the
// system, wins over the system's first command. When the system definitions can't be read, the
// result isn't Known and carries only the label a gamelist picked, if any.
func (l *Launchers) Launcher(system, rel string) (domain.GameLauncher, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	systems, err := l.loadSystems()
	label := l.gamelist(system).labelFor(rel)
	commands := systems[system]
	if len(commands) == 0 {
		return domain.GameLauncher{ESDECommand: domain.ESDECommand{Label: label}}, err
	}
	if label == "" {
		return domain.GameLauncher{ESDECommand: commands[0], Known: true}, nil
	}
	for _, c := range commands {
		if c.Label == label {
			return domain.GameLauncher{ESDECommand: c, Known: true}, nil
		}
	}
	return domain.GameLauncher{ESDECommand: domain.ESDECommand{Label: label}}, nil
}

// fileStamp tells whether a file changed since it was read.
type fileStamp struct {
	path string
	size int64
	mod  time.Time
}

func stamp(path string) fileStamp {
	s := fileStamp{path: path}
	if info, err := os.Stat(path); err == nil {
		s.size, s.mod = info.Size(), info.ModTime()
	}
	return s
}

// appImages are the names EmuDeck has given ES-DE's AppImage under ~/Applications, newest first.
var appImages = []string{"ES-DE.AppImage", "EmulationStation-DE.AppImage", "EmulationStation-DE-x64_SteamDeck.AppImage"}

func (l *Launchers) appImage() string {
	for _, name := range appImages {
		p := filepath.Join(l.home, "Applications", name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func (l *Launchers) customSystems() string {
	return filepath.Join(l.home, "ES-DE", "custom_systems", "es_systems.xml")
}

// loadSystems returns every system's commands: the AppImage's, replaced system by system by the
// user's custom_systems, which is how ES-DE merges them. l.mu must be held.
func (l *Launchers) loadSystems() (map[string][]domain.ESDECommand, error) {
	image := l.appImage()
	stamps := []fileStamp{stamp(image), stamp(l.customSystems())}
	if l.systems != nil && sameStamps(stamps, l.systemsAt) {
		return l.systems, nil
	}

	out := map[string][]domain.ESDECommand{}
	var problem error
	if image == "" {
		problem = errors.New("ES-DE's AppImage isn't in ~/Applications")
	} else if data, err := readBundled(image); err != nil {
		problem = fmt.Errorf("read ES-DE's systems from %s: %w", image, err)
	} else {
		maps.Copy(out, parseSystems(data))
	}
	if data, err := os.ReadFile(l.customSystems()); err == nil {
		maps.Copy(out, parseSystems(data))
	}
	l.systems, l.systemsAt = out, stamps
	return out, problem
}

func sameStamps(a, b []fileStamp) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].path != b[i].path || a[i].size != b[i].size || !a[i].mod.Equal(b[i].mod) {
			return false
		}
	}
	return true
}

// readBundled reads the system definitions out of an AppImage: an ELF runtime with the
// application's squashfs image appended right after its section headers.
func readBundled(image string) ([]byte, error) {
	f, err := os.Open(image)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	offset, err := squashfsOffset(f)
	if err != nil {
		return nil, err
	}
	r, err := squashfs.NewReaderAtOffset(f, offset)
	if err != nil {
		return nil, fmt.Errorf("open squashfs: %w", err)
	}
	if data, err := r.ReadFile(bundledSystems); err == nil {
		return data, nil
	}
	// An AppImage laid out differently still keeps the file under resources/.
	var found string
	_ = fs.WalkDir(r, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, "resources/systems/linux/es_systems.xml") {
			found = p
			return fs.SkipAll
		}
		return nil
	})
	if found == "" {
		return nil, errors.New("no es_systems.xml in the AppImage")
	}
	return r.ReadFile(found)
}

// squashfsOffset is where an AppImage's squashfs image starts: the end of its runtime's section
// header table.
func squashfsOffset(r io.ReaderAt) (int64, error) {
	ident := make([]byte, elf.EI_NIDENT)
	if _, err := r.ReadAt(ident, 0); err != nil || string(ident[:4]) != elf.ELFMAG {
		return 0, errors.New("not an AppImage: no ELF header")
	}
	var order binary.ByteOrder = binary.LittleEndian
	if elf.Data(ident[elf.EI_DATA]) == elf.ELFDATA2MSB {
		order = binary.BigEndian
	}
	sr := io.NewSectionReader(r, 0, 1<<16)
	switch elf.Class(ident[elf.EI_CLASS]) {
	case elf.ELFCLASS64:
		var hdr elf.Header64
		if err := binary.Read(sr, order, &hdr); err != nil {
			return 0, err
		}
		return int64(hdr.Shoff) + int64(hdr.Shentsize)*int64(hdr.Shnum), nil
	case elf.ELFCLASS32:
		var hdr elf.Header32
		if err := binary.Read(sr, order, &hdr); err != nil {
			return 0, err
		}
		return int64(hdr.Shoff) + int64(hdr.Shentsize)*int64(hdr.Shnum), nil
	}
	return 0, errors.New("not an AppImage: unknown ELF class")
}

// parseSystems reads the commands of each system in an es_systems.xml.
func parseSystems(data []byte) map[string][]domain.ESDECommand {
	var list struct {
		Systems []struct {
			Name     string `xml:"name"`
			Commands []struct {
				Label   string `xml:"label,attr"`
				Command string `xml:",chardata"`
			} `xml:"command"`
		} `xml:"system"`
	}
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	dec.Strict = false
	if err := dec.Decode(&list); err != nil {
		return nil
	}
	out := map[string][]domain.ESDECommand{}
	for _, s := range list.Systems {
		var cmds []domain.ESDECommand
		for _, c := range s.Commands {
			cmds = append(cmds, domain.ESDECommand{Label: strings.TrimSpace(c.Label), Command: strings.TrimSpace(c.Command)})
		}
		if name := strings.TrimSpace(s.Name); name != "" && len(cmds) > 0 {
			out[name] = cmds
		}
	}
	return out
}

// gamelist is what a system's gamelist.xml says about emulators: the system's alternative
// emulator, and each game's, by its path under the system's folder.
type gamelist struct {
	system string
	games  map[string]string
}

type cachedGamelist struct {
	at   fileStamp
	list gamelist
}

func (g gamelist) labelFor(rel string) string {
	if label := g.games[path.Clean(filepath.ToSlash(rel))]; label != "" {
		return label
	}
	return g.system
}

// gamelist reads a system's gamelist, an empty one when there is none. l.mu must be held.
func (l *Launchers) gamelist(system string) gamelist {
	p := filepath.Join(l.home, "ES-DE", "gamelists", system, "gamelist.xml")
	at := stamp(p)
	if c, ok := l.gamelists[p]; ok && sameStamps([]fileStamp{c.at}, []fileStamp{at}) {
		return c.list
	}
	var list gamelist
	if f, err := os.Open(p); err == nil {
		list = parseGamelist(f)
		_ = f.Close()
	}
	l.gamelists[p] = cachedGamelist{at: at, list: list}
	return list
}

// parseGamelist reads a gamelist.xml, whose top level holds an <alternativeEmulator> beside the
// <gameList>, so it has no single root element.
func parseGamelist(r io.Reader) gamelist {
	out := gamelist{games: map[string]string{}}
	dec := xml.NewDecoder(r)
	dec.Strict = false
	var stack []string
	var gamePath, gameAlt string
	for {
		tok, err := dec.Token()
		if err != nil {
			return out
		}
		switch t := tok.(type) {
		case xml.StartElement:
			stack = append(stack, t.Name.Local)
			if t.Name.Local == "game" {
				gamePath, gameAlt = "", ""
			}
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			if t.Name.Local == "game" && gamePath != "" && gameAlt != "" {
				out.games[path.Clean(strings.TrimPrefix(gamePath, "./"))] = gameAlt
			}
		case xml.CharData:
			text := strings.TrimSpace(string(t))
			if text == "" || len(stack) < 2 {
				continue
			}
			parent, el := stack[len(stack)-2], stack[len(stack)-1]
			switch {
			case parent == "alternativeEmulator" && el == "label":
				out.system = text
			case parent == "game" && el == "path":
				gamePath = text
			case parent == "game" && el == "altemulator":
				gameAlt = text
			}
		}
	}
}
