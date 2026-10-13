package romm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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

func TestPathInGame(t *testing.T) {
	for _, c := range []struct {
		name                                     string
		fsPath, fsName, filePath, fileName, want string
	}{
		{"single file, default layout", "roms/psx", "Crash.chd", "roms/psx", "Crash.chd", "Crash.chd"},
		{"folder game, default layout", "roms/psx", "FF7", "roms/psx/FF7", "FF7 (Disc 1).cue", "FF7 (Disc 1).cue"},
		{"platform-first layout", "psx/roms", "FF7", "psx/roms/FF7", "FF7 (Disc 1).cue", "FF7 (Disc 1).cue"},
		{"grouping levels above the game", "library/sony/psx/rpg", "FF7", "library/sony/psx/rpg/FF7", "a.cue", "a.cue"},
		{"nested inside the game", "roms/ps3", "Demon's Souls", "roms/ps3/Demon's Souls/PS3_GAME/USRDIR", "EBOOT.BIN", "PS3_GAME/USRDIR/EBOOT.BIN"},
		{"category folder", "roms/switch", "Zelda", "roms/switch/Zelda/dlc", "x.nsp", "dlc/x.nsp"},
		{"outside the game", "roms/psx", "FF7", "elsewhere", "a.cue", "a.cue"},
	} {
		got := pathInGame(c.fsPath, c.fsName, c.filePath, c.fileName, "")
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
		// full_path, when RomM sends it, says the same.
		if full := pathInGame(c.fsPath, c.fsName, "", c.fileName, c.filePath+"/"+c.fileName); full != c.want {
			t.Errorf("%s from full_path: got %q, want %q", c.name, full, c.want)
		}
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
	err := New(domain.Server{URL: srv.URL}, "rmm_x", ver).DownloadFile(context.Background(), 70, "Chrono #1?.sfc", 0, into(&out))
	if err != nil || !bytes.Equal(out.Bytes(), body) {
		t.Fatalf("got %d bytes, %v", out.Len(), err)
	}
	if path != "/api/roms/70/files/content/Chrono%20%231%3F.sfc" {
		t.Fatalf("path %s", path)
	}

	err = New(domain.Server{URL: srv.URL}, "rmm_old", ver).DownloadFile(context.Background(), 70, "x.sfc", 0, into(&out))
	if kindOf(err) != domain.KindSignedOut {
		t.Fatalf("rejected token: %v", err)
	}
}

// into opens w for a download, wherever its bytes start.
func into(w io.Writer) func(int64) (io.Writer, error) {
	return func(int64) (io.Writer, error) { return w, nil }
}

func TestDownloadResumes(t *testing.T) {
	body := bytes.Repeat([]byte("0123456789"), 1000)
	ranges := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ranges {
			r.Header.Del("Range")
		}
		http.ServeContent(w, r, "x", time.Time{}, bytes.NewReader(body))
	}))
	defer srv.Close()
	c := New(domain.Server{URL: srv.URL}, "rmm_x", ver)

	// The server sends the rest of the file from where the partial stopped.
	var out bytes.Buffer
	var start int64 = -1
	open := func(s int64) (io.Writer, error) {
		start = s
		return &out, nil
	}
	if err := c.DownloadFile(context.Background(), 1, "x", 4000, open); err != nil {
		t.Fatal(err)
	}
	if start != 4000 || !bytes.Equal(out.Bytes(), body[4000:]) {
		t.Fatalf("start %d, got %d bytes", start, out.Len())
	}

	// A partial longer than the file starts over.
	out.Reset()
	if err := c.DownloadFile(context.Background(), 1, "x", int64(len(body)+5), open); err != nil {
		t.Fatal(err)
	}
	if start != 0 || !bytes.Equal(out.Bytes(), body) {
		t.Fatalf("too long: start %d, got %d bytes", start, out.Len())
	}

	// A server that ignores Range sends the whole file, and says so.
	ranges = false
	out.Reset()
	if err := c.DownloadFile(context.Background(), 1, "x", 4000, open); err != nil {
		t.Fatal(err)
	}
	if start != 0 || !bytes.Equal(out.Bytes(), body) {
		t.Fatalf("no ranges: start %d, got %d bytes", start, out.Len())
	}
}

func TestDownloadServerErrorIsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	err := New(domain.Server{URL: srv.URL}, "rmm_x", ver).DownloadFile(context.Background(), 1, "x", 0, into(&bytes.Buffer{}))
	if kindOf(err) != domain.KindUnreachable {
		t.Fatalf("want unreachable, got %v", err)
	}
}

func TestDownloadCutOff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(1000))
		_, _ = w.Write([]byte("only part"))
	}))
	defer srv.Close()

	err := New(domain.Server{URL: srv.URL}, "rmm_x", ver).DownloadFile(context.Background(), 1, "x", 0, into(&bytes.Buffer{}))
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

	err := New(domain.Server{URL: srv.URL}, "rmm_x", ver).DownloadFile(context.Background(), 1, "x", 0, into(failingWriter{}))
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
	err := New(domain.Server{URL: srv.URL}, "rmm_x", ver).DownloadFile(ctx, 1, "x", 0, into(&bytes.Buffer{}))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want canceled, got %v", err)
	}
}
