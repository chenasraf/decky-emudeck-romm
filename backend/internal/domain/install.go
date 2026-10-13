package domain

import "time"

// InstallKind is how EmuDeck installs an emulator, which decides where the probe looks for it.
type InstallKind string

const (
	// InstallFlatpak is a Flathub app installed with --user.
	InstallFlatpak InstallKind = "flatpak"
	// InstallAppImage is an AppImage in ~/Applications.
	InstallAppImage InstallKind = "appimage"
	// InstallBinary is a native build unpacked into a folder in ~/Applications.
	InstallBinary InstallKind = "binary"
	// InstallProton is a Windows build run through Proton from inside its ROM folder.
	InstallProton InstallKind = "proton"
)

// Emulator is an emulator EmuDeck installs.
type Emulator struct {
	ID   string
	Name string
	Kind InstallKind
	// Location is the Flatpak app id for InstallFlatpak, the path under ~/Applications for
	// InstallAppImage and InstallBinary, and the path under the ROMs folder for InstallProton. It
	// is the same file EmuDeck's own install check looks at.
	Location string
	// SaveDirs are the folders under EmuDeck's saves folder that save sync reads and writes. Only
	// emulators with save sync have them.
	SaveDirs []string
}

// SaveSync says whether the plugin syncs this emulator's saves.
func (e Emulator) SaveSync() bool { return len(e.SaveDirs) > 0 }

// Emulators lists every emulator a System defaults to, and every one with save sync.
var Emulators = []Emulator{
	{ID: "retroarch", Name: "RetroArch", Kind: InstallFlatpak, Location: "org.libretro.RetroArch", SaveDirs: []string{"retroarch/saves", "retroarch/states"}},
	{ID: "dolphin", Name: "Dolphin", Kind: InstallFlatpak, Location: "org.DolphinEmu.dolphin-emu"},
	{ID: "ppsspp", Name: "PPSSPP", Kind: InstallFlatpak, Location: "org.ppsspp.PPSSPP"},
	{ID: "melonds", Name: "melonDS", Kind: InstallFlatpak, Location: "net.kuribo64.melonDS", SaveDirs: []string{"melonds/saves", "melonds/states"}},
	{ID: "scummvm", Name: "ScummVM", Kind: InstallFlatpak, Location: "org.scummvm.ScummVM"},
	{ID: "xemu", Name: "xemu", Kind: InstallFlatpak, Location: "app.xemu.xemu"},
	{ID: "supermodel", Name: "Supermodel", Kind: InstallFlatpak, Location: "com.supermodel3.Supermodel"},
	{ID: "rmg", Name: "RMG", Kind: InstallFlatpak, Location: "com.github.Rosalie241.RMG", SaveDirs: []string{"RMG/saves", "RMG/states"}},
	{ID: "duckstation", Name: "DuckStation", Kind: InstallAppImage, Location: "DuckStation.AppImage", SaveDirs: []string{"duckstation/saves", "duckstation/states"}},
	{ID: "pcsx2", Name: "PCSX2", Kind: InstallAppImage, Location: "pcsx2-Qt.AppImage"},
	{ID: "armsx2", Name: "ARMSX2", Kind: InstallAppImage, Location: "armsx2.AppImage"},
	{ID: "rpcs3", Name: "RPCS3", Kind: InstallAppImage, Location: "rpcs3.AppImage"},
	{ID: "azahar", Name: "Azahar", Kind: InstallAppImage, Location: "azahar.AppImage"},
	{ID: "cemu", Name: "Cemu", Kind: InstallAppImage, Location: "Cemu.AppImage"},
	{ID: "shadps4", Name: "shadPS4", Kind: InstallAppImage, Location: "Shadps4-qt.AppImage"},
	{ID: "mgba", Name: "mGBA", Kind: InstallAppImage, Location: "mGBA.AppImage", SaveDirs: []string{"mgba/saves", "mgba/states"}},
	{ID: "ryujinx", Name: "Ryujinx", Kind: InstallBinary, Location: "publish/Ryujinx"},
	{ID: "vita3k", Name: "Vita3K", Kind: InstallBinary, Location: "Vita3K/Vita3K"},
	{ID: "xenia", Name: "Xenia", Kind: InstallProton, Location: "xbox360/xenia_canary.exe"},
	{ID: "model2", Name: "Model 2 Emulator", Kind: InstallProton, Location: "model2/emulator_multicpu.exe"},
}

