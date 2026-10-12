package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/rpc"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/rpc/schema"
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

	srv := rpc.NewServer(os.Stdout, log)
	rpc.Routes(srv, version)

	if err := srv.Notify("ready", schema.Ready{Version: version, Protocol: schema.ProtocolVersion}); err != nil {
		return fmt.Errorf("send ready: %w", err)
	}
	log.Info("started", "version", version, "protocol", schema.ProtocolVersion)

	return srv.Serve(ctx, os.Stdin)
}
