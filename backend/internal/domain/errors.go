// Package domain holds the plugin's rules and vocabulary, free of I/O.
package domain

import "fmt"

// Kind is a stable error category the frontend switches on.
type Kind string

const (
	KindInvalidInput       Kind = "invalid_input"
	KindUnreachable        Kind = "unreachable"
	KindUntrustedCert      Kind = "untrusted_cert"
	KindNotRomM            Kind = "not_romm"
	KindUnsupportedVersion Kind = "unsupported_version"
	KindInvalidPairCode    Kind = "invalid_pair_code"
	KindPairRejected       Kind = "pair_rejected"
	KindRateLimited        Kind = "rate_limited"
	KindBadCredentials     Kind = "bad_credentials"
	KindAccountDisabled    Kind = "account_disabled"
	KindMissingPermission  Kind = "missing_permission"
	KindSignedOut          Kind = "signed_out"
	KindNotFound           Kind = "not_found"
)

// Error is a failure the user can act on. Fingerprint is set for KindUntrustedCert and Version for
// KindUnsupportedVersion.
type Error struct {
	Kind        Kind
	Message     string
	Fingerprint string
	Version     string
	Err         error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *Error) Unwrap() error { return e.Err }
