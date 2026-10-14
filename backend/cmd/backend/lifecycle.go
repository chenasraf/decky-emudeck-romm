package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/emudeck"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/settings"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/app"
)

// dataDirs are where the plugin keeps what it knows: settings and the token in settings, the
// databases, covers and save backups in runtime, and downloads on their way in staging, empty when
// EmuDeck's folder wasn't found.
type dataDirs struct {
	settings, runtime, staging string
}

func dataDirsFromEnv(log *slog.Logger) dataDirs {
	d := dataDirs{settings: os.Getenv("DECKY_PLUGIN_SETTINGS_DIR"), runtime: os.Getenv("DECKY_PLUGIN_RUNTIME_DIR")}
	if root := emudeck.New(userHome(log)).Probe().EmuDeck.Root; root != "" {
		d.staging = app.StagingFolder(root)
	}
	return d
}

// cacheFiles are the runtime files the plugin rebuilds from RomM on its own.
var cacheFiles = []string{"cache.db", "cache.db-wal", "cache.db-shm", "covers"}

// uninstall is what the plugin leaves behind when it's uninstalled: state.db and settings.json, so
// a reinstall still knows which games it downloaded once the user signs in again. The token goes,
// along with what rebuilds on its own and downloads that never finished.
func uninstall(d dataDirs) error {
	errs := []error{removeAll(d.staging), removeIn(d.runtime, cacheFiles...)}
	if d.settings != "" {
		errs = append(errs, settings.New(d.settings).SignOut())
	}
	return errors.Join(errs...)
}

// reset deletes everything the plugin knows: settings, the token, state.db and its backups, save
// backups, the caches and staging. Games and saves stay where they are, and the Device stays on
// RomM.
func reset(d dataDirs) error {
	errs := []error{removeAll(d.staging), removeIn(d.runtime, cacheFiles...), removeIn(d.runtime, "backups")}
	if d.runtime != "" {
		states, err := filepath.Glob(filepath.Join(d.runtime, "state.db*"))
		errs = append(errs, err)
		for _, f := range states {
			errs = append(errs, removeAll(f))
		}
	}
	if d.settings != "" {
		errs = append(errs, settings.New(d.settings).Reset())
	}
	return errors.Join(errs...)
}

func removeIn(dir string, names ...string) error {
	if dir == "" {
		return nil
	}
	var errs []error
	for _, n := range names {
		errs = append(errs, removeAll(filepath.Join(dir, n)))
	}
	return errors.Join(errs...)
}

func removeAll(path string) error {
	if path == "" {
		return nil
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}
