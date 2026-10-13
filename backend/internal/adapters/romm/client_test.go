package romm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

const (
	ver = "test"
	ua  = "decky-emudeck-romm/test"
)

func heartbeatHandler(version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/heartbeat" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"SYSTEM": map[string]any{"VERSION": version}})
	}
}

func kindOf(err error) domain.Kind {
	var de *domain.Error
	if errors.As(err, &de) {
		return de.Kind
	}
	return ""
}

func TestHeartbeatSendsUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.UserAgent()
		heartbeatHandler("5.4.0")(w, r)
	}))
	defer srv.Close()

	v, err := New(domain.Server{URL: srv.URL}, "", ver).Heartbeat(context.Background())
	if err != nil || v != "5.4.0" {
		t.Fatalf("got %q, %v", v, err)
	}
	if gotUA != ua {
		t.Fatalf("User-Agent %q, want %q", gotUA, ua)
	}
}

func TestHeartbeatNotRomM(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>hello</html>"))
	}))
	defer srv.Close()

	_, err := New(domain.Server{URL: srv.URL}, "", ver).Heartbeat(context.Background())
	if kindOf(err) != domain.KindNotRomM {
		t.Fatalf("want not_romm, got %v", err)
	}
}

func TestHeartbeatUnreachable(t *testing.T) {
	srv := httptest.NewServer(heartbeatHandler("5.4.0"))
	url := srv.URL
	srv.Close()

	_, err := New(domain.Server{URL: url}, "", ver).Heartbeat(context.Background())
	if kindOf(err) != domain.KindUnreachable {
		t.Fatalf("want unreachable, got %v", err)
	}
}

func TestSelfSignedCertificatePinning(t *testing.T) {
	srv := httptest.NewTLSServer(heartbeatHandler("5.4.0"))
	defer srv.Close()
	want := Fingerprint(srv.Certificate())

	_, err := New(domain.Server{URL: srv.URL}, "", ver).Heartbeat(context.Background())
	var de *domain.Error
	if !errors.As(err, &de) || de.Kind != domain.KindUntrustedCert {
		t.Fatalf("want untrusted_cert, got %v", err)
	}
	if de.Fingerprint != want {
		t.Fatalf("fingerprint %s, want %s", de.Fingerprint, want)
	}

	if _, err := New(domain.Server{URL: srv.URL, CertFingerprint: want}, "", ver).Heartbeat(context.Background()); err != nil {
		t.Fatalf("pinned: %v", err)
	}
}

func TestChangedCertificateFailsAgain(t *testing.T) {
	old := httptest.NewTLSServer(heartbeatHandler("5.4.0"))
	pin := Fingerprint(old.Certificate())
	old.Close()

	// httptest reuses its certificate across servers, so build one with a different key.
	srv := httptest.NewUnstartedServer(heartbeatHandler("5.4.0"))
	srv.TLS = newSelfSignedConfig(t)
	srv.StartTLS()
	defer srv.Close()

	_, err := New(domain.Server{URL: srv.URL, CertFingerprint: pin}, "", ver).Heartbeat(context.Background())
	var de *domain.Error
	if !errors.As(err, &de) || de.Kind != domain.KindUntrustedCert || de.Fingerprint == pin {
		t.Fatalf("want untrusted_cert with the new fingerprint, got %v", err)
	}
}

func TestExchangePairCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Code string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case r.Method != http.MethodPost || r.URL.Path != "/api/client-tokens/exchange":
			http.NotFound(w, r)
		case body.Code == "GOODCODE":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "raw_token": "rmm_abc", "scopes": []string{"roms.read"}, "device_id": nil})
		case body.Code == "SLOWDOWN":
			w.WriteHeader(http.StatusTooManyRequests)
		case body.Code == "DISABLED":
			w.WriteHeader(http.StatusForbidden)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := New(domain.Server{URL: srv.URL}, "", ver)

	in, err := c.ExchangePairCode(context.Background(), "GOODCODE")
	if err != nil || in.Token != "rmm_abc" || in.DeviceID != "" || !slices.Equal(in.Scopes, []string{"roms.read"}) {
		t.Fatalf("got %+v, %v", in, err)
	}
	for code, want := range map[string]domain.Kind{
		"BADCODE2": domain.KindInvalidPairCode,
		"SLOWDOWN": domain.KindRateLimited,
		"DISABLED": domain.KindPairRejected,
	} {
		if _, err := c.ExchangePairCode(context.Background(), code); kindOf(err) != want {
			t.Errorf("%s: want %s, got %v", code, want, err)
		}
	}
}

func TestStartDeviceAuth(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/romm/api/auth/device/init" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":                "secret",
			"user_code":                  "WDJB-MJHT",
			"verification_path":          "/pair/device",
			"verification_path_complete": "/pair/device?user_code=WDJB-MJHT",
			"expires_in":                 600,
			"interval":                   5,
		})
	}))
	defer srv.Close()

	device := domain.DeviceIdentity{Identifier: "abc", Name: "Steam Deck (steamdeck)", Platform: "steamos"}
	p, err := New(domain.Server{URL: srv.URL + "/romm"}, "", ver).StartDeviceAuth(context.Background(), device, []string{"roms.read"})
	if err != nil {
		t.Fatal(err)
	}
	want := domain.DevicePairing{
		DeviceCode:      "secret",
		UserCode:        "WDJB-MJHT",
		VerificationURL: srv.URL + "/romm/pair/device?user_code=WDJB-MJHT",
		ExpiresIn:       600 * time.Second,
		Interval:        5 * time.Second,
	}
	if p != want {
		t.Fatalf("got %+v, want %+v", p, want)
	}
	if got["client_device_identifier"] != "abc" || got["client"] != "decky-emudeck-romm" || got["client_version"] != ver ||
		got["platform"] != "steamos" || got["name"] != "Steam Deck (steamdeck)" {
		t.Fatalf("sent %v", got)
	}
}