// EmulatorByID returns the Emulator with id.
func EmulatorByID(id string) (Emulator, bool) {
	for _, e := range Emulators {
		if e.ID == id {
			return e, true
		}
	}
	return Emulator{}, false
}

// System is one of EmuDeck's ROM folders that a RomM platform maps to.
type System struct {
	// Folder is the ES-DE system name EmuDeck uses for the folder under roms/.
	Folder string
	// Slugs are the RomM platform slugs whose games go in Folder.
	Slugs []string
	// Emulator is the ID of the emulator EmuDeck sets up for Folder by default. Empty when EmuDeck
	// has none.
	Emulator string
	// EmulatorARM replaces Emulator on arm64, where EmuDeck installs a different build.
	EmulatorARM string
	// RomSubdir is where ROMs go inside Folder, for systems whose emulator keeps its own files at
	// the folder's top level.
	RomSubdir string
	// Archives is what the plugin does with games that arrive as archives; empty means
	// ArchivesOptional.
	Archives ArchivePolicy
}

// DefaultEmulator is the ID of the emulator EmuDeck sets up for s on a CPU of arch, as Go names
// it (runtime.GOARCH).
func (s System) DefaultEmulator(arch string) string {
	if arch == "arm64" && s.EmulatorARM != "" {
		return s.EmulatorARM
	}
	return s.Emulator
}

// ra is RetroArch, the default for every system without a standalone emulator.
const ra = "retroarch"

