package rpc

import (
	"context"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/rpc/schema"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/app"
)

func saveRoutes(s *Server, saves *app.Saves) {
	Register(s, "sync_saves", func(ctx context.Context, p schema.SyncSavesParams) (schema.SyncSavesResult, error) {
		if err := saves.Start(ctx, p.IDs); err != nil {
			return schema.SyncSavesResult{}, wireError(err)
		}
		return schema.SyncSavesResult{}, nil
	})

	Register(s, "save_status", func(ctx context.Context, p schema.SaveStatusParams) (schema.SaveStatus, error) {
		st, err := saves.Status(ctx, p.ID)
		if err != nil {
			return schema.SaveStatus{}, wireError(err)
		}
		return schema.SaveStatus{
			ID: st.ROMID, Syncs: st.Syncs, Why: st.Why, Path: st.Path, Emulator: st.Emulator, State: schema.SaveState(st.State),
			Problem: st.Problem, CheckedAt: rfc3339(st.CheckedAt), Syncing: st.Syncing,
		}, nil
	})

	Register(s, "save_summary", func(ctx context.Context, _ schema.SaveSummaryParams) (schema.SaveSummary, error) {
		counts, last, syncing, err := saves.Summary(ctx)
		if err != nil {
			return schema.SaveSummary{}, wireError(err)
		}
		return schema.SaveSummary{Counts: toWireCounts(counts), LastCheckedAt: rfc3339(last), Syncing: syncing}, nil
	})
}

// SaveEvents returns the listener that sends save syncs starting and ending as save_sync
// notifications.
func SaveEvents(s *Server) app.SaveListener {
	return func(e app.SaveSyncEvent) {
		event := schema.SaveSyncEvent{Syncing: e.Syncing, IDs: e.ROMIDs, Counts: toWireCounts(e.Summary)}
		if e.Problem != nil {
			event.Problem = problemOf(e.Problem)
		}
		if err := s.Notify("save_sync", event); err != nil {
			s.log.Error("send save sync", "err", err)
		}
	}
}

func toWireCounts(c app.SaveSummary) schema.SaveCounts {
	return schema.SaveCounts{
		Games: c.Games, Uploaded: c.Uploaded, Downloaded: c.Downloaded, Conflicts: c.Conflicts, Waiting: c.Waiting, Failed: c.Failed,
	}
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