func TestPollDeviceAuth(t *testing.T) {
	answer := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch answer {
		case "ok":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "rmm_dev", "device_id": "dev-1", "scopes": []string{"roms.read"}, "expires_at": nil})
		case "429":
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"detail": answer})
		}
	}))
	defer srv.Close()
	c := New(domain.Server{URL: srv.URL}, "", ver)

	answer = "ok"
	in, err := c.PollDeviceAuth(context.Background(), "secret")
	if err != nil || in.Token != "rmm_dev" || in.DeviceID != "dev-1" || !slices.Equal(in.Scopes, []string{"roms.read"}) {
		t.Fatalf("got %+v, %v", in, err)
	}
	for detail, want := range map[string]error{
		"authorization_pending": domain.ErrPairingPending,
		"slow_down":             domain.ErrPairingSlowDown,
		"429":                   domain.ErrPairingSlowDown,
		"access_denied":         domain.ErrPairingDenied,
		"expired_token":         domain.ErrPairingExpired,
	} {
		answer = detail
		if _, err := c.PollDeviceAuth(context.Background(), "secret"); !errors.Is(err, want) {
			t.Errorf("%s: want %v, got %v", detail, want, err)
		}
	}
}

func TestCheckToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/users/me" {
			http.NotFound(w, r)
			return
		}
		switch r.Header.Get("Authorization") {
		case "Bearer rmm_ok":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "username": "chen"})
		case "Bearer rmm_narrow":
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"detail": "Forbidden"})
		case "Bearer rmm_broken":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"detail": "Invalid token"})
		}
	}))
	defer srv.Close()
	server := domain.Server{URL: srv.URL}

	for token, want := range map[string]domain.Kind{"rmm_ok": "", "rmm_narrow": "", "rmm_revoked": domain.KindSignedOut} {
		if err := New(server, token, ver).CheckToken(context.Background()); kindOf(err) != want || (want == "" && err != nil) {
			t.Errorf("%s: want %q, got %v", token, want, err)
		}
	}
	if err := New(server, "rmm_broken", ver).CheckToken(context.Background()); err == nil || kindOf(err) == domain.KindSignedOut {
		t.Errorf("a server error should fail without signing out, got %v", err)
	}
}

func TestPasswordGrant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/token" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			http.NotFound(w, r)
			return
		}
		if r.FormValue("grant_type") != "password" || r.FormValue("scope") != "me.read me.write" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch r.FormValue("username") + ":" + r.FormValue("password") {
		case "chen:hunter2":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "jwt", "refresh_token": "r", "token_type": "bearer"})
		case "viewer:hunter2":
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"detail": "Insufficient scope"})
		case "disabled:hunter2":
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"detail": "User account is disabled"})
		default:
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"detail": "Invalid username or password"})
		}
	}))
	defer srv.Close()
	c := New(domain.Server{URL: srv.URL}, "", ver)
	scopes := domain.PasswordScopes()

	if token, err := c.PasswordGrant(context.Background(), "chen", "hunter2", scopes); err != nil || token != "jwt" {
		t.Fatalf("got %q, %v", token, err)
	}
	for user, want := range map[string]domain.Kind{
		"nobody":   domain.KindBadCredentials,
		"viewer":   domain.KindMissingPermission,
		"disabled": domain.KindAccountDisabled,
	} {
		if _, err := c.PasswordGrant(context.Background(), user, "hunter2", scopes); kindOf(err) != want {
			t.Errorf("%s: want %s, got %v", user, want, err)
		}
	}
}

func TestApproveOwnDeviceAuth(t *testing.T) {
	var approved map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer jwt" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/auth/device/pending/ABCD2345":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"requested_scopes": []string{"me.read", "roms.read", "assets.write"},
				"allowed_scopes":   []string{"me.read", "roms.read"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/auth/device/approve":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["user_code"] != "ABCD2345" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			approved = body
			_ = json.NewEncoder(w).Encode(map[string]any{"device_id": "dev-1", "device_name": "Steam Deck (steamdeck)"})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"detail": "Unknown or expired code"})
		}
	}))
	defer srv.Close()
	c := New(domain.Server{URL: srv.URL}, "jwt", ver)

	scopes, err := c.DeviceAuthScopes(context.Background(), "ABCD2345")
	if err != nil || !slices.Equal(scopes, []string{"me.read", "roms.read"}) {
		t.Fatalf("got %v, %v", scopes, err)
	}
	if err := c.ApproveDeviceAuth(context.Background(), "ABCD2345", scopes); err != nil {
		t.Fatal(err)
	}
	if approved["user_code"] != "ABCD2345" || approved["expires_in"] != "never" || len(approved["approved_scopes"].([]any)) != 2 {
		t.Fatalf("approved with %v", approved)
	}
	if _, err := c.DeviceAuthScopes(context.Background(), "GONE2345"); err == nil {
		t.Fatal("want an error for an expired pairing")
	}
	if err := c.ApproveDeviceAuth(context.Background(), "GONE2345", scopes); err == nil {
		t.Fatal("want an error for an expired pairing")
	}
}
