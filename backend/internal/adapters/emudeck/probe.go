// Package emudeck finds what EmuDeck and ES-DE installed on this Deck by reading their files. It
// never runs EmuDeck's scripts or changes anything on disk.
package emudeck

import (
	"bufio"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

type Probe struct {
	home string
	proc string
	arch string
	// flatpakRoots are where Flatpak keeps installed apps: the user's installation, where EmuDeck
	// puts them, then the system one.
	flatpakRoots []string
	now          func() time.Time
}

// New returns a probe for the user whose home directory is home.
func New(home string) *Probe {
	return &Probe{
		home:         home,
		proc:         "/proc",
		arch:         runtime.GOARCH,
		flatpakRoots: []string{filepath.Join(home, ".local/share/flatpak/app"), "/var/lib/flatpak/app"},
		now:          time.Now,
	}
}

// Probe looks at everything the plugin's disk features depend on. Whatever it can't find is
// recorded in the result rather than returned as an error.
func (p *Probe) Probe() domain.InstallState {
	state := domain.InstallState{CheckedAt: p.now(), EmuDeck: p.emuDeck(), ESDE: p.esde()}
	ed := state.EmuDeck
	if state.ESDE.ROMDirectory != "" && ed.ROMsPath != "" {
		state.ESDE.ROMDirectoryMatches = sameDir(state.ESDE.ROMDirectory, ed.ROMsPath)
	}

	systems := map[string][]string{}
	for _, s := range domain.Systems {
		emu := s.DefaultEmulator(p.arch)
		if emu != "" {
			systems[emu] = append(systems[emu], s.Folder)
		}
		if !state.EmuDeckFound() {
			continue
		}
		path := filepath.Join(ed.ROMsPath, s.Folder, s.RomSubdir)
		state.Systems = append(state.Systems, domain.SystemState{
			Folder:        s.Folder,
			ROMPath:       path,
			ROMPathExists: isDir(path),
			Emulator:      emu,
		})
	}

	for _, e := range domain.Emulators {
		if len(systems[e.ID]) == 0 && !e.SaveSync() {
			continue
		}
		es := domain.EmulatorState{ID: e.ID, Installed: p.installed(e, ed), Systems: systems[e.ID]}
		for _, dir := range e.SaveDirs {
			sd := domain.SaveDirState{Path: dir}
			if ed.SavesPath != "" {
				sd.Path = filepath.Join(ed.SavesPath, dir)
				sd.Resolved = resolveDir(sd.Path)
			}
			es.Saves = append(es.Saves, sd)
		}
		state.Emulators = append(state.Emulators, es)
	}
	return state
}

// emuDeck finds the Emulation folder the way EmuDeck's own app does: settings.sh first, then the
// storagePath in settings.json, then ~/Emulation. A folder counts only with roms/ in it.
func (p *Probe) emuDeck() domain.EmuDeckState {
	var state domain.EmuDeckState
	config := filepath.Join(p.home, ".config/EmuDeck")

	try := func(source domain.RootSource, root string, keys map[string]string) bool {
		if root == "" {
			return false
		}
		roms := orDefault(keys["romsPath"], filepath.Join(root, "roms"))
		state.Tried = append(state.Tried, roms)
		if !isDir(roms) {
			return false
		}
		state.Root = root
		state.Source = source
		state.ROMsPath = roms
		state.BIOSPath = orDefault(keys["biosPath"], filepath.Join(root, "bios"))
		state.SavesPath = orDefault(keys["savesPath"], filepath.Join(root, "saves"))
		return true
	}

	if data, err := os.ReadFile(filepath.Join(config, "settings.sh")); err == nil {
		keys := parseShellSettings(data, p.home)
		if try(domain.RootFromSettings, keys["emulationPath"], keys) {
			return state
		}
	}
	if data, err := os.ReadFile(filepath.Join(config, "settings.json")); err == nil {
		var app struct {
			StoragePath string `json:"storagePath"`
		}
		if json.Unmarshal(data, &app) == nil && app.StoragePath != "" {
			if try(domain.RootFromAppSettings, filepath.Join(expandHome(unquote(app.StoragePath), p.home), "Emulation"), nil) {
				return state
			}
		}
	}
	try(domain.RootDefault, filepath.Join(p.home, "Emulation"), nil)
	return state
}

// parseShellSettings reads the KEY=value lines of EmuDeck's settings.sh. EmuDeck writes values
// unquoted, but older versions quoted part of a path ("/run/media/deck/SD"/Emulation), and the
// internal-storage paths hold a literal $HOME that bash expands when it sources the file.
func parseShellSettings(data []byte, home string) map[string]string {
	keys := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok || key == "" || strings.HasPrefix(key, "#") || strings.ContainsAny(key, " \t") {
			continue
		}
		value = expandHome(unquote(value), home)
		if value != "" {
			keys[key] = filepath.Clean(value)
		}
	}
	return keys
}

