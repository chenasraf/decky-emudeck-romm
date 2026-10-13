package romm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

func TestNegotiateSync(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/sync/negotiate" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"session_id": 12, "operations": [
			{"action": "download", "rom_id": 7, "save_id": 70, "file_name": "Chrono [2026-10-13_10-00-00].srm", "slot": "autosave",
			 "emulator": "snes9x", "reason": "Server save is newer", "server_updated_at": "2026-10-13T10:00:00+00:00", "server_content_hash": "abc"},
			{"action": "upload", "rom_id": 8, "save_id": null, "file_name": "Metroid.srm", "slot": "autosave", "reason": "new"}
		], "total_upload": 1, "total_download": 1, "total_conflict": 0, "total_no_op": 0, "total_delete": 0}`))
	}))
	defer srv.Close()
	c := New(domain.Server{URL: srv.URL}, "rmm_x", ver)

	at := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	plan, err := c.NegotiateSync(context.Background(), domain.SyncRequest{
		DeviceID: "deck-1", ROMIDs: []int{7, 8}, RestoreUnlisted: true,
		Saves: []domain.ClientSave{{ROMID: 8, FileName: "Metroid.srm", Slot: "autosave", Emulator: "snes9x", ContentHash: "def", UpdatedAt: at, SizeBytes: 8192}},
	})
	if err != nil || plan.SessionID != 12 || len(plan.Operations) != 2 {
		t.Fatalf("plan %+v, %v", plan, err)
	}
	down := plan.Operations[0]
	if down.Action != domain.SyncDownload || down.SaveID != 70 || down.ServerHash != "abc" ||
		!down.ServerUpdatedAt.Equal(time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)) || down.Emulator != "snes9x" {
		t.Fatalf("download %+v", down)
	}
	if up := plan.Operations[1]; up.Action != domain.SyncUpload || up.SaveID != 0 {
		t.Fatalf("upload %+v", up)
	}
	saves := got["saves"].([]any)
	first := saves[0].(map[string]any)
	if got["device_id"] != "deck-1" || got["restore_unlisted"] != true || first["updated_at"] != "2026-10-13T09:00:00Z" ||
		first["content_hash"] != "def" || first["file_size_bytes"] != float64(8192) {
		t.Fatalf("sent %v", got)
	}
}

func TestNegotiateSyncDisabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"detail": "Sync is disabled for this device"}`))
	}))
	defer srv.Close()
	_, err := New(domain.Server{URL: srv.URL}, "rmm_x", ver).NegotiateSync(context.Background(), domain.SyncRequest{DeviceID: "d"})
	if kindOf(err) != domain.KindSyncDisabled {
		t.Fatalf("got %v", err)
	}
}

func TestUploadSave(t *testing.T) {
	var query map[string][]string
	var file, name string
	conflict := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if conflict {
			w.WriteHeader(http.StatusConflict)
			return
		}
		query = r.URL.Query()
		f, h, err := r.FormFile("saveFile")
		if err != nil {
			t.Error(err)
			return
		}
		data, _ := io.ReadAll(f)
		file, name = string(data), h.Filename
		_, _ = w.Write([]byte(`{"id": 71, "rom_id": 7, "file_name": "Chrono [x].srm", "slot": "autosave", "emulator": "snes9x",
			"content_hash": "abc", "updated_at": "2026-10-13T11:00:00.5Z"}`))
	}))
	defer srv.Close()
	c := New(domain.Server{URL: srv.URL}, "rmm_x", ver)
	u := domain.SaveUpload{ROMID: 7, FileName: "Chrono.srm", Slot: "autosave", Emulator: "snes9x", DeviceID: "deck-1",
		ContentHash: "abc", SessionID: 12, Data: []byte("sram")}

	saved, err := c.UploadSave(context.Background(), u)
	if err != nil || saved.ID != 71 || saved.ContentHash != "abc" || !saved.UpdatedAt.Equal(time.Date(2026, 10, 13, 11, 0, 0, 5e8, time.UTC)) {
		t.Fatalf("saved %+v, %v", saved, err)
	}
	if file != "sram" || name != "Chrono.srm" || query["rom_id"][0] != "7" || query["slot"][0] != "autosave" ||
		query["device_id"][0] != "deck-1" || query["session_id"][0] != "12" || query["autocleanup"][0] != "true" || query["emulator"][0] != "snes9x" {
		t.Fatalf("sent %q as %q, query %v", file, name, query)
	}

	conflict = true
	if _, err := c.UploadSave(context.Background(), u); kindOf(err) != domain.KindSaveConflict {
		t.Fatalf("conflict: %v", err)
	}
}

func TestDownloadAndConfirmSave(t *testing.T) {
	var confirmed map[string]string
	var optimistic string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/saves/70/content":
			optimistic = r.URL.Query().Get("optimistic")
			_, _ = w.Write([]byte("sram bytes"))
		case "/api/saves/70/downloaded":
			_ = json.NewDecoder(r.Body).Decode(&confirmed)
			_, _ = w.Write([]byte(`{}`))
		case "/api/sync/sessions/12/complete":
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := New(domain.Server{URL: srv.URL}, "rmm_x", ver)

	data, err := c.DownloadSave(context.Background(), 70, "deck-1", 12)
	if err != nil || string(data) != "sram bytes" || optimistic != "false" {
		t.Fatalf("got %q, %v, optimistic %q", data, err, optimistic)
	}
	if err := c.ConfirmSaveDownload(context.Background(), 70, "deck-1", "abc"); err != nil || confirmed["content_hash"] != "abc" || confirmed["device_id"] != "deck-1" {
		t.Fatalf("confirm %v, sent %v", err, confirmed)
	}
	if _, err := c.DownloadSave(context.Background(), 404, "deck-1", 0); kindOf(err) != domain.KindNotFound {
		t.Fatalf("missing: %v", err)
	}
	if err := c.CompleteSync(context.Background(), 12, 2, 0); err != nil {
		t.Fatal(err)
	}
}
