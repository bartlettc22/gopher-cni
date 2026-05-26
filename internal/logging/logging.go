package logging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
)

// Configure sets the default slog logger with the given level and format.
func Configure(level, format string) error {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "info":
		lvl = slog.LevelInfo
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		return fmt.Errorf("unknown log level: %s (valid: debug, info, warn, error)", level)
	}

	var handler slog.Handler
	switch format {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	case "text":
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	default:
		return fmt.Errorf("unknown log format: %s (valid: text, json)", format)
	}

	slog.SetDefault(slog.New(handler))
	return nil
}

type Logger struct {
	log *slog.Logger
}

func New(args ...any) *Logger {
	l := &Logger{
		log: slog.With(args...),
	}
	return l
}

func (l *Logger) Debug(msg string, args ...any) {
	l.log.Debug(msg, args...)
}

func (l *Logger) Info(msg string, args ...any) {
	l.log.Info(msg, args...)
}

func (l *Logger) Warn(msg string, args ...any) {
	l.log.Warn(msg, args...)
}

func (l *Logger) Error(msg string, args ...any) {
	l.log.Error(msg, args...)
}

func (l *Logger) With(args ...any) *Logger {
	return New(l.log.With(args...))
}

func (l *Logger) Level() string {
	if l.log.Enabled(context.Background(), slog.LevelDebug) {
		return "debug"
	}
	if l.log.Enabled(context.Background(), slog.LevelInfo) {
		return "info"
	}
	if l.log.Enabled(context.Background(), slog.LevelWarn) {
		return "warn"
	}
	return "error"
}

func (l *Logger) StructuredError(msg string, args ...any) *Error {
	return &Error{
		log:  l.log,
		msg:  msg,
		args: args,
	}
}
