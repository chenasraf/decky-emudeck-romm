package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/romm"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/rpc"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/rpc/schema"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/settings"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/app"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	// stdout carries only the protocol; logs go to stderr, which main.py forwards to decky.logger.
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if err := run(log); err != nil {
		log.Error("exiting", "err", err)
		os.Exit(1)
	}
	log.Info("stopped")
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Decky exports the plugin's settings dir to the plugin process, which passes it down.
	store := settings.New(os.Getenv("DECKY_PLUGIN_SETTINGS_DIR"))
	userAgent := "decky-emudeck-romm/" + version
	dial := func(server domain.Server, token string) app.RomM { return romm.New(server, token, userAgent) }

	srv := rpc.NewServer(os.Stdout, log)
	rpc.Routes(srv, version, app.NewConnection(store, dial))

	if err := srv.Notify("ready", schema.Ready{Version: version, Protocol: schema.ProtocolVersion}); err != nil {
		return fmt.Errorf("send ready: %w", err)
	}
	log.Info("started", "version", version, "protocol", schema.ProtocolVersion)

	return srv.Serve(ctx, os.Stdin)
}