func unquote(s string) string {
	return strings.NewReplacer(`"`, "", `'`, "").Replace(strings.TrimSpace(s))
}

func expandHome(s, home string) string {
	s = strings.NewReplacer("${HOME}", home, "$HOME", home).Replace(s)
	if s == "~" || strings.HasPrefix(s, "~/") {
		s = home + s[1:]
	}
	return s
}

func (p *Probe) esde() domain.ESDEState {
	state := domain.ESDEState{
		MediaDirectory: filepath.Join(p.home, "ES-DE/downloaded_media"),
		Running:        p.esdeRunning(),
	}
	path := filepath.Join(p.home, "ES-DE/settings/es_settings.xml")
	f, err := os.Open(path)
	if err != nil {
		return state
	}
	defer func() { _ = f.Close() }()
	state.SettingsPath = path

	settings := readESDESettings(f)
	if dir := expandHome(settings["MediaDirectory"], p.home); dir != "" {
		state.MediaDirectory = dir
	}
	state.ParseGamelistOnly = settings["ParseGamelistOnly"] == "true"
	// ES-DE reads ~/ROMs until its ROM directory is set, which EmuDeck does when it configures it.
	state.ROMDirectory = filepath.Join(p.home, "ROMs")
	if dir := expandHome(settings["ROMDirectory"], p.home); dir != "" {
		state.ROMDirectory = filepath.Clean(dir)
	}
	return state
}

// sameDir says whether a and b are the same folder once links are followed. A folder that doesn't
// exist is compared as written.
func sameDir(a, b string) bool {
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return filepath.Clean(p)
	}
	return resolve(a) == resolve(b)
}

// readESDESettings reads the name/value pairs of es_settings.xml, whose top level is a list of
// elements like <bool name="ParseGamelistOnly" value="true" /> with no single root element.
func readESDESettings(r io.Reader) map[string]string {
	out := map[string]string{}
	dec := xml.NewDecoder(r)
	dec.Strict = false
	for {
		tok, err := dec.Token()
		if err != nil {
			return out
		}
		el, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		var name, value string
		for _, a := range el.Attr {
			switch a.Name.Local {
			case "name":
				name = a.Value
			case "value":
				value = a.Value
			}
		}
		if name != "" {
			out[name] = value
		}
	}
}

// esdeRunning looks for the ES-DE AppImage, or the binary it mounts, among running processes.
func (p *Probe) esdeRunning() bool {
	entries, err := os.ReadDir(p.proc)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() || strings.TrimLeft(e.Name(), "0123456789") != "" {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join(p.proc, e.Name(), "cmdline"))
		if err != nil || len(cmdline) == 0 {
			continue
		}
		args := strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
		if isESDE(args) {
			return true
		}
	}
	return false
}

func isESDE(args []string) bool {
	if filepath.Base(args[0]) == "es-de" {
		return true
	}
	for _, a := range args {
		if strings.Contains(a, "ES-DE.AppImage") || strings.Contains(a, "/.mount_ES-DE") {
			return true
		}
	}
	return false
}

func (p *Probe) installed(e domain.Emulator, ed domain.EmuDeckState) bool {
	switch e.Kind {
	case domain.InstallFlatpak:
		for _, root := range p.flatpakRoots {
			if exists(filepath.Join(root, e.Location, "current/active")) {
				return true
			}
		}
		return false
	case domain.InstallAppImage, domain.InstallBinary:
		return isFile(filepath.Join(p.home, "Applications", e.Location))
	case domain.InstallProton:
		return ed.ROMsPath != "" && isFile(filepath.Join(ed.ROMsPath, e.Location))
	default:
		return false
	}
}

// resolveDir follows path's symlinks to the folder they lead to, or returns "" when they don't
// lead to one.
func resolveDir(path string) string {
	real, err := filepath.EvalSymlinks(path)
	if err != nil || !isDir(real) {
		return ""
	}
	return real
}

func orDefault(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
