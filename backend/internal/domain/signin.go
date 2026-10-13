package domain

import (
	"errors"
	"slices"
	"time"
)

// SignIn is everything a successful sign-in leaves behind.
type SignIn struct {
	Server Server
	Token  string
	// DeviceID is RomM's id for this Deck's Device, set when the token is bound to one.
	DeviceID string
	// Scopes are what RomM granted the token. Nil when unknown, which gates nothing.
	Scopes []string
	// SignedOut is set once the Server stops accepting Token. The sign-in is kept so the user can
	// sign in again on the same Server.
	SignedOut bool
}

// Feature is a part of the plugin that needs particular token scopes.
type Feature string

const (
	FeatureBrowse   Feature = "browse"
	FeatureDownload Feature = "download"
	FeatureBIOS     Feature = "bios"
	FeatureSaveSync Feature = "save_sync"
)

// Features lists every Feature in the order the UI shows them.
var Features = []Feature{FeatureBrowse, FeatureDownload, FeatureBIOS, FeatureSaveSync}

var featureScopes = map[Feature][]string{
	FeatureBrowse:   {"platforms.read", "roms.read"},
	FeatureDownload: {"roms.read"},
	FeatureBIOS:     {"firmware.read"},
	FeatureSaveSync: {"assets.read", "assets.write", "devices.read", "devices.write"},
}

// RequestedScopes is what the plugin asks RomM for: the union of every Feature's scopes, plus
// me.read to read the signed-in user.
func RequestedScopes() []string {
	scopes := []string{"me.read"}
	for _, f := range Features {
		for _, s := range featureScopes[f] {
			if !slices.Contains(scopes, s) {
				scopes = append(scopes, s)
			}
		}
	}
	return scopes
}

// PasswordScopes is what a password sign-in asks RomM for: just enough to approve this Deck's own
// device pairing, which then grants the RequestedScopes the account has.
func PasswordScopes() []string {
	return []string{"me.read", "me.write"}
}

// DisabledFeature is a Feature the token's scopes don't cover.
type DisabledFeature struct {
	Feature       Feature
	MissingScopes []string
}

// DisabledFeatures lists the Features that granted doesn't cover. A nil granted means the scopes
// are unknown, and nothing is disabled.
func DisabledFeatures(granted []string) []DisabledFeature {
	if granted == nil {
		return nil
	}
	var out []DisabledFeature
	for _, f := range Features {
		var missing []string
		for _, s := range featureScopes[f] {
			if !slices.Contains(granted, s) {
				missing = append(missing, s)
			}
		}
		if len(missing) > 0 {
			out = append(out, DisabledFeature{Feature: f, MissingScopes: missing})
		}
	}
	return out
}

// DeviceIdentity is how this Deck introduces itself to RomM.
type DeviceIdentity struct {
	// Identifier stays the same across plugin reinstalls and data resets, and differs between
	// Decks; RomM reuses the Device it already has for it.
	Identifier string
	Name       string
	Platform   string
}

// DevicePairing is a device pairing request waiting for the user to approve it in RomM.
type DevicePairing struct {
	DeviceCode string
	UserCode   string
	// VerificationURL opens the approval page with the user code filled in.
	VerificationURL string
	ExpiresIn       time.Duration
	Interval        time.Duration
}

// Outcomes of polling RomM while the user approves a device pairing in its web UI.
var (
	ErrPairingPending  = errors.New("pairing not approved yet")
	ErrPairingSlowDown = errors.New("pairing polled too fast")
	ErrPairingDenied   = errors.New("pairing denied")
	ErrPairingExpired  = errors.New("pairing expired")
)
