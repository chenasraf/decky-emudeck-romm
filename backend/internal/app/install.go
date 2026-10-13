package app

import (
	"log/slog"
	"sync"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// probeInterval is how old a probe may be before opening the QAM or the library probes again.
const probeInterval = time.Minute

// Prober looks at what EmuDeck and ES-DE installed.
type Prober interface {
	Probe() domain.InstallState
}

// Install keeps the last probe of the disk in memory, so the many places that need EmuDeck's paths
// don't each walk the disk.
type Install struct {
	prober Prober
	log    *slog.Logger
	now    func() time.Time

	// mu is held through a probe, so callers that arrive during one share its result.
	mu   sync.Mutex
	last *domain.InstallState
}

func NewInstall(prober Prober, log *slog.Logger) *Install {
	return &Install{prober: prober, log: log, now: time.Now}
}

// State returns what the plugin knows of the disk, probing again when the last probe is older than
// a minute, or always when refresh is set, as Check again asks.
func (i *Install) State(refresh bool) domain.InstallState {
	i.mu.Lock()
	defer i.mu.Unlock()
	if !refresh && i.last != nil && i.now().Sub(i.last.CheckedAt) < probeInterval {
		return *i.last
	}

	state := i.prober.Probe()
	i.logChanges(state)
	i.last = &state
	return state
}

// logChanges logs a probe's findings the first time and whenever they change, rather than on every
// QAM open.
func (i *Install) logChanges(s domain.InstallState) {
	summary := installSummary(s)
	if i.last != nil && installSummary(*i.last) == summary {
		return
	}
	i.log.Info("install state", "emudeck", s.EmuDeck.Root, "source", s.EmuDeck.Source, "tried", s.EmuDeck.Tried,
		"esde_settings", s.ESDE.SettingsPath, "parse_gamelist_only", s.ESDE.ParseGamelistOnly,
		"esde_running", s.ESDE.Running, "emulators", summary.emulators)
}

type summary struct {
	root, source, esde string
	gamelistOnly       bool
	running            bool
	emulators          string
}

func installSummary(s domain.InstallState) summary {
	var emus []byte
	for _, e := range s.Emulators {
		mark := "-"
		switch {
		case e.Installed && (len(e.Saves) == 0 || e.SaveSyncReady()):
			mark = "+"
		case e.Installed:
			mark = "~"
		}
		emus = append(emus, (mark + e.ID + " ")...)
	}
	return summary{
		root:         s.EmuDeck.Root,
		source:       string(s.EmuDeck.Source),
		esde:         s.ESDE.SettingsPath,
		gamelistOnly: s.ESDE.ParseGamelistOnly,
		running:      s.ESDE.Running,
		emulators:    string(emus),
	}
}
