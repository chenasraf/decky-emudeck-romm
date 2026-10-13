package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/romm"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/rpc"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/rpc/schema"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/settings"
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

	srv := rpc.NewServer(os.Stdout, log)
	conn := app.NewConnection(ctx, store, dial, identity, rpc.NewConnectionEvents(srv), log.With("component", "connection"))
	rpc.Routes(srv, version, conn)
	go conn.Watch(ctx, connectionCheckInterval)

	if err := srv.Notify("ready", schema.Ready{Version: version, Protocol: schema.ProtocolVersion}); err != nil {
		return fmt.Errorf("send ready: %w", err)
	}
	log.Info("started", "version", version, "protocol", schema.ProtocolVersion, "settings_dir", settingsDir)

	return srv.Serve(ctx, os.Stdin)
}
