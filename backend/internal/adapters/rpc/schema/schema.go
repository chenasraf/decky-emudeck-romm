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
	ErrorBadCredentials     ErrorKind = "bad_credentials"
	ErrorAccountDisabled    ErrorKind = "account_disabled"
	ErrorMissingPermission  ErrorKind = "missing_permission"
	ErrorSignedOut          ErrorKind = "signed_out"
	ErrorNotFound           ErrorKind = "not_found"
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
	SignInWithPassword  Method[SignInWithPasswordParams, ConnectionStatus]           `json:"sign_in_with_password"`
	StartDevicePairing  Method[StartDevicePairingParams, DevicePairing]              `json:"start_device_pairing"`
	CancelDevicePairing Method[CancelDevicePairingParams, CancelDevicePairingResult] `json:"cancel_device_pairing"`
	InstallState        Method[InstallStateParams, InstallState]                     `json:"install_state"`
	CatalogStatus       Method[CatalogStatusParams, CatalogStatus]                   `json:"catalog_status"`
	RefreshCatalog      Method[RefreshCatalogParams, CatalogStatus]                  `json:"refresh_catalog"`
	ListPlatforms       Method[ListPlatformsParams, ListPlatformsResult]             `json:"list_platforms"`
	ListGames           Method[ListGamesParams, ListGamesResult]                     `json:"list_games"`
	GetGame             Method[GetGameParams, Game]                                  `json:"get_game"`
	SearchGames         Method[SearchGamesParams, SearchGamesResult]                 `json:"search_games"`
}

// Events maps each notification the backend sends, which main.py re-emits to the frontend as an
// event of the same name, to its params.
type Events struct {
	DevicePairing DevicePairingEvent `json:"device_pairing"`
	// Connection fires each time the plugin signs in, wherever the sign-in finished, and once each
	// time the server stops accepting the plugin's token.
	Connection ConnectionStatus `json:"connection"`
	// Catalog fires each time a library refresh starts, makes progress or ends.
	Catalog CatalogStatus `json:"catalog"`
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
	// ConnectionSignedOut means the server no longer accepts the plugin's token. Work is paused
	// until the user signs in again on the same server.
	ConnectionSignedOut ConnectionState = "signed_out"
)

type ConnectionStatusParams struct{}

