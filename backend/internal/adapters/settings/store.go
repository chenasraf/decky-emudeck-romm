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
	// DownloadConcurrency is how many games download at once, 1 to 4; unset means 2.
	DownloadConcurrency int `json:"downloadConcurrency,omitempty"`
}

const (
	defaultConcurrency = 2
	maxConcurrency     = 4
)

// DownloadConcurrency returns how many games may download at once.
func (s *Store) DownloadConcurrency() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil || f.DownloadConcurrency <= 0 {
		return defaultConcurrency
	}
	return min(f.DownloadConcurrency, maxConcurrency)
}

type fileServer struct {
	URL             string   `json:"url"`
	CertFingerprint string   `json:"certFingerprint,omitempty"`
	DeviceID        string   `json:"deviceId,omitempty"`
	Scopes          []string `json:"scopes,omitempty"`
	SignedOut       bool     `json:"signedOut,omitempty"`
}

// SignIn returns the saved sign-in, or nil when there is no server or no token.
func (s *Store) SignIn() (*domain.SignIn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil || f.Server == nil || f.Server.URL == "" {
		return nil, err
	}
	token, err := s.token()
	if err != nil || token == "" {
		return nil, err
	}
	return &domain.SignIn{
		Server:    domain.Server{URL: f.Server.URL, CertFingerprint: f.Server.CertFingerprint},
		Token:     token,
		DeviceID:  f.Server.DeviceID,
		Scopes:    f.Server.Scopes,
		SignedOut: f.Server.SignedOut,
	}, nil
}

// SaveSignIn replaces the saved sign-in.
func (s *Store) SaveSignIn(in domain.SignIn) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return err
	}
	if err := s.write(tokenFile, []byte(in.Token+"\n"), 0o600); err != nil {
		return err
	}
	f.Server = &fileServer{
		URL:             in.Server.URL,
		CertFingerprint: in.Server.CertFingerprint,
		DeviceID:        in.DeviceID,
		Scopes:          in.Scopes,
		SignedOut:       in.SignedOut,
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return s.write(settingsFile, append(data, '\n'), 0o644)
}

func (s *Store) token() (string, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, tokenFile))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read token: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
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
