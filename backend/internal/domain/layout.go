package domain

import (
	"cmp"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// LayoutKind is how a System lays out a game of several files in its ROM folder.
type LayoutKind string

const (
	// LayoutPlaylist puts the files in a <Game>.m3u folder with a plugin-written <Game>.m3u listing
	// the discs. ES-DE shows the folder as one game, and emulators swap discs from the playlist.
	// Tracks keep their names inside the folder, so sets with generic track names (.gdi) can't
	// collide.
	LayoutPlaylist LayoutKind = "playlist"
	// LayoutPS3 puts the files in a <Game>.ps3 folder, which ES-DE and RPCS3 treat as the game.
	LayoutPS3 LayoutKind = "ps3"
	// LayoutFolder puts the files in a <Game> folder, as Cemu reads an unpacked Wii U game.
	LayoutFolder LayoutKind = "folder"
	// LayoutZip zips the files into <Game>.zip, which DOSBox-Pure mounts as a drive.
	LayoutZip LayoutKind = "zip"
)

// PlannedFile is a game file and where it goes: its path under the ROM folder, or inside the zip
// for LayoutZip. Paths use the host's separators.
type PlannedFile struct {
	File ROMFile
	Rel  string
}

// Playlist is an .m3u the plugin writes: its path under the ROM folder and its lines, which are
// relative to the playlist and use /.
type Playlist struct {
	Rel   string
	Lines []string
}

// Layout is where a game's files go under its System's ROM folder.
type Layout struct {
	// Root is the one entry the game adds to the ROM folder: its file, or the folder or zip holding
	// all of it.
	Root  string
	Files []PlannedFile
	// Playlist is set for LayoutPlaylist.
	Playlist *Playlist
	// Zip is set when Files are zipped into Root rather than placed.
	Zip bool
}

// PlanLayout decides where the game files of a game named game (its file or folder name on the
// Server) go in system's ROM folder. A single file goes in as it is, wherever the Server keeps it
// inside the game. Several files take system's layout, and a system without one can't place them.
func PlanLayout(system System, game string, files []ROMFile) (Layout, error) {
	if len(files) == 0 {
		return Layout{}, &Error{Kind: KindCantDownload, Message: "RomM lists no game files for this game."}
	}
	files = slices.Clone(files)
	for i, f := range files {
		if f.Path == "" {
			files[i].Path, f.Path = f.Name, f.Name
		}
		if !filepath.IsLocal(filepath.FromSlash(f.Path)) || !filepath.IsLocal(f.Name) {
			return Layout{}, &Error{Kind: KindCantDownload, Message: "RomM lists a file that would land outside the ROM folder: " + f.Path}
		}
	}
	if len(files) == 1 {
		f := files[0]
		return Layout{Root: f.Name, Files: []PlannedFile{{File: f, Rel: f.Name}}}, nil
	}

	inside := func(root string) []PlannedFile {
		out := make([]PlannedFile, 0, len(files))
		for _, f := range files {
			out = append(out, PlannedFile{File: f, Rel: filepath.Join(root, filepath.FromSlash(f.Path))})
		}
		return out
	}
	switch system.MultiFile {
	case LayoutPlaylist:
		return playlistLayout(game, files)
	case LayoutPS3:
		root := withSuffix(game, ".ps3")
		return Layout{Root: root, Files: inside(root)}, nil
	case LayoutFolder:
		return Layout{Root: game, Files: inside(game)}, nil
	case LayoutZip:
		root := withSuffix(game, ".zip")
		return Layout{Root: root, Files: inside(""), Zip: true}, nil
	}
	return Layout{}, &Error{Kind: KindCantDownload, Message: "The plugin has no way to lay out a game of several files in EmuDeck's " + system.Folder + " folder."}
}

func playlistLayout(game string, files []ROMFile) (Layout, error) {
	root := withSuffix(game, ".m3u")
	// The playlist is the plugin's own. One the Server has would list whatever the game's uploader
	// had, so it isn't downloaded.
	files = slices.DeleteFunc(slices.Clone(files), func(f ROMFile) bool { return extOf(f.Path) == "m3u" })
	discs := Discs(files)
	if len(discs) == 0 {
		return Layout{}, &Error{Kind: KindCantDownload, Message: "RomM lists no disc images for this game, so there's nothing to put in a playlist."}
	}
	planned := make([]PlannedFile, 0, len(files))
	for _, f := range files {
		planned = append(planned, PlannedFile{File: f, Rel: filepath.Join(root, filepath.FromSlash(f.Path))})
	}
	lines := make([]string, 0, len(discs))
	for _, f := range discs {
		lines = append(lines, f.Path)
	}
	return Layout{
		Root:     root,
		Files:    planned,
		Playlist: &Playlist{Rel: filepath.Join(root, root), Lines: lines},
	}, nil
}

// withSuffix adds suffix to name unless it already ends with it.
func withSuffix(name, suffix string) string {
	if strings.HasSuffix(strings.ToLower(name), suffix) {
		return name
	}
	return name + suffix
}

// descriptorExts are the sheets that describe a disc by pointing at its raw tracks.
var descriptorExts = []string{"cue", "gdi", "ccd", "mds"}

// companionExts are the raw tracks those sheets point at, which no emulator loads on their own.
var companionExts = []string{"bin", "raw", "img", "sub", "mdf", "wav", "ogg", "flac", "mp3"}

// Discs returns the files of a set that are discs, in disc order: numbered discs first, then the
// rest by name. Where a sheet is present its raw tracks aren't discs, but an image that stands on
// its own still is. This is RomM's own rule, so the plugin's playlist and RomM's agree.
func Discs(files []ROMFile) []ROMFile {
	discs := slices.DeleteFunc(slices.Clone(files), func(f ROMFile) bool { return extOf(f.Path) == "m3u" })
	if slices.ContainsFunc(discs, func(f ROMFile) bool { return slices.Contains(descriptorExts, extOf(f.Path)) }) {
		discs = slices.DeleteFunc(discs, func(f ROMFile) bool { return slices.Contains(companionExts, extOf(f.Path)) })
	}
	slices.SortStableFunc(discs, func(a, b ROMFile) int {
		na, oka := discNumber(a.Name)
		nb, okb := discNumber(b.Name)
		switch {
		case oka != okb:
			if oka {
				return -1
			}
			return 1
		case na != nb:
			return na - nb
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return discs
}

// discTag matches "(Disc 2)", "(CD 2)", "(Disk 1 of 2)", "(Disc 2A)" and "(Disc B)". A letter
// needs a space, so "(CDi)" isn't disc 9.
var discTag = regexp.MustCompile(`(?i)\((?:disc|disk|cd|disque)(?:\s*([0-9]+)[a-z]?|\s+([a-z]))(?:\s+of\s+[0-9]+)?\)`)

// discNumber is the disc a file name claims to be, with A as 1.
func discNumber(name string) (int, bool) {
	m := discTag.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}
	if m[1] != "" {
		n, err := strconv.Atoi(m[1])
		return n, err == nil
	}
	return int(strings.ToLower(m[2])[0]-'a') + 1, true
}

func extOf(p string) string {
	return strings.ToLower(strings.TrimPrefix(path.Ext(p), "."))
}

// SnapshotOfSet sums up a game's files in one snapshot, for telling whether bytes kept from an
// earlier attempt are still of the files the Server has: the first file's id, the total size and
// the latest change.
func SnapshotOfSet(files []ROMFile) FileSnapshot {
	var s FileSnapshot
	for i, f := range files {
		if i == 0 {
			s.FileID = f.ID
		}
		s.SizeBytes += f.SizeBytes
		if f.LastModified.After(s.LastModified) {
			s.LastModified = f.LastModified
		}
	}
	return s
}
