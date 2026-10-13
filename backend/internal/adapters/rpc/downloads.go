package rpc

import (
	"context"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/rpc/schema"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/app"
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

// DownloadListeners returns the listeners that send what the download queue does as download,
// delete and download_hold notifications.
func DownloadListeners(s *Server) app.DownloadListeners {
	return app.DownloadListeners{Change: downloadEvents(s), Delete: deleteEvents(s), Hold: holdEvents(s)}
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
		ID:            d.ROMID,
		Name:          d.Name,
		PlatformID:    d.PlatformID,
		PlatformName:  d.PlatformName,
		State:         schema.GameState(d.State),
		ReceivedBytes: d.Received,
		TotalBytes:    d.Total,
		Converting:    d.Converting,
	}
	if d.Problem != nil {
		item.Problem = problemOf(d.Problem)
	}
	return item
}
