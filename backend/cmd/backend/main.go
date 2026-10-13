package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	cachedb "github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/cache"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/covers"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/emudeck"
	esdecfg "github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/esde"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/romm"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/rpc"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/rpc/schema"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/settings"
	statedb "github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/state"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/system"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/app"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

// connectionCheckInterval is how often the backend asks RomM whether it still accepts the token:
// two small requests, and none while not signed in.
const connectionCheckInterval = 5 * time.Minute

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	// stdout carries only the protocol; logs go to stderr, which main.py forwards to decky.logger.
	level := slog.LevelInfo
	if v := os.Getenv("BACKEND_LOG_LEVEL"); v != "" {
		if err := level.UnmarshalText([]byte(v)); err != nil {
			fmt.Fprintf(os.Stderr, "ignoring BACKEND_LOG_LEVEL=%q: %v\n", v, err)
		}
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	if err := run(log); err != nil {
		log.Error("exiting", "err", err)
		os.Exit(1)
	}
	log.Info("stopped")
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Decky exports the plugin's dirs to the plugin process, which passes them down.
	settingsDir := os.Getenv("DECKY_PLUGIN_SETTINGS_DIR")
	store := settings.New(settingsDir)
	identity := system.NewIdentity(settingsDir)
	rommLog := log.With("component", "romm")
	dial := func(server domain.Server, token string) app.RomM {
		return romm.New(server, token, version).WithLogger(rommLog)
	}

	runtimeDir := os.Getenv("DECKY_PLUGIN_RUNTIME_DIR")
	if runtimeDir == "" {
		return errors.New("DECKY_PLUGIN_RUNTIME_DIR isn't set, so there's nowhere to keep the library cache")
	}
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}
	cache, err := cachedb.Open(ctx, filepath.Join(runtimeDir, "cache.db"), log.With("component", "cache"))
	if err != nil {
		return fmt.Errorf("open cache: %w", err)
	}
	defer func() { _ = cache.Close() }()

	srv := rpc.NewServer(os.Stdout, log)
	events := &connectionEvents{ConnectionEvents: rpc.NewConnectionEvents(srv)}
	conn := app.NewConnection(ctx, store, dial, identity, events, log.With("component", "connection"))
	install := app.NewInstall(emudeck.New(userHome(log)), log.With("component", "install"))
	catalogDial := func(server domain.Server, token string) app.CatalogServer {
		return romm.New(server, token, version).WithLogger(rommLog)
	}
	downloadDial := func(server domain.Server, token string) app.DownloadServer {
		return romm.New(server, token, version).WithLogger(rommLog)
	}

	// A state.db the plugin can't use turns downloads off rather than the whole backend, so the
	// library still browses and the QAM can say what's wrong.
	var ownership app.DownloadStore
	stateLog := log.With("component", "state")
	owned, stateErr := statedb.Open(ctx, filepath.Join(runtimeDir, "state.db"), stateLog)
	if stateErr != nil {
		stateLog.Error("state.db unusable; downloads are off", "err", stateErr)
	} else {
		defer func() { _ = owned.Close() }()
		ownership = owned
	}
	firmwareDial := func(server domain.Server, token string) app.FirmwareServer {
		return romm.New(server, token, version).WithLogger(rommLog)
	}
	var biosStore app.BIOSStore
	if owned != nil {
		biosStore = owned
	}
	bios := app.NewBIOS(ctx, cache, install, conn, firmwareDial, biosStore, stateErr, store, rpc.BIOSEvents(srv), log.With("component", "bios"))
	coverStore, err := covers.Open(filepath.Join(runtimeDir, "covers"))
	if err != nil {
		return err
	}
	coverDial := func(server domain.Server, token string) app.CoverServer {
		return romm.New(server, token, version).WithLogger(rommLog)
	}
	coverArt := app.NewCovers(ctx, cache, conn, coverDial, coverStore, ownership, log.With("component", "covers"))

	listeners := rpc.DownloadListeners(srv)
	listeners.Queued = func(d app.Download) { bios.GameQueued(d.PlatformID) }
	sendDownload := listeners.Change
	listeners.Change = func(d app.Download) {
		sendDownload(d)
		if d.State == domain.StateInstalled {
			coverArt.Prefetch(d.ROMID)
		}
	}
	gamesLog := log.With("component", "games")
	runningGame := func() (domain.Emulator, bool) {
		procs, err := system.Processes("/proc")
		if err != nil {
			gamesLog.Warn("list processes", "err", err)
			return domain.Emulator{}, false
		}
		return domain.RunningEmulator(procs)
	}
	games := app.NewGames(runningGame, store, gamesLog)
	esdeLog := log.With("component", "esde")
	esde := app.NewESDE(func() (int, bool) {
		procs, err := system.Processes("/proc")
		if err != nil {
			esdeLog.Warn("list processes", "err", err)
			return 0, false
		}
		return domain.RunningESDE(procs)
	}, rpc.ESDEEvents(srv), esdeLog)
	listeners.Folder = esde.FolderChanged
	mediaDial := func(server domain.Server, token string) app.MediaServer {
		return romm.New(server, token, version).WithLogger(rommLog)
	}
	media := app.NewMedia(coverArt, conn, mediaDial, store, log.With("component", "media"))
	downloads := app.NewDownloads(ctx, cache, install, conn, downloadDial, ownership, stateErr, system.FreeBytes, store,
		games, media, store.DownloadConcurrency(), listeners, log.With("component", "downloads"))

	sendCatalog := rpc.CatalogEvents(srv)
	catalog := app.NewCatalog(ctx, cache, downloads, conn, catalogDial, func(s app.CatalogStatus) {
		sendCatalog(s)
		// A refresh that worked may have brought new cover paths for games on this Deck.
		if !s.Refreshing && s.Problem == nil {
			coverArt.PrefetchOwned()
		}
	}, log.With("component", "catalog"))
	events.catalog = catalog
	servers := app.NewServerSwitch(conn, downloads, catalog, log.With("component", "servers"))
	saveDial := func(server domain.Server, token string) app.SaveServer {
		return romm.New(server, token, version).WithLogger(rommLog)
	}
	var saveStore app.SaveStore
	if owned != nil {
		saveStore = owned
	}
	saves := app.NewSaves(ctx, cache, install, conn, saveDial, ownership, saveStore, stateErr, esdecfg.New(userHome(log)), runningGame,
		filepath.Join(runtimeDir, "backups"), rpc.SaveEvents(srv), log.With("component", "saves"))
	rpc.Routes(srv, version, rpc.Services{
		Connection: conn, Install: install, Catalog: catalog, Downloads: downloads, BIOS: bios, Games: games, Covers: coverArt, Servers: servers, ESDE: esde, Media: media,
		Saves: saves,
	})
	go conn.Watch(ctx, connectionCheckInterval)
	go downloads.Resume(ctx)
	go downloads.SweepUpdates(ctx)
	// Without a folder watch, saves still sync on the periodic pulls and on demand.
	var saveWatch app.SaveWatch
	if w, err := system.WatchFolders(log.With("component", "watch")); err != nil {
		log.Warn("watch saves", "err", err)
	} else {
		defer func() { _ = w.Close() }()
		saveWatch = w
	}
	go saves.Run(ctx, saveWatch)
	catalog.Refresh(false)

	if err := srv.Notify("ready", schema.Ready{Version: version, Protocol: schema.ProtocolVersion}); err != nil {
		return fmt.Errorf("send ready: %w", err)
	}
	log.Info("started", "version", version, "protocol", schema.ProtocolVersion, "settings_dir", settingsDir)

	return srv.Serve(ctx, os.Stdin)
}

// connectionEvents sends connection changes to the frontend, and refreshes the library once a
// sign-in lands.
type connectionEvents struct {
	rpc.ConnectionEvents
	catalog *app.Catalog
}

func (e *connectionEvents) Changed(status app.ConnectionStatus) {
	e.ConnectionEvents.Changed(status)
	if status.State == app.Connected {
		e.catalog.Refresh(false)
	}
}

// userHome is the Deck user's home directory, where EmuDeck and ES-DE live. main.py passes Decky's
// DECKY_USER_HOME, which stays right even if the backend runs as another user.
func userHome(log *slog.Logger) string {
	if home := os.Getenv("DECKY_USER_HOME"); home != "" {
		return home
	}
	home, err := os.UserHomeDir()
	if err != nil {
		log.Error("no home directory; EmuDeck can't be found", "err", err)
	}
	return home
}
