package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

func TestEmptyStore(t *testing.T) {
	got, err := New(t.TempDir()).SignIn()
	if err != nil || got != nil {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := domain.SignIn{
		Server:    domain.Server{URL: "https://romm.lan", CertFingerprint: "AB:CD"},
		Token:     "rmm_secret",
		DeviceID:  "dev-1",
		Scopes:    []string{"roms.read", "me.read"},
		SignedOut: true,
	}
	if err := New(dir).SaveSignIn(want); err != nil {
		t.Fatal(err)
	}

	// A fresh store reads what an earlier process wrote.
	got, err := New(dir).SignIn()
	if err != nil || got == nil || !reflect.DeepEqual(*got, want) {
		t.Fatalf("got %+v, %v; want %+v", got, err, want)
	}
}

func TestKeepArchives(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if !s.KeepArchives("snes") {
		t.Fatal("Keep archives is off before anyone set it")
	}
	if err := s.SaveSignIn(domain.SignIn{Server: domain.Server{URL: "https://romm.lan"}, Token: "rmm_x"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetKeepArchives("snes", false); err != nil {
		t.Fatal(err)
	}

	s = New(dir)
	if s.KeepArchives("snes") || !s.KeepArchives("gba") {
		t.Fatalf("snes %v, gba %v", s.KeepArchives("snes"), s.KeepArchives("gba"))
	}
	// Setting it keeps the sign-in.
	if in, _ := s.SignIn(); in == nil || in.Server.URL != "https://romm.lan" {
		t.Fatalf("sign-in %+v", in)
	}
}

func TestForgetSignInKeepsOtherSettings(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if err := s.SaveSignIn(domain.SignIn{Server: domain.Server{URL: "https://romm.lan"}, Token: "rmm_x"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetKeepArchives("snes", false); err != nil {
		t.Fatal(err)
	}
	if err := s.ForgetSignIn(); err != nil {
		t.Fatal(err)
	}
	if in, err := s.SignIn(); in != nil || err != nil {
		t.Fatalf("sign-in %+v, %v", in, err)
	}
	if _, err := os.Stat(filepath.Join(dir, tokenFile)); !os.IsNotExist(err) {
		t.Fatalf("token file: %v", err)
	}
	if s.KeepArchives("snes") {
		t.Fatal("forgetting the server reset Keep archives")
	}
	// Forgetting again is fine.
	if err := s.ForgetSignIn(); err != nil {
		t.Fatal(err)
	}
}

func TestInstallTo(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if got := s.InstallTo("arcade"); got != "" {
		t.Fatalf("unset install to %q", got)
	}
	if err := s.SetInstallTo("arcade", "fbneo"); err != nil {
		t.Fatal(err)
	}
	if got := New(dir).InstallTo("arcade"); got != "fbneo" {
		t.Fatalf("install to %q", got)
	}
	// The system's own folder is the same as unset.
	if err := s.SetInstallTo("arcade", "arcade"); err != nil {
		t.Fatal(err)
	}
	if got := New(dir).InstallTo("arcade"); got != "" {
		t.Fatalf("after reset %q", got)
	}
}

func TestAutoBIOS(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if !s.AutoBIOS() {
		t.Fatal("BIOS downloads with games are off before anyone set it")
	}
	if err := s.SetAutoBIOS(false); err != nil {
		t.Fatal(err)
	}
	if New(dir).AutoBIOS() {
		t.Fatal("turning it off didn't stick")
	}
}

func TestWaitForGames(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if !s.WaitForGames() {
		t.Fatal("waiting for games is off before anyone set it")
	}
	if err := s.SetWaitForGames(false); err != nil {
		t.Fatal(err)
	}
	if New(dir).WaitForGames() {
		t.Fatal("turning it off didn't stick")
	}
}

func TestESDEMedia(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if !s.ESDEMedia() {
		t.Fatal("ES-DE media is off before anyone set it")
	}
	if err := s.SetESDEMedia(false); err != nil {
		t.Fatal(err)
	}
	if New(dir).ESDEMedia() {
		t.Fatal("turning it off didn't stick")
	}
}

func TestSignInNeedsToken(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if err := s.SaveSignIn(domain.SignIn{Server: domain.Server{URL: "http://romm.lan"}, Token: "rmm_x"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, tokenFile)); err != nil {
		t.Fatal(err)
	}
	if got, err := s.SignIn(); err != nil || got != nil {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestTokenIsPrivateAndKeptOutOfSettings(t *testing.T) {
	dir := t.TempDir()
	if err := New(dir).SaveSignIn(domain.SignIn{Server: domain.Server{URL: "http://romm.lan"}, Token: "rmm_secret"}); err != nil {
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
