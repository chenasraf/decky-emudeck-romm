package rpc

import (
	"context"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/rpc/schema"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/app"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

func saveRoutes(s *Server, saves *app.Saves) {
	Register(s, "sync_saves", func(ctx context.Context, p schema.SyncSavesParams) (schema.SyncSavesResult, error) {
		if p.Opened {
			saves.Opened()
			return schema.SyncSavesResult{}, nil
		}
		if err := saves.Start(ctx, p.IDs); err != nil {
			return schema.SyncSavesResult{}, wireError(err)
		}
		return schema.SyncSavesResult{}, nil
	})

	status := func(ctx context.Context, id int) (schema.SaveStatus, error) {
		st, err := saves.Status(ctx, id)
		if err != nil {
			return schema.SaveStatus{}, wireError(err)
		}
		out := schema.SaveStatus{
			ID: st.ROMID, Syncs: st.Syncs, Why: st.Why, Path: st.Path, Emulator: st.Emulator, State: schema.SaveState(st.State),
			Problem: st.Problem, CheckedAt: rfc3339(st.CheckedAt), Syncing: st.Syncing,
		}
		if c := st.Conflict; c != nil {
			out.Conflict = &schema.SaveConflictInfo{ServerSaveID: c.ServerSaveID, ServerAt: rfc3339(c.ServerAt), DeckAt: rfc3339(c.DeckAt), Parked: c.Parked}
		}
		return out, nil
	}

	Register(s, "save_status", func(ctx context.Context, p schema.SaveStatusParams) (schema.SaveStatus, error) {
		return status(ctx, p.ID)
	})

	Register(s, "resolve_save_conflict", func(ctx context.Context, p schema.ResolveSaveConflictParams) (schema.SaveStatus, error) {
		if err := saves.Resolve(ctx, p.ID, domain.SaveChoice(p.Keep), p.ServerSaveID); err != nil {
			return schema.SaveStatus{}, wireError(err)
		}
		return status(ctx, p.ID)
	})

	Register(s, "save_summary", func(ctx context.Context, _ schema.SaveSummaryParams) (schema.SaveSummary, error) {
		counts, last, syncing, err := saves.Summary(ctx)
		if err != nil {
			return schema.SaveSummary{}, wireError(err)
		}
		conflicts, err := saves.Conflicts(ctx)
		if err != nil {
			return schema.SaveSummary{}, wireError(err)
		}
		out := schema.SaveSummary{Counts: toWireCounts(counts), LastCheckedAt: rfc3339(last), Syncing: syncing, Conflicts: []schema.SaveConflictGame{}}
		for _, c := range conflicts {
			out.Conflicts = append(out.Conflicts, schema.SaveConflictGame{ID: c.ROMID, Name: c.Name})
		}
		return out, nil
	})
}

// SaveEvents returns the listener that sends save syncs starting and ending as save_sync
// notifications.
func SaveEvents(s *Server) app.SaveListener {
	return func(e app.SaveSyncEvent) {
		event := schema.SaveSyncEvent{Syncing: e.Syncing, IDs: e.ROMIDs, Counts: toWireCounts(e.Summary), Auto: e.Auto}
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
