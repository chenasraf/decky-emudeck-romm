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
	// ErrorNewerData means the plugin's data is from a newer version of the plugin.
	ErrorNewerData ErrorKind = "newer_data"
	// ErrorCantDownload means the game can't be placed: no EmuDeck, no folder for its platform, or
	// a layout the plugin doesn't place.
	ErrorCantDownload ErrorKind = "cant_download"
	// ErrorTargetExists means a file the plugin didn't download sits where the game would go.
	ErrorTargetExists ErrorKind = "target_exists"
	// ErrorChangedOnDisk means a file the plugin placed no longer matches what it recorded, so the
	// plugin stopped managing it rather than delete or replace it.
	ErrorChangedOnDisk ErrorKind = "changed_on_disk"
	// ErrorNoSpace means the Emulation drive hasn't room for a download.
	ErrorNoSpace ErrorKind = "no_space"
	// ErrorSaveConflict means RomM refused a save upload because its slot changed since this Deck
	// last synced it.
	ErrorSaveConflict ErrorKind = "save_conflict"
	// ErrorSyncDisabled means RomM has save sync turned off for this Deck's Device.
	ErrorSyncDisabled ErrorKind = "sync_disabled"
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
	MoveServer          Method[MoveServerParams, MoveServerResult]                   `json:"move_server"`
	ForgetServer        Method[ForgetServerParams, ConnectionStatus]                 `json:"forget_server"`
	InstallState        Method[InstallStateParams, InstallState]                     `json:"install_state"`
	CatalogStatus       Method[CatalogStatusParams, CatalogStatus]                   `json:"catalog_status"`
	RefreshCatalog      Method[RefreshCatalogParams, CatalogStatus]                  `json:"refresh_catalog"`
	ListPlatforms       Method[ListPlatformsParams, ListPlatformsResult]             `json:"list_platforms"`
	ListGames           Method[ListGamesParams, ListGamesResult]                     `json:"list_games"`
	ListCollections     Method[ListCollectionsParams, ListCollectionsResult]         `json:"list_collections"`
	LibraryShelves      Method[LibraryShelvesParams, LibraryShelves]                 `json:"library_shelves"`
	GetGame             Method[GetGameParams, Game]                                  `json:"get_game"`
	GetCover            Method[GetCoverParams, Cover]                                `json:"get_cover"`
	SearchGames         Method[SearchGamesParams, SearchGamesResult]                 `json:"search_games"`
	DownloadGame        Method[DownloadGameParams, DownloadItem]                     `json:"download_game"`
	CancelDownload      Method[CancelDownloadParams, CancelDownloadResult]           `json:"cancel_download"`
	ListDownloads       Method[ListDownloadsParams, ListDownloadsResult]             `json:"list_downloads"`
	DeleteGames         Method[DeleteGamesParams, DeleteGamesResult]                 `json:"delete_games"`
	CheckOnDeck         Method[CheckOnDeckParams, OnDeckCheck]                       `json:"check_on_deck"`
	AdoptGame           Method[AdoptGameParams, AdoptGameResult]                     `json:"adopt_game"`
	LinkGame            Method[LinkGameParams, LinkGameResult]                       `json:"link_game"`
	StopManaging        Method[StopManagingParams, StopManagingResult]               `json:"stop_managing"`
	IgnoreUpdate        Method[IgnoreUpdateParams, IgnoreUpdateResult]               `json:"ignore_update"`
	ESDENotice          Method[ESDENoticeParams, ESDENotice]                         `json:"esde_notice"`
	FindGames           Method[FindGamesParams, FindGamesStatus]                     `json:"find_games"`
	FindGamesStatus     Method[FindGamesStatusParams, FindGamesStatus]               `json:"find_games_status"`
	PlanDownloadAll     Method[PlanDownloadAllParams, DownloadAllPlan]               `json:"plan_download_all"`
	DownloadAll         Method[DownloadAllParams, DownloadAllResult]                 `json:"download_all"`
	SetKeepArchives     Method[SetKeepArchivesParams, SetKeepArchivesResult]         `json:"set_keep_archives"`
	SetInstallTo        Method[SetInstallToParams, SetInstallToResult]               `json:"set_install_to"`
	PlatformBIOS        Method[PlatformBIOSParams, PlatformBIOS]                     `json:"platform_bios"`
	DownloadBIOS        Method[DownloadBIOSParams, DownloadBIOSResult]               `json:"download_bios"`
	GetPreferences      Method[GetPreferencesParams, Preferences]                    `json:"get_preferences"`
	SetPreferences      Method[SetPreferencesParams, Preferences]                    `json:"set_preferences"`
	SyncSaves           Method[SyncSavesParams, SyncSavesResult]                     `json:"sync_saves"`
	SaveStatus          Method[SaveStatusParams, SaveStatus]                         `json:"save_status"`
	SaveSummary         Method[SaveSummaryParams, SaveSummary]                       `json:"save_summary"`
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
	// Download fires each time a download is queued, makes progress or ends.
	Download DownloadEvent `json:"download"`
	// Delete fires when a game's deletion starts and when it ends.
	Delete DeleteEvent `json:"delete"`
	// DownloadHold fires when the download queue goes on hold for space, and when it comes off.
	DownloadHold DownloadHoldEvent `json:"download_hold"`
	// Ownership fires when a game is adopted or linked, and when a library refresh finds an
	// installed game's files gone or changed.
	Ownership OwnershipEvent `json:"ownership"`
	// FindGames fires when a platform's Find my games starts, makes progress and ends.
	FindGames FindGamesStatus `json:"find_games"`
	// ESDERescan fires once the download queue is idle after downloads or deletes changed ROM
	// folders while ES-DE ran, which it sees only after a rescan.
	ESDERescan ESDERescanEvent `json:"esde_rescan"`
	// BIOS fires when a platform's BIOS download starts and when it ends.
	BIOS BIOSEvent `json:"bios"`
	// SaveSync fires when a save sync starts and when it ends.
	SaveSync SaveSyncEvent `json:"save_sync"`
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
	// FeatureCollections browses and downloads RomM collections.
	FeatureCollections Feature = "collections"
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

