package app

import (
	"context"
	"crypto/md5"  //nolint:gosec // matches files against RomM's MD5, not for security
	"crypto/sha1" //nolint:gosec // matches files against RomM's SHA-1, not for security
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// onDeckWait is how long a check of a file where a game would go waits for its hashing before it
// answers that it's still checking. Hashing a disc image can take longer than a call may.
const onDeckWait = 5 * time.Second

// LinkStore keeps the Linked games.
type LinkStore interface {
	SaveLinked(ctx context.Context, g domain.LinkedGame) error
	Linked(ctx context.Context, id int) (domain.LinkedGame, bool, error)
	LinkedGames(ctx context.Context) ([]domain.LinkedGame, error)
	DeleteLinked(ctx context.Context, id int) error
}

// OwnershipKind is what changed a game's ownership.
type OwnershipKind string

const (
	// OwnershipAdopted: the user Adopted the game's file, now Plugin-owned.
	OwnershipAdopted OwnershipKind = "adopted"
	// OwnershipLinked: the user Linked the game's file to RomM.
	OwnershipLinked OwnershipKind = "linked"
	// OwnershipGone: every file the plugin placed for the game is gone.
	OwnershipGone OwnershipKind = "gone"
	// OwnershipChanged: a file the plugin placed changed or went, so the rest are User-managed.
	OwnershipChanged OwnershipKind = "changed"
)

// OwnershipChange is a game whose ownership changed, and the state it's in now.
type OwnershipChange struct {
	ROMID int
	Name  string
	State domain.GameState
	Kind  OwnershipKind
}

// OwnershipListener hears each change to a game's ownership.
type OwnershipListener func(OwnershipChange)

// OnDeckCheck is how the User-managed file where a game would go compares with the game on RomM.
type OnDeckCheck struct {
	// Path is the file or folder.
	Path string
	// Checking is set while its files are still being hashed; ask again.
	Checking bool
	// Matches is set when its files are RomM's, so Adopt can take them.
	Matches bool
	// Why says why it doesn't match, empty when it does.
	Why    string
	Linked bool
}

// folderScan is what sits where a platform's games would go, as of its ROM folder's modification
// time and the Catalog's last sync, either of which changing makes it stale.
type folderScan struct {
	dir       string
	mod       time.Time
	synced    time.Time
	occupants map[int]string
}

// onDeckJob hashes the files where a game would go, once, for the check and Adopt to share.
type onDeckJob struct {
	done chan struct{}
	path string
	// files are what Adopt records, set when they match.
	files   []adoptFile
	matches bool
	why     string
	err     error
}

// adoptFile is a file on disk and RomM's file it holds.
type adoptFile struct {
	path string
	info os.FileInfo
	file domain.ROMFile
	sums domain.FileSums
}

// onDeck returns where each game with a User-managed file in its way has it, by ROM id. Games the
// plugin owns or queued are left to their own state by the caller.
func (d *Downloads) onDeck(ctx context.Context) (map[int]string, error) {
	out := map[int]string{}
	if !d.install.State(false).EmuDeckFound() {
		return out, nil
	}
	platforms, err := d.catalog.Platforms(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range platforms {
		occupants, err := d.scan(ctx, p.Platform)
		if err != nil {
			d.log.Warn("look for games already on this Deck", "platform", p.Slug, "err", err)
			continue
		}
		maps.Copy(out, occupants)
	}
	if d.store == nil {
		return out, nil
	}
	return out, d.elsewhere(ctx, out)
}

// scan returns where each of a platform's games has something in its way in the ROM folder its
// downloads go in, by ROM id.
func (d *Downloads) scan(ctx context.Context, platform domain.Platform) (map[int]string, error) {
	system, dir, err := d.targetDir(domain.ROM{}, platform)
	if err != nil {
		// No folder for the platform: nothing can be in the way.
		return nil, nil //nolint:nilerr // see above
	}
	info, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_, synced, err := d.catalog.SyncState(ctx)
	if err != nil {
		return nil, err
	}
	d.scanMu.Lock()
	s, ok := d.scans[platform.ID]
	d.scanMu.Unlock()
	if ok && s.dir == dir && s.mod.Equal(info.ModTime()) && s.synced.Equal(synced) {
		return s.occupants, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	folder := domain.NewRomFolder(names)
	occupants := map[int]string{}
	for offset := 0; ; offset += batchPage {
		page, err := d.catalog.ROMs(ctx, platform.ID, nil, domain.SortName, offset, batchPage)
		if err != nil {
			return nil, err
		}
		for _, rom := range page.ROMs {
			if e, ok := folder.Occupant(system, rom); ok {
				occupants[rom.ID] = filepath.Join(dir, e)
			}
		}
		if len(page.ROMs) < batchPage {
			break
		}
	}
	d.scanMu.Lock()
	d.scans[platform.ID] = folderScan{dir: dir, mod: info.ModTime(), synced: synced, occupants: occupants}
	d.scanMu.Unlock()
	return occupants, nil
}

// occupant returns the User-managed file of a game: what sits where it would go, else where Find my
// games found it or the file it was linked for, while that's still there.
func (d *Downloads) occupant(ctx context.Context, rom domain.ROM, platform domain.Platform) (string, bool, error) {
	occupants, err := d.scan(ctx, platform)
	if err != nil {
		return "", false, err
	}
	if path, ok := occupants[rom.ID]; ok {
		return path, true, nil
	}
	if g, ok := d.foundAt(rom.ID); ok {
		return g.Path, true, nil
	}
	g, linked, err := d.store.Linked(ctx, rom.ID)
	if err != nil || !linked {
		return "", false, err
	}
	if _, err := os.Stat(g.Path); err != nil {
		return "", false, nil //nolint:nilerr // a link whose file went has nothing in the way
	}
	return g.Path, true, nil
}

// elsewhere adds to out the User-managed files of games found or linked away from where they'd go.
func (d *Downloads) elsewhere(ctx context.Context, out map[int]string) error {
	d.mu.Lock()
	ids := slices.Collect(maps.Keys(d.found))
	d.mu.Unlock()
	for _, id := range ids {
		if g, ok := d.foundAt(id); ok {
			out[id] = g.Path
		}
	}
	linked, err := d.store.LinkedGames(ctx)
	if err != nil {
		return err
	}
	for _, g := range linked {
		if _, ok := out[g.ROMID]; ok {
			continue
		}
		if _, err := os.Stat(g.Path); err == nil {
			out[g.ROMID] = g.Path
		}
	}
	return nil
}

// errNotOnDeck is a game Adopt or Link was asked for that has nothing in its way.
var errNotOnDeck = &domain.Error{Kind: domain.KindNotFound, Message: "There's no file of that game on this Deck any more."}

// CheckOnDeck compares the User-managed file where a game would go with the game on RomM. Hashing
// runs in the background: while it does, the check comes back Checking, and asking again later
// gets the answer.
func (d *Downloads) CheckOnDeck(ctx context.Context, id int) (OnDeckCheck, error) {
	if d.store == nil {
		return OnDeckCheck{}, d.problem
	}
	j, err := d.onDeckJob(ctx, id)
	if err != nil {
		return OnDeckCheck{}, err
	}
	check := OnDeckCheck{Path: j.path}
	if _, linked, err := d.store.Linked(ctx, id); err == nil {
		check.Linked = linked
	}
	select {
	case <-j.done:
	case <-time.After(onDeckWait):
		check.Checking = true
		return check, nil
	case <-ctx.Done():
		return check, ctx.Err()
	}
	if j.err != nil {
		return check, j.err
	}
	check.Matches, check.Why = j.matches, j.why
	return check, nil
}

// onDeckJob returns the hashing of what sits where a game would go, starting it unless a current
// one runs or ran. One that failed, or whose path or files changed since, starts over.
func (d *Downloads) onDeckJob(ctx context.Context, id int) (*onDeckJob, error) {
	rom, platform, err := d.lookup(ctx, id)
	if err != nil {
		return nil, err
	}
	path, ok, err := d.occupant(ctx, rom, platform)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errNotOnDeck
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if j, ok := d.checks[id]; ok && j.path == path {
		select {
		case <-j.done:
			if j.err == nil && !j.stale() {
				return j, nil
			}
		default:
			return j, nil
		}
	}
	j := &onDeckJob{done: make(chan struct{}), path: path}
	d.checks[id] = j
	// Find my games compared what it found already, and matched it by hash, wherever it lies.
	if g, ok := d.found[id]; ok && g.Path == path {
		j.files, j.matches = g.files, true
		if !j.stale() {
			close(j.done)
			return j, nil
		}
		j.files, j.matches = nil, false
	}
	go func() {
		defer close(j.done)
		j.files, j.matches, j.why, j.err = d.compare(d.base, rom, platform, path)
	}()
	return j, nil
}

// stale says whether any file the job hashed changed since.
func (j *onDeckJob) stale() bool {
	for _, f := range j.files {
		info, err := os.Stat(f.path)
		if err != nil || info.Size() != f.info.Size() || !info.ModTime().Equal(f.info.ModTime()) {
			return true
		}
	}
	return false
}

// compare hashes the files at path against the game on RomM. They match when every file RomM has
// for the game is there, laid out the way the plugin would place it, with RomM's hash; or, for a
// game of one file, when the one file there holds it, as it is or as the only file in an archive,
// which is how RomM hashes a zipped ROM.
func (d *Downloads) compare(ctx context.Context, rom domain.ROM, platform domain.Platform, path string) (files []adoptFile, matches bool, why string, err error) {
	in, err := d.conn.activeSignIn()
	if err != nil {
		return nil, false, "", err
	}
	if in == nil {
		return nil, false, "", errNotSignedIn
	}
	system, dir, err := d.targetDir(rom, platform)
	if err != nil {
		return nil, false, "", err
	}
	server, err := d.dial(in.Server, in.Token).ROMFiles(ctx, rom.ID)
	if err != nil {
		return nil, false, "", d.conn.observe(*in, err)
	}
	game := domain.GameFiles(server)
	name := rom.FileName
	if name == "" {
		name = rom.Name
	}
	layout, err := domain.PlanLayout(system, name, game)
	if err != nil {
		return nil, false, "", err
	}

	var candidates []adoptFile
	if !layout.Zip {
		for _, f := range layout.Files {
			candidates = append(candidates, adoptFile{path: filepath.Join(dir, f.Rel), file: f.File})
		}
	}
	if !allExist(candidates) {
		candidates = nil
		if info, err := os.Stat(path); err == nil && !info.IsDir() && len(game) == 1 {
			candidates = []adoptFile{{path: path, file: game[0]}}
		}
	}
	if len(candidates) == 0 {
		return nil, false, "Its files aren't laid out the way the plugin places this game, so they can't be compared with RomM's.", nil
	}

	for i := range candidates {
		c := &candidates[i]
		if c.info, err = os.Stat(c.path); err != nil {
			return nil, false, "", err
		}
		if c.file.SHA1 == "" && c.file.MD5 == "" && c.file.CRC == "" {
			return nil, false, "RomM has no hash for this game, so the plugin can't tell it's the same file.", nil
		}
		if c.sums, err = d.sumsOf(c.path, c.info); err != nil {
			return nil, false, "", err
		}
		if c.file.Matches(c.sums) {
			continue
		}
		inner, ok, err := innerSums(c.path)
		if err != nil {
			return nil, false, "", err
		}
		if !ok || !c.file.Matches(inner) {
			return nil, false, filepath.Base(c.path) + " is different from RomM's copy.", nil
		}
	}
	return candidates, true, "", nil
}

func allExist(files []adoptFile) bool {
	if len(files) == 0 {
		return false
	}
	for _, f := range files {
		if _, err := os.Stat(f.path); err != nil {
			return false
		}
	}
	return true
}

// innerSums hashes the one file inside the archive at path. ok is false when path isn't an archive
// of exactly one file.
func innerSums(path string) (domain.FileSums, bool, error) {
	if !domain.IsArchive(path) {
		return domain.FileSums{}, false, nil
	}
	members, closeArchive, err := openArchive(path)
	if err != nil {
		// Not an archive the plugin can open: it simply doesn't match.
		return domain.FileSums{}, false, nil //nolint:nilerr // see above
	}
	defer func() { _ = closeArchive() }()
	if len(members) != 1 {
		return domain.FileSums{}, false, nil
	}
	r, err := members[0].open()
	if err != nil {
		return domain.FileSums{}, false, nil //nolint:nilerr // a damaged archive doesn't match
	}
	defer func() { _ = r.Close() }()
	sums, err := sumsOfReader(r)
	if err != nil {
		return domain.FileSums{}, false, fmt.Errorf("hash %s: %w", path, err)
	}
	return sums, true, nil
}

// sumsOf hashes the file at path, or returns the hashes from the last time while it looks the same.
func (d *Downloads) sumsOf(path string, info os.FileInfo) (domain.FileSums, error) {
	d.scanMu.Lock()
	c, ok := d.sums[path]
	d.scanMu.Unlock()
	if ok && c.size == info.Size() && c.mod.Equal(info.ModTime()) {
		return c.sums, nil
	}
	in, err := os.Open(path)
	if err != nil {
		return domain.FileSums{}, err
	}
	defer func() { _ = in.Close() }()
	sums, err := sumsOfReader(in)
	if err != nil {
		return domain.FileSums{}, fmt.Errorf("hash %s: %w", path, err)
	}
	d.scanMu.Lock()
	d.sums[path] = cachedSums{size: info.Size(), mod: info.ModTime(), sums: sums}
	d.scanMu.Unlock()
	return sums, nil
}

func sumsOfReader(r io.Reader) (domain.FileSums, error) {
	sha, md, crc := sha1.New(), md5.New(), crc32.NewIEEE() //nolint:gosec // see imports
	if _, err := io.Copy(io.MultiWriter(sha, md, crc), r); err != nil {
		return domain.FileSums{}, err
	}
	return sumsFrom(crc, md, sha), nil
}

// Adopt makes the User-managed file where a game would go Plugin-owned, once its check found it's
// RomM's game: the plugin records it as if it had downloaded it, and may update and delete it from
// then on. A download of the game that failed on the file is dismissed.
func (d *Downloads) Adopt(ctx context.Context, id int) error {
	if d.store == nil {
		return d.problem
	}
	if _, owned, err := d.store.Owned(ctx, id); err != nil || owned {
		if err != nil {
			return err
		}
		return &domain.Error{Kind: domain.KindInvalidInput, Message: "The plugin already manages that game."}
	}
	d.mu.Lock()
	running := d.find(id)
	busy := running != nil && running.State != domain.StateFailed
	d.mu.Unlock()
	if busy {
		return &domain.Error{Kind: domain.KindInvalidInput, Message: "That game is downloading."}
	}

	j, err := d.onDeckJob(ctx, id)
	if err != nil {
		return err
	}
	select {
	case <-j.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	switch {
	case j.err != nil:
		return j.err
	case !j.matches:
		return &domain.Error{Kind: domain.KindInvalidInput, Message: j.why + " Link it to RomM instead."}
	case j.stale():
		return &domain.Error{Kind: domain.KindChangedOnDisk, Message: "The file changed since it was checked. Open the game again to check it anew."}
	}

	rom, _, err := d.lookup(ctx, id)
	if err != nil {
		return err
	}
	owned := domain.OwnedROM{ROMID: id, PlatformID: rom.PlatformID, InstalledAt: d.now().UTC()}
	for _, f := range j.files {
		owned.Files = append(owned.Files, domain.PlacedFile{
			Path: f.path, SizeBytes: f.info.Size(), ModTime: f.info.ModTime().UTC(), SHA1: f.sums.SHA1,
		})
		owned.Snapshot = append(owned.Snapshot, domain.SnapshotOf(f.file))
	}
	if err := d.store.SaveOwned(ctx, owned); err != nil {
		return fmt.Errorf("record adopted game: %w", err)
	}
	// Plugin-owned games sync their saves as they are; the link has nothing left to do.
	if err := d.store.DeleteLinked(ctx, id); err != nil {
		d.log.Warn("drop link of adopted game", "rom_id", id, "err", err)
	}
	d.mu.Lock()
	delete(d.checks, id)
	delete(d.found, id)
	d.mu.Unlock()
	d.Cancel(id)
	d.log.Info("game adopted", "rom_id", id, "path", j.path)
	d.announceOwnership(OwnershipChange{ROMID: id, Name: rom.Name, State: domain.StateInstalled, Kind: OwnershipAdopted})
	return nil
}

// Link records the User-managed file where a game would go as a Linked game, so its saves sync with
// RomM's entry. The file stays User-managed.
func (d *Downloads) Link(ctx context.Context, id int) error {
	if d.store == nil {
		return d.problem
	}
	rom, platform, err := d.lookup(ctx, id)
	if err != nil {
		return err
	}
	if _, owned, err := d.store.Owned(ctx, id); err != nil || owned {
		if err != nil {
			return err
		}
		return &domain.Error{Kind: domain.KindInvalidInput, Message: "The plugin already manages that game."}
	}
	path, ok, err := d.occupant(ctx, rom, platform)
	if err != nil {
		return err
	}
	if !ok {
		return errNotOnDeck
	}
	if err := d.store.SaveLinked(ctx, domain.LinkedGame{ROMID: id, PlatformID: rom.PlatformID, Path: path, LinkedAt: d.now().UTC()}); err != nil {
		return fmt.Errorf("record link: %w", err)
	}
	d.Cancel(id)
	d.log.Info("game linked", "rom_id", id, "path", path)
	d.announceOwnership(OwnershipChange{ROMID: id, Name: rom.Name, State: domain.StateOnDeck, Kind: OwnershipLinked})
	return nil
}

// Linked returns the link of a Linked game.
func (d *Downloads) Linked(ctx context.Context, id int) (domain.LinkedGame, bool, error) {
	if d.store == nil {
		return domain.LinkedGame{}, false, nil
	}
	return d.store.Linked(ctx, id)
}

func (d *Downloads) announceOwnership(c OwnershipChange) {
	if d.onOwnership != nil {
		d.onOwnership(c)
	}
}
