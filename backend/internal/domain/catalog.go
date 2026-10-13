package domain

import "time"

// Platform is a platform on the Server, as the Catalog keeps it.
type Platform struct {
	ID   int
	Slug string
	Name string
	// ROMCount is how many ROMs the Server has for the platform.
	ROMCount int
}

// ROM is a game on the Server, as the Catalog keeps it.
type ROM struct {
	ID         int
	PlatformID int
	Name       string
	// FileName is the ROM's file or folder name on the Server.
	FileName  string
	Extension string
	SizeBytes int64
	Regions   []string
	Revision  string
	// MultiFile is set when the ROM is a folder of several files rather than a single file.
	MultiFile bool
	UpdatedAt time.Time
	// CoverSmall and CoverLarge are the Server-relative paths of its cover images, empty when it
	// has none.
	CoverSmall string
	CoverLarge string
}

// PlatformSummary is a platform with what the Catalog and this Deck hold of it.
type PlatformSummary struct {
	Platform
	// Games is how many of the platform's ROMs the Catalog holds.
	Games int
	// Installed is how many of them are on this Deck.
	Installed int
}

// ROMFilter narrows a platform's game list.
type ROMFilter string

const (
	FilterAll          ROMFilter = "all"
	FilterInstalled    ROMFilter = "installed"
	FilterNotInstalled ROMFilter = "not_installed"
	FilterUpdates      ROMFilter = "updates"
)

// CatalogSync is what one sync with the Server brings to the Catalog.
type CatalogSync struct {
	Platforms []Platform
	// ROMs are the ROMs the Server sent: all of them on a full sync, or those changed since the
	// last one.
	ROMs []ROM
	// Full replaces every ROM with ROMs.
	Full bool
	// KeepIDs, when not nil, are every ROM id the Server has; any other is deleted.
	KeepIDs []int
	// Cursor is the newest updated_at the Catalog has seen, which the next sync asks for changes
	// since.
	Cursor time.Time
	At     time.Time
}

// ROMPage is one page of a game list.
type ROMPage struct {
	ROMs []ROM
	// Total is how many games match, across every page.
	Total int
}

// SearchResult is what a search across every platform found.
type SearchResult struct {
	ROMs []ROM
	// PlatformNames names the platform of each ROM, by platform id.
	PlatformNames map[int]string
	// Local is set when the Catalog answered because the Server couldn't: matches are by name
	// only, not by hash.
	Local bool
}