// Systems maps RomM platforms to EmuDeck's ROM folders, from EmuDeck's roms/systems.txt and RomM's
// platform slugs. A platform missing here has no EmuDeck folder.
var Systems = []System{
	{Folder: "3do", Slugs: []string{"3do"}, Emulator: ra, Archives: ArchivesExtract},
	{Folder: "amstradcpc", Slugs: []string{"acpc"}, Emulator: ra},
	{Folder: "gx4000", Slugs: []string{"amstrad-gx4000"}, Emulator: ra},
	{Folder: "amiga", Slugs: []string{"amiga"}, Emulator: ra},
	{Folder: "amigacd32", Slugs: []string{"amiga-cd32"}, Emulator: ra, Archives: ArchivesExtract},
	{Folder: "cdtv", Slugs: []string{"commodore-cdtv"}, Emulator: ra, Archives: ArchivesExtract},
	{Folder: "android", Slugs: []string{"android"}, Archives: ArchivesExtract},
	{Folder: "apple2", Slugs: []string{"appleii"}, Emulator: ra},
	{Folder: "apple2gs", Slugs: []string{"apple-iigs"}, Emulator: ra},
	{Folder: "macintosh", Slugs: []string{"mac"}, Emulator: ra},
	{Folder: "arcade", Slugs: []string{"arcade"}, Emulator: ra, Archives: ArchivesKeep},
	{Folder: "cps1", Slugs: []string{"cps1"}, Emulator: ra, Archives: ArchivesKeep},
	{Folder: "cps2", Slugs: []string{"cps2"}, Emulator: ra, Archives: ArchivesKeep},
	{Folder: "cps3", Slugs: []string{"cps3"}, Emulator: ra, Archives: ArchivesKeep},
	{Folder: "neogeo", Slugs: []string{"neogeoaes", "neogeomvs"}, Emulator: ra, Archives: ArchivesKeep},
	{Folder: "model2", Slugs: []string{"model2"}, Emulator: "model2", RomSubdir: "roms", Archives: ArchivesKeep},
	{Folder: "model3", Slugs: []string{"model3"}, Emulator: "supermodel", Archives: ArchivesKeep},
	{Folder: "atari2600", Slugs: []string{"atari2600"}, Emulator: ra},
	{Folder: "atari5200", Slugs: []string{"atari5200"}, Emulator: ra},
	{Folder: "atari7800", Slugs: []string{"atari7800"}, Emulator: ra},
	{Folder: "atari800", Slugs: []string{"atari800", "atari8bit"}, Emulator: ra},
	{Folder: "atarixe", Slugs: []string{"atari-xegs"}, Emulator: ra},
	{Folder: "atarijaguar", Slugs: []string{"jaguar"}, Emulator: ra},
	{Folder: "atarijaguarcd", Slugs: []string{"atari-jaguar-cd"}, Emulator: ra, Archives: ArchivesExtract},
	{Folder: "atarilynx", Slugs: []string{"lynx"}, Emulator: ra},
	{Folder: "atarist", Slugs: []string{"atari-st"}, Emulator: ra},
	{Folder: "astrocde", Slugs: []string{"astrocade"}, Emulator: ra},
	{Folder: "arcadia", Slugs: []string{"arcadia-2001"}, Emulator: ra},
	{Folder: "arduboy", Slugs: []string{"arduboy"}, Emulator: ra},
	{Folder: "bbcmicro", Slugs: []string{"bbcmicro"}, Emulator: ra},
	{Folder: "c16", Slugs: []string{"c16", "c-plus-4"}, Emulator: ra},
	{Folder: "c64", Slugs: []string{"c64"}, Emulator: ra},
	{Folder: "vic20", Slugs: []string{"vic-20"}, Emulator: ra},
	{Folder: "colecovision", Slugs: []string{"colecovision"}, Emulator: ra},
	{Folder: "channelf", Slugs: []string{"fairchild-channel-f"}, Emulator: ra},
	{Folder: "coco", Slugs: []string{"trs-80-color-computer"}, Emulator: ra},
	{Folder: "trs-80", Slugs: []string{"trs-80"}, Emulator: ra},
	{Folder: "dragon32", Slugs: []string{"dragon-32-slash-64"}, Emulator: ra},
	{Folder: "dos", Slugs: []string{"dos"}, Emulator: ra, Archives: ArchivesKeep},
	{Folder: "pc88", Slugs: []string{"pc-8800-series"}, Emulator: ra},
	{Folder: "pc98", Slugs: []string{"pc-9800-series"}, Emulator: ra},
	{Folder: "fmtowns", Slugs: []string{"fm-towns"}, Emulator: ra, Archives: ArchivesExtract},
	{Folder: "x68000", Slugs: []string{"sharp-x68000"}, Emulator: ra},
	{Folder: "x1", Slugs: []string{"x1"}, Emulator: ra},
	{Folder: "msx", Slugs: []string{"msx"}, Emulator: ra},
	{Folder: "msx2", Slugs: []string{"msx2", "msx2plus"}, Emulator: ra},
	{Folder: "msxturbor", Slugs: []string{"msx-turbo"}, Emulator: ra},
	{Folder: "dreamcast", Slugs: []string{"dc"}, Emulator: ra, Archives: ArchivesExtract},
	{Folder: "famicom", Slugs: []string{"famicom"}, Emulator: ra},
	{Folder: "fds", Slugs: []string{"fds"}, Emulator: ra},
	{Folder: "nes", Slugs: []string{"nes"}, Emulator: ra},
	{Folder: "snes", Slugs: []string{"snes"}, Emulator: ra},
	{Folder: "sfc", Slugs: []string{"sfam"}, Emulator: ra},
	{Folder: "satellaview", Slugs: []string{"satellaview"}, Emulator: ra},
	{Folder: "sufami", Slugs: []string{"sufami-turbo"}, Emulator: ra},
	{Folder: "gb", Slugs: []string{"gb"}, Emulator: ra},
	{Folder: "gbc", Slugs: []string{"gbc"}, Emulator: ra},
	{Folder: "gba", Slugs: []string{"gba"}, Emulator: ra},
	{Folder: "gameandwatch", Slugs: []string{"g-and-w"}, Emulator: ra},
	{Folder: "pokemini", Slugs: []string{"pokemon-mini"}, Emulator: ra},
	{Folder: "virtualboy", Slugs: []string{"virtualboy"}, Emulator: ra},
	{Folder: "n64", Slugs: []string{"n64"}, Emulator: ra},
	{Folder: "n64dd", Slugs: []string{"64dd"}, Emulator: ra},
	{Folder: "nds", Slugs: []string{"nds", "nintendo-dsi"}, Emulator: "melonds"},
	{Folder: "n3ds", Slugs: []string{"3ds", "new-nintendo-3ds"}, Emulator: "azahar", Archives: ArchivesExtract},
	{Folder: "gc", Slugs: []string{"ngc"}, Emulator: "dolphin", Archives: ArchivesExtract},
	{Folder: "wii", Slugs: []string{"wii"}, Emulator: "dolphin", Archives: ArchivesExtract},
	{Folder: "wiiu", Slugs: []string{"wiiu"}, Emulator: "cemu", RomSubdir: "roms", Archives: ArchivesExtract},
	{Folder: "switch", Slugs: []string{"switch"}, Emulator: "ryujinx", Archives: ArchivesExtract},
	{Folder: "genesis", Slugs: []string{"genesis"}, Emulator: ra},
	{Folder: "mastersystem", Slugs: []string{"sms"}, Emulator: ra},
	{Folder: "gamegear", Slugs: []string{"gamegear"}, Emulator: ra},
	{Folder: "sg-1000", Slugs: []string{"sg1000"}, Emulator: ra},
	{Folder: "sega32x", Slugs: []string{"sega32"}, Emulator: ra},
	{Folder: "segacd", Slugs: []string{"segacd"}, Emulator: ra, Archives: ArchivesExtract},
	{Folder: "saturn", Slugs: []string{"saturn"}, Emulator: ra, Archives: ArchivesExtract},
	{Folder: "neogeocd", Slugs: []string{"neo-geo-cd"}, Emulator: ra, Archives: ArchivesExtract},
	{Folder: "ngp", Slugs: []string{"neo-geo-pocket"}, Emulator: ra},
	{Folder: "ngpc", Slugs: []string{"neo-geo-pocket-color"}, Emulator: ra},
	{Folder: "odyssey2", Slugs: []string{"odyssey-2"}, Emulator: ra},
	{Folder: "videopac", Slugs: []string{"videopac-g7400"}, Emulator: ra},
	{Folder: "intellivision", Slugs: []string{"intellivision"}, Emulator: ra},
	{Folder: "vectrex", Slugs: []string{"vectrex"}, Emulator: ra},
	{Folder: "tg16", Slugs: []string{"tg16"}, Emulator: ra},
	{Folder: "tg-cd", Slugs: []string{"turbografx-cd"}, Emulator: ra, Archives: ArchivesExtract},
	{Folder: "supergrafx", Slugs: []string{"supergrafx"}, Emulator: ra},
	{Folder: "pcfx", Slugs: []string{"pc-fx"}, Emulator: ra, Archives: ArchivesExtract},
	{Folder: "cdimono1", Slugs: []string{"philips-cd-i"}, Emulator: ra, Archives: ArchivesExtract},
	{Folder: "psx", Slugs: []string{"psx"}, Emulator: "duckstation", Archives: ArchivesExtract},
	{Folder: "ps2", Slugs: []string{"ps2"}, Emulator: "pcsx2", EmulatorARM: "armsx2", Archives: ArchivesExtract},
	{Folder: "ps3", Slugs: []string{"ps3"}, Emulator: "rpcs3", Archives: ArchivesExtract},
	{Folder: "ps4", Slugs: []string{"ps4"}, Emulator: "shadps4", Archives: ArchivesExtract},
	{Folder: "psp", Slugs: []string{"psp"}, Emulator: "ppsspp", Archives: ArchivesExtract},
	{Folder: "psvita", Slugs: []string{"psvita"}, Emulator: "vita3k", Archives: ArchivesExtract},
	{Folder: "xbox", Slugs: []string{"xbox"}, Emulator: "xemu", Archives: ArchivesExtract},
	{Folder: "xbox360", Slugs: []string{"xbox360"}, Emulator: "xenia", RomSubdir: "roms", Archives: ArchivesExtract},
	{Folder: "wonderswan", Slugs: []string{"wonderswan"}, Emulator: ra},
	{Folder: "wonderswancolor", Slugs: []string{"wonderswan-color"}, Emulator: ra},
	{Folder: "supervision", Slugs: []string{"supervision"}, Emulator: ra},
	{Folder: "megaduck", Slugs: []string{"mega-duck-slash-cougar-boy"}, Emulator: ra},
	{Folder: "multivision", Slugs: []string{"multivision"}, Emulator: ra},
	{Folder: "pv1000", Slugs: []string{"casio-pv-1000"}, Emulator: ra},
	{Folder: "crvision", Slugs: []string{"creativision"}, Emulator: ra},
	{Folder: "gamecom", Slugs: []string{"game-dot-com"}, Emulator: ra},
	{Folder: "vsmile", Slugs: []string{"vsmile"}, Emulator: ra},
	{Folder: "oric", Slugs: []string{"oric"}, Emulator: ra},
	{Folder: "samcoupe", Slugs: []string{"sam-coupe"}, Emulator: ra},
	{Folder: "spectravideo", Slugs: []string{"spectravideo"}, Emulator: ra},
	{Folder: "moto", Slugs: []string{"thomson-mo5", "thomson-to"}, Emulator: ra},
	{Folder: "ti99", Slugs: []string{"ti-99"}, Emulator: ra},
	{Folder: "zxspectrum", Slugs: []string{"zxs"}, Emulator: ra},
	{Folder: "zx81", Slugs: []string{"zx81"}, Emulator: ra},
	{Folder: "uzebox", Slugs: []string{"uzebox"}, Emulator: ra},
	{Folder: "j2me", Slugs: []string{"j2me"}, Emulator: ra},
	{Folder: "palm", Slugs: []string{"palm-os"}, Emulator: ra},
	{Folder: "symbian", Slugs: []string{"symbian"}, Emulator: ra},
	{Folder: "pico8", Slugs: []string{"pico"}, Emulator: ra},
	{Folder: "tic80", Slugs: []string{"tic-80"}, Emulator: ra},
	{Folder: "wasm4", Slugs: []string{"wasm-4"}, Emulator: ra},
	{Folder: "zmachine", Slugs: []string{"z-machine"}, Emulator: ra},
	{Folder: "scummvm", Slugs: []string{"scummvm"}, Emulator: "scummvm", Archives: ArchivesExtract},
	{Folder: "openbor", Slugs: []string{"openbor"}, Emulator: ra},
	{Folder: "mugen", Slugs: []string{"mugen"}, Emulator: ra},
	{Folder: "doom", Slugs: []string{"doom"}, Emulator: ra},
}

