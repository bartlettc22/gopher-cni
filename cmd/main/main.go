package main

import (
	"fmt"
	"log/slog"

	"github.com/bartlettc22/gopher-cni/cmd/daemon"
	initvalidation "github.com/bartlettc22/gopher-cni/cmd/init-validation"
	writecorednsconfig "github.com/bartlettc22/gopher-cni/cmd/write-coredns-config"
	"github.com/bartlettc22/gopher-cni/internal/logging"
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
	case "write-coredns-config":
		writecorednsconfig.Run()
	default:
		logging.Fatal(fmt.Errorf("unknown mode: %s (valid modes: daemon, init-validation, write-coredns-config)", config.Mode))
	}
}
