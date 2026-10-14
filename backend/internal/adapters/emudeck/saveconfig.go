package emudeck

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// SaveConfigs reads emulators' settings for where they keep their saves and what stops those from
// syncing.
type SaveConfigs struct {
	home string
}

func NewSaveConfigs(home string) *SaveConfigs { return &SaveConfigs{home: home} }

const (
	retroArchConfig = ".var/app/org.libretro.RetroArch/config/retroarch/retroarch.cfg"
	melonDSConfig   = ".var/app/net.kuribo64.melonDS/config/melonDS"
	mGBAConfig      = ".config/mgba/config.ini"
	rmgConfig       = ".var/app/com.github.Rosalie241.RMG/config/RMG/mupen64plus.cfg"
)

// Files are the settings files SaveConfig reads, of every emulator whose saves sync.
func (c *SaveConfigs) Files() []string {
	return []string{
		filepath.Join(c.home, retroArchConfig),
		filepath.Join(c.home, melonDSConfig, "melonDS.toml"),
		filepath.Join(c.home, melonDSConfig, "melonDS.ini"),
		filepath.Join(c.home, mGBAConfig),
		filepath.Join(c.home, rmgConfig),
		filepath.Join(c.home, duckStationData, "settings.ini"),
	}
}

// SaveConfig reads where the emulator with id keeps its saves, and what in its settings stops them
// from syncing. Settings that aren't there leave EmuDeck's saves folder standing.
func (c *SaveConfigs) SaveConfig(id string) domain.SaveConfig {
	switch id {
	case domain.RetroArchID:
		path := filepath.Join(c.home, retroArchConfig)
		if !isFile(path) {
			break
		}
		cfg := readINI(path)[""]
		switch {
		case isTrue(cfg["savefiles_in_content_dir"]):
			return problem("RetroArch writes saves next to the games, so its saves don't sync. Turn off Settings → Saving → \"Write Saves to Content Directory\".")
		case isTrue(cfg["sort_savefiles_enable"]), isTrue(cfg["sort_savefiles_by_content_enable"]):
			return problem("RetroArch sorts saves into folders, so its saves don't sync. Turn off both \"Sort Saves into Folders\" options in Settings → Saving.")
		}
		// RetroArch writes "default" for a folder that isn't set, and then saves next to the game.
		dir, ok := cfg["savefile_directory"]
		if dir = unquote(dir); ok && (dir == "" || dir == "default") {
			return problem("RetroArch has no saves folder set, so it writes saves next to the games and they don't sync. Set Settings → Directory → Save Files.")
		}
		if ok {
			return c.follow("RetroArch", "saves", dir, "")
		}
	case "melonds":
		// melonDS reads its .ini only when it has no .toml.
		var dir string
		var ok bool
		if toml := filepath.Join(c.home, melonDSConfig, "melonDS.toml"); isFile(toml) {
			dir, ok = readINI(toml)["Instance0"]["SaveFilePath"]
		} else if ini := filepath.Join(c.home, melonDSConfig, "melonDS.ini"); isFile(ini) {
			dir, ok = readINI(ini)[""]["SaveFilePath"]
		}
		if dir = unquote(dir); ok && dir == "" {
			return problem("melonDS has no saves folder set, so it writes saves next to the games and they don't sync. Set one in melonDS's Config → Path settings.")
		}
		if ok {
			return c.follow("melonDS", "saves", dir, "")
		}
	case "mgba":
		qt := readINI(filepath.Join(c.home, mGBAConfig))["ports.qt"]
		// mGBA's "Use player N save game" writes <game>.saN for players 2 to 4.
		if n, err := strconv.Atoi(qt["savePlayerId"]); err == nil && n > 1 {
			return problem(fmt.Sprintf("mGBA uses player %d's save files (.sa%d), and only player 1's sync. Pick player 1's save game in mGBA's File menu.", n, n))
		}
		dir, ok := qt["savegamePath"]
		if ok && dir == "" {
			return problem("mGBA has no saves folder set, so it writes saves next to the games and they don't sync. Set one in mGBA's Tools → Settings → Paths.")
		}
		if ok {
			return c.follow("mGBA", "saves", dir, "")
		}
	case "rmg":
		ini := readINI(filepath.Join(c.home, rmgConfig))
		// EmuDeck sets format 1, names from the ROM database and hash; 0 names saves after the
		// cart's header, which games share.
		if v := ini["Rosalie's Mupen GUI Core Overlay"]["SaveFilenameFormat"]; v != "" && v != "1" {
			return problem("RMG names saves after the cart's header, which several games can share, so its saves don't sync. Set Settings → Core → Save filename format back to the ROM database name.")
		}
		dir, ok := ini["Core"]["SaveSRAMPath"]
		if dir = unquote(dir); ok && dir == "" {
			return problem("RMG keeps saves inside its own app folder, so they don't sync. Set its save (SRAM) folder in RMG's settings.")
		}
		if ok {
			return c.follow("RMG", "saves", dir, "")
		}
	case "duckstation":
		data := filepath.Join(c.home, duckStationData)
		path := filepath.Join(data, "settings.ini")
		if !isFile(path) {
			break
		}
		// EmuDeck gives each game a card of its own in slot 1, named after its title, shared by the
		// discs of a game on several.
		cards := readINI(path)["MemoryCards"]
		if t := cards["Card1Type"]; t != "" && t != "PerGameTitle" {
			return problem("DuckStation's memory card in slot 1 isn't the game's own, named by its title, so its saves don't sync. Set Settings → Memory Cards → Card 1 to \"Separate Card Per Game (Title)\".")
		}
		if v := cards["UsePlaylistTitle"]; strings.EqualFold(v, "false") || v == "0" {
			return problem("DuckStation gives each disc of a game its own memory card, so its saves don't sync. Turn Settings → Memory Cards → \"Use Single Card For Multi-Disc Games\" back on.")
		}
		// DuckStation keeps cards in memcards under its data folder unless told otherwise, and reads
		// a relative folder from there too.
		return c.follow("DuckStation", "saves", orDefault(cards["Directory"], "memcards"), data)
	}
	return domain.SaveConfig{}
}

