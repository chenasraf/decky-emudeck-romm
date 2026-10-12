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

type ErrorData struct {
	Kind string `json:"kind"`
}

// Methods maps each RPC method name to its params and result.
type Methods struct {
	Ping Method[PingParams, PingResult] `json:"ping"`
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
