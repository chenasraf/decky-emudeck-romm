package domain

import (
	"path/filepath"
	"strings"
	"time"
)

// Firmware is a BIOS or firmware file the Server has for a platform.
type Firmware struct {
	ID         int
	PlatformID int
	FileName   string
	SizeBytes  int64
	CRC        string
	MD5        string
	SHA1       string
	// Verified is set when RomM found the file's hash on its list of known-good dumps.
	Verified bool
}

// FileSums are the hashes of a file on disk, as lowercase hex.
type FileSums struct {
	CRC  string
	MD5  string
	SHA1 string
}

// BIOSStatus is how a firmware file on the Server stands against the BIOS folder.
type BIOSStatus string

const (
	BIOSPresent BIOSStatus = "present"
	BIOSMissing BIOSStatus = "missing"
	// BIOSDifferent is a file of the same name with other content. The plugin never replaces it.
	BIOSDifferent BIOSStatus = "different"
)

// StatusOf compares the file at the firmware's place, of size bytes with sums, to the firmware. It
// compares the strongest hash the Server has, and the size when it has none.
func (f Firmware) StatusOf(size int64, sums FileSums) BIOSStatus {
	var same bool
	switch {
	case f.SHA1 != "":
		same = strings.EqualFold(f.SHA1, sums.SHA1)
	case f.MD5 != "":
		same = strings.EqualFold(f.MD5, sums.MD5)
	case f.CRC != "":
		same = strings.EqualFold(strings.TrimLeft(f.CRC, "0"), strings.TrimLeft(sums.CRC, "0"))
	default:
		same = size == f.SizeBytes
	}
	if same {
		return BIOSPresent
	}
	return BIOSDifferent
}

// SafeName is the firmware's file name when it names a file and nothing else: a name that would
// leave the BIOS folder returns false.
func (f Firmware) SafeName() (string, bool) {
	name := f.FileName
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) || filepath.Base(name) != name {
		return "", false
	}
	return name, true
}

// OwnedBIOS is the record that makes a BIOS file Plugin-owned.
type OwnedBIOS struct {
	FirmwareID  int
	PlatformID  int
	File        PlacedFile
	InstalledAt time.Time
}

// biosFolders are the platforms whose BIOS files EmuDeck keeps in a folder under bios/ rather than
// in bios/ itself, where RetroArch, DuckStation, PCSX2, melonDS and xemu all look.
var biosFolders = map[string]string{
	"dc":         "dc",
	"neo-geo-cd": "neocd",
}

// biosInEmulator are the platforms whose firmware EmuDeck keeps inside the emulator: keys and
// firmware the emulator installs or reads from its own data folder. A file placed in bios/ would
// do nothing there.
var biosInEmulator = map[string]bool{
	"switch":           true,
	"ps3":              true,
	"ps4":              true,
	"3ds":              true,
	"new-nintendo-3ds": true,
	"wiiu":             true,
}

// BIOSFolder returns where a platform's BIOS files go under EmuDeck's bios folder: "" for the bios
// folder itself. ok is false for a platform whose firmware EmuDeck keeps inside its emulator.
func BIOSFolder(slug string) (folder string, ok bool) {
	if biosInEmulator[slug] {
		return "", false
	}
	return biosFolders[slug], true
}
