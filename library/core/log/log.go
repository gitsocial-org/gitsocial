// log.go - Structured logging with configurable levels and output modes
package log

import (
	"io"
	"log/slog"
	"os"
	"sync"
)

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

type Mode int

const (
	ModeText Mode = iota
	ModeJSON
	ModeSilent
)

type Config struct {
	Level  Level
	Mode   Mode
	Output io.Writer
}

var (
	defaultLogger *slog.Logger
	mu            sync.RWMutex
)

func init() {
	defaultLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}

// Init configures the global logger with the given settings.
func Init(cfg Config) {
	mu.Lock()
	defer mu.Unlock()

	if cfg.Output == nil {
		cfg.Output = os.Stderr
	}

	if cfg.Mode == ModeSilent {
		defaultLogger = slog.New(slog.NewTextHandler(io.Discard, nil))
		return
	}

	level := toSlogLevel(cfg.Level)
	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if cfg.Mode == ModeJSON {
		handler = slog.NewJSONHandler(cfg.Output, opts)
	} else {
		handler = slog.NewTextHandler(cfg.Output, opts)
	}

	defaultLogger = slog.New(handler)
}

// toSlogLevel converts our Level to slog.Level.
func toSlogLevel(l Level) slog.Level {
	switch l {
	case LevelDebug:
		return slog.LevelDebug
	case LevelInfo:
		return slog.LevelInfo
	case LevelWarn:
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// ParseLevel parses a level string to Level constant.
func ParseLevel(s string) Level {
	switch s {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// logger returns the current global logger.
func logger() *slog.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return defaultLogger
}

// Debug logs a debug message.
func Debug(msg string, args ...any) {
	logger().Debug(msg, args...)
}

// Info logs an info message.
func Info(msg string, args ...any) {
	logger().Info(msg, args...)
}

// Warn logs a warning message.
func Warn(msg string, args ...any) {
	logger().Warn(msg, args...)
}

// Error logs an error message.
func Error(msg string, args ...any) {
	logger().Error(msg, args...)
}

type Logger struct {
	l *slog.Logger
}

// With returns a logger with additional context fields.
func With(args ...any) *Logger {
	return &Logger{l: logger().With(args...)}
}

func (l *Logger) Debug(msg string, args ...any) {
	l.l.Debug(msg, args...)
}

func (l *Logger) Info(msg string, args ...any) {
	l.l.Info(msg, args...)
}

func (l *Logger) Warn(msg string, args ...any) {
	l.l.Warn(msg, args...)
}

func (l *Logger) Error(msg string, args ...any) {
	l.l.Error(msg, args...)
}
