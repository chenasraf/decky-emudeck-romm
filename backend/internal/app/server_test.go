package app

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

const newAddress = "https://romm.new"

// serverSwitch returns a ServerSwitch over f's downloads, with RomM answering at newAddress too.
// other, when not nil, is what answers there instead of f's server.
func (f *downloadFixture) serverSwitch(t *testing.T, other *fakeContent) (*ServerSwitch, *fakeNet) {
	t.Helper()
	ok := func(domain.Server) (string, error) { return "4.4.0", nil }
	net := &fakeNet{heartbeats: map[string]func(domain.Server) (string, error){"https://romm.lan": ok, newAddress: ok}, revoked: map[string]bool{}}
	conn, _ := newConnectionWithEvents(t, f.settings, net)
	f.downloads.conn = conn
	f.downloads.dial = func(server domain.Server, _ string) DownloadServer {
		if server.URL == newAddress && other != nil {
			return other
		}
		return f.server
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	library := newLibrary(3)
	catalog := NewCatalog(t.Context(), f.catalog, f.downloads, conn, func(domain.Server, string) CatalogServer { return library },
		func(CatalogStatus) {}, log)
	return NewServerSwitch(conn, f.downloads, catalog, log), net
}

func TestEditURLToTheSameServerKeepsEverything(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	f.installedChrono(t)
	servers, _ := f.serverSwitch(t, nil)

	r, err := servers.Move(ctx, domain.Server{URL: newAddress})
	if err != nil || !r.Moved || r.Checked != 1 || r.Status.ServerURL != newAddress {
		t.Fatalf("move %+v, %v", r, err)
	}
	if in, _ := f.settings.SignIn(); in.Server.URL != newAddress || in.Token != "rmm_x" {
		t.Fatalf("sign-in %+v", in)
	}
	if _, ok, _ := f.owned.Owned(ctx, 1); !ok {
		t.Fatal("record dropped")
	}
}

func TestEditURLToAnotherServerWarns(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	f.installedChrono(t)
	other := &fakeContent{files: map[int][]domain.ROMFile{1: {{ID: 99, Name: "Something else.sfc", Category: "game"}}}}
	servers, net := f.serverSwitch(t, other)

	r, err := servers.Move(ctx, domain.Server{URL: newAddress})
	if err != nil || r.Moved || r.Why == "" || r.Checked != 1 {
		t.Fatalf("move %+v, %v", r, err)
	}
	if in, _ := f.settings.SignIn(); in.Server.URL != "https://romm.lan" {
		t.Fatalf("sign-in changed %+v", in)
	}

	// A server that doesn't take the token is another one too.
	net.revoked["rmm_x"] = true
	if r, err := servers.Move(ctx, domain.Server{URL: newAddress}); err != nil || r.Moved || r.Why == "" {
		t.Fatalf("move with a rejected token %+v, %v", r, err)
	}
}

func TestForgetServerLeavesFiles(t *testing.T) {
	f := newDownloadFixture(t, 2)
	ctx := context.Background()
	path := f.installedChrono(t)
	servers, _ := f.serverSwitch(t, nil)

	status, err := servers.Forget(ctx)
	if err != nil || status.State != NotSetUp {
		t.Fatalf("forget %+v, %v", status, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file gone: %v", err)
	}
	if ids, _ := f.owned.OwnedIDs(ctx); len(ids) != 0 {
		t.Fatalf("records left %v", ids)
	}
	if in, _ := f.settings.SignIn(); in != nil {
		t.Fatalf("sign-in kept %+v", in)
	}
	if _, roms, _ := f.catalog.Counts(ctx); roms != 0 {
		t.Fatalf("catalog holds %d games", roms)
	}
}
