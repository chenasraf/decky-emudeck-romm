package domain

import (
	"regexp"
	"strings"
)

// RetroArchID is RetroArch's Emulator ID.
const RetroArchID = "retroarch"

// ESDECommand is one of the ways ES-DE can launch a system's games: a <command> of its
// es_systems.xml.
type ESDECommand struct {
	Label   string
	Command string
}

var retroArchCore = regexp.MustCompile(`%CORE_RETROARCH%/([\w.-]+)_libretro\.so`)

// RetroArchCore is the libretro core the command runs in RetroArch, without the _libretro suffix,
// or empty when the command runs another emulator.
func (c ESDECommand) RetroArchCore() string {
	if !strings.Contains(c.Command, "%EMULATOR_RETROARCH%") {
		return ""
	}
	if m := retroArchCore.FindStringSubmatch(c.Command); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

// GameLauncher is the command ES-DE launches a game with, and how it was found.
type GameLauncher struct {
	ESDECommand
	// Known is set when the command came from ES-DE's own files. Unset, only Label may be known: the
	// alternative emulator a gamelist names, which no system definition the plugin could read has.
	Known bool
}

// standaloneMarks are what ES-DE's labels say of a command that runs something other than
// RetroArch.
var standaloneMarks = []string{"(Standalone)", "(Proton)", "(Native)", "Shortcut"}

// NotRetroArch says whether a launcher label alone shows the game runs outside RetroArch.
func (l GameLauncher) NotRetroArch() bool {
	for _, m := range standaloneMarks {
		if strings.Contains(l.Label, m) {
			return true
		}
	}
	return false
}

// DefaultRetroArchCore is the core ES-DE runs a system's games in as EmuDeck sets it up, for the
// systems whose games run in RetroArch by default. It stands in for ES-DE's own files when the
// plugin can't read them.
func DefaultRetroArchCore(folder string) (string, bool) {
	core, ok := defaultCores[folder]
	return core, ok
}

// defaultCores maps each ROM folder to the RetroArch core of its default command: the
// alternative emulator EmuDeck's shipped gamelist names, else ES-DE's first command for it.
var defaultCores = map[string]string{
	"3do": "opera", "amiga": "puae", "amiga1200": "puae", "amiga600": "puae", "amigacd32": "puae",
	"amstradcpc": "cap32", "apple2": "applewin", "apple2gs": "mame", "arcade": "mame", "arcadia": "amiarcadia",
	"arduboy": "arduous", "astrocde": "mame", "atari2600": "stella", "atari5200": "a5200", "atari7800": "prosystem",
	"atari800": "atari800", "atarilynx": "mednafen_lynx", "atarist": "hatari", "atarixe": "atari800",
	"c64": "vice_x64sc", "cdimono1": "same_cdi", "cdtv": "puae", "channelf": "freechaf", "colecovision": "bluemsx",
	"cps": "mame", "cps1": "mame", "cps2": "mame", "cps3": "mame", "crvision": "jollycv", "doom": "prboom",
	"dos": "dosbox_pure", "dreamcast": "flycast", "famicom": "mesen", "fba": "fbalpha2012", "fbneo": "fbneo",
	"fds": "mesen", "fmtowns": "mame", "gameandwatch": "mame", "gamecom": "mame", "gamegear": "genesis_plus_gx",
	"gb": "gambatte", "gba": "mgba", "gbc": "gambatte", "genesis": "genesis_plus_gx", "gx4000": "cap32",
	"intellivision": "freeintv", "j2me": "squirreljme", "lcdgames": "mame", "mame": "mame",
	"mastersystem": "genesis_plus_gx", "megacd": "genesis_plus_gx", "megacdjp": "genesis_plus_gx",
	"megadrive": "genesis_plus_gx", "megadrivejp": "genesis_plus_gx", "megaduck": "sameduck", "mess": "mess2015",
	"model3": "supermodel", "moto": "theodore", "msx": "bluemsx", "msx1": "bluemsx", "msx2": "bluemsx",
	"msxturbor": "bluemsx", "multivision": "gearsystem", "n64": "mupen64plus_next", "n64dd": "parallel_n64",
	"nds": "melondsds", "neogeo": "fbneo", "neogeocd": "neocd", "neogeocdjp": "neocd", "nes": "mesen",
	"ngp": "mednafen_ngp", "ngpc": "mednafen_ngp", "odyssey2": "o2em", "palm": "mu", "pc": "dosbox_pure",
	"pc88": "quasi88", "pc98": "np2kai", "pcengine": "mednafen_pce", "pcenginecd": "mednafen_pce",
	"pcfx": "mednafen_pcfx", "pokemini": "pokemini", "pv1000": "mame", "satellaview": "snes9x",
	"saturn": "mednafen_saturn", "saturnjp": "mednafen_saturn", "sega32x": "picodrive", "sega32xjp": "picodrive",
	"sega32xna": "picodrive", "segacd": "genesis_plus_gx", "sfc": "snes9x", "sg-1000": "genesis_plus_gx",
	"sgb": "mesen-s", "snes": "snes9x", "snesna": "snes9x", "spectravideo": "bluemsx", "sufami": "snes9x",
	"supergrafx": "mednafen_supergrafx", "supervision": "potator", "tg-cd": "mednafen_pce", "tg16": "mednafen_pce",
	"tic80": "tic80", "to8": "theodore", "uzebox": "uzem", "vectrex": "vecx", "vic20": "vice_xvic",
	"videopac": "o2em", "virtualboy": "mednafen_vb", "vsmile": "mame", "wasm4": "wasm4",
	"wonderswan": "mednafen_wswan", "wonderswancolor": "mednafen_wswan", "x1": "x1", "x68000": "px68k",
	"zmachine": "mojozork", "zx81": "81", "zxspectrum": "fuse",
}
