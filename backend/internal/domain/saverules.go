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

// SaveRule is how an emulator whose saves sync names them and tags them on RomM. Each keeps one
// save per game, named after the game's file without its extension, in the first of its SaveDirs.
type SaveRule struct {
	Emulator Emulator
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
