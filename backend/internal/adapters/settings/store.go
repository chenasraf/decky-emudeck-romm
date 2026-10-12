// Package settings persists the plugin's configuration in Decky's settings dir: settings.json for
// readable options, and the client token in its own 0600 file so sharing settings.json never leaks
// it.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

const (
	settingsFile = "settings.json"
	tokenFile    = "token"
)

type Store struct {
	dir string
	mu  sync.Mutex
}

func New(dir string) *Store {
	return &Store{dir: dir}
}

type fileSettings struct {
	Server *fileServer `json:"server,omitempty"`
}

type fileServer struct {
	URL             string `json:"url"`
	CertFingerprint string `json:"certFingerprint,omitempty"`
}

// Server returns the saved server, or nil when none is set up.
func (s *Store) Server() (*domain.Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil || f.Server == nil || f.Server.URL == "" {
		return nil, err
	}
	return &domain.Server{URL: f.Server.URL, CertFingerprint: f.Server.CertFingerprint}, nil
}

func (s *Store) SaveServer(server domain.Server) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return err
	}
	f.Server = &fileServer{URL: server.URL, CertFingerprint: server.CertFingerprint}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return s.write(settingsFile, append(data, '\n'), 0o644)
}

// Token returns the saved client token, or "" when there is none.
func (s *Store) Token() (string, error) {
	if err := s.checkDir(); err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(s.dir, tokenFile))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read token: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func (s *Store) SaveToken(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write(tokenFile, []byte(token+"\n"), 0o600)
}

func (s *Store) load() (fileSettings, error) {
	var f fileSettings
	if err := s.checkDir(); err != nil {
		return f, err
	}
	data, err := os.ReadFile(filepath.Join(s.dir, settingsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return f, fmt.Errorf("read settings: %w", err)
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return f, fmt.Errorf("parse %s: %w", settingsFile, err)
	}
	return f, nil
}

func (s *Store) checkDir() error {
	if s.dir == "" {
		return errors.New("no settings directory configured")
	}
	return nil
}

// write replaces name atomically, so a crash mid-write leaves the previous file intact.
func (s *Store) write(name string, data []byte, perm fs.FileMode) error {
	if err := s.checkDir(); err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, "."+name+".*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	// Set the mode before any bytes land, so the token is never readable by others.
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(s.dir, name)); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}