// MoveServerParams points the plugin at the same RomM at a new address, as check_server found it.
// The sign-in and every record carry over, once the new address proves to be the same server.
type MoveServerParams struct {
	Server Server `json:"server"`
}

type MoveServerResult struct {
	// Moved is set when the plugin now talks to the new address. When it isn't, nothing changed.
	Moved bool `json:"moved"`
	// Checked is how many of this Deck's games were looked up at the new address.
	Checked int `json:"checked"`
	// Why says why the new address looks like a different server, set when it wasn't moved to.
	Why    string            `json:"why,omitempty"`
	Status *ConnectionStatus `json:"status,omitempty"`
}

// ForgetServerParams drops the sign-in, the library and every record tied to the server,
// cancelling queued downloads. Files on disk stay, User-managed.
type ForgetServerParams struct{}

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
	// ROMsPath is EmuDeck's ROMs folder, set when found. It can be outside Root.
	ROMsPath string `json:"romsPath,omitempty"`
	// Source is where the Emulation folder's location came from, set when found.
	Source string `json:"source,omitempty"`
	// Tried lists the ROM folders the plugin looked for.
	Tried []string `json:"tried"`
}

type ESDEStatus struct {
	// SettingsFound is false when ES-DE's es_settings.xml doesn't exist.
	SettingsFound  bool   `json:"settingsFound"`
	MediaDirectory string `json:"mediaDirectory"`
	// ROMDirectory is the folder ES-DE reads games from, set when SettingsFound.
	ROMDirectory string `json:"romDirectory,omitempty"`
	// ROMDirectoryMatches is false when ROMDirectory isn't EmuDeck's ROMs folder, so ES-DE never
	// shows the games the plugin places.
	ROMDirectoryMatches bool `json:"romDirectoryMatches"`
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

type GameSort string

const (
	GameSortName GameSort = "name"
	// GameSortAdded puts the games added to RomM last first.
	GameSortAdded GameSort = "added"
	// GameSortSize puts the largest games first.
	GameSortSize GameSort = "size"
)

type CollectionKind string

const (
	// CollectionRegular is a collection a user put games in by hand.
	CollectionRegular CollectionKind = "regular"
	// CollectionSmart is a collection RomM fills from a saved filter.
	CollectionSmart CollectionKind = "smart"
)

// CollectionRef names a collection. Each kind numbers its collections on its own.
type CollectionRef struct {
	Kind CollectionKind `json:"kind"`
	ID   int            `json:"id"`
}

type ListCollectionsParams struct{}

type ListCollectionsResult struct {
	Collections []CollectionSummary `json:"collections"`
}

type CollectionSummary struct {
	Kind     CollectionKind `json:"kind"`
	ID       int            `json:"id"`
	Name     string         `json:"name"`
	Favorite bool           `json:"favorite"`
	// Games is how many of the collection's games the library holds.
	Games int `json:"games"`
	// Installed is how many of them are on this Deck.
	Installed int `json:"installed"`
}

// ListGamesParams lists a platform's games, or with Collection set a collection's, across
// platforms.
type ListGamesParams struct {
	PlatformID int            `json:"platformId,omitempty"`
	Collection *CollectionRef `json:"collection,omitempty"`
	Filter     GameFilter     `json:"filter,omitempty"`
	// Sort is empty for by name.
	Sort   GameSort `json:"sort,omitempty"`
	Offset int      `json:"offset,omitempty"`
	// Limit is capped at 200; zero means 50.
	Limit int `json:"limit,omitempty"`
}

type ListGamesResult struct {
	// Name is the platform's or the collection's.
	Name  string        `json:"name"`
	Games []GameSummary `json:"games"`
	// Total is how many games match the filter, across every page.
	Total int `json:"total"`
	// Archives is what the plugin does with the platform's games that arrive as archives; empty
	// when EmuDeck has no folder for it, and for a collection.
	Archives ArchivePolicy `json:"archives,omitempty"`
	// KeepArchives is the platform's Keep archives toggle, which applies when Archives is optional.
	KeepArchives bool `json:"keepArchives"`
	// InstallTo is the ROM folder the platform's new downloads go in, and InstallFolders the ones
	// it can pick from: the system's own first. Both are empty for a collection and for a platform
	// EmuDeck has no folder for.
	InstallTo      string          `json:"installTo,omitempty"`
	InstallFolders []InstallFolder `json:"installFolders,omitempty"`
}

type InstallFolder struct {
	Folder string `json:"folder"`
	Path   string `json:"path"`
	// Primary is set on the system's own folder.
	Primary bool `json:"primary"`
	// SRMSkips is set when Steam ROM Manager, as EmuDeck sets it up, never adds the folder's games
	// to Steam.
	SRMSkips bool `json:"srmSkips"`
}

// SetInstallToParams picks the ROM folder a platform's new downloads go in. Games already placed
// stay where they are.
type SetInstallToParams struct {
	PlatformID int    `json:"platformId"`
	Folder     string `json:"folder"`
}

type SetInstallToResult struct{}

type ArchivePolicy string

const (
	// ArchivesOptional extracts archives unless Keep archives is on, which keeps those holding a
	// single file.
	ArchivesOptional ArchivePolicy = "optional"
	// ArchivesKeep never extracts: arcade romsets and DOS games stay archives.
	ArchivesKeep ArchivePolicy = "keep"
	// ArchivesExtract always extracts: the platform's emulator can't open archives.
	ArchivesExtract ArchivePolicy = "extract"
)

// SetKeepArchivesParams sets the Keep archives toggle for the EmuDeck folder a platform's games go
// in. It applies to downloads from then on.
type SetKeepArchivesParams struct {
	PlatformID int  `json:"platformId"`
	Keep       bool `json:"keep"`
}

type SetKeepArchivesResult struct{}

type GameState string

const (
	// GameOnRomM is a game on the server and not on this Deck.
	GameOnRomM GameState = "on_romm"
	// GameQueued is waiting for a download slot.
	GameQueued      GameState = "queued"
	GameDownloading GameState = "downloading"
	// GameInstalled is on this Deck, downloaded by the plugin.
	GameInstalled GameState = "installed"
	// GameDeleting is installed, with its files being checked and deleted.
	GameDeleting GameState = "deleting"
	// GameFailed is a download that stopped on an error. It stays in the queue, keeping what it
	// downloaded, until download_game retries it or cancel_download dismisses it.
	GameFailed GameState = "failed"
	// GameOnDeck is a game with a file the plugin didn't download where its download would go: On
	// Deck (not managed). download_game refuses it; adopt_game and link_game take it in.
	GameOnDeck GameState = "on_deck"
	// GameUpdate is installed, with its files on the server changed since the plugin downloaded
	// it.
	GameUpdate GameState = "update_available"
)

type GameSummary struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	SizeBytes int64     `json:"sizeBytes"`
	Regions   []string  `json:"regions"`
	State     GameState `json:"state"`
	// PlatformName is set in a collection's list and on shelves, where games come from several
	// platforms.
	PlatformName string `json:"platformName,omitempty"`
	// HasCover is set when get_cover has a small cover to ask for.
	HasCover bool `json:"hasCover"`
	// Removed is set when RomM no longer has the game, which stays while it's on this Deck.
	Removed bool `json:"removed,omitempty"`
}

