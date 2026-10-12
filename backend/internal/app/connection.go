// Package app holds the plugin's use cases. It reaches the outside world only through the ports
// declared here, which the adapters implement.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// statusTimeout bounds the reachability check behind Status, which the QAM runs every time it
// opens.
const statusTimeout = 5 * time.Second

// RomM is the part of the RomM API the connection use cases need. Sign-ins it returns have no
// Server.
type RomM interface {
	Heartbeat(ctx context.Context) (string, error)
	ExchangePairCode(ctx context.Context, code string) (domain.SignIn, error)
	StartDeviceAuth(ctx context.Context, device domain.DeviceIdentity, scopes []string) (domain.DevicePairing, error)
	PollDeviceAuth(ctx context.Context, deviceCode string) (domain.SignIn, error)
}

// Dialer returns a RomM client for server, authenticated with token when it isn't empty.
type Dialer func(server domain.Server, token string) RomM

// Settings stores the sign-in across restarts.
type Settings interface {
	SignIn() (*domain.SignIn, error)
	SaveSignIn(domain.SignIn) error
}

// Identity says how this Deck introduces itself to RomM.
type Identity interface {
	Device() (domain.DeviceIdentity, error)
}

type ConnectionState string

const (
	NotSetUp  ConnectionState = "not_set_up"
	Connected ConnectionState = "connected"
	Offline   ConnectionState = "offline"
)

type ConnectionStatus struct {
	State     ConnectionState
	ServerURL string
	Version   string
	// Problem says why the server is Offline.
	Problem          error
	DisabledFeatures []domain.DisabledFeature
}

type PairingState string

const (
	PairingApproved PairingState = "approved"
	PairingDenied   PairingState = "denied"
	PairingExpired  PairingState = "expired"
	// PairingFailed means RomM approved the pairing but the plugin couldn't keep the sign-in.
	PairingFailed PairingState = "failed"
)

// PairingUpdate is how a device pairing ended. Status is set when it was approved, and Problem
// when it failed.
type PairingUpdate struct {
	State   PairingState
	Status  ConnectionStatus
	Problem error
}

// PairingListener hears how each device pairing ends. A cancelled pairing ends silently.
type PairingListener func(PairingUpdate)

type Connection struct {
	settings  Settings
	dial      Dialer
	identity  Identity
	onPairing PairingListener
	log       *slog.Logger
	// base outlives single calls: device pairings poll under it after their call returns.
	base context.Context

	// slowDownStep is how much a slow_down answer stretches the poll interval, as RFC 8628 asks.
	slowDownStep time.Duration

	mu sync.Mutex
	// pairingCode and cancelPairing belong to the pending device pairing.
	pairingCode   string
	cancelPairing context.CancelFunc
}

// NewConnection returns the connection use cases. Background work stops when ctx ends.
func NewConnection(ctx context.Context, settings Settings, dial Dialer, identity Identity, onPairing PairingListener, log *slog.Logger) *Connection {
	return &Connection{
		settings:     settings,
		dial:         dial,
		identity:     identity,
		onPairing:    onPairing,
		log:          log,
		base:         ctx,
		slowDownStep: 5 * time.Second,
	}
}

// Status reports whether a server is set up and, if so, whether it answers right now.
func (c *Connection) Status(ctx context.Context) (ConnectionStatus, error) {
	in, err := c.settings.SignIn()
	if err != nil {
		return ConnectionStatus{}, err
	}
	if in == nil {
		return ConnectionStatus{State: NotSetUp}, nil
	}

	status := ConnectionStatus{ServerURL: in.Server.URL, DisabledFeatures: domain.DisabledFeatures(in.Scopes)}
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	version, err := c.dial(in.Server, in.Token).Heartbeat(ctx)
	if err != nil {
		status.State = Offline
		status.Problem = err
		return status, nil
	}
	status.State = Connected
	status.Version = version
	return status, nil
}

// CheckServer finds a RomM server at the address the user typed and checks its version.
// trustFingerprint is a certificate the user chose to trust after an earlier untrusted_cert error.
// It returns the server with the URL that answered.
func (c *Connection) CheckServer(ctx context.Context, input, trustFingerprint string) (domain.Server, string, error) {
	candidates, err := domain.ServerURLCandidates(input)
	if err != nil {
		return domain.Server{}, "", err
	}

	var lastErr error
	for _, u := range candidates {
		server := domain.Server{URL: u, CertFingerprint: trustFingerprint}
		version, err := c.dial(server, "").Heartbeat(ctx)
		if err == nil {
			if err := domain.CheckServerVersion(version); err != nil {
				c.log.Info("server too old", "url", u, "version", version)
				return domain.Server{}, "", err
			}
			c.log.Info("found server", "url", u, "version", version, "pinned_cert", trustFingerprint != "")
			return server, version, nil
		}
		c.log.Info("no server at address", "url", u, "err", err)
		lastErr = err
		// Something answered at this address, so falling back to plain HTTP would reach a
		// different service, or the same one without encryption.
		var de *domain.Error
		if errors.As(err, &de) && (de.Kind == domain.KindUntrustedCert || de.Kind == domain.KindNotRomM) {
			return domain.Server{}, "", err
		}
	}
	return domain.Server{}, "", lastErr
}

