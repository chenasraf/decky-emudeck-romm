package app

import (
	"context"
	"fmt"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// batchPage is how many games PlanAll reads from the Catalog at a time.
const batchPage = 1000

// Batch names the games Download all missing looks at: a platform's, or with Collection set a
// collection's, which can span platforms.
type Batch struct {
	PlatformID int
	Collection *domain.CollectionRef
}

// BatchPlan is what downloading every missing game of a Batch would take.
type BatchPlan struct {
	// Name is the platform's or the collection's.
	Name string
	// Games are the missing games the plugin can place, by name.
	Games []domain.ROM
	// TotalBytes is how much the Games download.
	TotalBytes int64
	// NeedBytes is how much room they take while they download; an archive that's extracted counts
	// twice.
	NeedBytes int64
	// FreeBytes is the room left for them once the downloads already queued and the margin are taken
	// off, and FreeKnown says whether any drive could be measured at all. An unmeasured drive doesn't
	// block, as with single downloads. When the games go to folders on several drives, the fullest
	// one counts for all of them, so a plan never promises room a drive doesn't have.
	FreeBytes int64
	FreeKnown bool
	// Fit are the Games that fit in FreeBytes: each in name order that still fits, so one large game
	// doesn't keep out the smaller ones after it.
	Fit []domain.ROM
	// FitBytes is how much the Fit games download.
	FitBytes int64
	// Unplaceable counts the missing games the plugin has no way to place: their platform has no
	// EmuDeck folder, or no layout for a game of several files.
	Unplaceable int
}

// Fits says whether every game fits.
func (p BatchPlan) Fits() bool { return len(p.Fit) == len(p.Games) }

// PlanAll works out what downloading every missing game of a batch takes: the games on RomM that
// aren't on this Deck or in the queue.
func (d *Downloads) PlanAll(ctx context.Context, b Batch) (BatchPlan, error) {
	if d.store == nil {
		return BatchPlan{}, d.problem
	}
	// A ROM of one file stands in for every game: what fails it fails them all. A platform batch
	// checks its platform's folder too.
	var platform domain.Platform
	if b.Collection == nil {
		p, ok, err := d.catalog.Platform(ctx, b.PlatformID)
		if err != nil {
			return BatchPlan{}, err
		}
		if !ok {
			return BatchPlan{}, errNotInCatalog
		}
		platform = p
	}
	if _, _, err := d.targetDir(domain.ROM{}, platform); err != nil && (b.Collection == nil || !d.install.State(false).EmuDeckFound()) {
		return BatchPlan{}, err
	}
	name, roms, err := d.batchROMs(ctx, b)
	if err != nil {
		return BatchPlan{}, err
	}
	states, err := d.States(ctx)
	if err != nil {
		return BatchPlan{}, err
	}

	plan := BatchPlan{Name: name}
	needs := map[int]int64{}
	dirs := map[string]bool{}
	platforms := map[int]domain.Platform{}
	for _, rom := range roms {
		if domain.StateOf(states, rom.ID) != domain.StateOnRomM {
			continue
		}
		platform, ok := platforms[rom.PlatformID]
		if !ok {
			if platform, _, err = d.catalog.Platform(ctx, rom.PlatformID); err != nil {
				return BatchPlan{}, err
			}
			platforms[rom.PlatformID] = platform
		}
		system, dir, err := d.targetDir(rom, platform)
		if err != nil {
			plan.Unplaceable++
			continue
		}
		dirs[dir] = true
		need := roomFor(system, rom)
		needs[rom.ID] = need
		plan.Games = append(plan.Games, rom)
		plan.TotalBytes += rom.SizeBytes
		plan.NeedBytes += need
	}

	d.mu.Lock()
	ahead := d.pending()
	d.mu.Unlock()
	paths := []string{d.install.State(false).EmuDeck.Root}
	for dir := range dirs {
		paths = append(paths, dir)
	}
	plan.FreeBytes, plan.FreeKnown = d.room(paths...)
	plan.FreeBytes -= ahead + spaceMargin
	left := plan.FreeBytes
	for _, rom := range plan.Games {
		if plan.FreeKnown && needs[rom.ID] > left {
			continue
		}
		left -= needs[rom.ID]
		plan.Fit = append(plan.Fit, rom)
		plan.FitBytes += rom.SizeBytes
	}
	return plan, nil
}

// batchROMs returns a batch's name and its games, by name.
func (d *Downloads) batchROMs(ctx context.Context, b Batch) (string, []domain.ROM, error) {
	if b.Collection != nil {
		col, ok, err := d.catalog.Collection(ctx, *b.Collection)
		if err != nil {
			return "", nil, err
		}
		if !ok {
			return "", nil, errNotInCatalog
		}
		roms, err := d.catalog.ROMsByID(ctx, col.ROMIDs)
		return col.Name, roms, err
	}

	platform, ok, err := d.catalog.Platform(ctx, b.PlatformID)
	if err != nil {
		return "", nil, err
	}
	if !ok {
		return "", nil, errNotInCatalog
	}
	var roms []domain.ROM
	for offset := 0; ; offset += batchPage {
		page, err := d.catalog.ROMs(ctx, b.PlatformID, nil, offset, batchPage)
		if err != nil {
			return "", nil, err
		}
		roms = append(roms, page.ROMs...)
		if len(page.ROMs) < batchPage {
			return platform.Name, roms, nil
		}
	}
}

// QueueAll queues every missing game of a batch, or with onlyWhatFits those of them that fit, and
// returns how many it queued and how many it couldn't. Without onlyWhatFits, a batch that doesn't
// fit queues nothing.
func (d *Downloads) QueueAll(ctx context.Context, b Batch, onlyWhatFits bool) (queued, failed int, err error) {
	plan, err := d.PlanAll(ctx, b)
	if err != nil {
		return 0, 0, err
	}
	games := plan.Fit
	if !onlyWhatFits {
		if !plan.Fits() {
			return 0, 0, &domain.Error{
				Kind:    domain.KindNoSpace,
				Message: fmt.Sprintf("Needs %s, %s free.", domain.FormatSize(plan.NeedBytes), domain.FormatSize(max(plan.FreeBytes, 0))),
			}
		}
		games = plan.Games
	}
	for _, rom := range games {
		_, err := d.Queue(ctx, rom.ID)
		switch {
		case err == nil:
			queued++
		case queued == 0 && failed == 0 && kindOf(err) == domain.KindSignedOut:
			// Nothing in the batch would queue.
			return 0, 0, err
		default:
			failed++
			d.log.Warn("queue from batch", "rom_id", rom.ID, "err", err)
		}
	}
	d.log.Info("batch queued", "name", plan.Name, "queued", queued, "failed", failed, "only_what_fits", onlyWhatFits)
	return queued, failed, nil
}

// room is the least free space of the drives paths are on, and whether any could be measured.
func (d *Downloads) room(paths ...string) (int64, bool) {
	var least int64
	known := false
	for _, path := range paths {
		free, err := d.freeAt(path)
		if err != nil {
			d.log.Warn("measure free space", "path", path, "err", err)
			continue
		}
		if !known || free < least {
			least, known = free, true
		}
	}
	return least, known
}
