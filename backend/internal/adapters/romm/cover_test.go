package romm

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

func TestCover(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\ncover bytes")
	var gotQuery, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/assets/romm/resources/roms/1/7/cover/small.png":
			gotQuery, gotAuth = r.URL.RawQuery, r.Header.Get("Authorization")
			_, _ = w.Write(png)
		case "/broken":
			w.WriteHeader(http.StatusBadGateway)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := New(domain.Server{URL: srv.URL + "/"}, "rmm_x", ver)

	got, err := c.Cover(context.Background(), "/assets/romm/resources/roms/1/7/cover/small.png?ts=2026-05-22 13:24:40")
	if err != nil || !bytes.Equal(got, png) || gotQuery != "ts=2026-05-22%2013:24:40" || gotAuth != "Bearer rmm_x" {
		t.Fatalf("got %q, %v; query %q, auth %q", got, err, gotQuery, gotAuth)
	}
	if _, err := c.Cover(context.Background(), "/assets/none.png"); kindOf(err) != domain.KindNotFound {
		t.Fatalf("missing: %v", err)
	}
	if _, err := c.Cover(context.Background(), "/broken"); kindOf(err) != domain.KindUnreachable {
		t.Fatalf("bad gateway: %v", err)
	}
}

// A self-signed server the user trusted serves covers like any other.
func TestCoverFromPinnedServer(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("img"))
	}))
	defer srv.Close()
	pin := Fingerprint(srv.Certificate())

	if _, err := New(domain.Server{URL: srv.URL}, "rmm_x", ver).Cover(context.Background(), "/c.png"); kindOf(err) != domain.KindUntrustedCert {
		t.Fatalf("unpinned: %v", err)
	}
	got, err := New(domain.Server{URL: srv.URL, CertFingerprint: pin}, "rmm_x", ver).Cover(context.Background(), "/c.png")
	if err != nil || string(got) != "img" {
		t.Fatalf("pinned: %q, %v", got, err)
	}
}

// An absolute cover URL points somewhere other than RomM, which never sees the token.
func TestCoverElsewhereGetsNoToken(t *testing.T) {
	var gotAuth string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("img"))
	}))
	defer other.Close()
	c := New(domain.Server{URL: "http://romm.invalid"}, "rmm_x", ver)

	if got, err := c.Cover(context.Background(), other.URL+"/cover.jpg"); err != nil || string(got) != "img" || gotAuth != "" {
		t.Fatalf("got %q, %v; auth %q", got, err, gotAuth)
	}
}

func TestScreenshots(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/roms/7" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"id": 7, "merged_screenshots": ["/assets/romm/resources/roms/1/7/screenshots/0.jpg"]}`))
	}))
	defer srv.Close()
	c := New(domain.Server{URL: srv.URL}, "rmm_x", ver)

	got, err := c.Screenshots(context.Background(), 7)
	if err != nil || len(got) != 1 || got[0] != "/assets/romm/resources/roms/1/7/screenshots/0.jpg" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := c.Screenshots(context.Background(), 404); kindOf(err) != domain.KindNotFound {
		t.Fatalf("missing ROM: %v", err)
	}
}