// SignInWithPairCode redeems a pair code made in RomM's web UI and saves the sign-in. server is
// what CheckServer returned.
func (c *Connection) SignInWithPairCode(ctx context.Context, server domain.Server, code string) (ConnectionStatus, error) {
	if err := checkServerURL(server); err != nil {
		return ConnectionStatus{}, err
	}
	code, err := domain.NormalizePairCode(code)
	if err != nil {
		return ConnectionStatus{}, err
	}

	in, err := c.dial(server, "").ExchangePairCode(ctx, code)
	if err != nil {
		return ConnectionStatus{}, err
	}
	in.Server = server
	if err := c.settings.SaveSignIn(in); err != nil {
		return ConnectionStatus{}, err
	}
	c.logSignIn("pair code", in)
	return c.Status(ctx)
}

func (c *Connection) logSignIn(method string, in domain.SignIn) {
	c.log.Info("signed in", "method", method, "url", in.Server.URL, "device_id", in.DeviceID,
		"scopes", in.Scopes, "disabled_features", len(domain.DisabledFeatures(in.Scopes)))
}

// StartDevicePairing asks RomM to pair this Deck as a Device and returns the code for the user to
// approve in RomM's web UI. Polling for the approval carries on in the background, and the
// PairingListener hears how it ends. Starting another pairing cancels this one.
func (c *Connection) StartDevicePairing(ctx context.Context, server domain.Server) (domain.DevicePairing, error) {
	if err := checkServerURL(server); err != nil {
		return domain.DevicePairing{}, err
	}
	device, err := c.identity.Device()
	if err != nil {
		return domain.DevicePairing{}, err
	}
	client := c.dial(server, "")
	pairing, err := client.StartDeviceAuth(ctx, device, domain.RequestedScopes())
	if err != nil {
		return domain.DevicePairing{}, err
	}

	pctx, cancel := context.WithTimeout(c.base, pairing.ExpiresIn)
	c.mu.Lock()
	if c.cancelPairing != nil {
		c.log.Info("device pairing replaced", "user_code", c.pairingCode)
		c.cancelPairing()
	}
	c.pairingCode, c.cancelPairing = pairing.UserCode, cancel
	c.mu.Unlock()

	c.log.Info("device pairing started", "url", server.URL, "user_code", pairing.UserCode,
		"device", device.Name, "platform", device.Platform, "expires_in", pairing.ExpiresIn, "interval", pairing.Interval)
	go c.pollPairing(pctx, cancel, client, server, pairing)
	return pairing, nil
}

// CancelDevicePairing stops waiting for the pairing with userCode. It names the pairing so that a
// late cancel can't stop a newer one that replaced it.
func (c *Connection) CancelDevicePairing(userCode string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancelPairing != nil && c.pairingCode == userCode {
		c.log.Info("device pairing cancelled", "user_code", userCode)
		c.cancelPairing()
		c.pairingCode, c.cancelPairing = "", nil
	}
}

func (c *Connection) pollPairing(ctx context.Context, cancel context.CancelFunc, client RomM, server domain.Server, pairing domain.DevicePairing) {
	defer cancel()
	interval := pairing.Interval
	if interval <= 0 {
		interval = c.slowDownStep
	}

	log := c.log.With("user_code", pairing.UserCode)
	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				log.Info("device pairing expired", "reason", "no approval in time")
				c.onPairing(PairingUpdate{State: PairingExpired})
			}
			return
		case <-time.After(interval):
		}

		in, err := client.PollDeviceAuth(ctx, pairing.DeviceCode)
		switch {
		case err == nil:
			in.Server = server
			if err := c.settings.SaveSignIn(in); err != nil {
				log.Error("save device sign-in", "err", err)
				c.onPairing(PairingUpdate{State: PairingFailed, Problem: fmt.Errorf("save sign-in: %w", err)})
				return
			}
			c.logSignIn("device pairing", in)
			status, err := c.Status(ctx)
			if err != nil {
				log.Error("read status after device sign-in", "err", err)
			}
			c.onPairing(PairingUpdate{State: PairingApproved, Status: status})
			return
		case errors.Is(err, domain.ErrPairingSlowDown):
			interval += c.slowDownStep
			log.Info("device pairing polled too fast, slowing down", "interval", interval)
		case errors.Is(err, domain.ErrPairingDenied):
			log.Info("device pairing denied")
			c.onPairing(PairingUpdate{State: PairingDenied})
			return
		case errors.Is(err, domain.ErrPairingExpired):
			log.Info("device pairing expired", "reason", "RomM dropped the request")
			c.onPairing(PairingUpdate{State: PairingExpired})
			return
		case errors.Is(err, domain.ErrPairingPending):
			log.Debug("device pairing waiting for approval")
		case ctx.Err() != nil:
		default:
			// A dropped connection or a server hiccup; the request stays valid until it expires.
			log.Warn("poll device pairing", "err", err)
		}
	}
}

// checkServerURL rejects a server that didn't come from CheckServer, which always sets a scheme.
func checkServerURL(server domain.Server) error {
	if candidates, err := domain.ServerURLCandidates(server.URL); err != nil || len(candidates) != 1 {
		return &domain.Error{Kind: domain.KindInvalidInput, Message: "Check the server address again."}
	}
	return nil
}
