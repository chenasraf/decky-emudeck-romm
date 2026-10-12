package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

func TestEmptyStore(t *testing.T) {
	s := New(t.TempDir())
	server, err := s.Server()
	if err != nil || server != nil {
		t.Fatalf("got %v, %v", server, err)
	}
	tok, err := s.Token()
	if err != nil || tok != "" {
		t.Fatalf("got %q, %v", tok, err)
	}
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	want := domain.Server{URL: "https://romm.lan", CertFingerprint: "AB:CD"}
	if err := s.SaveServer(want); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveToken("rmm_secret"); err != nil {
		t.Fatal(err)
	}

	// A fresh store reads what an earlier process wrote.
	s = New(dir)
	got, err := s.Server()
	if err != nil || got == nil || *got != want {
		t.Fatalf("server: got %+v, %v", got, err)
	}
	tok, err := s.Token()
	if err != nil || tok != "rmm_secret" {
		t.Fatalf("token: got %q, %v", tok, err)
	}
}

func TestTokenIsPrivateAndKeptOutOfSettings(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if err := s.SaveServer(domain.Server{URL: "http://romm.lan"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveToken("rmm_secret"); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(dir, tokenFile))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("token mode %o, want 600", perm)
	}
	data, err := os.ReadFile(filepath.Join(dir, settingsFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "rmm_secret") {
		t.Fatalf("settings.json holds the token:\n%s", data)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("leftover temp files: %v", entries)
	}
}