type LibraryShelvesParams struct {
	// Limit is how many games each shelf holds, capped at 50; zero means 8.
	Limit int `json:"limit,omitempty"`
}

// LibraryShelves are the rows of the full-screen library's home.
type LibraryShelves struct {
	// Recent are the games added to RomM last, newest first.
	Recent []GameSummary `json:"recent"`
	// Installed are the games on this Deck, by name, and InstalledTotal how many there are.
	Installed      []GameSummary `json:"installed"`
	InstalledTotal int           `json:"installedTotal"`
	// Updates are the installed games RomM has an update of, by name, and UpdatesTotal how many
	// there are.
	Updates      []GameSummary `json:"updates"`
	UpdatesTotal int           `json:"updatesTotal"`
	// Platforms are every platform the library holds games for, by name, each with its first
	// games by name.
	Platforms []PlatformShelf `json:"platforms"`
}

type PlatformShelf struct {
	Platform PlatformSummary `json:"platform"`
	Games    []GameSummary   `json:"games"`
}

type SearchGamesParams struct {
	// Query is a title, a few words of one, or a ROM's CRC32, MD5 or SHA-1 hash.
	Query string `json:"query"`
	// PlatformID narrows the search to one platform; zero searches every platform.
	PlatformID int `json:"platformId,omitempty"`
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
	// HasCover is set when get_cover has a small cover to ask for.
	HasCover bool `json:"hasCover"`
	// Removed is set when RomM no longer has the game, which stays while it's on this Deck.
	Removed bool `json:"removed,omitempty"`
}

