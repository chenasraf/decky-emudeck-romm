package app

import (
	"context"
	"errors"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

type fakeSettings struct {
	server *domain.Server
	token  string
}

func (f *fakeSettings) Server() (*domain.Server, error) { return f.server, nil }
func (f *fakeSettings) SaveServer(s domain.Server) error {
	f.server = &s
	return nil
}
func (f *fakeSettings) Token() (string, error) { return f.token, nil }
func (f *fakeSettings) SaveToken(t string) error {
	f.token = t
	return nil
}

// fakeNet answers heartbeats per URL and redeems one pair code.
type fakeNet struct {
	heartbeats map[string]func(domain.Server) (string, error)
	pairCode   string
	dialed     []domain.Server
}

type fakeRomM struct {
	net    *fakeNet
	server domain.Server
}

func (n *fakeNet) dial(server domain.Server, _ string) RomM {
	n.dialed = append(n.dialed, server)
	return fakeRomM{net: n, server: server}
}

func (r fakeRomM) Heartbeat(context.Context) (string, error) {
	if h, ok := r.net.heartbeats[r.server.URL]; ok {
		return h(r.server)
	}
	return "", &domain.Error{Kind: domain.KindUnreachable, Message: "unreachable"}
}

func (r fakeRomM) ExchangePairCode(_ context.Context, code string) (string, error) {
	if code == r.net.pairCode {
		return "rmm_new", nil
	}
	return "", &domain.Error{Kind: domain.KindInvalidPairCode, Message: "bad code"}
}

func version(v string) func(domain.Server) (string, error) {
	return func(domain.Server) (string, error) { return v, nil }
}

func kindOf(err error) domain.Kind {
	var de *domain.Error
	if errors.As(err, &de) {
		return de.Kind
	}
	return ""
}

func TestCheckServerFallsBackToHTTP(t *testing.T) {
	n := &fakeNet{heartbeats: map[string]func(domain.Server) (string, error){"http://romm.lan": version("5.4.0")}}
	c := NewConnection(&fakeSettings{}, n.dial)

	server, v, err := c.CheckServer(context.Background(), "romm.lan", "")
	if err != nil || server.URL != "http://romm.lan" || v != "5.4.0" {
		t.Fatalf("got %+v %q %v", server, v, err)
	}
	if len(n.dialed) != 2 || n.dialed[0].URL != "https://romm.lan" {
		t.Fatalf("want https tried first, dialed %+v", n.dialed)
	}
}

func TestCheckServerStopsAtUntrustedCert(t *testing.T) {
	n := &fakeNet{heartbeats: map[string]func(domain.Server) (string, error){
		"https://romm.lan": func(s domain.Server) (string, error) {
			if s.CertFingerprint == "AA:BB" {
				return "5.4.0", nil
			}
			return "", &domain.Error{Kind: domain.KindUntrustedCert, Fingerprint: "AA:BB"}
		},
		"http://romm.lan": version("5.4.0"),
	}}
	c := NewConnection(&fakeSettings{}, n.dial)

	if _, _, err := c.CheckServer(context.Background(), "romm.lan", ""); kindOf(err) != domain.KindUntrustedCert {
		t.Fatalf("want untrusted_cert without falling back to http, got %v", err)
	}
	server, _, err := c.CheckServer(context.Background(), "romm.lan", "AA:BB")
	if err != nil || server.URL != "https://romm.lan" || server.CertFingerprint != "AA:BB" {
		t.Fatalf("trusted: got %+v, %v", server, err)
	}
}

func TestCheckServerBlocksOldVersion(t *testing.T) {
	n := &fakeNet{heartbeats: map[string]func(domain.Server) (string, error){"https://romm.lan": version("5.0.2")}}
	c := NewConnection(&fakeSettings{}, n.dial)

	_, _, err := c.CheckServer(context.Background(), "https://romm.lan", "")
	var de *domain.Error
	if !errors.As(err, &de) || de.Kind != domain.KindUnsupportedVersion || de.Version != "5.0.2" {
		t.Fatalf("want unsupported_version 5.0.2, got %v", err)
	}
}

func TestSignInWithPairCode(t *testing.T) {
	n := &fakeNet{
		heartbeats: map[string]func(domain.Server) (string, error){"https://romm.lan": version("5.4.0")},
		pairCode:   "ABCD2345",
	}
	settings := &fakeSettings{}
	c := NewConnection(settings, n.dial)
	server := domain.Server{URL: "https://romm.lan", CertFingerprint: "AA:BB"}

	if _, err := c.SignInWithPairCode(context.Background(), server, "WXYZ2345"); kindOf(err) != domain.KindInvalidPairCode {
		t.Fatalf("want invalid_pair_code, got %v", err)
	}
	if settings.token != "" || settings.server != nil {
		t.Fatal("a failed sign-in saved something")
	}

	status, err := c.SignInWithPairCode(context.Background(), server, "abcd-2345")
	if err != nil {
		t.Fatal(err)
	}
	if status.State != Connected || status.Version != "5.4.0" || status.ServerURL != "https://romm.lan" {
		t.Fatalf("status %+v", status)
	}
	if settings.token != "rmm_new" || *settings.server != server {
		t.Fatalf("saved %+v / %q", settings.server, settings.token)
	}
}

func TestStatus(t *testing.T) {
	n := &fakeNet{heartbeats: map[string]func(domain.Server) (string, error){"https://romm.lan": version("5.4.0")}}
	settings := &fakeSettings{}
	c := NewConnection(settings, n.dial)

	if s, _ := c.Status(context.Background()); s.State != NotSetUp {
		t.Fatalf("empty: %+v", s)
	}
	settings.server = &domain.Server{URL: "https://romm.lan"}
	settings.token = "rmm_x"
	if s, _ := c.Status(context.Background()); s.State != Connected || s.Version != "5.4.0" {
		t.Fatalf("connected: %+v", s)
	}
	settings.server = &domain.Server{URL: "https://gone.lan"}
	if s, _ := c.Status(context.Background()); s.State != Offline || kindOf(s.Problem) != domain.KindUnreachable {
		t.Fatalf("offline: %+v", s)
	}
}
