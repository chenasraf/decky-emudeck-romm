package romm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

func TestPlatforms(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": 1, "slug": "gba", "name": "Game Boy Advance", "custom_name": "", "display_name": "GBA", "rom_count": 10},
			{"id": 2, "slug": "nes", "name": "Nintendo Entertainment System", "custom_name": "Famicom", "rom_count": 0},
		})
	}))
	defer srv.Close()

	got, err := New(domain.Server{URL: srv.URL}, "rmm_x", ver).Platforms(context.Background())
	if err != nil || len(got) != 2 || got[0].Name != "GBA" || got[0].ROMCount != 10 || got[1].Name != "Famicom" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestCollections(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/collections":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 2, "name": "Favorites", "is_favorite": true, "rom_ids": []int{2089, 1}, "rom_count": 2},
			})
		case "/api/collections/smart":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 2, "name": "Fire Emblem", "is_smart": true, "rom_ids": []int{1274}, "rom_count": 1},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	got, err := New(domain.Server{URL: srv.URL}, "rmm_x", ver).Collections(context.Background())
	if err != nil || len(got) != 2 {
		t.Fatalf("got %+v, %v", got, err)
	}
	fav, smart := got[0], got[1]
	if fav.CollectionRef != (domain.CollectionRef{Kind: domain.CollectionRegular, ID: 2}) || !fav.Favorite || !slices.Equal(fav.ROMIDs, []int{2089, 1}) {
		t.Fatalf("favorites %+v", fav)
	}
	if smart.CollectionRef != (domain.CollectionRef{Kind: domain.CollectionSmart, ID: 2}) || smart.Name != "Fire Emblem" || !slices.Equal(smart.ROMIDs, []int{1274}) {
		t.Fatalf("smart %+v", smart)
	}
}

func TestROMsPage(t *testing.T) {
	var query map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{{
				"id": 7, "platform_id": 1, "name": "", "fs_name": "Golden Sun (USA).gba", "fs_extension": "gba",
				"fs_size_bytes": 8388608, "regions": []string{"USA"}, "revision": "", "has_multiple_files": false,
				"created_at": "2026-08-01T10:00:00+00:00", "updated_at": "2026-09-10T15:30:57.123456+00:00", "path_cover_small": "/assets/s.png", "path_cover_large": "/assets/l.png",
				"summary": "ignored",
			}},
			"total": 1,
		})
	}))
	defer srv.Close()

	since := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	got, err := New(domain.Server{URL: srv.URL}, "rmm_x", ver).ROMs(context.Background(), since, 500, 250)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	r := got[0]
	// A ROM RomM couldn't match has no name; its file name stands in.
	if r.Name != "Golden Sun (USA).gba" || r.SizeBytes != 8388608 || r.UpdatedAt.Nanosecond() != 123456000 ||
		!r.CreatedAt.Equal(time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)) || r.CoverLarge != "/assets/l.png" {
		t.Fatalf("rom %+v", r)
	}
	for key, want := range map[string]string{
		"offset": "500", "limit": "250", "order_by": "id", "updated_after": "2026-09-01T00:00:00Z",
		"with_char_index": "false", "with_filter_values": "false", "with_rom_id_index": "false",
	} {
		if !slices.Equal(query[key], []string{want}) {
			t.Errorf("%s = %v, want %s", key, query[key], want)
		}
	}
}

func TestROMIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("with_rom_id_index") != "true" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "rom_id_index": []int{3, 1, 2}})
	}))
	defer srv.Close()

	got, err := New(domain.Server{URL: srv.URL}, "rmm_x", ver).ROMIDs(context.Background())
	if err != nil || !slices.Equal(got, []int{3, 1, 2}) {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestSearchROMs(t *testing.T) {
	var query map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": 9}, {"id": 4}}, "total": 2})
	}))
	defer srv.Close()

	c := New(domain.Server{URL: srv.URL}, "rmm_x", ver)
	got, err := c.SearchROMs(context.Background(), domain.SearchQuery{Term: "golden sun", Limit: 50})
	if err != nil || !slices.Equal(got, []int{9, 4}) {
		t.Fatalf("got %v, %v", got, err)
	}
	for key, want := range map[string]string{
		"search_term": "golden sun", "limit": "50", "with_char_index": "false", "with_rom_id_index": "false",
	} {
		if !slices.Equal(query[key], []string{want}) {
			t.Errorf("%s = %v, want %s", key, query[key], want)
		}
	}
	// RomM orders by relevance only without an order_by.
	for _, key := range []string{"order_by", "platform_ids"} {
		if _, ok := query[key]; ok {
			t.Errorf("%s = %v", key, query[key])
		}
	}

	if _, err := c.SearchROMs(context.Background(), domain.SearchQuery{Term: "sun", PlatformID: 3, Limit: 50}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(query["platform_ids"], []string{"3"}) {
		t.Errorf("platform_ids = %v", query["platform_ids"])
	}
}

func TestCatalogSignedOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	if _, err := New(domain.Server{URL: srv.URL}, "rmm_x", ver).Platforms(context.Background()); kindOf(err) != domain.KindSignedOut {
		t.Fatalf("want signed_out, got %v", err)
	}
}
