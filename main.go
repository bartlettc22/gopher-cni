package main

import (
	"fmt"
	"log/slog"

	"github.com/bartlettc22/gopher-cni/cmd/daemon"
	initvalidation "github.com/bartlettc22/gopher-cni/cmd/init-validation"
	"github.com/bartlettc22/gopher-cni/cmd/sidecar"
	"github.com/bartlettc22/gopher-cni/pkg/logging"
)

func main() {
	config := LoadConfig()
	err := configureDefaultLogger(config)
	if err != nil {
		logging.Fatal(fmt.Errorf("failed to configure default logger: %w", err))
	}

	log := slog.With("component", "main")
	log.Info("starting Gopher CNI", "mode", config.Mode, "log_level", config.LogLevel, "log_format", config.LogFormat)

	// Run the appropriate mode
	// Exiting and error handling should handled within the corresponding Run function
	switch config.Mode {
	case "daemon":
		daemon.Run()
	case "init-validation":
		initvalidation.Run()
	case "sidecar":
		sidecar.Run()
	default:
		logging.Fatal(fmt.Errorf("unknown mode: %s (valid modes: daemon, init-validation, sidecar)", config.Mode))
	}
}
