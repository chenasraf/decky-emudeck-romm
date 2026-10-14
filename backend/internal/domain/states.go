package domain

import (
	"slices"
	"strconv"
	"time"
)

// Save states sync next to saves: every new state on the Deck goes to RomM as a new entry, and the
// newest state RomM has from elsewhere comes to the Deck in a slot the user picks. RomM keeps states
// as one list per game, with no slots or devices, so which states this Deck uploaded is the plugin's
// own record.

// StateEmulator is the tag RomM's web player keeps GBA states under, and the plugin uploads them
// with, so the web player lists them.
const StateEmulator = "mgba"

// StateSlotAuto is the state slot an emulator writes and loads by itself: RetroArch's .state.auto,
// the mGBA app's slot 0.
const StateSlotAuto = "auto"

// StateSlots are the state slots a setting can name: the auto-state and slots 0 to 9.
var StateSlots = []string{StateSlotAuto, "0", "1", "2", "3", "4", "5", "6", "7", "8", "9"}

// StateKeeps are the counts a game's states this Deck uploaded may be trimmed to; 0 keeps every one.
var StateKeeps = []int{5, 10, 20, 0}

// StateSettings is how a game's save states sync.
type StateSettings struct {
	// Sync turns state sync on.
	Sync bool
	// Target is the slot RomM's newest state goes in, Extra another slot that gets a copy, empty for
	// none.
	Target string
	Extra  string
	// Keep is how many of the game's states this Deck uploaded RomM keeps, 0 for every one.
	Keep int
}

// DefaultStateSettings is how states sync until the user changes it.
var DefaultStateSettings = StateSettings{Sync: true, Target: StateSlotAuto, Keep: 10}

// StatePrefs is a game's own state settings; each unset field follows the global one.
type StatePrefs struct {
	Sync   *bool
	Target *string
	Extra  *string
	Keep   *int
}

// Empty says the game follows every global setting.
func (p StatePrefs) Empty() bool {
	return p.Sync == nil && p.Target == nil && p.Extra == nil && p.Keep == nil
}

// Apply returns s with the game's own settings over it.
func (p StatePrefs) Apply(s StateSettings) StateSettings {
	if p.Sync != nil {
		s.Sync = *p.Sync
	}
	if p.Target != nil {
		s.Target = *p.Target
	}
	if p.Extra != nil {
		s.Extra = *p.Extra
	}
	if p.Keep != nil {
		s.Keep = *p.Keep
	}
	return s
}

// ValidStateSlot says whether slot names a state slot.
func ValidStateSlot(slot string) bool { return slices.Contains(StateSlots, slot) }

// ValidStateKeep says whether keep is a count states can be trimmed to.
func ValidStateKeep(keep int) bool { return slices.Contains(StateKeeps, keep) }

// StateSlotName is how a slot is shown: "the auto-state" or "slot 3".
func StateSlotName(slot string) string {
	if slot == StateSlotAuto {
		return "the auto-state"
	}
	if _, err := strconv.Atoi(slot); err == nil {
		return "slot " + slot
	}
	return slot
}

// ServerState is a save state RomM keeps.
type ServerState struct {
	ID        int
	ROMID     int
	FileName  string
	Emulator  string
	SizeBytes int64
	CreatedAt time.Time
	// Screenshot is the path RomM serves the state's screenshot at, empty when it has none.
	Screenshot string
}

// StateUpload is a save state to add to a game's list on RomM, with the screenshot RomM shows for it.
type StateUpload struct {
	ROMID      int
	Emulator   string
	FileName   string
	Data       []byte
	Screenshot []byte
}

// StateFile is what state sync last saw of a state file on the Deck: the content it uploaded, or
// wrote there from RomM.
type StateFile struct {
	ROMID int
	Path  string
	Hash  string
}

// UploadedState is a save state this Deck put on RomM.
type UploadedState struct {
	StateID int
	ROMID   int
	// Path is the file it came from.
	Path string
	At   time.Time
}

// StateGame is where a game's state sync stands.
type StateGame struct {
	ROMID int
	// Placed is RomM's state last put in the target slot, so it isn't put there again.
	Placed int
	// Waiting says RomM's newest state waits for the emulator to close.
	Waiting bool
	// Problem is why the last sync of its states failed, empty when it didn't.
	Problem   string
	CheckedAt time.Time
}
