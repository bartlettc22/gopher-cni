package logging

import (
	"context"
	"log/slog"
)

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
