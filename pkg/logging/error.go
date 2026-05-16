package logging

import (
	"fmt"
	"log/slog"
	"os"
)

type Error struct {
	log  *slog.Logger
	msg  string
	args []any
}

func (e *Error) Print() {
	e.log.Error(e.msg, e.args...)
}

func (e *Error) Error() error {
	return fmt.Errorf(e.msg, e.args...)
}

func Fatal(err error, args ...any) {
	if err != nil {
		args = append(args, "error", err)
		slog.Error("fatal error", args...)
		os.Exit(1)
	}
}