type GetGameParams struct {
	ID int `json:"id"`
}

// GetCoverParams names a game and the size of its cover: small for lists, large for its page.
type GetCoverParams struct {
	ID   int       `json:"id"`
	Size CoverSize `json:"size"`
}

type CoverSize string

const (
	CoverSmall CoverSize = "small"
	CoverLarge CoverSize = "large"
)

type Cover struct {
	// DataURL is the image as a data: URL, empty when the game has no cover or it can't be fetched
	// right now.
	DataURL string `json:"dataUrl,omitempty"`
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
	// HasCover is set when get_cover has a large cover to ask for.
	HasCover bool `json:"hasCover"`
	// Removed is set when RomM no longer has the game, which stays while it's on this Deck.
	Removed bool `json:"removed,omitempty"`
	// InstalledPaths are the files the plugin placed, set when installed.
	InstalledPaths []string `json:"installedPaths,omitempty"`
	// InstallNote says what's left before the game plays: it was placed in a format its emulator
	// can't use as it is, because RomM couldn't convert it. Set when installed.
	InstallNote string `json:"installNote,omitempty"`
	// LinkedPath is the User-managed file of a Linked game, set while it's linked.
	LinkedPath string `json:"linkedPath,omitempty"`
}

// CheckOnDeckParams names an on_deck game whose file to compare with RomM's.
type CheckOnDeckParams struct {
	ID int `json:"id"`
}

