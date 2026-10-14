package domain

import "strings"

// ArchiveNaming is how an emulator names the save of a game it opens from an archive.
type ArchiveNaming int

const (
	// ArchiveNamingUnknown means which name it uses isn't known, so such games' saves don't sync.
	ArchiveNamingUnknown ArchiveNaming = iota
	// ArchiveNamingOuter names the save after the archive.
	ArchiveNamingOuter
	// ArchiveNamingInner names it after the game file inside the archive.
	ArchiveNamingInner
)

// SaveKind is how an emulator keeps a game's save.
type SaveKind int

const (
	// SaveRawFile is one file named after the game's file without its extension, kept on RomM as is.
	SaveRawFile SaveKind = iota
	// SaveN64Files is mupen64plus's files of an N64 save, named after the ROM database's name and
	// the ROM's hash, kept on RomM as the unit N64Save describes.
	SaveN64Files
	// SavePSXCard is a PlayStation memory card DuckStation names after the game's title in its game
	// database, kept on RomM as is.
	SavePSXCard
)

// SaveRule is how an emulator whose saves sync names them and tags them on RomM. Each keeps a game's
// save in the first of its SaveDirs.
type SaveRule struct {
	Emulator Emulator
	Kind     SaveKind
	// Ext is a SaveRawFile's extension.
	Ext      string
	Archives ArchiveNaming
	// Tag is what the emulator's saves are tagged with on RomM. RetroArch's have none of their own:
	// they carry the core's name, as RomM's own clients tag them.
	Tag string
	// esdeVar is the variable ES-DE's commands run the emulator by, and label what its labels call
	// the standalone emulator.
	esdeVar, label string
}

var saveRules = func() []SaveRule {
	rule := func(id string, r SaveRule) SaveRule {
		r.Emulator, _ = EmulatorByID(id)
		return r
	}
	return []SaveRule{
		rule(RetroArchID, SaveRule{Ext: ".srm", esdeVar: "%EMULATOR_RETROARCH%"}),
		rule("melonds", SaveRule{Ext: ".sav", Archives: ArchiveNamingInner, Tag: "melonds", esdeVar: "%EMULATOR_MELONDS%", label: "melonDS (Standalone)"}),
		rule("mgba", SaveRule{Ext: ".sav", Archives: ArchiveNamingOuter, Tag: "mgba", esdeVar: "%EMULATOR_MGBA%", label: "mGBA (Standalone)"}),
		rule("rmg", SaveRule{
			Kind: SaveN64Files, Tag: "rmg", esdeVar: "%EMULATOR_ROSALIES-MUPEN-GUI%", label: "Rosalie's Mupen GUI (Standalone)",
		}),
		rule("duckstation", SaveRule{
			Kind: SavePSXCard, Tag: "duckstation", esdeVar: "%EMULATOR_DUCKSTATION%", label: "DuckStation (Standalone)",
		}),
	}
}()

// SaveRuleFor returns the save rule of the emulator with id, when its saves sync.
func SaveRuleFor(id string) (SaveRule, bool) {
	for _, r := range saveRules {
		if r.Emulator.ID == id {
			return r, true
		}
	}
	return SaveRule{}, false
}

// SaveRules lists every emulator whose saves sync.
func SaveRules() []SaveRule { return saveRules }

// SaveRule returns the save rule of the emulator the launcher runs, when its saves sync: by its
// command when the launcher is Known, else by the label a gamelist named it with.
func (l GameLauncher) SaveRule() (SaveRule, bool) {
	for _, r := range saveRules {
		if l.Known && strings.Contains(l.Command, r.esdeVar) || !l.Known && r.label != "" && strings.Contains(l.Label, r.label) {
			return r, true
		}
	}
	return SaveRule{}, false
}
