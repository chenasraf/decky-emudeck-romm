package domain

import (
	"path/filepath"
	"strings"
	"time"
)

// LinkedGame is a User-managed ROM tied to its RomM entry so its saves sync, without the plugin
// owning the file.
type LinkedGame struct {
	ROMID      int
	PlatformID int
	// Path is the file or folder the link was made for.
	Path     string
	LinkedAt time.Time
}

// Matches says whether sums are of f's content, by the strongest hash the Server has for it. A file
// the Server has no hash for never matches: without one, the plugin can't tell it's the same game.
func (f ROMFile) Matches(sums FileSums) bool {
	same, _ := sums.match(f.SHA1, f.MD5, f.CRC)
	return same
}

// RomFolder is the entries of a ROM folder, indexed to find what sits where a game would go.
type RomFolder struct {
	names map[string]bool
	// stems maps each entry's name without its extension, and each folder's whole name too, to the
	// entry; archives maps it for the archives only.
	stems    map[string]string
	archives map[string]string
}

// NewRomFolder indexes the names of a ROM folder's files and folders.
func NewRomFolder(files, folders []string) RomFolder {
	f := RomFolder{names: map[string]bool{}, stems: map[string]string{}, archives: map[string]string{}}
	for _, n := range files {
		f.names[n] = true
		stem := strings.TrimSuffix(n, filepath.Ext(n))
		f.stems[stem] = n
		if IsArchive(n) {
			f.archives[stem] = n
		}
	}
	// A folder may take an extension, as a game's .m3u folder does, or have a dot that's part of its
	// name, as in "Game (v1.1)": it's indexed both ways.
	for _, n := range folders {
		f.names[n] = true
		f.stems[strings.TrimSuffix(n, filepath.Ext(n))] = n
		f.stems[n] = n
	}
	return f
}

// Occupant returns the entry that sits where rom would go in s's ROM folder: one of the ROM's own
// file or folder name, or, where the plugin would place it under another name, one of the same
// name with another extension. That covers an archive the plugin extracts, a file the Server
// converts, a game of several files in s's layout, and a copy the user keeps zipped.
func (f RomFolder) Occupant(s System, rom ROM) (string, bool) {
	name := rom.FileName
	if name == "" {
		name = rom.Name
	}
	if f.names[name] {
		return name, true
	}
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	_, converts := s.ConversionFor(name)
	renamed := IsArchive(name) && s.ArchivePolicy() != ArchivesKeep || converts
	if rom.MultiFile {
		// A folder's name is the game's, dots and all; its layout adds the extension.
		stem, renamed = name, true
	}
	if e, ok := f.stems[stem]; ok && renamed {
		return e, true
	}
	if e, ok := f.archives[stem]; ok {
		return e, true
	}
	return "", false
}
