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
