package app

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// moveSample is how many of this Deck's records Edit URL looks up at the new address. A different
// server tells itself apart on the first few; looking up every one would take a request each.
const moveSample = 10

// forgetWait bounds how long Forget server waits for cancelled downloads to stop.
const forgetWait = 10 * time.Second

// ServerSwitch changes which Server the plugin talks to: Edit URL moves to the same RomM at a new
// address, Forget server drops everything tied to it.
type ServerSwitch struct {
	conn      *Connection
	downloads *Downloads
	catalog   *Catalog
	log       *slog.Logger
}

func NewServerSwitch(conn *Connection, downloads *Downloads, catalog *Catalog, log *slog.Logger) *ServerSwitch {
	return &ServerSwitch{conn: conn, downloads: downloads, catalog: catalog, log: log}
}

// MoveResult is how Edit URL went.
type MoveResult struct {
	// Moved is set when the plugin now talks to the new address.
	Moved bool
	// Checked is how many of this Deck's games were looked up at the new address.
	Checked int
	// Why says why the new address looks like another server, when it wasn't moved to.
	Why    string
	Status ConnectionStatus
}

// Move points the plugin at server, the same RomM at a new address, keeping the sign-in and every
// record. It first checks the new address accepts this Deck's sign-in and knows its games under
// the same ids; when it doesn't, it's another server, and nothing changes. server is what
// CheckServer returned.
func (s *ServerSwitch) Move(ctx context.Context, server domain.Server) (MoveResult, error) {
	if err := checkServerURL(server); err != nil {
		return MoveResult{}, err
	}
	in, err := s.conn.settings.SignIn()
	if err != nil {
		return MoveResult{}, err
	}
	if in == nil {
		return MoveResult{}, &domain.Error{Kind: domain.KindInvalidInput, Message: "Set up RomM first."}
	}
	if in.SignedOut {
		return MoveResult{}, &domain.Error{
			Kind:    domain.KindSignedOut,
			Message: "Sign in again before changing the address, so the plugin can check it's the same server.",
		}
	}

	if err := s.conn.dial(server, in.Token).CheckToken(ctx); err != nil {
		if kindOf(err) == domain.KindSignedOut {
			return MoveResult{Why: "That server doesn't accept this Deck's sign-in, so it's likely a different RomM server."}, nil
		}
		return MoveResult{}, err
	}
	checked, mismatched, err := s.downloads.recognizedBy(ctx, s.downloads.dial(server, in.Token))
	if err != nil {
		return MoveResult{}, err
	}
	if mismatched > 0 {
		s.log.Info("new address is another server", "url", server.URL, "checked", checked, "mismatched", mismatched)
		return MoveResult{
			Checked: checked,
			Why:     fmt.Sprintf("%d of the %d games checked aren't the same there, so it's likely a different RomM server.", mismatched, checked),
		}, nil
	}

	moved := *in
	moved.Server = server
	if err := s.conn.saveSignIn(moved); err != nil {
		return MoveResult{}, fmt.Errorf("save sign-in: %w", err)
	}
	s.log.Info("server address changed", "from", in.Server.URL, "to", server.URL, "checked", checked)
	// The Catalog syncs anew from the new address, so nothing it held from the old one lingers.
	if err := s.catalog.Clear(ctx); err != nil {
		return MoveResult{}, err
	}
	status, err := s.conn.Status(ctx)
	if err != nil {
		return MoveResult{}, err
	}
	s.conn.events.Changed(status)
	s.catalog.Refresh(true)
	return MoveResult{Moved: true, Checked: checked, Status: status}, nil
}

