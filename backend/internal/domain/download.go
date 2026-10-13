package domain

import (
	"fmt"
	"math"
	"slices"
	"time"
)

// GameState is where a game stands between the Server and this Deck.
type GameState string

const (
	StateOnRomM      GameState = "on_romm"
	StateQueued      GameState = "queued"
	StateDownloading GameState = "downloading"
	StateInstalled   GameState = "installed"
	// StateDeleting is an installed game whose files are being checked and deleted.
	StateDeleting GameState = "deleting"
	// StateFailed is a download that stopped on an error. It stays in the queue, keeping what it
	// downloaded, until it is retried or dismissed.
	StateFailed GameState = "failed"
)

// QueuedDownload is a download in the queue as state.db keeps it, so the queue outlives the
// backend.
type QueuedDownload struct {
	ROMID        int
	PlatformID   int
	PlatformName string
	Name         string
	TotalBytes   int64
	// Problem is why the download failed; nil while it is queued or running.
	Problem *Error
	// Partial is the file whose bytes are in staging, so a later attempt resumes them only while
	// the Server still has that same file. Its FileID is zero before the first attempt.
	Partial FileSnapshot
}

// ROMFile is one file of a ROM on the Server.
type ROMFile struct {
	ID        int
	Name      string
	SizeBytes int64
	// Category is the kind of file: "game" for the game itself, or manual, dlc, update and so on.
	// Nested files outside a category folder have none.
	Category     string
	LastModified time.Time
	CRC          string
	MD5          string
	SHA1         string
}

// IsGame says whether the file is part of the game itself, rather than a manual, DLC, soundtrack
// or the like.
func (f ROMFile) IsGame() bool { return f.Category == "" || f.Category == "game" }

// GameFiles returns the files that make up the game itself.
func GameFiles(files []ROMFile) []ROMFile {
	var out []ROMFile
	for _, f := range files {
		if f.IsGame() {
			out = append(out, f)
		}
	}
	return out
}

// FileSnapshot is a game file as the Server described it when the plugin downloaded it, which
// later fetches compare against to find updates.
type FileSnapshot struct {
	FileID       int
	SizeBytes    int64
	LastModified time.Time
	// HashKind and Hash are the strongest hash the Server had for the file: sha1, md5 or crc. Both
	// are empty when it had none.
	HashKind string
	Hash     string
}

// SnapshotOf takes the snapshot of f.
func SnapshotOf(f ROMFile) FileSnapshot {
	s := FileSnapshot{FileID: f.ID, SizeBytes: f.SizeBytes, LastModified: f.LastModified}
	switch {
	case f.SHA1 != "":
		s.HashKind, s.Hash = "sha1", f.SHA1
	case f.MD5 != "":
		s.HashKind, s.Hash = "md5", f.MD5
	case f.CRC != "":
		s.HashKind, s.Hash = "crc", f.CRC
	}
	return s
}

// PlacedFile is a file the plugin put on disk, as it was right after placing it.
type PlacedFile struct {
	Path      string
	SizeBytes int64
	ModTime   time.Time
	// SHA1 is of the bytes on disk. RomM hashes a zipped ROM's contents rather than the zip, so it
	// can differ from the Server's hash of the same file.
	SHA1 string
}

// OwnedROM is the record that makes a ROM Plugin-owned: what the plugin placed, and what the
// Server had when it did.
type OwnedROM struct {
	ROMID       int
	PlatformID  int
	Files       []PlacedFile
	Snapshot    []FileSnapshot
	InstalledAt time.Time
}

var sizeUnits = []string{"B", "KB", "MB", "GB", "TB"}

// FormatSize writes a size in bytes as a short label, e.g. "1.4 GB", the way the frontend does.
func FormatSize(bytes int64) string {
	value, unit := float64(bytes), 0
	for value >= 1024 && unit < len(sizeUnits)-1 {
		value /= 1024
		unit++
	}
	if value < 10 && unit > 0 {
		return fmt.Sprintf("%.1f %s", value, sizeUnits[unit])
	}
	return fmt.Sprintf("%.0f %s", math.Round(value), sizeUnits[unit])
}

// SystemForSlug returns the EmuDeck System a RomM platform's games go in.
func SystemForSlug(slug string) (System, bool) {
	for _, s := range Systems {
		if slices.Contains(s.Slugs, slug) {
			return s, true
		}
	}
	return System{}, false
}
