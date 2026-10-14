package romm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

func TestStates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/states" || r.URL.Query().Get("rom_id") != "2105" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`[
			{"id": 84, "rom_id": 2105, "file_name": "SoulGold [2026-10-14 06-28-25-961].state", "emulator": "mgba",
			 "file_size_bytes": 528472, "created_at": "2026-10-14T06:28:26+00:00",
			 "screenshot": {"download_path": "/api/screenshots/15/content?name=x.png"}},
			{"id": 85, "rom_id": 2105, "file_name": "other.state", "emulator": null, "file_size_bytes": 1,
			 "created_at": "2026-10-14T07:00:00Z", "screenshot": null}
		]`))
	}))
	defer srv.Close()
	states, err := New(domain.Server{URL: srv.URL}, "rmm_x", ver).States(context.Background(), 2105)
	if err != nil || len(states) != 2 {
		t.Fatalf("states %+v, %v", states, err)
	}
	s := states[0]
	if s.ID != 84 || s.Emulator != "mgba" || s.SizeBytes != 528472 || s.Screenshot != "/api/screenshots/15/content?name=x.png" ||
		!s.CreatedAt.Equal(time.Date(2026, 10, 14, 6, 28, 26, 0, time.UTC)) {
		t.Fatalf("state %+v", s)
	}
	if states[1].Emulator != "" || states[1].Screenshot != "" {
		t.Fatalf("state without emulator or screenshot %+v", states[1])
	}
}

func TestUploadState(t *testing.T) {
	var query map[string][]string
	files := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/states" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		query = r.URL.Query()
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		for field, fhs := range r.MultipartForm.File {
			f, _ := fhs[0].Open()
			data, _ := io.ReadAll(f)
			files[field] = fhs[0].Filename + ":" + string(data)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 90, "rom_id": 7, "file_name": "G.state", "created_at": "2026-10-14T12:00:00Z"})
	}))
	defer srv.Close()
	c := New(domain.Server{URL: srv.URL}, "rmm_x", ver)
	got, err := c.UploadState(context.Background(), domain.StateUpload{
		ROMID: 7, Emulator: "mgba", FileName: "G [2026-10-14 12-00-00-000].state", Data: []byte("state"), Screenshot: []byte("png"),
	})
	if err != nil || got.ID != 90 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if query["rom_id"][0] != "7" || query["emulator"][0] != "mgba" {
		t.Fatalf("query %v", query)
	}
	if files["stateFile"] != "G [2026-10-14 12-00-00-000].state:state" || files["screenshotFile"] != "G [2026-10-14 12-00-00-000].png:png" {
		t.Fatalf("files %v", files)
	}
}

func TestDownloadStateAndScreenshot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/states/84/content":
			_, _ = w.Write([]byte("RASTATE"))
		case "/api/screenshots/15/content":
			if r.URL.Query().Get("timestamp") != "2026-10-14 11:41:57" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte("PNG"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := New(domain.Server{URL: srv.URL}, "rmm_x", ver)
	if data, err := c.DownloadState(context.Background(), 84); err != nil || string(data) != "RASTATE" {
		t.Fatalf("state %q, %v", data, err)
	}
	if data, err := c.StateScreenshot(context.Background(), "/api/screenshots/15/content?timestamp=2026-10-14 11:41:57&name=x%20y.png"); err != nil || string(data) != "PNG" {
		t.Fatalf("screenshot %q, %v", data, err)
	}
	if _, err := c.DownloadState(context.Background(), 1); kindOf(err) != domain.KindNotFound {
		t.Fatalf("missing: %v", err)
	}
	if _, err := c.StateScreenshot(context.Background(), "https://elsewhere.example/x.png"); err == nil {
		t.Fatal("fetched a screenshot from outside RomM")
	}
}

func TestDeleteStates(t *testing.T) {
	var deleted [][]int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			States []int `json:"states"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		deleted = append(deleted, body.States)
		// RomM stops at a state that's gone.
		if slices.Contains(body.States, 2) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(body.States)
	}))
	defer srv.Close()
	c := New(domain.Server{URL: srv.URL}, "rmm_x", ver)
	if err := c.DeleteStates(context.Background(), []int{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 4 || !slices.Equal(deleted[3], []int{3}) {
		t.Fatalf("requests %v", deleted)
	}
}

func TestStatesOfEveryGame(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/states" || r.URL.RawQuery != "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`[{"id": 1, "rom_id": 7, "created_at": "2026-10-14T06:00:00Z"}, {"id": 2, "rom_id": 8, "created_at": "2026-10-14T07:00:00Z"}]`))
	}))
	defer srv.Close()
	states, err := New(domain.Server{URL: srv.URL}, "rmm_x", ver).States(context.Background(), 0)
	if err != nil || len(states) != 2 || states[1].ROMID != 8 {
		t.Fatalf("states %+v, %v", states, err)
	}
}
