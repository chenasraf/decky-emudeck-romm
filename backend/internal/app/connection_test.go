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

func (f *fakeSettings) ForgetSignIn() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.in = nil
	return nil
}

type fakeIdentity struct{}

func (fakeIdentity) Device() (domain.DeviceIdentity, error) {
	return domain.DeviceIdentity{Identifier: "abc", Name: "Steam Deck (steamdeck)", Platform: "steamos"}, nil
}

// fakeNet answers heartbeats per URL, redeems one pair code, accepts one password, and replays
// poll answers in order.
type fakeNet struct {
	heartbeats map[string]func(domain.Server) (string, error)
	pairCode   string
	password   string
	// grantErr fails a password grant whose password is right.
	grantErr error
	// allowed is what the signed-in account may grant a pending pairing.
	allowed []string
	// revoked holds the tokens RomM no longer accepts.
	revoked map[string]bool

	mu          sync.Mutex
	tokenChecks int
	dialed      []domain.Server
	started     []domain.DeviceIdentity
	scopes      []string
	polls       []error
	pollCalls   int
	pollTimes   []time.Time
	approved    domain.SignIn
	// grantScopes, approvals and approvedScopes record the password sign-in's calls.
	grantScopes    []string
	approvals      []string
	approvedScopes []string
}

type fakeRomM struct {
	net    *fakeNet
	server domain.Server
	token  string
}

func (n *fakeNet) dial(server domain.Server, token string) RomM {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.dialed = append(n.dialed, server)
	return fakeRomM{net: n, server: server, token: token}
}

func (r fakeRomM) PasswordGrant(_ context.Context, username, password string, scopes []string) (string, error) {
	r.net.mu.Lock()
	defer r.net.mu.Unlock()
	r.net.grantScopes = scopes
	if username != "chen" || password != r.net.password {
		return "", &domain.Error{Kind: domain.KindBadCredentials, Message: "bad credentials"}
	}
	if r.net.grantErr != nil {
		return "", r.net.grantErr
	}
	return "jwt", nil
}

func (r fakeRomM) DeviceAuthScopes(_ context.Context, userCode string) ([]string, error) {
	if r.token != "jwt" {
		return nil, errors.New("not signed in")
	}
	return r.net.allowed, nil
}

func (r fakeRomM) ApproveDeviceAuth(_ context.Context, userCode string, scopes []string) error {
	if r.token != "jwt" {
		return errors.New("not signed in")
	}
	r.net.mu.Lock()
	defer r.net.mu.Unlock()
	r.net.approvals = append(r.net.approvals, userCode)
	r.net.approvedScopes = scopes
	return nil
}

func (r fakeRomM) Heartbeat(context.Context) (string, error) {
	if h, ok := r.net.heartbeats[r.server.URL]; ok {
		return h(r.server)
	}
	return "", &domain.Error{Kind: domain.KindUnreachable, Message: "unreachable"}
}

