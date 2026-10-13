package system

import (
	"log/slog"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// FolderWatch reports the files written in the folders it watches, each folder on its own, not
// the folders inside it.
type FolderWatch struct {
	w       *fsnotify.Watcher
	changes chan string
	log     *slog.Logger

	mu   sync.Mutex
	dirs map[string]bool
}

// WatchFolders starts a watch with no folders in it.
func WatchFolders(log *slog.Logger) (*FolderWatch, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	f := &FolderWatch{w: w, changes: make(chan string, 64), log: log, dirs: map[string]bool{}}
	go f.run()
	return f, nil
}

// Watch makes the folders in dirs the ones watched, dropping any other.
func (f *FolderWatch) Watch(dirs ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	want := map[string]bool{}
	for _, d := range dirs {
		want[filepath.Clean(d)] = true
	}
	for d := range f.dirs {
		if !want[d] {
			_ = f.w.Remove(d)
			delete(f.dirs, d)
		}
	}
	for d := range want {
		if f.dirs[d] {
			continue
		}
		if err := f.w.Add(d); err != nil {
			return err
		}
		f.dirs[d] = true
	}
	return nil
}

// Changes are the paths of files written, created or moved into a watched folder.
func (f *FolderWatch) Changes() <-chan string { return f.changes }

func (f *FolderWatch) Close() error { return f.w.Close() }

func (f *FolderWatch) run() {
	defer close(f.changes)
	for {
		select {
		case e, ok := <-f.w.Events:
			if !ok {
				return
			}
			if e.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				f.changes <- e.Name
			}
		case err, ok := <-f.w.Errors:
			if !ok {
				return
			}
			f.log.Warn("watch folders", "err", err)
		}
	}
}
