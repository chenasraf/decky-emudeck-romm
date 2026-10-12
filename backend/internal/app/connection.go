// Package app holds the plugin's use cases. It reaches the outside world only through the ports
// declared here, which the adapters implement.
package app

import (
	"context"
	"errors"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// statusTimeout bounds the reachability check behind Status, which the QAM runs every time it
// opens.
const statusTimeout = 5 * time.Second

// RomM is the part of the RomM API the connection use cases need.
type RomM interface {
	Heartbeat(ctx context.Context) (string, error)
	ExchangePairCode(ctx context.Context, code string) (string, error)
}

// Dialer returns a RomM client for server, authenticated with token when it isn't empty.
type Dialer func(server domain.Server, token string) RomM

// Settings stores the server and its client token across restarts.
type Settings interface {
	Server() (*domain.Server, error)
	SaveServer(domain.Server) error
	Token() (string, error)
	SaveToken(string) error
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
	Problem error
}

type Connection struct {
	settings Settings
	dial     Dialer
}

func NewConnection(settings Settings, dial Dialer) *Connection {
	return &Connection{settings: settings, dial: dial}
}

// Status reports whether a server is set up and, if so, whether it answers right now.
func (c *Connection) Status(ctx context.Context) (ConnectionStatus, error) {
	server, err := c.settings.Server()
	if err != nil {
		return ConnectionStatus{}, err
	}
	token, err := c.settings.Token()
	if err != nil {
		return ConnectionStatus{}, err
	}
	if server == nil || token == "" {
		return ConnectionStatus{State: NotSetUp}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	version, err := c.dial(*server, token).Heartbeat(ctx)
	if err != nil {
		return ConnectionStatus{State: Offline, ServerURL: server.URL, Problem: err}, nil
	}
	return ConnectionStatus{State: Connected, ServerURL: server.URL, Version: version}, nil
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
				return domain.Server{}, "", err
			}
			return server, version, nil
		}
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

// SignInWithPairCode redeems a pair code made in RomM's web UI and saves the server and token.
// server is what CheckServer returned.
func (c *Connection) SignInWithPairCode(ctx context.Context, server domain.Server, code string) (ConnectionStatus, error) {
	if candidates, err := domain.ServerURLCandidates(server.URL); err != nil || len(candidates) != 1 {
		return ConnectionStatus{}, &domain.Error{Kind: domain.KindInvalidInput, Message: "Check the server address again."}
	}
	code, err := domain.NormalizePairCode(code)
	if err != nil {
		return ConnectionStatus{}, err
	}

	token, err := c.dial(server, "").ExchangePairCode(ctx, code)
	if err != nil {
		return ConnectionStatus{}, err
	}
	if err := c.settings.SaveToken(token); err != nil {
		return ConnectionStatus{}, err
	}
	if err := c.settings.SaveServer(server); err != nil {
		return ConnectionStatus{}, err
	}
	return c.Status(ctx)
}
