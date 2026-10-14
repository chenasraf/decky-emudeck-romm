package app

import (
	"context"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// findPage is how many ROMs Find my games asks the Server for at once. Each carries its file list.
const findPage = 200

// FindStatus is how Find my games stands for a platform.
type FindStatus struct {
	PlatformID int
	// Running is set while it hashes the platform's folder.
	Running bool
	// Hashed and Total count the files it hashes, the ones of a size one of RomM's has.
	Hashed, Total int
	// Found are the games it found that are still neither Plugin-owned nor Linked.
	Found []FoundGame
	// Problem says why the last run stopped short, nil when it finished.
	Problem error
	// Done is set once a run has finished, so Found is its answer.
	Done bool
	// Adopting is set while Adopt all runs, which has adopted or failed Adopted of AdoptTotal games.
	Adopting            bool
	Adopted, AdoptTotal int
}

// FoundGame is a game of RomM's that Find my games found on this Deck, matched by content.
type FoundGame struct {
	ROMID int
	Name  string
	// Path is the file or folder holding it.
	Path string
}

// FindListener hears Find my games start, make progress and end.
type FindListener func(FindStatus)

// foundGame is a found game and the files that hold it, for Adopt to record.
type foundGame struct {
	FoundGame
	platformID int
	files      []adoptFile
}

// serverFile is one of RomM's game files and the ROM it belongs to.
type serverFile struct {
	romID int
	file  domain.ROMFile
}

// FindGames starts Find my games for a platform, unless it runs already, and returns how it stands.
// It hashes the files in the folder the platform's games download to and matches them against
// RomM's, so the games already on this Deck, under RomM's names or others, show as On Deck (not
// managed) for the user to Adopt or Link. It never changes a file or a record.
func (d *Downloads) FindGames(ctx context.Context, platformID int) (FindStatus, error) {
	if d.store == nil {
		return FindStatus{}, d.problem
	}
	if in, err := d.conn.activeSignIn(); err != nil || in == nil {
		if err != nil {
			return FindStatus{}, err
		}
		return FindStatus{}, errNotSignedIn
	}
	platform, ok, err := d.catalog.Platform(ctx, platformID)
	if err != nil {
		return FindStatus{}, err
	}
	if !ok {
		return FindStatus{}, &domain.Error{Kind: domain.KindNotFound, Message: "That platform is no longer on RomM. Refresh the library."}
	}
	if _, _, err := d.targetDir(domain.ROM{}, platform); err != nil {
		return FindStatus{}, err
	}

	d.mu.Lock()
	if s := d.finds[platformID]; s != nil && (s.Running || s.Adopting) {
		d.mu.Unlock()
		return d.FindStatus(ctx, platformID), nil
	}
	d.finds[platformID] = &FindStatus{PlatformID: platformID, Running: true}
	d.mu.Unlock()

	d.log.Info("find my games started", "platform", platform.Slug)
	d.announceFind(ctx, platformID)
	go d.runFind(platform)
	return d.FindStatus(ctx, platformID), nil
}

// FindStatus returns how Find my games stands for a platform. Found leaves out the games adopted or
// linked since, and the ones whose files went.
func (d *Downloads) FindStatus(ctx context.Context, platformID int) FindStatus {
	d.mu.Lock()
	s := FindStatus{PlatformID: platformID}
	if cur := d.finds[platformID]; cur != nil {
		s = *cur
	}
	var found []foundGame
	for _, g := range d.found {
		if g.platformID == platformID {
			found = append(found, g)
		}
	}
	d.mu.Unlock()

	s.Found = []FoundGame{}
	for _, g := range found {
		if !d.stillFound(ctx, g) {
			continue
		}
		s.Found = append(s.Found, g.FoundGame)
	}
	slices.SortFunc(s.Found, func(a, b FoundGame) int { return strings.Compare(a.Name, b.Name) })
	return s
}

// stillFound says whether a found game is still the user's, there, and neither adopted nor linked.
func (d *Downloads) stillFound(ctx context.Context, g foundGame) bool {
	if _, err := os.Stat(g.Path); err != nil {
		return false
	}
	if _, owned, err := d.store.Owned(ctx, g.ROMID); err != nil || owned {
		return false
	}
	_, linked, err := d.store.Linked(ctx, g.ROMID)
	return err == nil && !linked
}

func (d *Downloads) announceFind(ctx context.Context, platformID int) {
	if d.onFind != nil {
		d.onFind(d.FindStatus(ctx, platformID))
	}
}

// runFind runs Find my games for platform, recording what it finds and how it ended.
func (d *Downloads) runFind(platform domain.Platform) {
	found, err := d.findIn(d.base, platform)
	d.mu.Lock()
	for id, g := range d.found {
		if g.platformID == platform.ID {
			delete(d.found, id)
		}
	}
	for _, g := range found {
		d.found[g.ROMID] = g
		// A check made before compared what sat where the game would go, not what was found.
		delete(d.checks, g.ROMID)
	}
	s := d.finds[platform.ID]
	s.Running, s.Done, s.Problem = false, err == nil, err
	d.mu.Unlock()

	if err != nil {
		d.log.Warn("find my games", "platform", platform.Slug, "err", err)
	} else {
		d.log.Info("find my games done", "platform", platform.Slug, "found", len(found))
	}
	d.announceFind(d.base, platform.ID)
}

// findIn hashes the files in platform's download folder and matches them against RomM's files.
func (d *Downloads) findIn(ctx context.Context, platform domain.Platform) ([]foundGame, error) {
	in, err := d.conn.activeSignIn()
	if err != nil {
		return nil, err
	}
	if in == nil {
		return nil, errNotSignedIn
	}
	_, dir, err := d.targetDir(domain.ROM{}, platform)
	if err != nil {
		return nil, err
	}
	server := d.dial(in.Server, in.Token)

	// Files that already sit where a game would go are looked at first, so a game kept both there
	// and under another name is found where it belongs. Their names alone prove nothing: they're
	// found only when their content matches, like any other file.
	occupants, err := d.scan(ctx, platform)
	if err != nil {
		return nil, err
	}
	inPlace := map[string]bool{}
	for _, p := range occupants {
		inPlace[p] = true
	}
	// The plugin's games aren't looked for, and their files aren't taken for another game's.
	taken := map[string]bool{}
	skip := func(id int) bool {
		if _, owned, err := d.store.Owned(ctx, id); err != nil || owned {
			return true
		}
		if _, linked, err := d.store.Linked(ctx, id); err != nil || linked {
			return true
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.find(id) != nil || d.deleting[id]
	}
	if err := d.takenByRecords(ctx, taken); err != nil {
		return nil, err
	}

	games, err := platformGames(ctx, server, platform.ID, skip)
	if err != nil {
		return nil, d.conn.observe(*in, err)
	}
	byHash, sizes := indexGames(games)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	type entry struct {
		path  string
		files []string
	}
	var work []entry
	total := 0
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if strings.HasPrefix(e.Name(), ".") || taken[path] {
			continue
		}
		files, err := candidateFiles(path, sizes)
		if err != nil {
			d.log.Warn("find my games: read", "path", path, "err", err)
			continue
		}
		if len(files) > 0 {
			work = append(work, entry{path: path, files: files})
			total += len(files)
		}
	}
	slices.SortStableFunc(work, func(a, b entry) int {
		switch {
		case inPlace[a.path] == inPlace[b.path]:
			return 0
		case inPlace[a.path]:
			return -1
		}
		return 1
	})
	d.findProgress(ctx, platform.ID, 0, total)

	var out []foundGame
	claimed := map[int]bool{}
	hashed := 0
	last := time.Now()
	for _, w := range work {
		// The files in this entry holding each of RomM's files, by ROM id, then by file id. Files
		// of the same content, such as a disc's identical audio tracks, each hold all of them.
		hits := map[int]map[int][]adoptFile{}
		for _, path := range w.files {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			for _, h := range d.matchFile(path, byHash) {
				if hits[h.romID] == nil {
					hits[h.romID] = map[int][]adoptFile{}
				}
				hits[h.romID][h.file.file.ID] = append(hits[h.romID][h.file.file.ID], h.file)
			}
			hashed++
			if time.Since(last) >= progressEvery {
				last = time.Now()
				d.findProgress(ctx, platform.ID, hashed, total)
			}
		}
		if g, ok := d.wholeGame(ctx, w.path, hits, games, claimed); ok {
			g.platformID = platform.ID
			claimed[g.ROMID] = true
			out = append(out, g)
		}
	}
	d.findProgress(ctx, platform.ID, hashed, total)
	return out, nil
}

// platformGames returns the game files of each of a platform's ROMs on the Server, by ROM id,
// leaving out the ROMs skip says to and those with no game files.
func platformGames(ctx context.Context, server DownloadServer, platformID int, skip func(id int) bool) (map[int][]domain.ROMFile, error) {
	games := map[int][]domain.ROMFile{}
	for offset := 0; ; offset += findPage {
		page, err := server.PlatformFiles(ctx, platformID, offset, findPage)
		if err != nil {
			return nil, err
		}
		for _, r := range page {
			game := domain.GameFiles(r.Files)
			if len(game) == 0 || skip(r.ROMID) {
				continue
			}
			games[r.ROMID] = game
		}
		if len(page) < findPage {
			return games, nil
		}
	}
}

// indexGames indexes games' hashed files by hashKey, and returns the sizes they come in.
func indexGames(games map[int][]domain.ROMFile) (map[string][]serverFile, map[int64]bool) {
	byHash := map[string][]serverFile{}
	sizes := map[int64]bool{}
	for id, game := range games {
		for _, f := range game {
			if !f.Hashed() {
				continue
			}
			key := hashKey(f)
			byHash[key] = append(byHash[key], serverFile{romID: id, file: f})
			sizes[f.SizeBytes] = true
		}
	}
	return byHash, sizes
}

// takenByRecords marks the files of every Plugin-owned and Linked game as taken.
func (d *Downloads) takenByRecords(ctx context.Context, taken map[string]bool) error {
	ids, err := d.store.OwnedIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		rec, ok, err := d.store.Owned(ctx, id)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		for _, f := range rec.Files {
			taken[f.Path] = true
		}
	}
	linked, err := d.store.LinkedGames(ctx)
	if err != nil {
		return err
	}
	for _, g := range linked {
		taken[g.Path] = true
	}
	return nil
}

