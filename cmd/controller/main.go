package main

import (
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	gopherv1alpha1 "github.com/bartlettc22/gopher-cni/api/v1alpha1"
	"github.com/bartlettc22/gopher-cni/internal/controller"
	"github.com/bartlettc22/gopher-cni/internal/logging"
	"github.com/bartlettc22/gopher-cni/internal/utils"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(gopherv1alpha1.AddToScheme(scheme))
	// Register corev1 separately in case clientgoscheme doesn't pull it in context.
	utilruntime.Must(corev1.AddToScheme(scheme))
}

type config struct {
	MetricsAddr    string
	ProbeAddr      string
	LeaderElection bool
	ProxyImage     string
	LogLevel       string
	LogFormat      string
}

func loadConfig() (*config, error) {
	c := &config{
		MetricsAddr:    utils.GetEnv("METRICS_ADDR", ":8080"),
		ProbeAddr:      utils.GetEnv("PROBE_ADDR", ":8081"),
		LeaderElection: utils.GetEnv("LEADER_ELECTION", false),
		ProxyImage:     utils.GetEnv("PROXY_IMAGE", ""),
		LogLevel:       utils.GetEnv("LOG_LEVEL", "info"),
		LogFormat:      utils.GetEnv("LOG_FORMAT", "text"),
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

	opts := zap.Options{Development: cfg.LogLevel == "debug"}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: cfg.ProbeAddr,
		LeaderElection:         cfg.LeaderElection,
		LeaderElectionID:       "gopher-cni-controller",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to create manager: %v\n", err)
		os.Exit(1)
	}

	if err := (&controller.GopherProxyReconciler{
		Client:     mgr.GetClient(),
		Scheme:     mgr.GetScheme(),
		ProxyImage: cfg.ProxyImage,
	}).SetupWithManager(mgr); err != nil {
		fmt.Fprintf(os.Stderr, "unable to create GopherProxy controller: %v\n", err)
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		fmt.Fprintf(os.Stderr, "unable to set up health check: %v\n", err)
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		fmt.Fprintf(os.Stderr, "unable to set up ready check: %v\n", err)
		os.Exit(1)
	}

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		fmt.Fprintf(os.Stderr, "problem running manager: %v\n", err)
		os.Exit(1)
	}
}
