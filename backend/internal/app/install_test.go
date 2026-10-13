package app

import (
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

type countingProber struct {
	mu    sync.Mutex
	now   *time.Time
	calls int
}

func (p *countingProber) Probe() domain.InstallState {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return domain.InstallState{CheckedAt: *p.now, EmuDeck: domain.EmuDeckState{Root: "/home/deck/Emulation"}}
}

func TestInstallStateThrottlesProbes(t *testing.T) {
	now := time.Unix(1000, 0)
	prober := &countingProber{now: &now}
	install := NewInstall(prober, slog.New(slog.NewTextHandler(io.Discard, nil)))
	install.now = func() time.Time { return now }

	install.State(false)
	now = now.Add(30 * time.Second)
	install.State(false)
	if prober.calls != 1 {
		t.Fatalf("probed %d times within a minute", prober.calls)
	}

	// Check again always looks at the disk.
	install.State(true)
	if prober.calls != 2 {
		t.Fatalf("refresh didn't probe: %d", prober.calls)
	}

	now = now.Add(probeInterval)
	if s := install.State(false); prober.calls != 3 || !s.EmuDeckFound() {
		t.Fatalf("a minute later: %d probes, %+v", prober.calls, s)
	}
}
