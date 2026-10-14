package emudeck

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SaveConfigs reads emulators' settings for what stops their saves from syncing.
type SaveConfigs struct {
	home string
}

func NewSaveConfigs(home string) *SaveConfigs { return &SaveConfigs{home: home} }

// SaveProblem says why the saves of the emulator with id don't sync as the plugin names them, empty
// when nothing in its settings stops them.
func (c *SaveConfigs) SaveProblem(id string) string {
	switch id {
	case "mgba":
		// mGBA's "Use player N save game" writes <game>.saN for players 2 to 4.
		ini := readINI(filepath.Join(c.home, ".config", "mgba", "config.ini"))
		if n, err := strconv.Atoi(ini["ports.qt"]["savePlayerId"]); err == nil && n > 1 {
			return fmt.Sprintf("mGBA uses player %d's save files (.sa%d), and only player 1's sync. Pick player 1's save game in mGBA's File menu.", n, n)
		}
	case "rmg":
		// EmuDeck sets format 1, names from the ROM database and hash; 0 names saves after the
		// cart's header, which games share.
		ini := readINI(filepath.Join(c.home, ".var", "app", "com.github.Rosalie241.RMG", "config", "RMG", "mupen64plus.cfg"))
		if v := ini["Rosalie's Mupen GUI Core Overlay"]["SaveFilenameFormat"]; v != "" && v != "1" {
			return "RMG names saves after the cart's header, which several games can share, so its saves don't sync. Set Settings → Core → Save filename format back to the ROM database name."
		}
	}
	return ""
}

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