func (d *Downloads) findProgress(ctx context.Context, platformID, hashed, total int) {
	d.mu.Lock()
	if s := d.finds[platformID]; s != nil {
		s.Hashed, s.Total = hashed, total
	}
	d.mu.Unlock()
	d.announceFind(ctx, platformID)
}

// hashKey is the strongest hash the Server has for f, which is the one a match is decided by.
func hashKey(f domain.ROMFile) string {
	switch {
	case f.SHA1 != "":
		return "sha1:" + strings.ToLower(f.SHA1)
	case f.MD5 != "":
		return "md5:" + strings.ToLower(f.MD5)
	default:
		return "crc:" + strings.TrimLeft(strings.ToLower(f.CRC), "0")
	}
}

// keysOf are the keys sums is found under in an index built with hashKey.
func keysOf(sums domain.FileSums) []string {
	return []string{
		"sha1:" + strings.ToLower(sums.SHA1),
		"md5:" + strings.ToLower(sums.MD5),
		"crc:" + strings.TrimLeft(strings.ToLower(sums.CRC), "0"),
	}
}

// candidateFiles returns the files under path worth hashing: those of a size one of RomM's files
// has, and archives, whose contents may be. A folder is walked whole.
func candidateFiles(path string, sizes map[int64]bool) ([]string, error) {
	var out []string
	err := filepath.WalkDir(path, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !e.Type().IsRegular() {
			return nil
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if sizes[info.Size()] || domain.IsArchive(p) {
			out = append(out, p)
		}
		return nil
	})
	return out, err
}

