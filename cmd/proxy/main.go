// cmd/proxy is the entrypoint for the gopher-proxy pod. All WireGuard and
// routing setup is handled by the CNI plugin at pod-creation time; this binary
// just serves a liveness probe so Kubernetes can track pod health.
package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/bartlettc22/gopher-cni/internal/logging"
	"github.com/bartlettc22/gopher-cni/internal/utils"
)

func main() {
	logLevel := utils.GetEnv("LOG_LEVEL", "info")
	logFormat := utils.GetEnv("LOG_FORMAT", "text")
	if err := logging.Configure(logLevel, logFormat); err != nil {
		fmt.Fprintf(os.Stderr, "failed to configure logger: %v\n", err)
		os.Exit(1)
	}

	log := slog.With("component", "proxy")
	log.Info("starting gopher-proxy health server", "addr", ":8080")

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Error("health server exited", "err", err)
		os.Exit(1)
	}
}
