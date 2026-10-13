package rpc

import (
	"context"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/rpc/schema"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/app"
)

func biosRoutes(s *Server, bios *app.BIOS) {
	Register(s, "platform_bios", func(ctx context.Context, p schema.PlatformBIOSParams) (schema.PlatformBIOS, error) {
		status, err := bios.Status(ctx, p.PlatformID)
		if err != nil {
			return schema.PlatformBIOS{}, wireError(err)
		}
		out := schema.PlatformBIOS{PlatformID: status.PlatformID, InEmulator: status.InEmulator, Downloading: status.Downloading, Files: []schema.BIOSFile{}}
		for _, f := range status.Files {
			out.Files = append(out.Files, schema.BIOSFile{
				FileName:  f.FileName,
				SizeBytes: f.SizeBytes,
				Path:      f.Path,
				Status:    schema.BIOSStatus(f.Status),
				Verified:  f.Verified,
				Owned:     f.Owned,
			})
		}
		return out, nil
	})

	Register(s, "download_bios", func(ctx context.Context, p schema.DownloadBIOSParams) (schema.DownloadBIOSResult, error) {
		n, err := bios.Download(ctx, p.PlatformID)
		if err != nil {
			return schema.DownloadBIOSResult{}, wireError(err)
		}
		return schema.DownloadBIOSResult{Count: n}, nil
	})

	Register(s, "get_preferences", func(context.Context, schema.GetPreferencesParams) (schema.Preferences, error) {
		return schema.Preferences{AutoBIOS: bios.AutoDownload()}, nil
	})

	Register(s, "set_preferences", func(_ context.Context, p schema.SetPreferencesParams) (schema.Preferences, error) {
		if p.AutoBIOS != nil {
			if err := bios.SetAutoDownload(*p.AutoBIOS); err != nil {
				return schema.Preferences{}, wireError(err)
			}
		}
		return schema.Preferences{AutoBIOS: bios.AutoDownload()}, nil
	})
}

// BIOSEvents returns the listener that sends BIOS downloads starting and ending as bios
// notifications.
func BIOSEvents(s *Server) app.BIOSListener {
	return func(u app.BIOSUpdate) {
		event := schema.BIOSEvent{
			PlatformID:   u.PlatformID,
			PlatformName: u.PlatformName,
			Auto:         u.Auto,
			Done:         u.Done,
			Files:        u.Files,
			Downloaded:   u.Downloaded,
		}
		if u.Problem != nil {
			event.Problem = problemOf(u.Problem)
		}
		if err := s.Notify("bios", event); err != nil {
			s.log.Error("send bios", "err", err)
		}
	}
}
