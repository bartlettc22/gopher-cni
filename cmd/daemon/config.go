package daemon

import (
	"fmt"

	"github.com/bartlettc22/gopher-cni/internal/utils"
	"github.com/bartlettc22/gopher-cni/internal/webhook"
)

// Config struct defines the CNI installation options
type Config struct {
	MountedHostDir string

	// Host of the Kubernetes API server
	K8sServiceHost string

	// Port of the Kubernetes API server
	K8sServicePort string

	// Protocol of the Kubernetes API server
	K8sServiceProtocol string

	CNINetDir string

	// // Directory from where the CNI binaries should be copied
	CNIBinSourceDir string

	// // Directoriy into which to copy the CNI binaries
	CNIBinTargetDir string

	UDSSocketAddress string

	WebhookImage        string
	WebhookCoreDNSImage string
	WebhookPort         int
	WebhookTLSCertPath  string
	WebhookTLSKeyPath   string
}

// LoadConfig parses command-line flags and environment variables to build configuration
func LoadConfig() (*Config, error) {

	config := &Config{
		MountedHostDir:      utils.GetEnv("MOUNTED_HOST_DIR", "/host"),
		CNINetDir:           utils.GetEnv("CNI_NET_DIR", "/etc/cni/net.d"),
		CNIBinSourceDir:     utils.GetEnv("CNI_BIN_SOURCE_DIR", "/cni"),
		CNIBinTargetDir:     utils.GetEnv("CNI_BIN_TARGET_DIR", "/opt/cni/bin"),
		UDSSocketAddress:    utils.GetEnv("UDS_SOCKET_ADDRESS", "/var/run/gopher-cni/log.sock"),
		WebhookImage:        utils.GetEnv("WEBHOOK_IMAGE", ""),
		WebhookCoreDNSImage: utils.GetEnv("WEBHOOK_COREDNS_IMAGE", webhook.DefaultCoreDNSImage),
		WebhookPort:         utils.GetEnv("WEBHOOK_PORT", 8443),
		WebhookTLSCertPath:  utils.GetEnv("WEBHOOK_TLS_CERT_PATH", "/etc/webhook/certs/tls.crt"),
		WebhookTLSKeyPath:   utils.GetEnv("WEBHOOK_TLS_KEY_PATH", "/etc/webhook/certs/tls.key"),
	}

	return config, config.Validate()
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.CNIBinSourceDir == "" {
		return fmt.Errorf("cni-bin-source-dir is required")
	}
	if c.CNIBinTargetDir == "" {
		return fmt.Errorf("cni-bin-target-dir is required")
	}
	if c.UDSSocketAddress == "" {
		return fmt.Errorf("uds-socket-address is required")
	}
	if c.WebhookPort <= 0 || c.WebhookPort > 65535 {
		return fmt.Errorf("webhook-port must be between 1 and 65535")
	}
	if c.WebhookTLSCertPath == "" {
		return fmt.Errorf("webhook-tls-cert is required")
	}
	if c.WebhookTLSKeyPath == "" {
		return fmt.Errorf("webhook-tls-key is required")
	}
	if c.WebhookImage == "" {
		return fmt.Errorf("webhook-image is required")
	}
	return nil
}
