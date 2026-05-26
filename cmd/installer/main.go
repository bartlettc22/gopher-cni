package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	install "github.com/bartlettc22/gopher-cni/internal/installer"
	"github.com/bartlettc22/gopher-cni/internal/logging"
	"github.com/bartlettc22/gopher-cni/internal/udslog"
	"github.com/bartlettc22/gopher-cni/internal/utils"
	"golang.org/x/sync/errgroup"
)

var log = slog.Default()

type config struct {
	MountedHostDir   string
	CNINetDir        string
	CNIBinSourceDir  string
	CNIBinTargetDir  string
	UDSSocketAddress string
	LogLevel         string
	LogFormat        string
}

func loadConfig() (*config, error) {
	c := &config{
		MountedHostDir:   utils.GetEnv("MOUNTED_HOST_DIR", "/host"),
		CNINetDir:        utils.GetEnv("CNI_NET_DIR", "/etc/cni/net.d"),
		CNIBinSourceDir:  utils.GetEnv("CNI_BIN_SOURCE_DIR", "/cni"),
		CNIBinTargetDir:  utils.GetEnv("CNI_BIN_TARGET_DIR", "/opt/cni/bin"),
		UDSSocketAddress: utils.GetEnv("UDS_SOCKET_ADDRESS", "/var/run/gopher-cni/log.sock"),
		LogLevel:         utils.GetEnv("LOG_LEVEL", "info"),
		LogFormat:        utils.GetEnv("LOG_FORMAT", "text"),
	}

	if c.CNIBinSourceDir == "" {
		return nil, fmt.Errorf("CNI_BIN_SOURCE_DIR is required")
	}
	if c.CNIBinTargetDir == "" {
		return nil, fmt.Errorf("CNI_BIN_TARGET_DIR is required")
	}
	if c.UDSSocketAddress == "" {
		return nil, fmt.Errorf("UDS_SOCKET_ADDRESS is required")
	}
	return c, nil
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		logging.Fatal(fmt.Errorf("failed to load configuration"), "error", err)
	}

	if err := logging.Configure(cfg.LogLevel, cfg.LogFormat); err != nil {
		logging.Fatal(fmt.Errorf("failed to configure logger: %w", err))
	}
	log = slog.With("component", "installer")
	log.Info("starting gopher-cni installer")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		udslogger := udslog.NewUDSLogger()
		if lErr := udslogger.StartUDSLogServer(ctx, filepath.Join(cfg.MountedHostDir, cfg.UDSSocketAddress)); lErr != nil {
			lErr.Print()
			return lErr.Error()
		}
		return nil
	})

	g.Go(func() error {
		installer := install.NewInstaller(&install.Config{
			MountedHostDir:   cfg.MountedHostDir,
			CNINetDir:        cfg.CNINetDir,
			CNIBinSourceDir:  cfg.CNIBinSourceDir,
			CNIBinTargetDir:  cfg.CNIBinTargetDir,
			UDSSocketAddress: cfg.UDSSocketAddress,
			LogLevel:         logging.New().Level(),
		})
		if lErr := installer.Install(ctx); lErr != nil {
			lErr.Print()
			return lErr.Error()
		}
		return nil
	})

	shutdownInitiated := make(chan struct{})
	g.Go(func() error {
		defer close(shutdownInitiated)
		<-ctx.Done()
		log.Info("received shutdown signal, initiating shutdown")
		return nil
	})

	shutdownComplete := make(chan struct{})
	go func() {
		defer close(shutdownComplete)
		if err := g.Wait(); err != nil {
			logging.Fatal(fmt.Errorf("exiting due to error"), "error", err)
		}
	}()

	<-shutdownInitiated

	gracePeriod := time.Second * 15
	select {
	case <-shutdownComplete:
	case <-time.After(gracePeriod):
		logging.Fatal(fmt.Errorf("timeout waiting for shutdown to complete"), "grace_period", gracePeriod)
	}

	log.Info("shutdown complete")
}

