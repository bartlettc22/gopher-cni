package install

import (
	"context"
	"os"
	"path/filepath"

	"github.com/bartlettc22/gopher-cni/pkg/cni"
	"github.com/bartlettc22/gopher-cni/pkg/logging"
	"github.com/bartlettc22/gopher-cni/pkg/utils"
)

var (
	log = logging.New()
)

type Installer struct {
	cfg                *Config
	saToken            string
	kubeconfigFilepath string
	cniConfigFilepath  string
}

// NewInstaller returns an instance of Installer with the given config
func NewInstaller(cfg *Config) *Installer {

	// Default to standard Kubernetes environment variables
	if cfg.K8sServiceHost == "" {
		cfg.K8sServiceHost = os.Getenv("KUBERNETES_SERVICE_HOST")
	}

	// Default to standard Kubernetes environment variables
	if cfg.K8sServicePort == "" {
		cfg.K8sServicePort = os.Getenv("KUBERNETES_SERVICE_PORT")
	}

	if cfg.K8sServiceProtocol == "" {
		cfg.K8sServiceProtocol = "https"
	}

	if cfg.KubeconfigFilename == "" {
		cfg.KubeconfigFilename = "ZZZ-gopher-cni-kubeconfig"
	}

	if cfg.KubeconfigMode == 0 {
		cfg.KubeconfigMode = 0o600
	}

	if cfg.MountedHostDir == "" {
		cfg.MountedHostDir = "/host"
	}

	if cfg.CNINetDir == "" {
		cfg.CNINetDir = filepath.Join(cfg.MountedHostDir, "/etc/cni/net.d")
	}

	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}

	return &Installer{
		cfg: cfg,
	}
}

func (in *Installer) Install(ctx context.Context) *logging.Error {

	var err error
	log = logging.New("component", "cni-installer")

	// Cleanup on context cancellation or error
	defer in.Uninstall()

	log.Info("installing CNI plugin", "config", in.cfg)

	if err := copyPluginBinary(
		filepath.Join(in.cfg.CNIBinSourceDir, cni.PluginBinaryName), filepath.Join(in.cfg.MountedHostDir, in.cfg.CNIBinTargetDir)); err != nil {
		return log.StructuredError("failed to copy CNI plugin binary", "error", err)
	}

	if in.saToken, err = readServiceAccountToken(); err != nil {
		return log.StructuredError("error creating Kubernetes service account token", "error", err)
	}

	if in.kubeconfigFilepath, err = createKubeconfigFile(in.cfg, in.saToken); err != nil {
		return log.StructuredError("error creating kubeconfig file", "error", err)
	}

	if in.cniConfigFilepath, err = createCNIConfigFile(in.cfg, in.kubeconfigFilepath); err != nil {
		return log.StructuredError("error creating CNI config file", "error", err)
	}

	// Wait for context cancellation
	// Uninstall will be triggered by the defer statement above
	<-ctx.Done()
	log.Info("context cancelled, cleaning up")

	return nil
}

// Uninstall uninstalls the CNI plugin and associated configs from the host node.
// Should be idempotent, and safe to call even if the plugin is not installed.
// As the uninstall should happen on termination, a best effort attempt is made to clean up any remaining resources
// and output any errors to the log.
func (in *Installer) Uninstall() {
	log.Info("uninstalling CNI plugin")

	if len(in.cniConfigFilepath) > 0 && utils.FileExists(in.cniConfigFilepath) {
		log.Info("removing CNI plugin config from CNI config file", "path", in.cniConfigFilepath)
		if err := uninstallCNIConfig(in.cniConfigFilepath); err != nil {
			log.Error("error removing CNI plugin config from CNI config file", "error", err)
		}
	}

	if len(in.kubeconfigFilepath) > 0 && utils.FileExists(in.kubeconfigFilepath) {
		log.Info("removing CNI kubeconfig file", "path", in.kubeconfigFilepath)
		if err := os.Remove(in.kubeconfigFilepath); err != nil {
			log.Error("error removing kubeconfig", "error", err)
		}
	}

	if cniBin := filepath.Join(in.cfg.MountedHostDir, in.cfg.CNIBinTargetDir, cni.PluginBinaryName); utils.FileExists(cniBin) {
		log.Info("removing binary", "path", cniBin)
		if err := os.Remove(cniBin); err != nil {
			log.Error("error removing binary", "error", err)
		}
	}

	log.Info("uninstalled")
}
