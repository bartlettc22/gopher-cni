package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bartlettc22/gopher-cni/internal/install-cni"
	"github.com/bartlettc22/gopher-cni/internal/kubernetes"
	"github.com/bartlettc22/gopher-cni/internal/logging"
	"github.com/bartlettc22/gopher-cni/internal/udslog"
	"github.com/bartlettc22/gopher-cni/internal/webhook"
	"golang.org/x/sync/errgroup"
)

var (
	log = slog.Default()
)

// Run starts the daemon mode with the following
func Run() {

	// Set package-level logger
	// This is done here to ensure we get any changes to the default logger that were made
	// before this function was called.
	log = slog.With("component", "daemon")

	config, err := LoadConfig()
	if err != nil {
		logging.Fatal(fmt.Errorf("failed to load configuration"), "error", err)
	}

	// Set up context handling
	ctx := createSignalHandling()
	g, ctx := errgroup.WithContext(ctx)

	// Start UDS logging server
	g.Go(func() error {
		udslogger := udslog.NewUDSLogger()
		if lErr := udslogger.StartUDSLogServer(ctx, filepath.Join(config.MountedHostDir, config.UDSSocketAddress)); lErr != nil {
			lErr.Print()
			return lErr.Error()
		}
		return nil
	})

	// Start CNI plugin installation and watcher for uninstall
	g.Go(func() error {
		installer := install.NewInstaller(&install.Config{
			MountedHostDir:   config.MountedHostDir,
			CNINetDir:        config.CNINetDir,
			CNIBinSourceDir:  config.CNIBinSourceDir,
			CNIBinTargetDir:  config.CNIBinTargetDir,
			UDSSocketAddress: config.UDSSocketAddress,

			// Sets the log level to the current slog level
			LogLevel: logging.New().Level(),
		})
		if lErr := installer.Install(ctx); lErr != nil {
			lErr.Print()
			return lErr.Error()
		}
		return nil
	})

	// Start mutating/validating webhook server
	g.Go(func() error {
		// Create Kubernetes client for the webhook to read secrets
		kubeClient, err := kubernetes.NewInClusterClient()
		if err != nil {
			log.Error("failed to create kubernetes client for webhook", "error", err)
			return err
		}

		webhookConfig := &webhook.WebhookConfig{
			Image:        config.WebhookImage,
			CoreDNSImage: config.WebhookCoreDNSImage,
			Port:         config.WebhookPort,
			TLSCertPath:  config.WebhookTLSCertPath,
			TLSKeyPath:   config.WebhookTLSKeyPath,
			KubeClient:   kubeClient,
		}

		server := webhook.NewServer(webhookConfig)
		if lErr := server.Run(ctx); lErr != nil {
			lErr.Print()
			return lErr.Error()
		}
		return nil
	})

	// Watch for context cancellation so we can signal that a shutdown is in progress
	shutdownInitiated := make(chan struct{})
	g.Go(func() error {
		defer close(shutdownInitiated)
		<-ctx.Done()
		log.Info("received shutdown signal, initiating shutdown")
		return nil

	})

	// This will asyncronously wait for all components to shutdown
	// and then mark the shutdown as complete, exiting with an error if any component failed
	shutdownComplete := make(chan struct{})
	go func() {
		defer close(shutdownComplete)
		if err := g.Wait(); err != nil {
			logging.Fatal(fmt.Errorf("exiting due to error"), "error", err)
		}
	}()

	// Block here until shutdown is initiated, either by signal or error
	<-shutdownInitiated

	// Wait for shutdown to complete (with a grace period)
	gracePeriod := time.Second * 15
	select {
	case <-shutdownComplete:
	case <-time.After(gracePeriod):
		logging.Fatal(fmt.Errorf("timeout waiting for shutdown to complete"), "grace_period", gracePeriod)
	}

	log.Info("shutdown complete")
}

// createSignalHandling creates context that cancels on termination signals
func createSignalHandling() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func(sigChan chan os.Signal, cancel context.CancelFunc) {
		sig := <-sigChan
		signal.Stop(sigChan)
		log.Info("exit signal received", "signal", sig.String())
		cancel()
	}(sigChan, cancel)
	return ctx
}
