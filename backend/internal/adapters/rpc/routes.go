package rpc

import (
	"context"
	"errors"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/rpc/schema"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/app"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// Routes registers a handler for every method in schema.Methods.
func Routes(s *Server, version string, conn *app.Connection) {
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
}

// PairingEvents returns a listener that sends each pairing update as a device_pairing
// notification.
func PairingEvents(s *Server) app.PairingListener {
	return func(u app.PairingUpdate) {
		event := schema.DevicePairingEvent{State: schema.PairingState(u.State)}
		if u.State == app.PairingApproved {
			status := toWireStatus(u.Status)
			event.Status = &status
		}
		if u.Problem != nil {
			event.Problem = problemOf(u.Problem)
		}
		if err := s.Notify("device_pairing", event); err != nil {
			s.log.Error("send device_pairing", "err", err)
		}
	}
}

func toWireStatus(s app.ConnectionStatus) schema.ConnectionStatus {
	out := schema.ConnectionStatus{
		State:     schema.ConnectionState(s.State),
		ServerURL: s.ServerURL,
		Version:   s.Version,
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
