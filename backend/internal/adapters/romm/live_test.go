package romm_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/romm"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/app"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// The live tests run against the real server in ROMM_URL (see `make test-live`) and are skipped
// without it.

func liveServer(t *testing.T) domain.Server {
	t.Helper()
	url := os.Getenv("ROMM_URL")
	if url == "" {
		t.Skip("ROMM_URL not set")
	}
	dial := func(server domain.Server, token string) app.RomM { return romm.New(server, token, "test") }
	conn := app.NewConnection(context.Background(), nil, dial, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	server, version, err := conn.CheckServer(context.Background(), url, os.Getenv("ROMM_CERT_FINGERPRINT"))
	if err != nil {
		t.Fatalf("check %s: %v", url, err)
	}
	t.Logf("RomM %s at %s", version, server.URL)
	return server
}

func TestLiveCheckServer(t *testing.T) {
	liveServer(t)
}

// TestLiveStartDeviceAuth checks RomM accepts the pairing request the plugin sends. Nobody
// approves it, so it expires on the server without creating a Device.
func TestLiveStartDeviceAuth(t *testing.T) {
	server := liveServer(t)
	device := domain.DeviceIdentity{Identifier: "decky-emudeck-romm-live-test", Name: "Steam Deck (live test)", Platform: "steamos"}

	p, err := romm.New(server, "", "test").StartDeviceAuth(context.Background(), device, domain.RequestedScopes())
	if err != nil {
		t.Fatal(err)
	}
	if p.DeviceCode == "" || p.UserCode == "" || p.ExpiresIn <= 0 || !strings.HasPrefix(p.VerificationURL, server.URL+"/") {
		t.Fatalf("got %+v", p)
	}
	t.Logf("user code %s, expires in %v, poll every %v", p.UserCode, p.ExpiresIn, p.Interval)
}
