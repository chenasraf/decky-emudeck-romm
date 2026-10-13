package romm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

func TestROMFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/roms/404" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 7,
			"files": []map[string]any{
				{"id": 70, "file_name": "Chrono Trigger.sfc", "file_size_bytes": 4194304, "category": "game",
					"last_modified": "2026-05-22T14:47:28+00:00", "crc_hash": "2d206bf7", "md5_hash": nil, "sha1_hash": "de5822f4"},
				{"id": 71, "file_name": "manual.pdf", "file_size_bytes": 100, "category": "manual", "last_modified": "2026-05-22T14:47:28+00:00"},
			},
		})
	}))
	defer srv.Close()
	c := New(domain.Server{URL: srv.URL}, "rmm_x", ver)

	got, err := c.ROMFiles(context.Background(), 7)
	if err != nil || len(got) != 2 {
		t.Fatalf("got %+v, %v", got, err)
	}
	f := got[0]
	if f.ID != 70 || f.SizeBytes != 4194304 || !f.IsGame() || f.MD5 != "" || f.SHA1 != "de5822f4" ||
		!f.LastModified.Equal(time.Date(2026, 5, 22, 14, 47, 28, 0, time.UTC)) {
		t.Fatalf("file %+v", f)
	}
	if got[1].IsGame() {
		t.Fatalf("manual counted as game: %+v", got[1])
	}
	if _, err := c.ROMFiles(context.Background(), 404); kindOf(err) != domain.KindNotFound {
		t.Fatalf("missing ROM: %v", err)
	}
}

func TestDownloadFile(t *testing.T) {
	body := bytes.Repeat([]byte("rom!"), 10000)
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.EscapedPath()
		if r.Header.Get("Authorization") != "Bearer rmm_x" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	var out bytes.Buffer
	err := New(domain.Server{URL: srv.URL}, "rmm_x", ver).DownloadFile(context.Background(), 70, "Chrono #1?.sfc", &out)
	if err != nil || !bytes.Equal(out.Bytes(), body) {
		t.Fatalf("got %d bytes, %v", out.Len(), err)
	}
	if path != "/api/roms/70/files/content/Chrono%20%231%3F.sfc" {
		t.Fatalf("path %s", path)
	}

	err = New(domain.Server{URL: srv.URL}, "rmm_old", ver).DownloadFile(context.Background(), 70, "x.sfc", &out)
	if kindOf(err) != domain.KindSignedOut {
		t.Fatalf("rejected token: %v", err)
	}
}

func TestDownloadCutOff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(1000))
		_, _ = w.Write([]byte("only part"))
	}))
	defer srv.Close()

	err := New(domain.Server{URL: srv.URL}, "rmm_x", ver).DownloadFile(context.Background(), 1, "x", &bytes.Buffer{})
	if kindOf(err) != domain.KindUnreachable {
		t.Fatalf("want unreachable, got %v", err)
	}
}

type failingWriter struct{}

var errDiskFull = errors.New("no space left on device")

func (failingWriter) Write([]byte) (int, error) { return 0, errDiskFull }

func TestDownloadWriteFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("data"))
	}))
	defer srv.Close()

	err := New(domain.Server{URL: srv.URL}, "rmm_x", ver).DownloadFile(context.Background(), 1, "x", failingWriter{})
	if !errors.Is(err, errDiskFull) {
		t.Fatalf("want the disk's error, got %v", err)
	}
}

func TestDownloadCancelled(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000000")
		_, _ = w.Write([]byte("first"))
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()
	err := New(domain.Server{URL: srv.URL}, "rmm_x", ver).DownloadFile(ctx, 1, "x", &bytes.Buffer{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want canceled, got %v", err)
	}
}
