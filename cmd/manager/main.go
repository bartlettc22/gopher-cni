// cmd/manager is the combined entry point that runs the admission webhook server
// and the GopherProxy controller in a single process.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"golang.org/x/sync/errgroup"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	gopherv1alpha1 "github.com/bartlettc22/gopher-cni/api/v1alpha1"
	"github.com/bartlettc22/gopher-cni/internal/controller"
	"github.com/bartlettc22/gopher-cni/internal/kubernetes"
	"github.com/bartlettc22/gopher-cni/internal/logging"
	"github.com/bartlettc22/gopher-cni/internal/utils"
	"github.com/bartlettc22/gopher-cni/internal/webhook"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(gopherv1alpha1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
}

var log = slog.Default()

type config struct {
	// Webhook
	WebhookSidecarImage string
	WebhookCoreDNSImage string
	WebhookPort         int
	WebhookTLSCertPath  string
	WebhookTLSKeyPath   string
	// Controller
	ProxyImage     string
	ProbeAddr      string
	LeaderElection bool
	// Shared
	LogLevel  string
	LogFormat string
}

func loadConfig() (*config, error) {
	c := &config{
		WebhookSidecarImage: utils.GetEnv("WEBHOOK_SIDECAR_IMAGE", ""),
		WebhookCoreDNSImage: utils.GetEnv("WEBHOOK_COREDNS_IMAGE", ""),
		WebhookPort:         utils.GetEnv("WEBHOOK_PORT", 8443),
		WebhookTLSCertPath:  utils.GetEnv("WEBHOOK_TLS_CERT_PATH", "/etc/webhook/certs/tls.crt"),
		WebhookTLSKeyPath:   utils.GetEnv("WEBHOOK_TLS_KEY_PATH", "/etc/webhook/certs/tls.key"),
		ProxyImage:          utils.GetEnv("PROXY_IMAGE", ""),
		ProbeAddr:           utils.GetEnv("PROBE_ADDR", ":8081"),
		LeaderElection:      utils.GetEnv("LEADER_ELECTION", false),
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
	if c.ProxyImage == "" {
		return nil, fmt.Errorf("PROXY_IMAGE is required")
	}
	return c, nil
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		logging.Fatal(fmt.Errorf("failed to load configuration: %w", err))
	}
	if err := logging.Configure(cfg.LogLevel, cfg.LogFormat); err != nil {
		logging.Fatal(fmt.Errorf("failed to configure logger: %w", err))
	}
	log = slog.With("component", "manager")
	log.Info("starting gopher-cni manager")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	g, ctx := errgroup.WithContext(ctx)

	// Webhook server (existing admission webhook, unchanged).
	g.Go(func() error {
		kubeClient, err := kubernetes.NewInClusterClient()
		if err != nil {
			return fmt.Errorf("creating kubernetes client for webhook: %w", err)
		}
		srv := webhook.NewServer(&webhook.WebhookConfig{
			Image:        cfg.WebhookSidecarImage,
			CoreDNSImage: cfg.WebhookCoreDNSImage,
			Port:         cfg.WebhookPort,
			TLSCertPath:  cfg.WebhookTLSCertPath,
			TLSKeyPath:   cfg.WebhookTLSKeyPath,
			KubeClient:   kubeClient,
		})
		if lErr := srv.Run(ctx); lErr != nil {
			lErr.Print()
			return lErr.Error()
		}
		return nil
	})

	// Controller manager (GopherProxy reconciler).
	g.Go(func() error {
		opts := zap.Options{Development: cfg.LogLevel == "debug"}
		ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

		mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
			Scheme:                 scheme,
			HealthProbeBindAddress: cfg.ProbeAddr,
			LeaderElection:         cfg.LeaderElection,
			LeaderElectionID:       "gopher-cni-controller",
		})
		if err != nil {
			return fmt.Errorf("creating controller manager: %w", err)
		}
		if err := (&controller.GopherProxyReconciler{
			Client:     mgr.GetClient(),
			Scheme:     mgr.GetScheme(),
			ProxyImage: cfg.ProxyImage,
		}).SetupWithManager(mgr); err != nil {
			return fmt.Errorf("setting up GopherProxy controller: %w", err)
		}
		if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
			return fmt.Errorf("adding health check: %w", err)
		}
		if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
			return fmt.Errorf("adding ready check: %w", err)
		}
		return mgr.Start(ctx)
	})

	// Graceful shutdown: wait for the signal, then allow up to 15 s for clean exit.
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
			logging.Fatal(fmt.Errorf("exiting due to error: %w", err))
		}
	}()

	<-shutdownInitiated

	gracePeriod := 15 * time.Second
	select {
	case <-shutdownComplete:
	case <-time.After(gracePeriod):
		logging.Fatal(fmt.Errorf("timeout waiting for shutdown to complete"), "grace_period", gracePeriod)
	}

	log.Info("shutdown complete")
}
