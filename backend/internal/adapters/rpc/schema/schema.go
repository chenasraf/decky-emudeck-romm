// Package schema is the RPC contract between the backend and the plugin frontend. Every exported
// type here is generated into TypeScript (see tygo.yaml), so it must stay free of logic.
package schema

//go:generate sh -c "cd ../../../.. && go tool tygo generate"

// ProtocolVersion changes whenever the contract changes incompatibly. main.py refuses a backend
// whose handshake reports a different value.
const ProtocolVersion = 1

// Ready is the params of the "ready" notification the backend sends once it accepts calls.
type Ready struct {
	Version  string `json:"version"`
	Protocol int    `json:"protocol"`
}

// Error is a JSON-RPC error object. Codes from -32768 to -32000 are the protocol's own; domain
// errors use the application range and carry a stable Data.Kind the frontend switches on.
type Error struct {
	Code    int        `json:"code"`
	Message string     `json:"message"`
	Data    *ErrorData `json:"data,omitempty"`
}

// CodeDomain is the error code for every domain error; Data.Kind tells them apart.
const CodeDomain = 1000

type ErrorKind string

const (
	ErrorInvalidInput       ErrorKind = "invalid_input"
	ErrorUnreachable        ErrorKind = "unreachable"
	ErrorUntrustedCert      ErrorKind = "untrusted_cert"
	ErrorNotRomM            ErrorKind = "not_romm"
	ErrorUnsupportedVersion ErrorKind = "unsupported_version"
	ErrorInvalidPairCode    ErrorKind = "invalid_pair_code"
	ErrorPairRejected       ErrorKind = "pair_rejected"
	ErrorRateLimited        ErrorKind = "rate_limited"
)

type ErrorData struct {
	Kind ErrorKind `json:"kind"`
	// Fingerprint is the SHA-256 of the certificate behind an untrusted_cert error, to show the
	// user and send back as trustFingerprint.
	Fingerprint string `json:"fingerprint,omitempty"`
	// Version is the server's RomM version behind an unsupported_version error.
	Version string `json:"version,omitempty"`
}

// Methods maps each RPC method name to its params and result.
type Methods struct {
	Ping                Method[PingParams, PingResult]                               `json:"ping"`
	ConnectionStatus    Method[ConnectionStatusParams, ConnectionStatus]             `json:"connection_status"`
	CheckServer         Method[CheckServerParams, CheckServerResult]                 `json:"check_server"`
	SignInWithPairCode  Method[SignInWithPairCodeParams, ConnectionStatus]           `json:"sign_in_with_pair_code"`
	StartDevicePairing  Method[StartDevicePairingParams, DevicePairing]              `json:"start_device_pairing"`
	CancelDevicePairing Method[CancelDevicePairingParams, CancelDevicePairingResult] `json:"cancel_device_pairing"`
}

// Events maps each notification the backend sends, which main.py re-emits to the frontend as an
// event of the same name, to its params.
type Events struct {
	DevicePairing DevicePairingEvent `json:"device_pairing"`
}

// Method pairs a method's params with its result.
type Method[P, R any] struct {
	Params P `json:"params"`
	Result R `json:"result"`
}

type PingParams struct{}

type PingResult struct {
	Version string `json:"version"`
}

type ConnectionState string

const (
	ConnectionNotSetUp  ConnectionState = "not_set_up"
	ConnectionConnected ConnectionState = "connected"
	ConnectionOffline   ConnectionState = "offline"
)

type ConnectionStatusParams struct{}

type ConnectionStatus struct {
	State     ConnectionState `json:"state"`
	ServerURL string          `json:"serverUrl,omitempty"`
	// Version is the server's RomM version, set when connected.
	Version string `json:"version,omitempty"`
	// Problem says why the server is offline.
	Problem *Error `json:"problem,omitempty"`
	// DisabledFeatures are the features the token's scopes don't cover.
	DisabledFeatures []DisabledFeature `json:"disabledFeatures,omitempty"`
}

type Feature string

const (
	FeatureBrowse   Feature = "browse"
	FeatureDownload Feature = "download"
	FeatureBIOS     Feature = "bios"
	FeatureSaveSync Feature = "save_sync"
)

type DisabledFeature struct {
	Feature       Feature  `json:"feature"`
	MissingScopes []string `json:"missingScopes"`
}

type CheckServerParams struct {
	// URL is the address as the user typed it; the scheme is optional.
	URL string `json:"url"`
	// TrustFingerprint is a certificate the user chose to trust after an untrusted_cert error.
	TrustFingerprint string `json:"trustFingerprint,omitempty"`
}

type CheckServerResult struct {
	Server  Server `json:"server"`
	Version string `json:"version"`
}

// Server is a RomM server as check_server found it, passed back unchanged to sign in.
type Server struct {
	URL             string `json:"url"`
	CertFingerprint string `json:"certFingerprint,omitempty"`
}

type SignInWithPairCodeParams struct {
	Server Server `json:"server"`
	Code   string `json:"code"`
}

type StartDevicePairingParams struct {
	Server Server `json:"server"`
}

// DevicePairing is a pairing request for the user to approve in RomM's web UI. How it ends
// arrives as a device_pairing event.
type DevicePairing struct {
	UserCode string `json:"userCode"`
	// VerificationURL opens the approval page with the code filled in; the QR code encodes it.
	VerificationURL  string `json:"verificationUrl"`
	ExpiresInSeconds int    `json:"expiresInSeconds"`
}

type CancelDevicePairingParams struct {
	// UserCode names the pairing to cancel; a newer pairing is left alone.
	UserCode string `json:"userCode"`
}

type CancelDevicePairingResult struct{}

type PairingState string

const (
	PairingApproved PairingState = "approved"
	PairingDenied   PairingState = "denied"
	PairingExpired  PairingState = "expired"
	// PairingFailed means RomM approved the pairing but the plugin couldn't keep the sign-in.
	PairingFailed PairingState = "failed"
)

type DevicePairingEvent struct {
	State PairingState `json:"state"`
	// Status is the connection once signed in, set when approved.
	Status *ConnectionStatus `json:"status,omitempty"`
	// Problem says what went wrong, set when failed.
	Problem *Error `json:"problem,omitempty"`
}