func (r fakeRomM) CheckToken(context.Context) error {
	r.net.mu.Lock()
	defer r.net.mu.Unlock()
	r.net.tokenChecks++
	if r.net.revoked[r.token] {
		return &domain.Error{Kind: domain.KindSignedOut, Message: "signed out"}
	}
	return nil
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

type updates chan PairingUpdate

type fakeEvents struct {
	pairing updates
	changed chan ConnectionStatus
}

func (e fakeEvents) Pairing(u PairingUpdate)    { e.pairing <- u }
func (e fakeEvents) Changed(s ConnectionStatus) { e.changed <- s }

// next returns the next connection change, failing when none comes.
func (e fakeEvents) next(t *testing.T) ConnectionStatus {
	t.Helper()
	select {
	case s := <-e.changed:
		return s
	case <-time.After(time.Second):
		t.Fatal("no connection change")
		return ConnectionStatus{}
	}
}

// none fails if a connection change is waiting.
func (e fakeEvents) none(t *testing.T) {
	t.Helper()
	select {
	case s := <-e.changed:
		t.Fatalf("unexpected connection change %+v", s)
	default:
	}
}

func newConnection(t *testing.T, settings Settings, n *fakeNet) (*Connection, updates) {
	t.Helper()
	c, events := newConnectionWithEvents(t, settings, n)
	return c, events.pairing
}

func newConnectionWithEvents(t *testing.T, settings Settings, n *fakeNet) (*Connection, fakeEvents) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	events := fakeEvents{pairing: make(updates, 4), changed: make(chan ConnectionStatus, 16)}
	c := NewConnection(ctx, settings, n.dial, fakeIdentity{}, events, slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.slowDownStep = 20 * time.Millisecond
	return c, events
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

func TestSignInWithPassword(t *testing.T) {
	allowed := []string{"me.read", "platforms.read", "roms.read", "collections.read", "firmware.read"}
	n := &fakeNet{
		heartbeats: map[string]func(domain.Server) (string, error){"https://romm.lan": version("5.4.0")},
		password:   "hunter2",
		allowed:    allowed,
		approved:   domain.SignIn{Token: "rmm_dev", DeviceID: "dev-1", Scopes: allowed},
	}
	settings := &fakeSettings{}
	c, _ := newConnection(t, settings, n)
	server := domain.Server{URL: "https://romm.lan", CertFingerprint: "AA:BB"}

	if _, err := c.SignInWithPassword(context.Background(), server, "chen", "wrong"); kindOf(err) != domain.KindBadCredentials {
		t.Fatalf("want bad_credentials, got %v", err)
	}
	if _, err := c.SignInWithPassword(context.Background(), server, "  ", "hunter2"); kindOf(err) != domain.KindInvalidInput {
		t.Fatalf("want invalid_input for a blank username, got %v", err)
	}
	if settings.in != nil || len(n.started) != 0 {
		t.Fatalf("a failed sign-in went on: saved %+v, started %+v", settings.in, n.started)
	}

	status, err := c.SignInWithPassword(context.Background(), server, " chen ", "hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if status.State != Connected || len(status.DisabledFeatures) != 1 || status.DisabledFeatures[0].Feature != domain.FeatureSaveSync {
		t.Fatalf("status %+v", status)
	}
	// Only the device-bound token is kept: no password, no OAuth token.
	if settings.in.Token != "rmm_dev" || settings.in.DeviceID != "dev-1" || settings.in.Server != server {
		t.Fatalf("saved %+v", settings.in)
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	if !slices.Equal(n.grantScopes, domain.PasswordScopes()) {
		t.Fatalf("password grant asked for %v", n.grantScopes)
	}
	// The pairing is the one QR pairing would start, approved with what the account may grant.
	if len(n.started) != 1 || n.started[0].Identifier != "abc" || !slices.Equal(n.scopes, domain.RequestedScopes()) {
		t.Fatalf("started %+v with %v", n.started, n.scopes)
	}
	if !slices.Equal(n.approvals, []string{"CODE0001"}) || !slices.Equal(n.approvedScopes, allowed) {
		t.Fatalf("approved %v with %v", n.approvals, n.approvedScopes)
	}
}

func TestSignInWithPasswordMissingPermission(t *testing.T) {
	n := &fakeNet{
		password: "hunter2",
		grantErr: &domain.Error{Kind: domain.KindMissingPermission, Message: "no me.write"},
	}
	settings := &fakeSettings{}
	c, _ := newConnection(t, settings, n)

	_, err := c.SignInWithPassword(context.Background(), domain.Server{URL: "https://romm.lan"}, "chen", "hunter2")
	if kindOf(err) != domain.KindMissingPermission {
		t.Fatalf("want missing_permission, got %v", err)
	}
	if settings.in != nil || len(n.started) != 0 {
		t.Fatal("a refused sign-in went on")
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

func TestRevokedTokenSignsOutOnce(t *testing.T) {
	n := &fakeNet{heartbeats: map[string]func(domain.Server) (string, error){"https://romm.lan": version("5.4.0")}}
	server := domain.Server{URL: "https://romm.lan", CertFingerprint: "AA:BB"}
	settings := &fakeSettings{in: &domain.SignIn{Server: server, Token: "rmm_old", DeviceID: "dev-1"}}
	c, events := newConnectionWithEvents(t, settings, n)

	if s, _ := c.Status(context.Background()); s.State != Connected {
		t.Fatalf("before revoking: %+v", s)
	}
	n.mu.Lock()
	n.revoked = map[string]bool{"rmm_old": true}
	n.mu.Unlock()

	s, err := c.Status(context.Background())
	if err != nil || s.State != SignedOut || s.ServerURL != server.URL || s.CertFingerprint != "AA:BB" {
		t.Fatalf("after revoking: %+v, %v", s, err)
	}
	if got := events.next(t); got.State != SignedOut || got.ServerURL != server.URL {
		t.Fatalf("event %+v", got)
	}
	// Every local record stays, so signing in again picks up where it left off.
	if !settings.in.SignedOut || settings.in.Token != "rmm_old" || settings.in.DeviceID != "dev-1" || settings.in.Server != server {
		t.Fatalf("saved %+v", settings.in)
	}

	checks := n.tokenChecks
	for range 3 {
		if s, _ := c.Status(context.Background()); s.State != SignedOut {
			t.Fatalf("still signed out: %+v", s)
		}
	}
	if n.tokenChecks != checks {
		t.Fatal("a signed-out status asked RomM again")
	}
	// Only the lapse itself is reported, not every check after it.
	events.none(t)
}

func TestWatchNoticesRevokedToken(t *testing.T) {
	n := &fakeNet{
		heartbeats: map[string]func(domain.Server) (string, error){"https://romm.lan": version("5.4.0")},
		revoked:    map[string]bool{"rmm_old": true},
	}
	settings := &fakeSettings{in: &domain.SignIn{Server: domain.Server{URL: "https://romm.lan"}, Token: "rmm_old"}}
	c, events := newConnectionWithEvents(t, settings, n)

	go c.Watch(t.Context(), 5*time.Millisecond)

	if got := events.next(t); got.State != SignedOut {
		t.Fatalf("change %+v", got)
	}
}

func TestSignInAgainResumesPausedWork(t *testing.T) {
	n := &fakeNet{
		heartbeats: map[string]func(domain.Server) (string, error){"https://romm.lan": version("5.4.0")},
		pairCode:   "ABCD2345",
	}
	server := domain.Server{URL: "https://romm.lan"}
	old := domain.SignIn{Server: server, Token: "rmm_old", DeviceID: "dev-1", SignedOut: true}
	settings := &fakeSettings{in: &old}
	c, events := newConnectionWithEvents(t, settings, n)

	resumed := make(chan error, 1)
	go func() { resumed <- c.WaitSignedIn(context.Background()) }()
	select {
	case err := <-resumed:
		t.Fatalf("work ran while signed out: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	if s, err := c.SignInWithPairCode(context.Background(), server, "ABCD2345"); err != nil || s.State != Connected {
		t.Fatalf("sign in again: %+v, %v", s, err)
	}
	select {
	case err := <-resumed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("paused work didn't resume after signing in again")
	}
	if settings.in.SignedOut || settings.in.Token != "rmm_new" {
		t.Fatalf("saved %+v", settings.in)
	}
	// The QAM hears of the sign-in even when the page that made it is gone.
	if got := events.next(t); got.State != Connected {
		t.Fatalf("change %+v", got)
	}

	// A request made with the old token that fails late leaves the new sign-in alone.
	_ = c.observe(old, &domain.Error{Kind: domain.KindSignedOut})
	if settings.in.SignedOut {
		t.Fatal("a stale rejection signed the new token out")
	}
	events.none(t)
}

func TestWaitSignedInStopsWithContext(t *testing.T) {
	c, _ := newConnection(t, &fakeSettings{}, &fakeNet{})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := c.WaitSignedIn(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
}

func TestDevicePairingApproved(t *testing.T) {
	n := &fakeNet{
		heartbeats: map[string]func(domain.Server) (string, error){"https://romm.lan": version("5.4.0")},
		polls:      []error{domain.ErrPairingPending, domain.ErrPairingSlowDown, &domain.Error{Kind: domain.KindUnreachable}, domain.ErrPairingPending},
		approved:   domain.SignIn{Token: "rmm_dev", DeviceID: "dev-1", Scopes: []string{"me.read", "roms.read", "platforms.read", "collections.read"}},
	}
	settings := &fakeSettings{}
	c, events := newConnectionWithEvents(t, settings, n)
	updates := events.pairing
	server := domain.Server{URL: "https://romm.lan"}

	pairing, err := c.StartDevicePairing(context.Background(), server)
	if err != nil || pairing.UserCode != "CODE0001" {
		t.Fatalf("got %+v, %v", pairing, err)
	}
	u := updates.next(t)
	if u.State != PairingApproved || u.Status.State != Connected {
		t.Fatalf("update %+v", u)
	}
	// A pairing approved after the setup page closed still reaches the QAM.
	if got := events.next(t); got.State != Connected {
		t.Fatalf("change %+v", got)
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
