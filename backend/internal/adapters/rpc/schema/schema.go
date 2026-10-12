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
	Ping               Method[PingParams, PingResult]                     `json:"ping"`
	ConnectionStatus   Method[ConnectionStatusParams, ConnectionStatus]   `json:"connection_status"`
	CheckServer        Method[CheckServerParams, CheckServerResult]       `json:"check_server"`
	SignInWithPairCode Method[SignInWithPairCodeParams, ConnectionStatus] `json:"sign_in_with_pair_code"`
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
