package app

import "log/slog"

// LoggingPrefs keeps the debug logging setting.
type LoggingPrefs interface {
	DebugLogs() bool
	SetDebugLogs(on bool) error
}

// Logging switches the backend's log between info and debug while it runs, so a user can turn on
// debug lines, reproduce a problem, and send a log that shows it.
type Logging struct {
	level *slog.LevelVar
	prefs LoggingPrefs
	log   *slog.Logger
}

// NewLogging lowers level to debug when the setting is on. A level already at debug, as
// BACKEND_LOG_LEVEL sets it, stays.
func NewLogging(level *slog.LevelVar, prefs LoggingPrefs, log *slog.Logger) *Logging {
	if prefs.DebugLogs() && level.Level() > slog.LevelDebug {
		level.Set(slog.LevelDebug)
	}
	return &Logging{level: level, prefs: prefs, log: log}
}

// Debug says whether debug lines are logged.
func (l *Logging) Debug() bool { return l.level.Level() <= slog.LevelDebug }

// SetDebug turns debug lines on or off, from the next line on.
func (l *Logging) SetDebug(on bool) error {
	if err := l.prefs.SetDebugLogs(on); err != nil {
		return err
	}
	// Logged at info on both sides of the switch, so the log shows where debug lines start and end.
	if on {
		l.level.Set(slog.LevelDebug)
		l.log.Info("debug logging on")
	} else {
		l.log.Info("debug logging off")
		l.level.Set(slog.LevelInfo)
	}
	return nil
}
