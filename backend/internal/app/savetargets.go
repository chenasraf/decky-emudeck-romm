package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// SaveConfigs reads emulators' own settings for where they keep their saves and what stops those
// from syncing.
type SaveConfigs interface {
	SaveConfig(id string) domain.SaveConfig
	// Files are the settings files SaveConfig reads.
	Files() []string
}

// N64Database is the ROM database RMG names N64 saves from.
type N64Database interface {
	// GoodName returns the database's name of a ROM; known is false when it lacks the ROM.
	GoodName(rom domain.N64ROM) (name string, known bool, err error)
}

// PSXDiscs reads PlayStation discs as DuckStation does to tell their game.
type PSXDiscs interface {
	Disc(path string) (domain.PSXDisc, error)
}

// DuckStationGames are what DuckStation names memory cards after: its game database, and the
// titles and settings the user gave games.
type DuckStationGames interface {
	// Game returns how DuckStation names the memory card of the game it boots from path, whose disc
	// is disc. why says what in DuckStation's settings for the game stops the plugin from naming it.
	Game(path string, disc domain.PSXDisc) (game domain.DuckStationGame, why string, err error)
}

// SaveSources are what save sync reads, besides the Deck's saves, to tell how each game's save is
// named and which emulator writes it.
type SaveSources struct {
	Launchers   Launchers
	Configs     SaveConfigs
	N64         N64Database
	PSX         PSXDiscs
	DuckStation DuckStationGames
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
	// slot is the RomM slot the save syncs in.
	slot string
	// format reads and writes the save, kept at path as RomM keeps it: for a save kept in several
	// files, path names them all, and isn't a file itself.
	format saveFormat
}

// gameIDs are the games whose saves sync: Plugin-owned and Linked ones, by ID.
func (s *Saves) gameIDs(ctx context.Context) ([]int, error) {
	ids, err := s.owned.OwnedIDs(ctx)
	if err != nil {
		return nil, err
	}
	linked, err := s.owned.LinkedGames(ctx)
	if err != nil {
		return nil, err
	}
	for _, g := range linked {
		if !slices.Contains(ids, g.ROMID) {
			ids = append(ids, g.ROMID)
		}
	}
	slices.Sort(ids)
	return ids, nil
}

// placed is where a game whose saves sync is on the Deck: the files of a Plugin-owned game, or the
// file or folder a Linked game was linked for, which is the entry ES-DE lists.
func (s *Saves) placed(ctx context.Context, id int) (platformID int, files []domain.PlacedFile, linked string, why string, err error) {
	rec, ok, err := s.owned.Owned(ctx, id)
	if err != nil {
		return 0, nil, "", "", err
	}
	if ok && len(rec.Files) > 0 {
		return rec.PlatformID, rec.Files, "", "", nil
	}
	link, ok, err := s.owned.Linked(ctx, id)
	if err != nil {
		return 0, nil, "", "", err
	}
	if !ok {
		return 0, nil, "", "Only games the plugin downloaded or linked to RomM sync their saves.", nil
	}
	if _, err := os.Stat(link.Path); err != nil {
		return 0, nil, "", "The file this game was linked to RomM for isn't there any more.", nil
	}
	return link.PlatformID, []domain.PlacedFile{{Path: link.Path}}, link.Path, "", nil
}

// target finds where the save of a Plugin-owned or Linked game is and how RomM tags it. why says
// why the plugin doesn't sync it, empty when it does.
func (s *Saves) target(ctx context.Context, id int) (saveTarget, string, error) {
	return s.resolve(ctx, id, true)
}

// resolve is target, looking for another game that syncs the same memory card when shared is set.
func (s *Saves) resolve(ctx context.Context, id int, shared bool) (saveTarget, string, error) {
	platformID, files, linked, why, err := s.placed(ctx, id)
	if err != nil || why != "" {
		return saveTarget{}, why, err
	}
	rom, ok, err := s.catalog.ROM(ctx, id)
	if err != nil {
		return saveTarget{}, "", err
	}
	name := "Game " + strconv.Itoa(id)
	if ok {
		name = rom.Name
	}
	platform, _, err := s.catalog.Platform(ctx, platformID)
	if err != nil {
		return saveTarget{}, "", err
	}
	system, ok := domain.SystemForSlug(platform.Slug)
	if !ok {
		return saveTarget{}, "EmuDeck has no folder for this game's platform.", nil
	}
	state := s.install.State(false)
	folder, dir := folderHolding(state, system, files[0].Path)
	if dir == "" {
		return saveTarget{}, "The game isn't in one of EmuDeck's ROM folders.", nil
	}
	entry := linked
	if entry == "" {
		if entry, why = gameEntry(files, dir); why != "" {
			return saveTarget{}, why, nil
		}
	}

	rel, err := filepath.Rel(filepath.Join(state.EmuDeck.ROMsPath, folder), entry)
	if err != nil || !filepath.IsLocal(rel) {
		rel = filepath.Base(entry)
	}
	rule, tag, why := s.launch(folder, filepath.ToSlash(rel), state)
	if why != "" {
		return saveTarget{}, why, nil
	}
	saves, why := s.saveDir(state, rule.Emulator)
	if why != "" {
		return saveTarget{}, why, nil
	}
	slot, err := s.slotOf(ctx, id)
	if err != nil {
		return saveTarget{}, "", err
	}
	t := saveTarget{romID: id, name: name, rule: rule, emulator: tag, slot: slot}
	switch rule.Kind {
	case domain.SavePSXCard:
		card, set, why := s.psxCardName(entry, saves)
		if why != "" {
			return saveTarget{}, why, nil
		}
		t.path = filepath.Join(saves, card)
		t.format = psxCard{rawFile{path: t.path}}
		if set && shared {
			owner, err := s.cardOwner(ctx, id, platformID, t.path)
			if err != nil {
				return saveTarget{}, "", err
			}
			if owner != "" {
				return saveTarget{}, fmt.Sprintf("DuckStation keeps one memory card for every disc of this game, and it syncs with %s.", owner), nil
			}
		}
		return t, "", nil
	case domain.SaveN64Files:
		base, why := s.n64Base(entry, saves)
		if why != "" {
			return saveTarget{}, why, nil
		}
		// RomM keeps the files as one zip, named for them.
		t.path, t.format = base+".zip", n64Files{base: base}
		return t, "", nil
	}
	file, why := saveName(entry, rule)
	if why != "" {
		return saveTarget{}, why, nil
	}
	t.path = filepath.Join(saves, file+rule.Ext)
	t.format = rawFile{path: t.path}
	return t, "", nil
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

// saveDir is the folder the emulator keeps its saves in, links followed: the one its own settings
// name, else EmuDeck's. why says why its saves don't sync, its settings having moved them where
// the plugin can't follow.
func (s *Saves) saveDir(state domain.InstallState, e domain.Emulator) (dir, why string) {
	cfg := s.configs.SaveConfig(e.ID)
	switch {
	case cfg.Problem != "":
		return "", cfg.Problem
	case cfg.Dir != "":
		return cfg.Dir, ""
	}
	i := slices.IndexFunc(state.Emulators, func(es domain.EmulatorState) bool { return es.ID == e.ID })
	if i < 0 || !state.Emulators[i].SaveSyncReady() {
		return "", e.Name + "'s saves folder wasn't found. Set it up in EmuDeck, then check Setup status again."
	}
	return state.Emulators[i].Saves[0].Resolved, ""
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
