package romm_test

import (
	"context"
	"os"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/romm"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/app"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// TestLiveCheckServer runs check_server's logic against the real server in ROMM_URL (see
// `make test-live`), and is skipped without it.
func TestLiveCheckServer(t *testing.T) {
	url := os.Getenv("ROMM_URL")
	if url == "" {
		t.Skip("ROMM_URL not set")
	}
	dial := func(server domain.Server, token string) app.RomM {
		return romm.New(server, token, "decky-emudeck-romm/test")
	}
	conn := app.NewConnection(nil, dial)

	server, version, err := conn.CheckServer(context.Background(), url, os.Getenv("ROMM_CERT_FINGERPRINT"))
	if err != nil {
		t.Fatalf("check %s: %v", url, err)
	}
	t.Logf("RomM %s at %s", version, server.URL)
}
