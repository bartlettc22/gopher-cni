package cmd

import (
	"fmt"
	"log/slog"
)

// e outputs the error message (with args) to the standard logger and
// returns an error with the same message (minus args).
func e(log *slog.Logger, msg string, err error, args ...any) error {
	args = append(args, "error", err)
	log.Error(msg, args...)
	return fmt.Errorf(msg+": %w", err)
}
