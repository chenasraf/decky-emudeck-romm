package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

type fakeSettings struct {
	mu sync.Mutex
	in *domain.SignIn
}

func (f *fakeSettings) SignIn() (*domain.SignIn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.in, nil
}

func (f *fakeSettings) SaveSignIn(in domain.SignIn) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.in = &in
	return nil
}

type fakeIdentity struct{}

func (fakeIdentity) Device() (domain.DeviceIdentity, error) {
	return domain.DeviceIdentity{Identifier: "abc", Name: "Steam Deck (steamdeck)", Platform: "steamos"}, nil
}

// fakeNet answers heartbeats per URL, redeems one pair code, and replays poll answers in order.
type fakeNet struct {
	heartbeats map[string]func(domain.Server) (string, error)
	pairCode   string

	mu        sync.Mutex
	dialed    []domain.Server
	started   []domain.DeviceIdentity
	scopes    []string
	polls     []error
	pollCalls int
	pollTimes []time.Time
	approved  domain.SignIn
}

type fakeRomM struct {
	net    *fakeNet
	server domain.Server
}

func (n *fakeNet) dial(server domain.Server, _ string) RomM {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.dialed = append(n.dialed, server)
	return fakeRomM{net: n, server: server}
}

func (r fakeRomM) Heartbeat(context.Context) (string, error) {
	if h, ok := r.net.heartbeats[r.server.URL]; ok {
		return h(r.server)
	}
	return "", &domain.Error{Kind: domain.KindUnreachable, Message: "unreachable"}
}

func (r fakeRomM) ExchangePairCode(_ context.Context, code string) (domain.SignIn, error) {
	if code == r.net.pairCode {
		return domain.SignIn{Token: "rmm_new", Scopes: []string{"roms.read"}}, nil
	}
	return domain.SignIn{}, &domain.Error{Kind: domain.KindInvalidPairCode, Message: "bad code"}
}

func (r fakeRomM) StartDeviceAuth(_ context.Context, device domain.DeviceIdentity, scopes []string) (domain.DevicePairing, error) {
	r.net.mu.Lock()
	defer r.net.mu.Unlock()
	r.net.started = append(r.net.started, device)
	r.net.scopes = scopes
	code := fmt.Sprintf("CODE%04d", len(r.net.started))
	return domain.DevicePairing{DeviceCode: "dc", UserCode: code, ExpiresIn: time.Second, Interval: time.Millisecond}, nil
}

