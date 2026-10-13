package rpc

import (
	"context"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/rpc/schema"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/app"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

func downloadRoutes(s *Server, downloads *app.Downloads) {
	Register(s, "download_game", func(ctx context.Context, p schema.DownloadGameParams) (schema.DownloadItem, error) {
		d, err := downloads.Queue(ctx, p.ID)
		if err != nil {
			return schema.DownloadItem{}, wireError(err)
		}
		return toWireDownload(d), nil
	})

	Register(s, "cancel_download", func(_ context.Context, p schema.CancelDownloadParams) (schema.CancelDownloadResult, error) {
		return schema.CancelDownloadResult{Cancelled: downloads.Cancel(p.ID)}, nil
	})

	Register(s, "delete_games", func(ctx context.Context, p schema.DeleteGamesParams) (schema.DeleteGamesResult, error) {
		n, err := downloads.Delete(ctx, p.IDs, p.PlatformID)
		if err != nil {
			return schema.DeleteGamesResult{}, wireError(err)
		}
		return schema.DeleteGamesResult{Count: n}, nil
	})

	Register(s, "check_on_deck", func(ctx context.Context, p schema.CheckOnDeckParams) (schema.OnDeckCheck, error) {
		c, err := downloads.CheckOnDeck(ctx, p.ID)
		if err != nil {
			return schema.OnDeckCheck{}, wireError(err)
		}
		return schema.OnDeckCheck{Path: c.Path, Checking: c.Checking, Matches: c.Matches, Why: c.Why, Linked: c.Linked}, nil
	})

	Register(s, "adopt_game", func(ctx context.Context, p schema.AdoptGameParams) (schema.AdoptGameResult, error) {
		if err := downloads.Adopt(ctx, p.ID); err != nil {
			return schema.AdoptGameResult{}, wireError(err)
		}
		return schema.AdoptGameResult{}, nil
	})

	Register(s, "link_game", func(ctx context.Context, p schema.LinkGameParams) (schema.LinkGameResult, error) {
		if err := downloads.Link(ctx, p.ID); err != nil {
			return schema.LinkGameResult{}, wireError(err)
		}
		return schema.LinkGameResult{}, nil
	})

	Register(s, "plan_download_all", func(ctx context.Context, p schema.PlanDownloadAllParams) (schema.DownloadAllPlan, error) {
		plan, err := downloads.PlanAll(ctx, toBatch(p.PlatformID, p.Collection))
		if err != nil {
			return schema.DownloadAllPlan{}, wireError(err)
		}
		return schema.DownloadAllPlan{
			Name:        plan.Name,
			Count:       len(plan.Games),
			TotalBytes:  plan.TotalBytes,
			NeedBytes:   plan.NeedBytes,
			FreeBytes:   max(plan.FreeBytes, 0),
			FreeKnown:   plan.FreeKnown,
			Fits:        plan.Fits(),
			FitCount:    len(plan.Fit),
			FitBytes:    plan.FitBytes,
			Unplaceable: plan.Unplaceable,
		}, nil
	})

	Register(s, "download_all", func(ctx context.Context, p schema.DownloadAllParams) (schema.DownloadAllResult, error) {
		queued, failed, err := downloads.QueueAll(ctx, toBatch(p.PlatformID, p.Collection), p.OnlyWhatFits)
		if err != nil {
			return schema.DownloadAllResult{}, wireError(err)
		}
		return schema.DownloadAllResult{Queued: queued, Failed: failed}, nil
	})

	Register(s, "set_install_to", func(ctx context.Context, p schema.SetInstallToParams) (schema.SetInstallToResult, error) {
		if err := downloads.SetInstallTo(ctx, p.PlatformID, p.Folder); err != nil {
			return schema.SetInstallToResult{}, wireError(err)
		}
		return schema.SetInstallToResult{}, nil
	})

	Register(s, "set_keep_archives", func(ctx context.Context, p schema.SetKeepArchivesParams) (schema.SetKeepArchivesResult, error) {
		if err := downloads.SetKeepArchives(ctx, p.PlatformID, p.Keep); err != nil {
			return schema.SetKeepArchivesResult{}, wireError(err)
		}
		return schema.SetKeepArchivesResult{}, nil
	})

	Register(s, "list_downloads", func(context.Context, schema.ListDownloadsParams) (schema.ListDownloadsResult, error) {
		out := schema.ListDownloadsResult{Downloads: []schema.DownloadItem{}}
		for _, d := range downloads.List() {
			out.Downloads = append(out.Downloads, toWireDownload(d))
		}
		if err := downloads.Problem(); err != nil {
			out.Problem = problemOf(err)
		}
		if err := downloads.Held(); err != nil {
			out.Held = problemOf(err)
		}
		return out, nil
	})
}

func toBatch(platformID int, collection *schema.CollectionRef) app.Batch {
	if collection == nil {
		return app.Batch{PlatformID: platformID}
	}
	ref := fromWireCollection(*collection)
	return app.Batch{Collection: &ref}
}

func fromWireCollection(c schema.CollectionRef) domain.CollectionRef {
	return domain.CollectionRef{Kind: domain.CollectionKind(c.Kind), ID: c.ID}
}

// DownloadListeners returns the listeners that send what the download queue does as download,
// delete, download_hold and ownership notifications.
func DownloadListeners(s *Server) app.DownloadListeners {
	return app.DownloadListeners{Change: downloadEvents(s), Delete: deleteEvents(s), Hold: holdEvents(s), Ownership: ownershipEvents(s)}
}

func ownershipEvents(s *Server) app.OwnershipListener {
	return func(c app.OwnershipChange) {
		event := schema.OwnershipEvent{ID: c.ROMID, Name: c.Name, State: schema.GameState(c.State), Linked: c.Linked}
		if err := s.Notify("ownership", event); err != nil {
			s.log.Error("send ownership", "err", err)
		}
	}
}

func holdEvents(s *Server) app.HoldListener {
	return func(held error) {
		var event schema.DownloadHoldEvent
		if held != nil {
			event.Held = problemOf(held)
		}
		if err := s.Notify("download_hold", event); err != nil {
			s.log.Error("send download hold", "err", err)
		}
	}
}

func downloadEvents(s *Server) app.DownloadListener {
	return func(d app.Download) {
		if err := s.Notify("download", schema.DownloadEvent(toWireDownload(d))); err != nil {
			s.log.Error("send download", "err", err)
		}
	}
}

func deleteEvents(s *Server) app.DeleteListener {
	return func(u app.DeleteUpdate) {
		event := schema.DeleteEvent{ID: u.ROMID, Name: u.Name, State: schema.GameState(u.State)}
		if u.Problem != nil {
			event.Problem = problemOf(u.Problem)
		}
		if err := s.Notify("delete", event); err != nil {
			s.log.Error("send delete", "err", err)
		}
	}
}

func toWireDownload(d app.Download) schema.DownloadItem {
	item := schema.DownloadItem{
		ID:             d.ROMID,
		Name:           d.Name,
		PlatformID:     d.PlatformID,
		PlatformName:   d.PlatformName,
		State:          schema.GameState(d.State),
		ReceivedBytes:  d.Received,
		TotalBytes:     d.Total,
		Converting:     d.Converting,
		WaitingForGame: d.WaitingForGame,
	}
	if d.Problem != nil {
		item.Problem = problemOf(d.Problem)
	}
	return item
}