// InstallState is what the plugin found on disk the last time it looked.
type InstallState struct {
	CheckedAt time.Time
	EmuDeck   EmuDeckState
	ESDE      ESDEState
	// Systems has one entry per System, in the same order. Empty when EmuDeck wasn't found.
	Systems []SystemState
	// Emulators has one entry for each emulator a System defaults to on this CPU, and each one with
	// save sync, in the order of Emulators.
	Emulators []EmulatorState
}

// EmuDeckFound says whether the plugin found EmuDeck's Emulation folder. Without it the plugin
// has nowhere to place ROMs, BIOS or saves.
func (s InstallState) EmuDeckFound() bool { return s.EmuDeck.Root != "" }

// RootSource is where the probe found EmuDeck's Emulation folder.
type RootSource string

const (
	// RootFromSettings is ~/.config/EmuDeck/settings.sh, which EmuDeck's scripts read.
	RootFromSettings RootSource = "settings_sh"
	// RootFromAppSettings is the storagePath in ~/.config/EmuDeck/settings.json, which EmuDeck's
	// app writes.
	RootFromAppSettings RootSource = "settings_json"
	// RootDefault is ~/Emulation.
	RootDefault RootSource = "default"
)

type EmuDeckState struct {
	// Root is the Emulation folder, empty when none of the places EmuDeck uses has one with a roms
	// folder in it.
	Root      string
	Source    RootSource
	ROMsPath  string
	BIOSPath  string
	SavesPath string
	// Tried lists every folder the probe looked at, for the status page to show when none worked.
	Tried []string
}

