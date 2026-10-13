package rpc

import (
	"context"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/rpc/schema"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/app"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

const (
	defaultGamesPage = 50
	maxGamesPage     = 200
	defaultSearch    = 50
	maxSearch        = 100
)

func catalogRoutes(s *Server, catalog *app.Catalog, downloads *app.Downloads) {
	Register(s, "catalog_status", func(ctx context.Context, _ schema.CatalogStatusParams) (schema.CatalogStatus, error) {
		status, err := catalog.Status(ctx)
		if err != nil {
			return schema.CatalogStatus{}, wireError(err)
		}
		return toWireCatalog(status), nil
	})

	Register(s, "refresh_catalog", func(ctx context.Context, p schema.RefreshCatalogParams) (schema.CatalogStatus, error) {
		catalog.Refresh(p.Force)
		status, err := catalog.Status(ctx)
		if err != nil {
			return schema.CatalogStatus{}, wireError(err)
		}
		return toWireCatalog(status), nil
	})

	Register(s, "list_platforms", func(ctx context.Context, _ schema.ListPlatformsParams) (schema.ListPlatformsResult, error) {
		platforms, err := catalog.Platforms(ctx)
		if err != nil {
			return schema.ListPlatformsResult{}, wireError(err)
		}
		out := schema.ListPlatformsResult{Platforms: []schema.PlatformSummary{}}
		for _, p := range platforms {
			out.Platforms = append(out.Platforms, toWirePlatform(p))
		}
		return out, nil
	})

	Register(s, "list_games", func(ctx context.Context, p schema.ListGamesParams) (schema.ListGamesResult, error) {
		limit := p.Limit
		if limit <= 0 {
			limit = defaultGamesPage
		}
		filter := domain.ROMFilter(p.Filter)
		if filter == "" {
			filter = domain.FilterAll
		}
		platform, page, err := catalog.Games(ctx, p.PlatformID, filter, max(p.Offset, 0), min(limit, maxGamesPage))
		if err != nil {
			return schema.ListGamesResult{}, wireError(err)
		}
		out := schema.ListGamesResult{
			PlatformName: platform.Name,
			Games:        []schema.GameSummary{},
			Total:        page.Total,
		}
		for _, r := range page.ROMs {
			out.Games = append(out.Games, schema.GameSummary{
				ID:        r.ID,
				Name:      r.Name,
				SizeBytes: r.SizeBytes,
				Regions:   nonNil(r.Regions),
				State:     schema.GameState(domain.StateOf(page.States, r.ID)),
			})
		}
		return out, nil
	})

	Register(s, "search_games", func(ctx context.Context, p schema.SearchGamesParams) (schema.SearchGamesResult, error) {
		limit := p.Limit
		if limit <= 0 {
			limit = defaultSearch
		}
		result, err := catalog.Search(ctx, domain.SearchQuery{Term: p.Query, PlatformID: p.PlatformID, Limit: min(limit, maxSearch)})
		if err != nil {
			return schema.SearchGamesResult{}, wireError(err)
		}
		out := schema.SearchGamesResult{Games: []schema.SearchHit{}, Local: result.Local}
		for _, r := range result.ROMs {
			out.Games = append(out.Games, schema.SearchHit{
				ID:           r.ID,
				Name:         r.Name,
				PlatformID:   r.PlatformID,
				PlatformName: result.PlatformNames[r.PlatformID],
				SizeBytes:    r.SizeBytes,
				Regions:      nonNil(r.Regions),
				State:        schema.GameState(domain.StateOf(result.States, r.ID)),
			})
		}
		return out, nil
	})

	Register(s, "get_game", func(ctx context.Context, p schema.GetGameParams) (schema.Game, error) {
		rom, platform, state, err := catalog.Game(ctx, p.ID)
		if err != nil {
			return schema.Game{}, wireError(err)
		}
		var paths []string
		if state == domain.StateInstalled {
			owned, _, err := downloads.Owned(ctx, p.ID)
			if err != nil {
				return schema.Game{}, wireError(err)
			}
			for _, f := range owned.Files {
				paths = append(paths, f.Path)
			}
		}
		return schema.Game{
			ID:             rom.ID,
			PlatformID:     rom.PlatformID,
			PlatformName:   platform.Name,
			Name:           rom.Name,
			FileName:       rom.FileName,
			SizeBytes:      rom.SizeBytes,
			Regions:        nonNil(rom.Regions),
			Revision:       rom.Revision,
			MultiFile:      rom.MultiFile,
			State:          schema.GameState(state),
			InstalledPaths: paths,
		}, nil
	})
}

// CatalogEvents returns a listener that sends each catalog status as a catalog notification.
func CatalogEvents(s *Server) app.CatalogListener {
	return func(status app.CatalogStatus) {
		if err := s.Notify("catalog", toWireCatalog(status)); err != nil {
			s.log.Error("send catalog", "err", err)
		}
	}
}

func toWireCatalog(s app.CatalogStatus) schema.CatalogStatus {
	out := schema.CatalogStatus{
		Refreshing: s.Refreshing,
		Fetched:    s.Fetched,
		Platforms:  s.Platforms,
		Games:      s.ROMs,
	}
	if !s.SyncedAt.IsZero() {
		out.SyncedAt = s.SyncedAt.UTC().Format(time.RFC3339)
	}
	if s.Problem != nil {
		out.Problem = problemOf(s.Problem)
	}
	return out
}

func toWirePlatform(p domain.PlatformSummary) schema.PlatformSummary {
	return schema.PlatformSummary{ID: p.ID, Slug: p.Slug, Name: p.Name, Games: p.Games, Installed: p.Installed}
}