type ConnectionStatus struct {
	State     ConnectionState `json:"state"`
	ServerURL string          `json:"serverUrl,omitempty"`
	// CertFingerprint is the certificate pinned for the server, which signing in again passes back.
	CertFingerprint string `json:"certFingerprint,omitempty"`
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

type SignInWithPasswordParams struct {
	Server   Server `json:"server"`
	Username string `json:"username"`
	Password string `json:"password"`
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

type InstallStateParams struct {
	// Refresh looks at the disk again even when the last look is under a minute old.
	Refresh bool `json:"refresh,omitempty"`
}

// InstallState is what the plugin found of EmuDeck and ES-DE on disk.
type InstallState struct {
	EmuDeck   EmuDeckStatus    `json:"emudeck"`
	ESDE      ESDEStatus       `json:"esde"`
	Emulators []EmulatorStatus `json:"emulators"`
}

type EmuDeckStatus struct {
	// Found is false when no Emulation folder was found, which turns off everything that writes
	// to disk.
	Found bool `json:"found"`
	// Root is the Emulation folder, set when found.
	Root string `json:"root,omitempty"`
	// Source is where the Emulation folder's location came from, set when found.
	Source string `json:"source,omitempty"`
	// Tried lists the ROM folders the plugin looked for.
	Tried []string `json:"tried"`
}

type ESDEStatus struct {
	// SettingsFound is false when ES-DE's es_settings.xml doesn't exist.
	SettingsFound  bool   `json:"settingsFound"`
	MediaDirectory string `json:"mediaDirectory"`
	// ParseGamelistOnly hides games the plugin adds until ES-DE's gamelists list them.
	ParseGamelistOnly bool `json:"parseGamelistOnly"`
	Running           bool `json:"running"`
}

type EmulatorStatus struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	// Systems are the ROM folders this emulator is EmuDeck's default for.
	Systems []string `json:"systems"`
	// SaveSync says whether the plugin syncs this emulator's saves.
	SaveSync bool `json:"saveSync"`
	// SaveDirs are the emulator's save folders under EmuDeck's saves folder, set when SaveSync.
	SaveDirs []SaveDirStatus `json:"saveDirs,omitempty"`
}

type SaveDirStatus struct {
	Path string `json:"path"`
	// Resolved is where Path leads once symlinks are followed, empty when it leads nowhere.
	Resolved string `json:"resolved,omitempty"`
}

type CatalogStatusParams struct{}

type RefreshCatalogParams struct {
	// Force refreshes even when the library is under five minutes old, as Refresh library does.
	Force bool `json:"force,omitempty"`
}

// CatalogStatus is the state of the plugin's local copy of the server's library.
type CatalogStatus struct {
	Refreshing bool `json:"refreshing"`
	// Fetched is how many games the running refresh has received so far.
	Fetched int `json:"fetched"`
	// SyncedAt is when the library was last refreshed, as RFC 3339, empty before the first time.
	SyncedAt  string `json:"syncedAt,omitempty"`
	Platforms int    `json:"platforms"`
	Games     int    `json:"games"`
	// Problem says why the last refresh failed.
	Problem *Error `json:"problem,omitempty"`
}

type ListPlatformsParams struct{}

type ListPlatformsResult struct {
	Platforms []PlatformSummary `json:"platforms"`
}

type PlatformSummary struct {
	ID   int    `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
	// Games is how many of the platform's games the library holds.
	Games int `json:"games"`
	// Installed is how many of them are on this Deck.
	Installed int `json:"installed"`
}

type GameFilter string

const (
	GameFilterAll          GameFilter = "all"
	GameFilterInstalled    GameFilter = "installed"
	GameFilterNotInstalled GameFilter = "not_installed"
	GameFilterUpdates      GameFilter = "updates"
)

type ListGamesParams struct {
	PlatformID int        `json:"platformId"`
	Filter     GameFilter `json:"filter,omitempty"`
	Offset     int        `json:"offset,omitempty"`
	// Limit is capped at 200; zero means 50.
	Limit int `json:"limit,omitempty"`
}

type ListGamesResult struct {
	PlatformName string        `json:"platformName"`
	Games        []GameSummary `json:"games"`
	// Total is how many games match the filter, across every page.
	Total int `json:"total"`
}

type GameState string

const (
	// GameOnRomM is a game on the server and not on this Deck.
	GameOnRomM GameState = "on_romm"
)

type GameSummary struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	SizeBytes int64     `json:"sizeBytes"`
	Regions   []string  `json:"regions"`
	State     GameState `json:"state"`
}

type SearchGamesParams struct {
	// Query is a title, a few words of one, or a ROM's CRC32, MD5 or SHA-1 hash.
	Query string `json:"query"`
	// Limit is capped at 100; zero means 50.
	Limit int `json:"limit,omitempty"`
}

type SearchGamesResult struct {
	Games []SearchHit `json:"games"`
	// Local is set when the library's local copy answered because the server couldn't: games
	// match by name only, not by hash.
	Local bool `json:"local"`
}

type SearchHit struct {
	ID           int       `json:"id"`
	Name         string    `json:"name"`
	PlatformID   int       `json:"platformId"`
	PlatformName string    `json:"platformName"`
	SizeBytes    int64     `json:"sizeBytes"`
	Regions      []string  `json:"regions"`
	State        GameState `json:"state"`
}

type GetGameParams struct {
	ID int `json:"id"`
}

type Game struct {
	ID           int    `json:"id"`
	PlatformID   int    `json:"platformId"`
	PlatformName string `json:"platformName"`
	Name         string `json:"name"`
	// FileName is the game's file or folder name on the server.
	FileName  string   `json:"fileName"`
	SizeBytes int64    `json:"sizeBytes"`
	Regions   []string `json:"regions"`
	Revision  string   `json:"revision,omitempty"`
	// MultiFile is set when the game is a folder of several files.
	MultiFile bool      `json:"multiFile"`
	State     GameState `json:"state"`
}
