package rpc

import (
	"context"
	"errors"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/rpc/schema"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/app"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// Services are the use cases the routes call.
type Services struct {
	Connection *app.Connection
	Install    *app.Install
	Catalog    *app.Catalog
	Downloads  *app.Downloads
}

// Routes registers a handler for every method in schema.Methods.
func Routes(s *Server, version string, svc Services) {
	conn, install := svc.Connection, svc.Install
	catalogRoutes(s, svc.Catalog, svc.Downloads)
	downloadRoutes(s, svc.Downloads)

	Register(s, "ping", func(context.Context, schema.PingParams) (schema.PingResult, error) {
		return schema.PingResult{Version: version}, nil
	})

	Register(s, "connection_status", func(ctx context.Context, _ schema.ConnectionStatusParams) (schema.ConnectionStatus, error) {
		status, err := conn.Status(ctx)
		if err != nil {
			return schema.ConnectionStatus{}, wireError(err)
		}
		return toWireStatus(status), nil
	})

	Register(s, "check_server", func(ctx context.Context, p schema.CheckServerParams) (schema.CheckServerResult, error) {
		server, version, err := conn.CheckServer(ctx, p.URL, p.TrustFingerprint)
		if err != nil {
			return schema.CheckServerResult{}, wireError(err)
		}
		return schema.CheckServerResult{
			Server:  schema.Server{URL: server.URL, CertFingerprint: server.CertFingerprint},
			Version: version,
		}, nil
	})

	Register(s, "sign_in_with_pair_code", func(ctx context.Context, p schema.SignInWithPairCodeParams) (schema.ConnectionStatus, error) {
		server := domain.Server{URL: p.Server.URL, CertFingerprint: p.Server.CertFingerprint}
		status, err := conn.SignInWithPairCode(ctx, server, p.Code)
		if err != nil {
			return schema.ConnectionStatus{}, wireError(err)
		}
		return toWireStatus(status), nil
	})

	Register(s, "sign_in_with_password", func(ctx context.Context, p schema.SignInWithPasswordParams) (schema.ConnectionStatus, error) {
		server := domain.Server{URL: p.Server.URL, CertFingerprint: p.Server.CertFingerprint}
		status, err := conn.SignInWithPassword(ctx, server, p.Username, p.Password)
		if err != nil {
			return schema.ConnectionStatus{}, wireError(err)
		}
		return toWireStatus(status), nil
	})

	Register(s, "start_device_pairing", func(ctx context.Context, p schema.StartDevicePairingParams) (schema.DevicePairing, error) {
		server := domain.Server{URL: p.Server.URL, CertFingerprint: p.Server.CertFingerprint}
		pairing, err := conn.StartDevicePairing(ctx, server)
		if err != nil {
			return schema.DevicePairing{}, wireError(err)
		}
		return schema.DevicePairing{
			UserCode:         pairing.UserCode,
			VerificationURL:  pairing.VerificationURL,
			ExpiresInSeconds: int(pairing.ExpiresIn.Seconds()),
		}, nil
	})

	Register(s, "cancel_device_pairing", func(_ context.Context, p schema.CancelDevicePairingParams) (schema.CancelDevicePairingResult, error) {
		conn.CancelDevicePairing(p.UserCode)
		return schema.CancelDevicePairingResult{}, nil
	})

	Register(s, "install_state", func(_ context.Context, p schema.InstallStateParams) (schema.InstallState, error) {
		return toWireInstall(install.State(p.Refresh)), nil
	})
}

func toWireInstall(s domain.InstallState) schema.InstallState {
	out := schema.InstallState{
		EmuDeck: schema.EmuDeckStatus{
			Found:    s.EmuDeckFound(),
			Root:     s.EmuDeck.Root,
			ROMsPath: s.EmuDeck.ROMsPath,
			Source:   string(s.EmuDeck.Source),
			Tried:    nonNil(s.EmuDeck.Tried),
		},
		ESDE: schema.ESDEStatus{
			SettingsFound:       s.ESDE.SettingsPath != "",
			MediaDirectory:      s.ESDE.MediaDirectory,
			ParseGamelistOnly:   s.ESDE.ParseGamelistOnly,
			ROMDirectory:        s.ESDE.ROMDirectory,
			ROMDirectoryMatches: s.ESDE.ROMDirectoryMatches,
			Running:             s.ESDE.Running,
		},
		Emulators: []schema.EmulatorStatus{},
	}
	for _, e := range s.Emulators {
		def, _ := domain.EmulatorByID(e.ID)
		es := schema.EmulatorStatus{ID: e.ID, Name: def.Name, Installed: e.Installed, Systems: nonNil(e.Systems), SaveSync: def.SaveSync()}
		for _, d := range e.Saves {
			es.SaveDirs = append(es.SaveDirs, schema.SaveDirStatus{Path: d.Path, Resolved: d.Resolved})
		}
		out.Emulators = append(out.Emulators, es)
	}
	return out
}

// nonNil keeps a list the schema declares as an array from going over the wire as null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// ConnectionEvents sends the connection's background events as notifications.
type ConnectionEvents struct{ s *Server }

func NewConnectionEvents(s *Server) ConnectionEvents { return ConnectionEvents{s: s} }

// Pairing sends a device_pairing notification.
func (e ConnectionEvents) Pairing(u app.PairingUpdate) {
	event := schema.DevicePairingEvent{State: schema.PairingState(u.State)}
	if u.State == app.PairingApproved {
		status := toWireStatus(u.Status)
		event.Status = &status
	}
	if u.Problem != nil {
		event.Problem = problemOf(u.Problem)
	}
	e.notify("device_pairing", event)
}

// Changed sends a connection notification.
func (e ConnectionEvents) Changed(status app.ConnectionStatus) {
	e.notify("connection", toWireStatus(status))
}

func (e ConnectionEvents) notify(method string, params any) {
	if err := e.s.Notify(method, params); err != nil {
		e.s.log.Error("send "+method, "err", err)
	}
}

func toWireStatus(s app.ConnectionStatus) schema.ConnectionStatus {
	out := schema.ConnectionStatus{
		State:           schema.ConnectionState(s.State),
		ServerURL:       s.ServerURL,
		CertFingerprint: s.CertFingerprint,
		Version:         s.Version,
	}
	for _, f := range s.DisabledFeatures {
		out.DisabledFeatures = append(out.DisabledFeatures, schema.DisabledFeature{
			Feature:       schema.Feature(f.Feature),
			MissingScopes: f.MissingScopes,
		})
	}
	if s.Problem != nil {
		out.Problem = problemOf(s.Problem)
	}
	return out
}

// problemOf describes err for a field the frontend shows, rather than for a rejected call.
func problemOf(err error) *schema.Error {
	var rpcErr *Error
	if errors.As(wireError(err), &rpcErr) {
		e := schema.Error(*rpcErr)
		return &e
	}
	return &schema.Error{Code: CodeInternalError, Message: err.Error()}
}

// wireError turns a domain error into a JSON-RPC error the frontend can switch on. Any other error
// passes through and is reported as an internal error.
func wireError(err error) error {
	var de *domain.Error
	if !errors.As(err, &de) {
		return err
	}
	return &Error{
		Code:    schema.CodeDomain,
		Message: de.Message,
		Data: &schema.ErrorData{
			Kind:        schema.ErrorKind(de.Kind),
			Fingerprint: de.Fingerprint,
			Version:     de.Version,
		},
	}
}