// OnDeckCheck is how the file where an on_deck game would go compares with the game on RomM.
type OnDeckCheck struct {
	Path string `json:"path"`
	// Checking is set while the file is still being hashed; call again for the answer.
	Checking bool `json:"checking"`
	// Matches is set when the file is RomM's game, so adopt_game can take it.
	Matches bool `json:"matches"`
	// Why says why it doesn't match, set once checked when it doesn't.
	Why    string `json:"why,omitempty"`
	Linked bool   `json:"linked"`
}

// AdoptGameParams names an on_deck game whose matching file the plugin takes over, as if it had
// downloaded it.
type AdoptGameParams struct {
	ID int `json:"id"`
}

type AdoptGameResult struct{}

// LinkGameParams names an on_deck game to link to RomM, so its saves sync while its file stays
// the user's.
type LinkGameParams struct {
	ID int `json:"id"`
}

type LinkGameResult struct{}

// StopManagingParams names an installed game the plugin stops managing. Its files stay as they
// are, User-managed: the plugin never updates or deletes them again.
type StopManagingParams struct {
	ID int `json:"id"`
}

type StopManagingResult struct{}

// IgnoreUpdateParams names an update_available game to keep on the version it has. The update RomM
// has now isn't offered again; one changing the game's files further is.
type IgnoreUpdateParams struct {
	ID int `json:"id"`
}

type IgnoreUpdateResult struct{}

// FindGamesParams names the platform whose ROM folder Find my games hashes, to find RomM's games
// kept under other names. It changes nothing: what it finds becomes on_deck, for adopt_game or
// link_game.
type FindGamesParams struct {
	PlatformID int `json:"platformId"`
}

type FindGamesStatusParams struct {
	PlatformID int `json:"platformId"`
}

// FindGamesStatus is how a platform's Find my games stands.
type FindGamesStatus struct {
	PlatformID int  `json:"platformId"`
	Running    bool `json:"running"`
	// Done is set once a run has finished, so Found is its answer.
	Done bool `json:"done"`
	// Hashed and Total count the files a run hashes.
	Hashed int `json:"hashed"`
	Total  int `json:"total"`
	// Found are the games found that are still neither adopted nor linked, by name.
	Found []FoundGame `json:"found"`
	// Problem says why the last run stopped short.
	Problem *Error `json:"problem,omitempty"`
}

// FoundGame is a game of RomM's that Find my games found under another name.
type FoundGame struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	// Path is the file or folder holding it.
	Path string `json:"path"`
}

// OwnershipKind is what changed a game's ownership.
type OwnershipKind string

const (
	// OwnershipAdopted is a game adopted, now installed.
	OwnershipAdopted OwnershipKind = "adopted"
	// OwnershipLinked is a game linked to RomM, still on_deck.
	OwnershipLinked OwnershipKind = "linked"
	// OwnershipGone is an installed game whose files are all gone, found on a library refresh.
	OwnershipGone OwnershipKind = "gone"
	// OwnershipChanged is an installed game with a file changed or gone, found on a library
	// refresh; the plugin no longer manages what's left.
	OwnershipChanged OwnershipKind = "changed"
	// OwnershipReleased is an installed game the user stopped managing; its files stay, now theirs.
	OwnershipReleased OwnershipKind = "released"
	// OwnershipUpdate is an installed game RomM has an update of, now update_available, or one
	// whose update went away, installed again.
	OwnershipUpdate OwnershipKind = "update"
)

// OwnershipEvent is a change to a game's ownership, and the state it left the game in.
type OwnershipEvent struct {
	ID    int           `json:"id"`
	Name  string        `json:"name"`
	State GameState     `json:"state"`
	Kind  OwnershipKind `json:"kind"`
}

