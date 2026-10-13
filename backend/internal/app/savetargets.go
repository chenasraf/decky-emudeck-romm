package app

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// SaveConfigs reads emulators' own settings for what stops their saves from syncing.
type SaveConfigs interface {
	// SaveProblem says why the saves of the emulator with id don't sync as the plugin names them,
	// empty when nothing in its settings stops them.
	SaveProblem(id string) string
}

// saveTarget is a game whose save the plugin syncs.
type saveTarget struct {
	romID int
	name  string
	path  string
	// rule is how the emulator that plays the game names its save, and emulator the tag RomM keeps
	// the save under.
	rule     domain.SaveRule
	emulator string
}

// target finds where a Plugin-owned game's save is and how RomM tags it. why says why the plugin
// doesn't sync it, empty when it does.
func (s *Saves) target(ctx context.Context, id int) (saveTarget, string, error) {
	rec, ok, err := s.owned.Owned(ctx, id)
	if err != nil {
		return saveTarget{}, "", err
	}
	if !ok || len(rec.Files) == 0 {
		return saveTarget{}, "Only games the plugin downloaded sync their saves.", nil
	}
	rom, ok, err := s.catalog.ROM(ctx, id)
	if err != nil {
		return saveTarget{}, "", err
	}
	name := "Game " + strconv.Itoa(id)
	if ok {
		name = rom.Name
	}
	platform, _, err := s.catalog.Platform(ctx, rec.PlatformID)
	if err != nil {
		return saveTarget{}, "", err
	}
	system, ok := domain.SystemForSlug(platform.Slug)
	if !ok {
		return saveTarget{}, "EmuDeck has no folder for this game's platform.", nil
	}
	state := s.install.State(false)
	folder, dir := folderHolding(state, system, rec.Files[0].Path)
	if dir == "" {
		return saveTarget{}, "The game isn't in one of EmuDeck's ROM folders.", nil
	}
	entry, why := gameEntry(rec.Files, dir)
	if why != "" {
		return saveTarget{}, why, nil
	}

	rel, err := filepath.Rel(filepath.Join(state.EmuDeck.ROMsPath, folder), entry)
	if err != nil || !filepath.IsLocal(rel) {
		rel = filepath.Base(entry)
	}
	rule, tag, why := s.launch(folder, filepath.ToSlash(rel), state)
	if why != "" {
		return saveTarget{}, why, nil
	}
	if why := s.configs.SaveProblem(rule.Emulator.ID); why != "" {
		return saveTarget{}, why, nil
	}
	saves, ok := saveDir(state, rule.Emulator.ID)
	if !ok {
		return saveTarget{}, rule.Emulator.Name + "'s saves folder wasn't found. Set it up in EmuDeck, then check Setup status again.", nil
	}
	file, why := saveName(entry, rule)
	if why != "" {
		return saveTarget{}, why, nil
	}
	return saveTarget{romID: id, name: name, path: filepath.Join(saves, file+rule.Ext), rule: rule, emulator: tag}, "", nil
}

// saveName is the name an emulator gives the save of the game at entry, without its extension.
func saveName(entry string, rule domain.SaveRule) (string, string) {
	stem := func(name string) string { return strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)) }
	if !domain.IsArchive(entry) {
		return stem(entry), ""
	}
	switch rule.Archives {
	case domain.ArchiveNamingOuter:
		return stem(entry), ""
	case domain.ArchiveNamingInner:
		members, closeArchive, err := openArchive(entry)
		if err != nil {
			return "", "The game's archive couldn't be read to tell its save's name."
		}
		defer func() { _ = closeArchive() }()
		if len(members) != 1 {
			return "", fmt.Sprintf("The game's archive holds %d files, so which one %s names its save after can't be told.", len(members), rule.Emulator.Name)
		}
		return stem(members[0].Name), ""
	}
	return "", fmt.Sprintf("Saves of games kept as archives don't sync with %s: which name it gives them isn't known.", rule.Emulator.Name)
}

