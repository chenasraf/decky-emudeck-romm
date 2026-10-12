// Package system reads facts about the machine the backend runs on.
package system

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// identifierSalt keeps the identifier specific to this plugin: systemd asks that machine-id not
// be sent off the machine as-is.
const identifierSalt = "decky-emudeck-romm\x00"

// fallbackFile holds a random identifier where /etc/machine-id doesn't exist, as on a desktop
// running the dev harness.
const fallbackFile = "device-identifier"

type Identity struct {
	machineIDPath string
	osReleasePath string
	fallbackDir   string
}

// NewIdentity reads /etc/machine-id, falling back to an identifier kept in fallbackDir.
func NewIdentity(fallbackDir string) *Identity {
	return &Identity{machineIDPath: "/etc/machine-id", osReleasePath: "/etc/os-release", fallbackDir: fallbackDir}
}

// Device returns how this machine introduces itself to RomM. Every Deck's hostname is
// "steamdeck", so the identifier comes from machine-id, which survives plugin reinstalls and
// changes only when SteamOS is reinstalled.
func (i *Identity) Device() (domain.DeviceIdentity, error) {
	seed, err := i.seed()
	if err != nil {
		return domain.DeviceIdentity{}, err
	}
	sum := sha256.Sum256([]byte(identifierSalt + seed))
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	return domain.DeviceIdentity{
		Identifier: hex.EncodeToString(sum[:]),
		Name:       fmt.Sprintf("Steam Deck (%s)", host),
		Platform:   i.platform(),
	}, nil
}

func (i *Identity) seed() (string, error) {
	data, err := os.ReadFile(i.machineIDPath)
	if err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id, nil
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("read machine id: %w", err)
	}
	return i.fallback()
}

func (i *Identity) fallback() (string, error) {
	if i.fallbackDir == "" {
		return "", errors.New("no machine id and no directory to keep a device identifier in")
	}
	path := filepath.Join(i.fallbackDir, fallbackFile)
	data, err := os.ReadFile(path)
	if err == nil && strings.TrimSpace(string(data)) != "" {
		return strings.TrimSpace(string(data)), nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("read device identifier: %w", err)
	}
	id := rand.Text()
	if err := os.MkdirAll(i.fallbackDir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("save device identifier: %w", err)
	}
	return id, nil
}

// platform is the OS's ID from os-release (steamos on a Deck), or Go's name for the OS.
func (i *Identity) platform() string {
	f, err := os.Open(i.osReleasePath)
	if err != nil {
		return runtime.GOOS
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if v, ok := strings.CutPrefix(scanner.Text(), "ID="); ok {
			if v = strings.Trim(v, `"'`); v != "" {
				return v
			}
		}
	}
	// A read error falls back the same way as a missing ID.
	_ = scanner.Err()
	return runtime.GOOS
}