// DownloadGameParams names the game to download. A failed download is retried, resuming what it
// downloaded. An update_available game downloads its update, which replaces its files once it's
// all there; cancelling it leaves the old version as it was.
type DownloadGameParams struct {
	ID int `json:"id"`
}

// CancelDownloadParams names the download to cancel, or the failed download to dismiss. Either
// way, what it downloaded is deleted.
type CancelDownloadParams struct {
	ID int `json:"id"`
}

type CancelDownloadResult struct {
	// Cancelled is false when the game wasn't queued or downloading.
	Cancelled bool `json:"cancelled"`
}

type ListDownloadsParams struct{}

type ListDownloadsResult struct {
	// Downloads are the queued, running and failed downloads, in queue order.
	Downloads []DownloadItem `json:"downloads"`
	// Problem says why downloading is off.
	Problem *Error `json:"problem,omitempty"`
	// Held says why the queue is on hold: the drive filled, and no download starts until there's
	// room for the next one.
	Held *Error `json:"held,omitempty"`
}

// DownloadHoldEvent is the download queue going on hold, with Held saying why, or coming off it.
type DownloadHoldEvent struct {
	Held *Error `json:"held,omitempty"`
}

type DownloadItem struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	PlatformID   int    `json:"platformId"`
	PlatformName string `json:"platformName"`
	// State is queued, downloading or failed.
	State         GameState `json:"state"`
	ReceivedBytes int64     `json:"receivedBytes"`
	TotalBytes    int64     `json:"totalBytes"`
	// Problem says why the download failed.
	Problem *Error `json:"problem,omitempty"`
	// Converting is set while RomM converts the game to a format its emulator plays.
	Converting bool `json:"converting,omitempty"`
	// WaitingForGame is set while the download, transferred, waits for a running game to close
	// before it's extracted or zipped.
	WaitingForGame bool `json:"waitingForGame,omitempty"`
}

// DownloadEvent is a change to a download. One that leaves the queue is installed, or on_romm when
// it was cancelled or dismissed.
type DownloadEvent struct {
	ID             int       `json:"id"`
	Name           string    `json:"name"`
	PlatformID     int       `json:"platformId"`
	PlatformName   string    `json:"platformName"`
	State          GameState `json:"state"`
	ReceivedBytes  int64     `json:"receivedBytes"`
	TotalBytes     int64     `json:"totalBytes"`
	Problem        *Error    `json:"problem,omitempty"`
	Converting     bool      `json:"converting,omitempty"`
	WaitingForGame bool      `json:"waitingForGame,omitempty"`
}

// PlanDownloadAllParams names the platform, or with Collection set the collection, whose missing
// games Download all missing would queue.
type PlanDownloadAllParams struct {
	PlatformID int            `json:"platformId,omitempty"`
	Collection *CollectionRef `json:"collection,omitempty"`
}

// DownloadAllPlan is what downloading every missing game of a platform or collection would take:
// those on the server that aren't on this Deck or in the queue.
type DownloadAllPlan struct {
	// Name is the platform's or the collection's.
	Name string `json:"name"`
	// Count is how many missing games the plugin can place, and TotalBytes how much they download.
	Count      int   `json:"count"`
	TotalBytes int64 `json:"totalBytes"`
	// NeedBytes is the room they take while downloading; an archive that's extracted counts twice.
	NeedBytes int64 `json:"needBytes"`
	// FreeBytes is the room left once the queue ahead and a margin are taken off, set when
	// FreeKnown.
	FreeBytes int64 `json:"freeBytes"`
	FreeKnown bool  `json:"freeKnown"`
	// Fits says every game fits. When it doesn't, FitCount games fit, taken by name, downloading
	// FitBytes.
	Fits     bool  `json:"fits"`
	FitCount int   `json:"fitCount"`
	FitBytes int64 `json:"fitBytes"`
	// Unplaceable counts missing games the plugin can't place in EmuDeck: their platform has no
	// folder, or no layout for a game of several files.
	Unplaceable int `json:"unplaceable"`
}