// Forget drops the sign-in, the Catalog and every record tied to the Server, cancelling the
// downloads in the queue. The files the plugin placed stay on disk, User-managed.
func (s *ServerSwitch) Forget(ctx context.Context) (ConnectionStatus, error) {
	if err := s.downloads.forgetServer(ctx); err != nil {
		return ConnectionStatus{}, err
	}
	if err := s.catalog.Clear(ctx); err != nil {
		return ConnectionStatus{}, err
	}
	s.conn.CancelDevicePairing(s.conn.pairing())
	if err := s.conn.settings.ForgetSignIn(); err != nil {
		return ConnectionStatus{}, fmt.Errorf("forget sign-in: %w", err)
	}
	s.log.Info("server forgotten")
	status := ConnectionStatus{State: NotSetUp}
	s.conn.events.Changed(status)
	return status, nil
}

// pairing is the user code of the pending device pairing, empty when there is none.
func (c *Connection) pairing() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pairingCode
}

// recognizedBy looks up some of this Deck's games on server: a Plugin-owned game must still have
// the files the plugin downloaded, under the same file ids, and a Linked one must still be there.
// It returns how many it looked up and how many didn't match.
func (d *Downloads) recognizedBy(ctx context.Context, server DownloadServer) (checked, mismatched int, err error) {
	if d.store == nil {
		return 0, 0, nil
	}
	owned, err := d.store.OwnedIDs(ctx)
	if err != nil {
		return 0, 0, err
	}
	for _, id := range owned[:min(len(owned), moveSample)] {
		rec, ok, err := d.store.Owned(ctx, id)
		if err != nil {
			return 0, 0, err
		}
		if !ok {
			continue
		}
		files, err := server.ROMFiles(ctx, id)
		checked++
		if err != nil {
			if kindOf(err) != domain.KindNotFound {
				return 0, 0, err
			}
			mismatched++
			continue
		}
		for _, snap := range rec.Snapshot {
			if !slices.ContainsFunc(files, func(f domain.ROMFile) bool { return f.ID == snap.FileID }) {
				mismatched++
				break
			}
		}
	}
	links, err := d.store.LinkedGames(ctx)
	if err != nil {
		return 0, 0, err
	}
	for _, g := range links[:min(len(links), max(moveSample-checked, 0))] {
		_, err := server.ROMFiles(ctx, g.ROMID)
		checked++
		if err != nil {
			if kindOf(err) != domain.KindNotFound {
				return 0, 0, err
			}
			mismatched++
		}
	}
	return checked, mismatched, nil
}

// forgetServer cancels every download and drops every record tied to the Server. Deleting games
// must finish first, since what it deletes is decided by those records.
func (d *Downloads) forgetServer(ctx context.Context) error {
	if d.store == nil {
		return nil
	}
	d.mu.Lock()
	deleting := len(d.deleting) > 0
	d.mu.Unlock()
	if deleting {
		return &domain.Error{Kind: domain.KindInvalidInput, Message: "Wait for the games being deleted to finish first."}
	}
	for _, j := range d.List() {
		d.Cancel(j.ROMID)
	}
	deadline := time.Now().Add(forgetWait)
	for len(d.List()) > 0 {
		if time.Now().After(deadline) {
			return &domain.Error{Kind: domain.KindInvalidInput, Message: "Downloads are still stopping. Try again in a moment."}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	if err := d.store.ForgetServer(ctx); err != nil {
		return fmt.Errorf("forget records: %w", err)
	}
	d.mu.Lock()
	clear(d.checks)
	clear(d.finds)
	clear(d.found)
	d.mu.Unlock()
	d.scanMu.Lock()
	clear(d.scans)
	d.scanMu.Unlock()
	return nil
}

// Clear empties the Catalog and says so.
func (c *Catalog) Clear(ctx context.Context) error {
	c.mu.Lock()
	if c.retry != nil {
		c.retry.Stop()
		c.retry = nil
	}
	c.failures, c.problem = 0, nil
	c.mu.Unlock()
	if err := c.store.Clear(ctx); err != nil {
		return fmt.Errorf("clear catalog: %w", err)
	}
	c.announce()
	return nil
}
