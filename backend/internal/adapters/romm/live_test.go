package romm_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strconv"
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

// liveClient is a client for the server in ROMM_URL signed in with the read-only client token in
// ROMM_TOKEN, skipping the test without one.
func liveClient(t *testing.T) *romm.Client {
	t.Helper()
	server := liveServer(t)
	token := os.Getenv("ROMM_TOKEN")
	if token == "" {
		t.Skip("ROMM_TOKEN not set")
	}
	return romm.New(server, token, "test")
}

func TestLiveCollections(t *testing.T) {
	cols, err := liveClient(t).Collections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cols {
		if c.ID == 0 || c.Name == "" || c.ROMIDs == nil {
			t.Fatalf("collection %+v", c)
		}
		t.Logf("%s %d %q: %d games", c.Kind, c.ID, c.Name, len(c.ROMIDs))
	}
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

// TestLiveStates reads the save states of the game in ROMM_STATES_ROM: a game with GBA states saved
// in RomM's web player.
func TestLiveStates(t *testing.T) {
	c := liveClient(t)
	id, err := strconv.Atoi(os.Getenv("ROMM_STATES_ROM"))
	if err != nil {
		t.Skip("ROMM_STATES_ROM not set")
	}
	ctx := context.Background()
	states, err := c.States(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) == 0 {
		t.Fatalf("game %d has no states", id)
	}
	s := states[len(states)-1]
	t.Logf("%d states; newest listed %d %q, %s, %d bytes, screenshot %q", len(states), s.ID, s.FileName, s.CreatedAt, s.SizeBytes, s.Screenshot)
	if s.CreatedAt.IsZero() || s.Emulator == "" {
		t.Fatalf("state %+v", s)
	}
	data, err := c.DownloadState(ctx, s.ID)
	if err != nil || int64(len(data)) != s.SizeBytes {
		t.Fatalf("downloaded %d bytes, %v", len(data), err)
	}
	if _, err := domain.GBAStateForRetroArch(data); err != nil {
		t.Fatalf("convert for RetroArch: %v", err)
	}
	if s.Screenshot != "" {
		shot, err := c.StateScreenshot(ctx, s.Screenshot)
		if err != nil || !strings.HasPrefix(string(shot), "\x89PNG") {
			t.Fatalf("screenshot %d bytes, %v", len(shot), err)
		}
	}
}
