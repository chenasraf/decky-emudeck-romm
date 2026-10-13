package rpc

import (
	"context"
	"path/filepath"
	"runtime"
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
	defaultShelf     = 8
	maxShelf         = 50
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
		offset, limit := max(p.Offset, 0), min(limit, maxGamesPage)
		out := schema.ListGamesResult{Games: []schema.GameSummary{}}
		var page domain.ROMPage
		if p.Collection != nil {
			col, colPage, err := catalog.CollectionGames(ctx, fromWireCollection(*p.Collection), filter, domain.ROMSort(p.Sort), offset, limit)
			if err != nil {
				return schema.ListGamesResult{}, wireError(err)
			}
			out.Name, page = col.Name, colPage
		} else {
			platform, platPage, err := catalog.Games(ctx, p.PlatformID, filter, domain.ROMSort(p.Sort), offset, limit)
			if err != nil {
				return schema.ListGamesResult{}, wireError(err)
			}
			out.Name, page = platform.Name, platPage
			policy, keep := downloads.Archives(ctx, p.PlatformID)
			out.Archives, out.KeepArchives = schema.ArchivePolicy(policy), keep
			current, folders := downloads.InstallTo(ctx, p.PlatformID)
			out.InstallTo = current
			for _, f := range folders {
				out.InstallFolders = append(out.InstallFolders, schema.InstallFolder{
					Folder: f.Folder, Path: f.Path, Primary: f.Primary, SRMSkips: f.SRMSkips,
				})
			}
		}
		out.Total = page.Total
		names := map[int]string{}
		for _, r := range page.ROMs {
			g := toWireGame(r, page.States)
			if p.Collection != nil {
				if _, ok := names[r.PlatformID]; !ok {
					names[r.PlatformID] = catalog.PlatformName(ctx, r.PlatformID)
				}
				g.PlatformName = names[r.PlatformID]
			}
			out.Games = append(out.Games, g)
		}
		return out, nil
	})

	Register(s, "list_collections", func(ctx context.Context, _ schema.ListCollectionsParams) (schema.ListCollectionsResult, error) {
		collections, err := catalog.Collections(ctx)
		if err != nil {
			return schema.ListCollectionsResult{}, wireError(err)
		}
		out := schema.ListCollectionsResult{Collections: []schema.CollectionSummary{}}
		for _, c := range collections {
			out.Collections = append(out.Collections, schema.CollectionSummary{
				Kind:      schema.CollectionKind(c.Kind),
				ID:        c.ID,
				Name:      c.Name,
				Favorite:  c.Favorite,
				Games:     c.Games,
				Installed: c.Installed,
			})
		}
		return out, nil
	})

	Register(s, "library_shelves", func(ctx context.Context, p schema.LibraryShelvesParams) (schema.LibraryShelves, error) {
		limit := p.Limit
		if limit <= 0 {
			limit = defaultShelf
		}
		shelves, err := catalog.Shelves(ctx, min(limit, maxShelf))
		if err != nil {
			return schema.LibraryShelves{}, wireError(err)
		}
		summaries := func(roms []domain.ROM) []schema.GameSummary {
			out := []schema.GameSummary{}
			for _, r := range roms {
				g := toWireGame(r, shelves.States)
				g.PlatformName = shelves.PlatformNames[r.PlatformID]
				out = append(out, g)
			}
			return out
		}
		out := schema.LibraryShelves{
			Recent:         summaries(shelves.Recent),
			Installed:      summaries(shelves.Installed.ROMs),
			InstalledTotal: shelves.Installed.Total,
			Platforms:      []schema.PlatformShelf{},
		}
		for _, ps := range shelves.Platforms {
			out.Platforms = append(out.Platforms, schema.PlatformShelf{
				Platform: toWirePlatform(ps.Platform),
				Games:    summaries(ps.ROMs),
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
				HasCover:     r.CoverSmall != "",
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
		var note string
		if state == domain.StateInstalled {
			owned, _, err := downloads.Owned(ctx, p.ID)
			if err != nil {
				return schema.Game{}, wireError(err)
			}
			system, _ := domain.SystemForSlug(platform.Slug)
			for _, f := range owned.Files {
				paths = append(paths, f.Path)
				if note == "" {
					note = system.InstallNote(filepath.Base(f.Path), runtime.GOARCH)
				}
			}
		}
		var linked string
		if state == domain.StateOnDeck {
			link, ok, err := downloads.Linked(ctx, p.ID)
			if err != nil {
				return schema.Game{}, wireError(err)
			}
			if ok {
				linked = link.Path
			}
		}
		return schema.Game{
			ID:             rom.ID,
			PlatformID:     rom.PlatformID,
			PlatformName:   platform.Name,
			Name:           rom.Name,
			FileName:       rom.FileName,
			SizeBytes:      rom.SizeBytes,
			HasCover:       rom.CoverLarge != "",
			Regions:        nonNil(rom.Regions),
			Revision:       rom.Revision,
			MultiFile:      rom.MultiFile,
			State:          schema.GameState(state),
			InstalledPaths: paths,
			InstallNote:    note,
			LinkedPath:     linked,
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

func toWireGame(r domain.ROM, states map[int]domain.GameState) schema.GameSummary {
	return schema.GameSummary{
		ID:        r.ID,
		Name:      r.Name,
		SizeBytes: r.SizeBytes,
		HasCover:  r.CoverSmall != "",
		Regions:   nonNil(r.Regions),
		State:     schema.GameState(domain.StateOf(states, r.ID)),
	}
}

func toWirePlatform(p domain.PlatformSummary) schema.PlatformSummary {
	return schema.PlatformSummary{ID: p.ID, Slug: p.Slug, Name: p.Name, Games: p.Games, Installed: p.Installed}
}
