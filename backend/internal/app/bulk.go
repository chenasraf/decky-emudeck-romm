package app

import (
	"context"
	"fmt"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// batchPage is how many games PlanAll reads from the Catalog at a time.
const batchPage = 1000

// BatchPlan is what downloading every missing game of a platform would take.
type BatchPlan struct {
	PlatformName string
	// Games are the missing games the plugin can place, by name.
	Games []domain.ROM
	// TotalBytes is how much the Games download.
	TotalBytes int64
	// NeedBytes is how much room they take while they download; an archive that's extracted counts
	// twice.
	NeedBytes int64
	// FreeBytes is the room left for them once the downloads already queued and the margin are taken
	// off, and FreeKnown says whether the drive could be measured at all. An unmeasured drive doesn't
	// block, as with single downloads.
	FreeBytes int64
	FreeKnown bool
	// Fit are the Games that fit in FreeBytes: each in name order that still fits, so one large game
	// doesn't keep out the smaller ones after it.
	Fit []domain.ROM
	// FitBytes is how much the Fit games download.
	FitBytes int64
	// Unplaceable counts the missing games the plugin has no way to lay out in EmuDeck's folder.
	Unplaceable int
}

// Fits says whether every game fits.
func (p BatchPlan) Fits() bool { return len(p.Fit) == len(p.Games) }

// PlanAll works out what downloading every missing game of a platform takes: the games on RomM that
// aren't on this Deck or in the queue.
func (d *Downloads) PlanAll(ctx context.Context, platformID int) (BatchPlan, error) {
	if d.store == nil {
		return BatchPlan{}, d.problem
	}
	platform, ok, err := d.catalog.Platform(ctx, platformID)
	if err != nil {
		return BatchPlan{}, err
	}
	if !ok {
		return BatchPlan{}, errNotInCatalog
	}
	// A ROM of one file stands in for the platform: whatever fails it fails every game.
	system, dir, err := d.targetDir(domain.ROM{}, platform)
	if err != nil {
		return BatchPlan{}, err
	}
	states, err := d.States(ctx)
	if err != nil {
		return BatchPlan{}, err
	}

	plan := BatchPlan{PlatformName: platform.Name}
	needs := map[int]int64{}
	for offset := 0; ; offset += batchPage {
		page, err := d.catalog.ROMs(ctx, platformID, nil, offset, batchPage)
		if err != nil {
			return BatchPlan{}, err
		}
		for _, rom := range page.ROMs {
			if domain.StateOf(states, rom.ID) != domain.StateOnRomM {
				continue
			}
			if rom.MultiFile && system.MultiFile == "" {
				plan.Unplaceable++
				continue
			}
			need := roomFor(system, rom)
			needs[rom.ID] = need
			plan.Games = append(plan.Games, rom)
			plan.TotalBytes += rom.SizeBytes
			plan.NeedBytes += need
		}
		if len(page.ROMs) < batchPage {
			break
		}
	}

	d.mu.Lock()
	ahead := d.pending()
	d.mu.Unlock()
	plan.FreeBytes, plan.FreeKnown = d.room(dir)
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

// QueueAll queues every missing game of a platform, or with onlyWhatFits those of them that fit,
// and returns how many it queued and how many it couldn't. Without onlyWhatFits, a batch that
// doesn't fit queues nothing.
func (d *Downloads) QueueAll(ctx context.Context, platformID int, onlyWhatFits bool) (queued, failed int, err error) {
	plan, err := d.PlanAll(ctx, platformID)
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
	d.log.Info("batch queued", "platform_id", platformID, "queued", queued, "failed", failed, "only_what_fits", onlyWhatFits)
	return queued, failed, nil
}

// room is the least free space of staging's drive and dir's, and whether either could be measured.
func (d *Downloads) room(dir string) (int64, bool) {
	var least int64
	known := false
	for _, path := range []string{d.install.State(false).EmuDeck.Root, dir} {
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