// fileHit is a file on disk holding one of RomM's files.
type fileHit struct {
	romID int
	file  adoptFile
}

// matchFile hashes the file at path, and the one file inside it when it's an archive of one, and
// returns RomM's files it holds. A file that can't be read matches nothing.
func (d *Downloads) matchFile(path string, byHash map[string][]serverFile) []fileHit {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	var hits []fileHit
	lookup := func(sums, own domain.FileSums) {
		for _, key := range keysOf(sums) {
			for _, s := range byHash[key] {
				if s.file.Matches(sums) {
					hits = append(hits, fileHit{romID: s.romID, file: adoptFile{path: path, info: info, file: s.file, sums: own}})
				}
			}
		}
	}
	sums, err := d.sumsOf(path, info)
	if err != nil {
		d.log.Warn("find my games: hash", "path", path, "err", err)
		return nil
	}
	lookup(sums, sums)
	if len(hits) == 0 {
		if inner, ok, err := innerSums(path); err == nil && ok {
			lookup(inner, sums)
		}
	}
	return hits
}

// wholeGame picks the game an entry holds every game file of, if any. A game more than one entry
// holds goes to the first.
func (d *Downloads) wholeGame(ctx context.Context, path string, hits map[int]map[int][]adoptFile, games map[int][]domain.ROMFile, claimed map[int]bool) (foundGame, bool) {
	ids := make([]int, 0, len(hits))
	for id := range hits {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		game := games[id]
		if claimed[id] || len(hits[id]) != len(game) {
			continue
		}
		files, ok := assignFiles(game, hits[id])
		if !ok {
			continue
		}
		rom, _, err := d.lookup(ctx, id)
		if err != nil {
			continue
		}
		return foundGame{FoundGame: FoundGame{ROMID: id, Name: rom.Name, Path: path}, files: files}, true
	}
	return foundGame{}, false
}

