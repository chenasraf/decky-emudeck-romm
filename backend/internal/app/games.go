package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// gamePoll is how often work waiting on a game looks again whether it's still running.
const gamePoll = 5 * time.Second

// RunningGame returns the emulator running a game, if one is.
type RunningGame func() (domain.Emulator, bool)

// GamePrefs keeps the Wait for games to close setting.
type GamePrefs interface {
	WaitForGames() bool
	SetWaitForGames(on bool) error
}

// Games holds work that reads and writes whole games, extracting and hashing, while an emulator
// runs: on a Deck that work can make a demanding game stutter.
type Games struct {
	running RunningGame
	prefs   GamePrefs
	poll    time.Duration
	log     *slog.Logger
}

func NewGames(running RunningGame, prefs GamePrefs, log *slog.Logger) *Games {
	return &Games{running: running, prefs: prefs, poll: gamePoll, log: log}
}

// WaitForGames says whether work waits for games to close.
func (g *Games) WaitForGames() bool { return g.prefs.WaitForGames() }

// SetWaitForGames turns waiting for games to close on or off. Work waiting already goes ahead once
// it's off.
func (g *Games) SetWaitForGames(on bool) error { return g.prefs.SetWaitForGames(on) }

// Wait returns once no emulator runs, the setting is off, or ctx ends. When it has to wait it calls
// waiting with true first and false before it returns. A nil Games never waits.
func (g *Games) Wait(ctx context.Context, waiting func(bool)) error {
	if g == nil {
		return nil
	}
	emu, ok := g.blocking()
	if !ok {
		return nil
	}
	g.log.Info("waiting for game to close", "emulator", emu.ID)
	waiting(true)
	defer waiting(false)
	t := time.NewTicker(g.poll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
		if _, ok := g.blocking(); !ok {
			g.log.Info("game closed", "emulator", emu.ID)
			return nil
		}
	}
}

// blocking returns the emulator work has to wait for, if any.
func (g *Games) blocking() (domain.Emulator, bool) {
	if !g.prefs.WaitForGames() {
		return domain.Emulator{}, false
	}
	return g.running()
}
