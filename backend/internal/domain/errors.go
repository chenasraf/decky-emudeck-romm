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
	// KindNewerData means state.db is from a newer version of the plugin than the one running.
	KindNewerData Kind = "newer_data"
	// KindCantDownload means the game can't be placed: no EmuDeck, no folder for its platform, or
	// a layout the plugin doesn't place.
	KindCantDownload Kind = "cant_download"
	// KindTargetExists means a file the plugin didn't download sits where the game would go.
	KindTargetExists Kind = "target_exists"
	// KindChangedOnDisk means a file the plugin placed no longer matches what it recorded, so the
	// plugin stopped managing it rather than delete or replace it.
	KindChangedOnDisk Kind = "changed_on_disk"
	// KindNoSpace means the Emulation drive hasn't room for a download.
	KindNoSpace Kind = "no_space"
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
