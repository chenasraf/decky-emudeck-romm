package domain

import (
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// AutosaveSlot is the RomM save slot a game's live save syncs in: the one RomM's web player and
// its other clients use, so they all share one timeline.
const AutosaveSlot = "autosave"

// SaveState is where a game's save stands after the last sync that looked at it.
type SaveState string

const (
	// SaveSynced means the save on the Deck is the one RomM has, or neither has one.
	SaveSynced SaveState = "synced"
	// SaveConflict means both the Deck's save and RomM's changed since they last matched. Nothing
	// was overwritten; the user decides which to keep.
	SaveConflict SaveState = "conflict"
	// SaveRemovedOnServer means RomM's slot was emptied after this Deck synced it. The save on the
	// Deck stays.
	SaveRemovedOnServer SaveState = "removed_on_server"
	// SaveWaiting means RomM has a newer save that waits for the emulator to close.
	SaveWaiting SaveState = "waiting"
	// SaveFailed means the last sync of the game failed; Problem says why.
	SaveFailed SaveState = "failed"
)

// SaveRecord is what the plugin knows of a game's save: where it is, and what it and RomM's were
// when they last matched.
type SaveRecord struct {
	ROMID    int
	Path     string
	Slot     string
	Emulator string
	// LocalHash is the MD5 of the save on the Deck when it last matched RomM's, empty before it
	// ever did.
	LocalHash    string
	ServerSaveID int
	ServerHash   string
	// SyncedAt is when RomM's save it last matched was written.
	SyncedAt  time.Time
	State     SaveState
	Problem   string
	CheckedAt time.Time
	// ConflictSaveID is RomM's save a conflict is with, and ConflictServerAt when it was written;
	// zero outside a conflict, or when the conflict surfaced without RomM naming its save.
	ConflictSaveID   int
	ConflictServerAt time.Time
	// ParkedHash is the MD5 of the Deck's save as parked on RomM in a conflict, so the same copy
	// isn't parked twice.
	ParkedHash string
}

// ParkedSaveName is the file name the Deck's save of a conflict is kept under on RomM: outside every
// slot, so no sync ever pairs it, and named for when it was parked.
func ParkedSaveName(fileName string, at time.Time) string {
	ext := filepath.Ext(fileName)
	return strings.TrimSuffix(fileName, ext) + " [conflict " + at.UTC().Format("2006-01-02_15-04-05") + "]" + ext
}

// ActivityKind is what save sync did, as the activity log records it.
type ActivityKind string

const (
	ActivityUploaded        ActivityKind = "uploaded"
	ActivityDownloaded      ActivityKind = "downloaded"
	ActivityConflict        ActivityKind = "conflict"
	ActivityResolved        ActivityKind = "resolved"
	ActivityFailed          ActivityKind = "failed"
	ActivityRemovedOnServer ActivityKind = "removed_on_server"
	ActivityRestored        ActivityKind = "restored"
)

// SaveActivity is one entry of the activity log: something save sync did to a game's save.
type SaveActivity struct {
	ID      int64
	ROMID   int
	At      time.Time
	Kind    ActivityKind
	Message string
	// Resolved is set on a conflict entry once the user settled it. Unresolved ones are never
	// pruned.
	Resolved bool
}

// ActivityKeep is how long, and how many, activity log entries are kept.
const (
	ActivityKeepFor = 90 * 24 * time.Hour
	ActivityKeepMax = 5000
)

// SaveBackup is a copy of a game's save the plugin set aside before replacing it.
type SaveBackup struct {
	Name      string
	At        time.Time
	SizeBytes int64
}

// SaveChoice is which copy of a conflicting save the user keeps.
type SaveChoice string

const (
	KeepDeck   SaveChoice = "deck"
	KeepServer SaveChoice = "server"
)

// ClientSave is a save on the Deck as negotiating a sync describes it to RomM.
type ClientSave struct {
	ROMID       int
	FileName    string
	Slot        string
	Emulator    string
	ContentHash string
	UpdatedAt   time.Time
	SizeBytes   int64
}

// SyncRequest asks RomM what a sync of ROMIDs takes, given the Deck's saves of them.
type SyncRequest struct {
	DeviceID string
	Saves    []ClientSave
	ROMIDs   []int
	// RestoreUnlisted offers RomM's save of a ROM the Deck lists none for even when this Device
	// synced it before: a save missing from the Deck comes back rather than counting as deleted.
	RestoreUnlisted bool
}

// SyncAction is what RomM says a sync does with one save.
type SyncAction string

const (
	SyncUpload   SyncAction = "upload"
	SyncDownload SyncAction = "download"
	SyncConflict SyncAction = "conflict"
	SyncNoOp     SyncAction = "no_op"
	// SyncDelete means the slot the Deck still holds a save of was emptied on RomM.
	SyncDelete SyncAction = "delete"
)

// SyncOperation is one step RomM plans for a sync.
type SyncOperation struct {
	Action SyncAction
	ROMID  int
	// SaveID is RomM's save, zero for uploads and deletes.
	SaveID          int
	FileName        string
	Slot            string
	Emulator        string
	Reason          string
	ServerUpdatedAt time.Time
	ServerHash      string
}

// SyncPlan is RomM's answer to a SyncRequest. The session it opens is completed once the steps are
// done.
type SyncPlan struct {
	SessionID  int
	Operations []SyncOperation
}

// SaveUpload is a save sent to RomM.
type SaveUpload struct {
	ROMID       int
	FileName    string
	Slot        string
	Emulator    string
	DeviceID    string
	ContentHash string
	SessionID   int
	Data        []byte
	// Overwrite makes the upload the slot's newest even if the slot changed since this Device last
	// synced it.
	Overwrite bool
}

// ServerSave is a save as RomM keeps it.
type ServerSave struct {
	ID          int
	ROMID       int
	FileName    string
	Slot        string
	Emulator    string
	ContentHash string
	UpdatedAt   time.Time
}

// otherSaveFormats are the emulators that tag their RomM saves with their own ID and write them in a
// format RetroArch's cores don't read. mGBA and melonDS keep the same raw battery save their cores
// do.
var otherSaveFormats = func() []string {
	var out []string
	for _, e := range Emulators {
		if e.ID != RetroArchID && e.ID != "mgba" && e.ID != "melonds" {
			out = append(out, e.ID)
		}
	}
	return out
}()

// RawBatterySave says whether a save RomM tagged with emulator is the raw battery save RetroArch's
// cores, mGBA and melonDS all read. Untagged saves, and those tagged with a core, are.
func RawBatterySave(emulator string) bool {
	return !slices.Contains(otherSaveFormats, emulator)
}
