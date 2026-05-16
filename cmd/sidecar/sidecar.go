package sidecar

import "log/slog"

func Run() {
	log := slog.With("component", "sidecar")
	log.Info("starting sidecar")
	log.Warn("not implemented; waiting forever")
	done := make(chan struct{})
	<-done
}
