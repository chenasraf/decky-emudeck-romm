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
	// KeepArchives is the Keep archives toggle of each EmuDeck system folder that has one set;
	// unset means on.
	KeepArchives map[string]bool `json:"keepArchives,omitempty"`
	// InstallTo is the ROM folder each EmuDeck system's new downloads go in, for those set to one
	// of its alt folders; unset means the system's own folder.
	InstallTo map[string]string `json:"installTo,omitempty"`
	// AutoBIOS downloads a platform's missing BIOS files with its first game download; unset means
	// on.
	AutoBIOS *bool `json:"autoBios,omitempty"`
	// WaitForGames holds extracting and hashing while an emulator runs; unset means on.
	WaitForGames *bool `json:"waitForGames,omitempty"`
}

// WaitForGames says whether extracting and hashing wait for a running game to close.
func (s *Store) WaitForGames() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil || f.WaitForGames == nil {
		return true
	}
	return *f.WaitForGames
}

// SetWaitForGames turns waiting for a running game to close on or off.
func (s *Store) SetWaitForGames(on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return err
	}
	f.WaitForGames = &on
	return s.save(f)
}

// InstallTo returns the ROM folder an EmuDeck system's new downloads go in, empty when it's the
// system's own folder.
func (s *Store) InstallTo(system string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return ""
	}
	return f.InstallTo[system]
}

// SetInstallTo sets the ROM folder an EmuDeck system's new downloads go in; empty or the system's
// own folder goes back to it.
func (s *Store) SetInstallTo(system, folder string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return err
	}
	if folder == "" || folder == system {
		delete(f.InstallTo, system)
	} else {
		if f.InstallTo == nil {
			f.InstallTo = map[string]string{}
		}
		f.InstallTo[system] = folder
	}
	return s.save(f)
}

// AutoBIOS says whether queueing a game also downloads its platform's missing BIOS files.
func (s *Store) AutoBIOS() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil || f.AutoBIOS == nil {
		return true
	}
	return *f.AutoBIOS
}

// SetAutoBIOS turns downloading BIOS files with games on or off.
func (s *Store) SetAutoBIOS(on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return err
	}
	f.AutoBIOS = &on
	return s.save(f)
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

// KeepArchives says whether games in an EmuDeck system folder keep archives holding a single
// file rather than extract them.
func (s *Store) KeepArchives(folder string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return true
	}
	keep, ok := f.KeepArchives[folder]
	return keep || !ok
}

// SetKeepArchives sets the Keep archives toggle of an EmuDeck system folder.
func (s *Store) SetKeepArchives(folder string, keep bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return err
	}
	if f.KeepArchives == nil {
		f.KeepArchives = map[string]bool{}
	}
	f.KeepArchives[folder] = keep
	return s.save(f)
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
	return s.save(f)
}

// ForgetSignIn drops the saved server and its token. Every other setting stays.
func (s *Store) ForgetSignIn() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return err
	}
	f.Server = nil
	if err := s.save(f); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(s.dir, tokenFile)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove token: %w", err)
	}
	return nil
}

func (s *Store) save(f fileSettings) error {
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