// launch is the emulator ES-DE runs the game at rel in folder with, and the tag RomM keeps its saves
// under: from ES-DE's own files, else EmuDeck's default for the folder. why says why the game's saves
// don't sync.
func (s *Saves) launch(folder, rel string, state domain.InstallState) (domain.SaveRule, string, string) {
	launcher, err := s.launchers.Launcher(folder, rel)
	if err != nil {
		s.log.Debug("ES-DE launcher", "system", folder, "err", err)
	}
	if rule, ok := launcher.SaveRule(); ok {
		if rule.Emulator.ID != domain.RetroArchID {
			return rule, rule.Tag, ""
		}
		if core := launcher.RetroArchCore(); core != "" {
			return rule, core, ""
		}
	}
	if launcher.Known || launcher.NotRetroArch() {
		return domain.SaveRule{}, "", fmt.Sprintf("ES-DE runs this game with %s, whose saves don't sync.", launcher.Label)
	}
	// EmuDeck's default for the system, which is what ES-DE runs unless told otherwise.
	i := slices.IndexFunc(state.Systems, func(s domain.SystemState) bool { return s.Folder == folder })
	emulator := domain.RetroArchID
	if i >= 0 && state.Systems[i].Emulator != "" {
		emulator = state.Systems[i].Emulator
	}
	rule, ok := domain.SaveRuleFor(emulator)
	if !ok {
		name := emulator
		if e, ok := domain.EmulatorByID(emulator); ok {
			name = e.Name
		}
		return domain.SaveRule{}, "", fmt.Sprintf("This game runs in %s, whose saves don't sync.", name)
	}
	if emulator != domain.RetroArchID {
		return rule, rule.Tag, ""
	}
	if core, ok := domain.DefaultRetroArchCore(folder); ok {
		return rule, core, ""
	}
	return domain.SaveRule{}, "", "Which RetroArch core runs this game can't be told."
}

// saveDir is the folder the emulator with id keeps its saves in, links followed.
func saveDir(state domain.InstallState, id string) (string, bool) {
	i := slices.IndexFunc(state.Emulators, func(e domain.EmulatorState) bool { return e.ID == id })
	if i < 0 || !state.Emulators[i].SaveSyncReady() {
		return "", false
	}
	return state.Emulators[i].Saves[0].Resolved, true
}

// folderHolding is the ROM folder of system's that path is in, by its folder name and path.
func folderHolding(state domain.InstallState, system domain.System, path string) (string, string) {
	for _, f := range system.Folders() {
		if dir := folderPath(state, system, f); dir != "" && strings.HasPrefix(path, dir+string(filepath.Separator)) {
			return f, dir
		}
	}
	return "", ""
}

// launchExts are the files an emulator loads a game of several by, in the order the plugin picks
// them when a game's folder entry holds several files.
var launchExts = []string{".m3u", ".cue", ".gdi", ".ccd", ".chd"}

// gameEntry is the path of what ES-DE lists for a game whose files are in dir: its file, or the
// folder holding them. why says why it can't be told.
func gameEntry(files []domain.PlacedFile, dir string) (string, string) {
	tops := map[string]bool{}
	for _, f := range files {
		rel, err := filepath.Rel(dir, f.Path)
		if err != nil || !filepath.IsLocal(rel) {
			return "", "The game's files aren't where the plugin placed them."
		}
		top, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
		tops[top] = true
	}
	if len(tops) == 1 {
		for top := range tops {
			return filepath.Join(dir, top), ""
		}
	}
	for _, ext := range launchExts {
		var hits []string
		for top := range tops {
			if strings.EqualFold(filepath.Ext(top), ext) {
				hits = append(hits, top)
			}
		}
		if len(hits) == 1 {
			return filepath.Join(dir, hits[0]), ""
		}
	}
	return "", "The game is several files, and which one its emulator loads, and so names its save after, can't be told."
}

// runs says whether the emulator with id is running a game.
func (s *Saves) runs(id string) bool {
	emu, ok := s.running()
	return ok && emu.ID == id
}

// closeFirst is the error of a change to a save its emulator would undo when the game closes.
func closeFirst(e domain.Emulator) error {
	return &domain.Error{
		Kind:    domain.KindInvalidInput,
		Message: fmt.Sprintf("Close %s first: it writes the save it holds when the game closes, which would undo this.", e.Name),
	}
}