// follow is the folder an emulator's setting names for what it keeps there ("saves", "save
// states"), links followed, a relative one under base.
func (c *SaveConfigs) follow(emulator, what, dir, base string) domain.SaveConfig {
	dir = expandHome(dir, c.home)
	if !filepath.IsAbs(dir) {
		if base == "" {
			return problem(fmt.Sprintf("%s's %s folder, %q, isn't a full path, so its %s don't sync.", emulator, what, dir, what))
		}
		dir = filepath.Join(base, dir)
	}
	if real := resolveDir(dir); real != "" {
		return domain.SaveConfig{Dir: real}
	}
	return problem(fmt.Sprintf("%s keeps %s in %s, which isn't there, so its %s don't sync.", emulator, what, dir, what))
}

// StateConfig reads where the emulator with id keeps its save states, and what in its settings
// stops them from syncing. Settings that aren't there leave EmuDeck's states folder standing.
func (c *SaveConfigs) StateConfig(id string) domain.SaveConfig {
	switch id {
	case domain.RetroArchID:
		path := filepath.Join(c.home, retroArchConfig)
		if !isFile(path) {
			break
		}
		cfg := readINI(path)[""]
		switch {
		case isTrue(cfg["savestates_in_content_dir"]):
			return problem("RetroArch writes save states next to the games, so its states don't sync. Turn off Settings → Saving → \"Write Save States to Content Directory\".")
		case isTrue(cfg["sort_savestates_enable"]), isTrue(cfg["sort_savestates_by_content_enable"]):
			return problem("RetroArch sorts save states into folders, so its states don't sync. Turn off both \"Sort Save States into Folders\" options in Settings → Saving.")
		}
		dir, ok := cfg["savestate_directory"]
		if dir = unquote(dir); ok && (dir == "" || dir == "default") {
			return problem("RetroArch has no save states folder set, so it writes states next to the games and they don't sync. Set Settings → Directory → Save States.")
		}
		if ok {
			return c.follow("RetroArch", "save states", dir, "")
		}
	case "mgba":
		dir, ok := readINI(filepath.Join(c.home, mGBAConfig))["ports.qt"]["savestatePath"]
		if ok && dir == "" {
			return problem("mGBA has no save states folder set, so it writes states next to the games and they don't sync. Set one in mGBA's Tools → Settings → Paths.")
		}
		if ok {
			return c.follow("mGBA", "save states", dir, "")
		}
	}
	return domain.SaveConfig{}
}

func problem(why string) domain.SaveConfig { return domain.SaveConfig{Problem: why} }

func isTrue(v string) bool { return strings.EqualFold(unquote(v), "true") }

// readINI reads an INI file's keys by section, empty when it can't be read.
func readINI(path string) map[string]map[string]string {
	out := map[string]map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer func() { _ = f.Close() }()
	section := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "", strings.HasPrefix(line, ";"), strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			section = strings.TrimSpace(line[1 : len(line)-1])
		default:
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			if out[section] == nil {
				out[section] = map[string]string{}
			}
			out[section][strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return out
}
