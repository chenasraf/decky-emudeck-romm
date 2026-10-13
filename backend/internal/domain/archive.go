package domain

import (
	"path/filepath"
	"strings"
)

// ArchivePolicy is what the plugin does with a game that arrives as a .zip or .7z.
type ArchivePolicy string

const (
	// ArchivesOptional is for cartridge and computer systems, whose emulators open archives:
	// archives are extracted unless Keep archives is on, which keeps those holding a single file.
	ArchivesOptional ArchivePolicy = "optional"
	// ArchivesKeep never extracts: the archive is the game's format (arcade romsets) or what its
	// emulator prefers (DOS).
	ArchivesKeep ArchivePolicy = "keep"
	// ArchivesExtract always extracts: the default emulator can't open archives, or RetroArch would
	// extract only the first file of a disc's tracks.
	ArchivesExtract ArchivePolicy = "extract"
)

// ArchivePolicy is what the plugin does with archives in s.
func (s System) ArchivePolicy() ArchivePolicy {
	if s.Archives == "" {
		return ArchivesOptional
	}
	return s.Archives
}

// IsArchive says whether a file name is an archive the plugin can extract.
func IsArchive(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".zip", ".7z":
		return true
	}
	return false
}

// Extracts says whether the plugin extracts an archive of members files in s, with Keep archives
// set to keep.
func (s System) Extracts(members int, keep bool) bool {
	switch s.ArchivePolicy() {
	case ArchivesKeep:
		return false
	case ArchivesExtract:
		return true
	default:
		return !keep || members != 1
	}
}
