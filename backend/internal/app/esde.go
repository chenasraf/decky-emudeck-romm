package app

import (
	"log/slog"
	"slices"
	"sync"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// FolderChange is what one download or delete changed in a ROM folder, and whether the queue has
// nothing left to do after it.
type FolderChange struct {
	Added, Removed int
	Idle           bool
}

// FolderListener hears each download or delete end.
type FolderListener func(FolderChange)

// folderChanged tells the folder listener what a download or delete changed.
func (d *Downloads) folderChanged(added, removed int) {
	if d.onFolder == nil {
		return
	}
	d.mu.Lock()
	idle := len(d.deleting) == 0 && !slices.ContainsFunc(d.jobs, func(j *job) bool { return j.State != domain.StateFailed })
	d.mu.Unlock()
	d.onFolder(FolderChange{Added: added, Removed: removed, Idle: idle})
}

// ESDERunning returns an id of the running ES-DE that changes when it restarts.
type ESDERunning func() (instance int, ok bool)

// ESDENotice counts the games ES-DE doesn't know of yet: added to or removed from ROM folders while
// it ran.
type ESDENotice struct {
	Added, Removed int
}

func (n ESDENotice) empty() bool { return n.Added == 0 && n.Removed == 0 }

// ESDEListener hears each batch of changes ES-DE needs a rescan to see, and what it needs in all.
type ESDEListener func(batch, notice ESDENotice)

// ESDE keeps track of what ES-DE, scanning ROM folders only at startup, hasn't seen. Changes made
// while it runs gather into one batch until the download queue is idle, then are announced at once;
// they stay in the notice until ES-DE restarts. Changes made while it's closed need nothing.
type ESDE struct {
	running  ESDERunning
	onNotice ESDEListener
	log      *slog.Logger

	mu       sync.Mutex
	instance int
	batch    ESDENotice
	notice   ESDENotice
}

func NewESDE(running ESDERunning, onNotice ESDEListener, log *slog.Logger) *ESDE {
	return &ESDE{running: running, onNotice: onNotice, log: log}
}

// FolderChanged takes in what a download or delete changed, announcing the batch once the queue is
// idle.
func (e *ESDE) FolderChanged(c FolderChange) {
	if c.Added+c.Removed > 0 {
		if instance, ok := e.running(); ok {
			e.mu.Lock()
			if instance != e.instance {
				// A restarted ES-DE scanned everything up to now.
				e.instance, e.batch, e.notice = instance, ESDENotice{}, ESDENotice{}
			}
			e.batch.Added += c.Added
			e.batch.Removed += c.Removed
			e.mu.Unlock()
		}
	}
	if !c.Idle {
		return
	}
	e.mu.Lock()
	batch := e.batch
	e.batch = ESDENotice{}
	e.notice.Added += batch.Added
	e.notice.Removed += batch.Removed
	notice := e.notice
	e.mu.Unlock()
	if batch.empty() {
		return
	}
	e.log.Info("ES-DE needs a rescan", "added", batch.Added, "removed", batch.Removed)
	if e.onNotice != nil {
		e.onNotice(batch, notice)
	}
}

// Notice returns what ES-DE hasn't seen, empty once it restarted or closed.
func (e *ESDE) Notice() ESDENotice {
	instance, ok := e.running()
	e.mu.Lock()
	defer e.mu.Unlock()
	if !ok || instance != e.instance {
		e.notice = ESDENotice{}
	}
	return e.notice
}