// DownloadAllParams queues a platform's or collection's missing games: every one, which fails with
// no_space when they don't fit, or with OnlyWhatFits those that fit.
type DownloadAllParams struct {
	PlatformID   int            `json:"platformId,omitempty"`
	Collection   *CollectionRef `json:"collection,omitempty"`
	OnlyWhatFits bool           `json:"onlyWhatFits,omitempty"`
}

type DownloadAllResult struct {
	Queued int `json:"queued"`
	// Failed counts games that didn't queue; the backend log says why.
	Failed int `json:"failed"`
}

// DeleteGamesParams names the games to delete: IDs, or with PlatformID every game the plugin
// downloaded for that platform.
type DeleteGamesParams struct {
	IDs        []int `json:"ids,omitempty"`
	PlatformID int   `json:"platformId,omitempty"`
}

type DeleteGamesResult struct {
	// Count is how many games will be deleted. Each one's progress arrives as delete events.
	Count int `json:"count"`
}

// DeleteEvent is a change to a game being deleted: deleting when it starts, then on_romm once its
// files are gone, or when the plugin refused to delete them, with Problem saying why.
type DeleteEvent struct {
	ID      int       `json:"id"`
	Name    string    `json:"name"`
	State   GameState `json:"state"`
	Problem *Error    `json:"problem,omitempty"`
}

type PlatformBIOSParams struct {
	PlatformID int `json:"platformId"`
}

// PlatformBIOS is the BIOS and firmware files RomM has for a platform, and how each stands in
// EmuDeck's BIOS folder.
type PlatformBIOS struct {
	PlatformID int `json:"platformId"`
	// InEmulator is set when EmuDeck keeps the platform's firmware inside its emulator, where the
	// plugin doesn't place it. Files then have no path or status.
	InEmulator bool       `json:"inEmulator"`
	Files      []BIOSFile `json:"files"`
	// Downloading is set while the plugin downloads the platform's missing files.
	Downloading bool `json:"downloading"`
}

type BIOSStatus string

const (
	BIOSPresent BIOSStatus = "present"
	BIOSMissing BIOSStatus = "missing"
	// BIOSDifferent is a file of the same name with other content, which the plugin leaves alone.
	BIOSDifferent BIOSStatus = "different"
)

type BIOSFile struct {
	FileName  string     `json:"fileName"`
	SizeBytes int64      `json:"sizeBytes"`
	Path      string     `json:"path,omitempty"`
	Status    BIOSStatus `json:"status,omitempty"`
	// Verified is set when RomM knows the file as a good dump.
	Verified bool `json:"verified"`
	// Owned is set when the plugin placed the file.
	Owned bool `json:"owned"`
}

// DownloadBIOSParams names the platform whose missing BIOS files to download. Files already in the
// BIOS folder are never replaced.
type DownloadBIOSParams struct {
	PlatformID int `json:"platformId"`
}

type DownloadBIOSResult struct {
	// Count is how many files will download. How it ends arrives as a bios event.
	Count int `json:"count"`
}

// BIOSEvent is a platform's BIOS download starting, or ending when Done.
type BIOSEvent struct {
	PlatformID   int    `json:"platformId"`
	PlatformName string `json:"platformName"`
	// Auto is set when downloading a game started it.
	Auto bool `json:"auto"`
	Done bool `json:"done"`
	// Files is how many files the download fetches, and Downloaded how many it placed.
	Files      int `json:"files"`
	Downloaded int `json:"downloaded"`
	// Problem says why a file didn't download, set when done.
	Problem *Error `json:"problem,omitempty"`
}

type GetPreferencesParams struct{}

// Preferences are the plugin's settings.
type Preferences struct {
	// AutoBIOS downloads a platform's missing BIOS files with its first game download.
	AutoBIOS bool `json:"autoBios"`
	// WaitForGames holds extracting and hashing while an emulator runs a game.
	WaitForGames bool `json:"waitForGames"`
	// ESDEMedia copies a downloaded game's cover and screenshot into ES-DE's media folder.
	ESDEMedia bool `json:"esdeMedia"`
}

