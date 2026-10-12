package romm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

const ua = "decky-emudeck-romm/test"

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

	v, err := New(domain.Server{URL: srv.URL}, "", ua).Heartbeat(context.Background())
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

	_, err := New(domain.Server{URL: srv.URL}, "", ua).Heartbeat(context.Background())
	if kindOf(err) != domain.KindNotRomM {
		t.Fatalf("want not_romm, got %v", err)
	}
}

func TestHeartbeatUnreachable(t *testing.T) {
	srv := httptest.NewServer(heartbeatHandler("5.4.0"))
	url := srv.URL
	srv.Close()

	_, err := New(domain.Server{URL: url}, "", ua).Heartbeat(context.Background())
	if kindOf(err) != domain.KindUnreachable {
		t.Fatalf("want unreachable, got %v", err)
	}
}

func TestSelfSignedCertificatePinning(t *testing.T) {
	srv := httptest.NewTLSServer(heartbeatHandler("5.4.0"))
	defer srv.Close()
	want := Fingerprint(srv.Certificate())

	_, err := New(domain.Server{URL: srv.URL}, "", ua).Heartbeat(context.Background())
	var de *domain.Error
	if !errors.As(err, &de) || de.Kind != domain.KindUntrustedCert {
		t.Fatalf("want untrusted_cert, got %v", err)
	}
	if de.Fingerprint != want {
		t.Fatalf("fingerprint %s, want %s", de.Fingerprint, want)
	}

	if _, err := New(domain.Server{URL: srv.URL, CertFingerprint: want}, "", ua).Heartbeat(context.Background()); err != nil {
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

	_, err := New(domain.Server{URL: srv.URL, CertFingerprint: pin}, "", ua).Heartbeat(context.Background())
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
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "raw_token": "rmm_abc"})
		case body.Code == "SLOWDOWN":
			w.WriteHeader(http.StatusTooManyRequests)
		case body.Code == "DISABLED":
			w.WriteHeader(http.StatusForbidden)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := New(domain.Server{URL: srv.URL}, "", ua)

	tok, err := c.ExchangePairCode(context.Background(), "GOODCODE")
	if err != nil || tok != "rmm_abc" {
		t.Fatalf("got %q, %v", tok, err)
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
