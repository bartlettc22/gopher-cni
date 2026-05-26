package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/bartlettc22/gopher-cni/internal/logging"
	"github.com/bartlettc22/gopher-cni/internal/sidecar"
	"github.com/bartlettc22/gopher-cni/internal/utils"
)

func main() {
	if len(os.Args) < 2 {
		logging.Fatal(fmt.Errorf("subcommand is required, can be one of: init-validation, write-coredns-config"))
	} else if len(os.Args) > 2 {
		logging.Fatal(fmt.Errorf("exactly 1 argument (subcommand) is required, have %d", len(os.Args)-1))
	}
	mode := os.Args[1]

	if err := logging.Configure(utils.GetEnv("LOG_LEVEL", "info"), utils.GetEnv("LOG_FORMAT", "text")); err != nil {
		logging.Fatal(fmt.Errorf("failed to configure logger: %w", err))
	}

	log := slog.With("component", "sidecar")
	log.Info("starting Gopher CNI", "mode", mode)

	switch mode {
	case "init-validation":
		sidecar.RunInitValidation()
	case "write-coredns-config":
		sidecar.RunWriteCoreDNSConfig()
	default:
		logging.Fatal(fmt.Errorf("unknown mode: %s (valid modes: init-validation, write-coredns-config)", mode))
	}
}
