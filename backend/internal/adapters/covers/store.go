// Package covers keeps cover images on disk, one file per ROM and size, named after the Server
// path it came from so a cover the Server changed is fetched again.
package covers

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// Store is the cover cache. It keeps an index of its files in memory, so lookups and trims never
// list the folder. A file's modification time is when it was last used, which carries the order
// of eviction across restarts.
type Store struct {
	dir string
	now func() time.Time

	mu    sync.Mutex
	files map[string]*entry
	total int64
}

type entry struct {
	romID int
	size  domain.CoverSize
	bytes int64
	used  time.Time
}

// Open returns the cache in dir, creating dir if needed. Files it doesn't recognise, such as a
// write cut short, are deleted.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create cover cache: %w", err)
	}
	s := &Store{dir: dir, now: time.Now, files: map[string]*entry{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read cover cache: %w", err)
	}
	for _, e := range entries {
		romID, size, ok := parseName(e.Name())
		info, err := e.Info()
		if !ok || err != nil || !info.Mode().IsRegular() {
			_ = os.RemoveAll(filepath.Join(dir, e.Name()))
			continue
		}
		s.files[e.Name()] = &entry{romID: romID, size: size, bytes: info.Size(), used: info.ModTime()}
		s.total += info.Size()
	}
	return s, nil
}

// Get returns the cover of a ROM in size that came from path, marking it used.
func (s *Store) Get(romID int, size domain.CoverSize, path string) ([]byte, bool, error) {
	return s.read(fileName(romID, size, path))
}

// Stale returns any cover the cache holds of a ROM in size, whatever path it came from: the one to
// show while the Server can't be reached.
func (s *Store) Stale(romID int, size domain.CoverSize) ([]byte, bool, error) {
	s.mu.Lock()
	var name string
	for n, e := range s.files {
		if e.romID == romID && e.size == size {
			name = n
			break
		}
	}
	s.mu.Unlock()
	if name == "" {
		return nil, false, nil
	}
	return s.read(name)
}

func (s *Store) read(name string) ([]byte, bool, error) {
	s.mu.Lock()
	e, ok := s.files[name]
	s.mu.Unlock()
	if !ok {
		return nil, false, nil
	}
	full := filepath.Join(s.dir, name)
	data, err := os.ReadFile(full)
	if errors.Is(err, fs.ErrNotExist) {
		s.forget(name)
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	now := s.now()
	s.mu.Lock()
	e.used = now
	s.mu.Unlock()
	_ = os.Chtimes(full, now, now)
	return data, true, nil
}

// Put stores the cover of a ROM in size that came from path, replacing any other copy of it.
func (s *Store) Put(romID int, size domain.CoverSize, path string, data []byte) error {
	name := fileName(romID, size, path)
	tmp, err := os.CreateTemp(s.dir, ".put-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(s.dir, name)); err != nil {
		return err
	}

	s.mu.Lock()
	var replaced []string
	for n, e := range s.files {
		if n != name && e.romID == romID && e.size == size {
			replaced = append(replaced, n)
		}
	}
	if old, ok := s.files[name]; ok {
		s.total -= old.bytes
	}
	s.files[name] = &entry{romID: romID, size: size, bytes: int64(len(data)), used: s.now()}
	s.total += int64(len(data))
	s.mu.Unlock()
	for _, n := range replaced {
		s.remove(n)
	}
	return nil
}

// Trim deletes the least recently used covers until the cache holds at most limit bytes, never
// deleting those of ROMs keep holds on to.
func (s *Store) Trim(limit int64, keep func(romID int) bool) {
	s.mu.Lock()
	if s.total <= limit {
		s.mu.Unlock()
		return
	}
	type candidate struct {
		name string
		used time.Time
	}
	var candidates []candidate
	for n, e := range s.files {
		if !keep(e.romID) {
			candidates = append(candidates, candidate{n, e.used})
		}
	}
	s.mu.Unlock()

	slices.SortFunc(candidates, func(a, b candidate) int { return a.used.Compare(b.used) })
	for _, c := range candidates {
		if s.Size() <= limit {
			return
		}
		s.remove(c.name)
	}
}

// Size is how many bytes the cached covers take.
func (s *Store) Size() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.total
}

func (s *Store) remove(name string) {
	if err := os.Remove(filepath.Join(s.dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return
	}
	s.forget(name)
}

func (s *Store) forget(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.files[name]; ok {
		s.total -= e.bytes
		delete(s.files, name)
	}
}

// fileName is <rom id>-<size>-<hash of path>.
func fileName(romID int, size domain.CoverSize, path string) string {
	sum := sha256.Sum256([]byte(path))
	return fmt.Sprintf("%d-%s-%s", romID, size, hex.EncodeToString(sum[:8]))
}

func parseName(name string) (int, domain.CoverSize, bool) {
	parts := strings.Split(name, "-")
	if len(parts) != 3 || len(parts[2]) != 16 {
		return 0, "", false
	}
	id, err := strconv.Atoi(parts[0])
	size := domain.CoverSize(parts[1])
	if err != nil || (size != domain.CoverSmall && size != domain.CoverLarge) {
		return 0, "", false
	}
	return id, size, true
}