type ESDEState struct {
	// SettingsPath is ES-DE's es_settings.xml, empty when it doesn't exist.
	SettingsPath string
	// MediaDirectory is where ES-DE reads game media from.
	MediaDirectory string
	// ROMDirectory is the folder ES-DE reads systems from: its own setting, or its default when
	// unset. Empty when ES-DE's settings weren't found.
	ROMDirectory string
	// ROMDirectoryMatches says whether ROMDirectory is EmuDeck's ROMs folder, links followed. When
	// it isn't, ES-DE never shows the games the plugin places.
	ROMDirectoryMatches bool
	// ParseGamelistOnly is ES-DE's "only show games from gamelist files" option, which hides games
	// the plugin adds until ES-DE's gamelists list them.
	ParseGamelistOnly bool
	Running           bool
}

type SystemState struct {
	Folder string
	// ROMPath is the folder ROMs for this system go in.
	ROMPath string
	// ROMPathExists is false until EmuDeck or the plugin creates the folder.
	ROMPathExists bool
	// Emulator is the ID of the emulator EmuDeck sets up for this system on this CPU.
	Emulator string
}

type EmulatorState struct {
	ID        string
	Installed bool
	// Systems are the folders of the Systems this emulator is the default for on this CPU.
	Systems []string
	// Saves has one entry per Emulator.SaveDirs.
	Saves []SaveDirState
}

// SaveSyncReady says whether save sync can read and write every save folder of the emulator.
func (e EmulatorState) SaveSyncReady() bool {
	for _, s := range e.Saves {
		if s.Resolved == "" {
			return false
		}
	}
	return len(e.Saves) > 0
}

type SaveDirState struct {
	// Path is the folder under EmuDeck's saves folder.
	Path string
	// Resolved is where Path leads once symlinks are followed, empty when it doesn't lead to a
	// folder: the emulator's setup never ran on this install, or a link dangles.
	Resolved string
}