// assignFiles gives each of game's files a file on disk of its own from those holding its content,
// the one of the same name first. ok is unset when there are too few to go round.
func assignFiles(game []domain.ROMFile, hits map[int][]adoptFile) ([]adoptFile, bool) {
	files := make([]adoptFile, len(game))
	used := map[string]bool{}
	pick := func(i int, sameName bool) {
		for _, c := range hits[game[i].ID] {
			if used[c.path] || (sameName && filepath.Base(c.path) != path.Base(game[i].Name)) {
				continue
			}
			files[i], used[c.path] = c, true
			return
		}
	}
	for _, sameName := range []bool{true, false} {
		for i := range game {
			if files[i].path == "" {
				pick(i, sameName)
			}
		}
	}
	for _, f := range files {
		if f.path == "" {
			return nil, false
		}
	}
	return files, true
}

// AdoptFailure is a found game Adopt all couldn't adopt, and why.
type AdoptFailure struct {
	Name string
	Err  error
}

// AdoptFound adopts every game Find my games found on a platform and still lists, returning how
// many it adopted and the ones it couldn't. One failing doesn't stop the rest.
func (d *Downloads) AdoptFound(ctx context.Context, platformID int) (int, []AdoptFailure) {
	games := d.FindStatus(ctx, platformID).Found
	d.adoptProgress(ctx, platformID, true, 0, len(games))
	adopted := 0
	var failed []AdoptFailure
	last := time.Now()
	for i, g := range games {
		if err := ctx.Err(); err != nil {
			failed = append(failed, AdoptFailure{Name: g.Name, Err: err})
		} else if err := d.Adopt(ctx, g.ROMID); err != nil {
			failed = append(failed, AdoptFailure{Name: g.Name, Err: err})
		} else {
			adopted++
		}
		if time.Since(last) >= progressEvery {
			last = time.Now()
			d.adoptProgress(ctx, platformID, true, i+1, len(games))
		}
	}
	d.log.Info("adopted found games", "platform_id", platformID, "adopted", adopted, "failed", len(failed))
	d.adoptProgress(ctx, platformID, false, len(games), len(games))
	return adopted, failed
}

func (d *Downloads) adoptProgress(ctx context.Context, platformID int, adopting bool, done, total int) {
	d.mu.Lock()
	if d.finds[platformID] == nil {
		d.finds[platformID] = &FindStatus{PlatformID: platformID}
	}
	s := d.finds[platformID]
	s.Adopting, s.Adopted, s.AdoptTotal = adopting, done, total
	d.mu.Unlock()
	d.announceFind(ctx, platformID)
}

// foundAt returns where Find my games found a game, while it's still there.
func (d *Downloads) foundAt(id int) (foundGame, bool) {
	d.mu.Lock()
	g, ok := d.found[id]
	d.mu.Unlock()
	if !ok {
		return foundGame{}, false
	}
	if _, err := os.Stat(g.Path); err != nil {
		return foundGame{}, false
	}
	return g, true
}
