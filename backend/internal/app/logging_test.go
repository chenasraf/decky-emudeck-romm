package app

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

type memLoggingPrefs struct{ on bool }

func (m *memLoggingPrefs) DebugLogs() bool { return m.on }

func (m *memLoggingPrefs) SetDebugLogs(on bool) error {
	m.on = on
	return nil
}

func TestLogging(t *testing.T) {
	var out bytes.Buffer
	level := new(slog.LevelVar)
	log := slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: level}))
	prefs := &memLoggingPrefs{}
	l := NewLogging(level, prefs, log)

	log.Debug("hidden")
	if l.Debug() || strings.Contains(out.String(), "hidden") {
		t.Fatalf("debug lines logged before turning them on: %q", out.String())
	}
	if err := l.SetDebug(true); err != nil {
		t.Fatal(err)
	}
	log.Debug("shown")
	if !l.Debug() || !prefs.on || !strings.Contains(out.String(), "shown") {
		t.Fatalf("turning debug on: saved %v, log %q", prefs.on, out.String())
	}
	if err := l.SetDebug(false); err != nil {
		t.Fatal(err)
	}
	log.Debug("hidden again")
	if l.Debug() || prefs.on || strings.Contains(out.String(), "hidden again") {
		t.Fatalf("turning debug off: saved %v, log %q", prefs.on, out.String())
	}
}

// A setting saved on starts the backend at debug.
func TestLoggingStartsFromSetting(t *testing.T) {
	level := new(slog.LevelVar)
	if !NewLogging(level, &memLoggingPrefs{on: true}, slog.New(slog.DiscardHandler)).Debug() {
		t.Fatal("saved setting ignored")
	}
}