// PollDeviceAuth answers with the next scripted error, and approves once they run out.
func (r fakeRomM) PollDeviceAuth(context.Context, string) (domain.SignIn, error) {
	r.net.mu.Lock()
	defer r.net.mu.Unlock()
	r.net.pollTimes = append(r.net.pollTimes, time.Now())
	i := r.net.pollCalls
	r.net.pollCalls++
	if i < len(r.net.polls) {
		return domain.SignIn{}, r.net.polls[i]
	}
	return r.net.approved, nil
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

type updates chan PairingUpdate

func newConnection(t *testing.T, settings Settings, n *fakeNet) (*Connection, updates) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ch := make(updates, 4)
	c := NewConnection(ctx, settings, n.dial, fakeIdentity{}, func(u PairingUpdate) { ch <- u }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.slowDownStep = 20 * time.Millisecond
	return c, ch
}

func (u updates) next(t *testing.T) PairingUpdate {
	t.Helper()
	select {
	case got := <-u:
		return got
	case <-time.After(3 * time.Second):
		t.Fatal("no pairing update")
		return PairingUpdate{}
	}
}

func TestCheckServerFallsBackToHTTP(t *testing.T) {
	n := &fakeNet{heartbeats: map[string]func(domain.Server) (string, error){"http://romm.lan": version("5.4.0")}}
	c, _ := newConnection(t, &fakeSettings{}, n)

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
	c, _ := newConnection(t, &fakeSettings{}, n)

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
	c, _ := newConnection(t, &fakeSettings{}, n)

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
	c, _ := newConnection(t, settings, n)
	server := domain.Server{URL: "https://romm.lan", CertFingerprint: "AA:BB"}

	if _, err := c.SignInWithPairCode(context.Background(), server, "WXYZ2345"); kindOf(err) != domain.KindInvalidPairCode {
		t.Fatalf("want invalid_pair_code, got %v", err)
	}
	if settings.in != nil {
		t.Fatal("a failed sign-in saved something")
	}

	status, err := c.SignInWithPairCode(context.Background(), server, "abcd-2345")
	if err != nil {
		t.Fatal(err)
	}
	if status.State != Connected || status.Version != "5.4.0" || status.ServerURL != "https://romm.lan" {
		t.Fatalf("status %+v", status)
	}
	if settings.in.Token != "rmm_new" || settings.in.Server != server || !slices.Equal(settings.in.Scopes, []string{"roms.read"}) {
		t.Fatalf("saved %+v", settings.in)
	}
	if len(status.DisabledFeatures) == 0 {
		t.Fatal("a token with only roms.read should disable features")
	}
}

func TestStatus(t *testing.T) {
	n := &fakeNet{heartbeats: map[string]func(domain.Server) (string, error){"https://romm.lan": version("5.4.0")}}
	settings := &fakeSettings{}
	c, _ := newConnection(t, settings, n)

	if s, _ := c.Status(context.Background()); s.State != NotSetUp {
		t.Fatalf("empty: %+v", s)
	}
	settings.in = &domain.SignIn{Server: domain.Server{URL: "https://romm.lan"}, Token: "rmm_x"}
	if s, _ := c.Status(context.Background()); s.State != Connected || s.Version != "5.4.0" || s.DisabledFeatures != nil {
		t.Fatalf("connected: %+v", s)
	}
	settings.in = &domain.SignIn{Server: domain.Server{URL: "https://gone.lan"}, Token: "rmm_x"}
	if s, _ := c.Status(context.Background()); s.State != Offline || kindOf(s.Problem) != domain.KindUnreachable {
		t.Fatalf("offline: %+v", s)
	}
}

func TestDevicePairingApproved(t *testing.T) {
	n := &fakeNet{
		heartbeats: map[string]func(domain.Server) (string, error){"https://romm.lan": version("5.4.0")},
		polls:      []error{domain.ErrPairingPending, domain.ErrPairingSlowDown, &domain.Error{Kind: domain.KindUnreachable}, domain.ErrPairingPending},
		approved:   domain.SignIn{Token: "rmm_dev", DeviceID: "dev-1", Scopes: []string{"me.read", "roms.read", "platforms.read"}},
	}
	settings := &fakeSettings{}
	c, updates := newConnection(t, settings, n)
	server := domain.Server{URL: "https://romm.lan"}

	pairing, err := c.StartDevicePairing(context.Background(), server)
	if err != nil || pairing.UserCode != "CODE0001" {
		t.Fatalf("got %+v, %v", pairing, err)
	}
	u := updates.next(t)
	if u.State != PairingApproved || u.Status.State != Connected {
		t.Fatalf("update %+v", u)
	}
	if settings.in.Token != "rmm_dev" || settings.in.DeviceID != "dev-1" || settings.in.Server != server {
		t.Fatalf("saved %+v", settings.in)
	}
	if len(u.Status.DisabledFeatures) != 2 {
		t.Fatalf("want BIOS and save sync off, got %+v", u.Status.DisabledFeatures)
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	if n.started[0].Identifier != "abc" || !slices.Equal(n.scopes, domain.RequestedScopes()) {
		t.Fatalf("started with %+v %v", n.started, n.scopes)
	}
	// The poll after slow_down waits at least one slowDownStep longer.
	if gap := n.pollTimes[2].Sub(n.pollTimes[1]); gap < c.slowDownStep {
		t.Fatalf("poll after slow_down came after %v", gap)
	}
}

type brokenSettings struct{ fakeSettings }

func (*brokenSettings) SaveSignIn(domain.SignIn) error { return errors.New("disk full") }

func TestDevicePairingSaveFails(t *testing.T) {
	n := &fakeNet{approved: domain.SignIn{Token: "rmm_dev", DeviceID: "dev-1"}}
	c, updates := newConnection(t, &brokenSettings{}, n)
	if _, err := c.StartDevicePairing(context.Background(), domain.Server{URL: "https://romm.lan"}); err != nil {
		t.Fatal(err)
	}
	u := updates.next(t)
	if u.State != PairingFailed || u.Problem == nil || !strings.Contains(u.Problem.Error(), "disk full") {
		t.Fatalf("got %+v", u)
	}
}

func TestDevicePairingDeniedAndExpired(t *testing.T) {
	for _, tt := range []struct {
		poll error
		want PairingState
	}{
		{domain.ErrPairingDenied, PairingDenied},
		{domain.ErrPairingExpired, PairingExpired},
	} {
		n := &fakeNet{polls: []error{tt.poll}}
		settings := &fakeSettings{}
		c, updates := newConnection(t, settings, n)
		if _, err := c.StartDevicePairing(context.Background(), domain.Server{URL: "https://romm.lan"}); err != nil {
			t.Fatal(err)
		}
		if u := updates.next(t); u.State != tt.want {
			t.Fatalf("got %+v, want %s", u, tt.want)
		}
		if settings.in != nil {
			t.Fatalf("%s saved a sign-in", tt.want)
		}
	}
}

func TestDevicePairingRunsOutOfTime(t *testing.T) {
	pending := make([]error, 10000)
	for i := range pending {
		pending[i] = domain.ErrPairingPending
	}
	c, updates := newConnection(t, &fakeSettings{}, &fakeNet{polls: pending})
	if _, err := c.StartDevicePairing(context.Background(), domain.Server{URL: "https://romm.lan"}); err != nil {
		t.Fatal(err)
	}
	if u := updates.next(t); u.State != PairingExpired {
		t.Fatalf("got %+v", u)
	}
}

func TestCancelledPairingEndsSilently(t *testing.T) {
	pending := make([]error, 10000)
	for i := range pending {
		pending[i] = domain.ErrPairingPending
	}
	c, updates := newConnection(t, &fakeSettings{}, &fakeNet{polls: pending})
	if _, err := c.StartDevicePairing(context.Background(), domain.Server{URL: "https://romm.lan"}); err != nil {
		t.Fatal(err)
	}
	c.CancelDevicePairing("CODE0001")
	select {
	case u := <-updates:
		t.Fatalf("cancelled pairing reported %+v", u)
	case <-time.After(1200 * time.Millisecond):
	}
}

func TestStaleCancelLeavesNewerPairing(t *testing.T) {
	pending := make([]error, 10000)
	for i := range pending {
		pending[i] = domain.ErrPairingPending
	}
	c, updates := newConnection(t, &fakeSettings{}, &fakeNet{polls: pending})
	server := domain.Server{URL: "https://romm.lan"}
	if _, err := c.StartDevicePairing(context.Background(), server); err != nil {
		t.Fatal(err)
	}
	if _, err := c.StartDevicePairing(context.Background(), server); err != nil {
		t.Fatal(err)
	}
	c.CancelDevicePairing("CODE0001")

	// The first pairing was replaced silently; the second still runs until it expires.
	if u := updates.next(t); u.State != PairingExpired {
		t.Fatalf("got %+v", u)
	}
}