// SetPreferencesParams changes the preferences it sets, leaving the rest.
type SetPreferencesParams struct {
	AutoBIOS     *bool `json:"autoBios,omitempty"`
	WaitForGames *bool `json:"waitForGames,omitempty"`
	ESDEMedia    *bool `json:"esdeMedia,omitempty"`
}

type ESDENoticeParams struct{}

// ESDENotice counts the games ES-DE hasn't seen: added to or removed from ROM folders while it ran.
// Both are zero once it restarts or closes, or when nothing changed.
type ESDENotice struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
}

type ESDERescanEvent struct {
	// Batch is what the downloads and deletes just finished changed.
	Batch ESDENotice `json:"batch"`
	// Notice is everything ES-DE needs a rescan to see.
	Notice ESDENotice `json:"notice"`
}

// SyncSavesParams syncs the saves of the games with ids, or of every game when ids is empty. The
// sync runs in the background; save_sync events tell when it starts and ends.
type SyncSavesParams struct {
	IDs []int `json:"ids,omitempty"`
	// Opened says the QAM just opened: every game syncs, unless a sync ran moments ago.
	Opened bool `json:"opened,omitempty"`
}

type SyncSavesResult struct{}

type SaveStatusParams struct {
	ID int `json:"id"`
}

// SaveState is where a game's save stands after the last sync that looked at it.
type SaveState string

const (
	// SaveSynced means the save on this Deck is the one RomM has, or neither has one.
	SaveSynced SaveState = "synced"
	// SaveConflict means both this Deck's save and RomM's changed since they last matched; neither
	// was overwritten.
	SaveConflict SaveState = "conflict"
	// SaveRemovedOnServer means RomM's slot was emptied after this Deck synced it.
	SaveRemovedOnServer SaveState = "removed_on_server"
	// SaveWaiting means a newer save from RomM waits for RetroArch to close.
	SaveWaiting SaveState = "waiting"
	// SaveFailed means the last sync of the game failed; problem says why.
	SaveFailed SaveState = "failed"
)

// SaveStatus is where a game's save sync stands.
type SaveStatus struct {
	ID int `json:"id"`
	// Syncs is unset when the plugin doesn't sync the game's saves; why says why.
	Syncs bool   `json:"syncs"`
	Why   string `json:"why,omitempty"`
	// Path is the save file, and emulator the RetroArch core it's tagged with on RomM.
	Path     string `json:"path,omitempty"`
	Emulator string `json:"emulator,omitempty"`
	// State is unset until a sync looks at the game.
	State   SaveState `json:"state,omitempty"`
	Problem string    `json:"problem,omitempty"`
	// CheckedAt is when a sync last looked at the game, as RFC 3339.
	CheckedAt string `json:"checkedAt,omitempty"`
	Syncing   bool   `json:"syncing"`
}

type SaveSummaryParams struct{}

// SaveCounts counts what a sync did, or where save sync stands across games.
type SaveCounts struct {
	Games      int `json:"games"`
	Uploaded   int `json:"uploaded"`
	Downloaded int `json:"downloaded"`
	Conflicts  int `json:"conflicts"`
	Waiting    int `json:"waiting"`
	Failed     int `json:"failed"`
}

// SaveSummary is where save sync stands across every game a sync looked at.
type SaveSummary struct {
	Counts SaveCounts `json:"counts"`
	// LastCheckedAt is when a sync last looked at any game, as RFC 3339.
	LastCheckedAt string `json:"lastCheckedAt,omitempty"`
	Syncing       bool   `json:"syncing"`
}

type SaveSyncEvent struct {
	Syncing bool `json:"syncing"`
	// IDs are the games the sync covers, empty for every game.
	IDs []int `json:"ids,omitempty"`
	// Counts is what the sync did, set when it ends.
	Counts SaveCounts `json:"counts"`
	// Problem says why the sync couldn't run.
	Problem *Error `json:"problem,omitempty"`
	// Auto is set on a sync nobody asked for: a save written, a periodic pull, the QAM opening.
	Auto bool `json:"auto"`
}
