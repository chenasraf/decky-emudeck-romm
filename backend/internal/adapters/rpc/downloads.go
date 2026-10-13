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

	Register(s, "list_downloads", func(context.Context, schema.ListDownloadsParams) (schema.ListDownloadsResult, error) {
		out := schema.ListDownloadsResult{Downloads: []schema.DownloadItem{}}
		for _, d := range downloads.List() {
			out.Downloads = append(out.Downloads, toWireDownload(d))
		}
		if err := downloads.Problem(); err != nil {
			out.Problem = problemOf(err)
		}
		return out, nil
	})
}

// DownloadEvents returns a listener that sends each download change as a download notification.
func DownloadEvents(s *Server) app.DownloadListener {
	return func(u app.DownloadUpdate) {
		event := schema.DownloadEvent{
			ID:            u.ROMID,
			Name:          u.Name,
			PlatformID:    u.PlatformID,
			PlatformName:  u.PlatformName,
			State:         schema.GameState(u.State),
			ReceivedBytes: u.Received,
			TotalBytes:    u.Total,
		}
		if u.Problem != nil {
			event.Problem = problemOf(u.Problem)
		}
		if err := s.Notify("download", event); err != nil {
			s.log.Error("send download", "err", err)
		}
	}
}

func toWireDownload(d app.Download) schema.DownloadItem {
	return schema.DownloadItem{
		ID:            d.ROMID,
		Name:          d.Name,
		PlatformID:    d.PlatformID,
		PlatformName:  d.PlatformName,
		State:         schema.GameState(d.State),
		ReceivedBytes: d.Received,
		TotalBytes:    d.Total,
	}
}
