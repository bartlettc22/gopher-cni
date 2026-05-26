package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bartlettc22/gopher-cni/internal/kubernetes"
	"github.com/bartlettc22/gopher-cni/internal/logging"
	"github.com/bartlettc22/gopher-cni/internal/utils"
	"github.com/bartlettc22/gopher-cni/internal/webhook"
	"golang.org/x/sync/errgroup"
)

var log = slog.Default()

type config struct {
	WebhookSidecarImage string
	WebhookCoreDNSImage string
	WebhookPort         int
	WebhookTLSCertPath  string
	WebhookTLSKeyPath   string
	LogLevel            string
	LogFormat           string
}

func loadConfig() (*config, error) {
	c := &config{
		WebhookSidecarImage: utils.GetEnv("WEBHOOK_SIDECAR_IMAGE", ""),
		WebhookCoreDNSImage: utils.GetEnv("WEBHOOK_COREDNS_IMAGE", ""),
		WebhookPort:         utils.GetEnv("WEBHOOK_PORT", 8443),
		WebhookTLSCertPath:  utils.GetEnv("WEBHOOK_TLS_CERT_PATH", "/etc/webhook/certs/tls.crt"),
		WebhookTLSKeyPath:   utils.GetEnv("WEBHOOK_TLS_KEY_PATH", "/etc/webhook/certs/tls.key"),
		LogLevel:            utils.GetEnv("LOG_LEVEL", "info"),
		LogFormat:           utils.GetEnv("LOG_FORMAT", "text"),
	}

	if c.WebhookPort <= 0 || c.WebhookPort > 65535 {
		return nil, fmt.Errorf("WEBHOOK_PORT must be between 1 and 65535")
	}
	if c.WebhookTLSCertPath == "" {
		return nil, fmt.Errorf("WEBHOOK_TLS_CERT_PATH is required")
	}
	if c.WebhookTLSKeyPath == "" {
		return nil, fmt.Errorf("WEBHOOK_TLS_KEY_PATH is required")
	}
	if c.WebhookSidecarImage == "" {
		return nil, fmt.Errorf("WEBHOOK_SIDECAR_IMAGE is required")
	}
	if c.WebhookCoreDNSImage == "" {
		return nil, fmt.Errorf("WEBHOOK_COREDNS_IMAGE is required")
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
	log = slog.With("component", "webhook")
	log.Info("starting gopher-cni webhook server")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		kubeClient, err := kubernetes.NewInClusterClient()
		if err != nil {
			log.Error("failed to create kubernetes client", "error", err)
			return err
		}

		server := webhook.NewServer(&webhook.WebhookConfig{
			Image:        cfg.WebhookSidecarImage,
			CoreDNSImage: cfg.WebhookCoreDNSImage,
			Port:         cfg.WebhookPort,
			TLSCertPath:  cfg.WebhookTLSCertPath,
			TLSKeyPath:   cfg.WebhookTLSKeyPath,
			KubeClient:   kubeClient,
		})
		if lErr := server.Run(ctx); lErr != nil {
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
