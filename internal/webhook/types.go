package webhook

import (
	"context"
	"fmt"

	"github.com/bartlettc22/gopher-cni/internal/cni"
	"github.com/bartlettc22/gopher-cni/internal/kubernetes"
	"github.com/bartlettc22/gopher-cni/internal/wireguard"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// InitContainerName is the name of the injected validator init container
	InitContainerName = "gopher-cni-validator"

	// SidecarContainerName is the name of the injected sidecar container
	SidecarContainerName = "gopher-cni-sidecar"

	// CoreDNSConfigContainerName is the name of the init container that writes the CoreDNS Corefile
	CoreDNSConfigContainerName = "gopher-cni-coredns-config"

	// CoreDNSContainerName is the name of the injected CoreDNS sidecar container
	CoreDNSContainerName = "gopher-cni-coredns"

	// CoreDNSVolumeName is the emptyDir volume shared between the CoreDNS config init container and sidecar
	CoreDNSVolumeName = "gopher-cni-coredns"
)

// PatchOperation represents a JSON patch operation
type PatchOperation struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
}

// WebhookConfig holds the webhook configuration
type WebhookConfig struct {
	// Image is the container image for injected gopher-cni containers
	Image string

	// CoreDNSImage is the container image for the injected CoreDNS sidecar
	CoreDNSImage string

	// TLSCertPath is the path to the TLS certificate
	TLSCertPath string

	// TLSKeyPath is the path to the TLS private key
	TLSKeyPath string

	// Port is the webhook server port
	Port int

	// KubeClient is the Kubernetes client for reading secrets and services
	KubeClient kubernetes.Client
}

// DefaultWebhookConfig returns the default webhook configuration
func DefaultWebhookConfig() *WebhookConfig {
	return &WebhookConfig{
		Image:        "gopher-cni:latest",
		CoreDNSImage: "",
		TLSCertPath:  "/etc/webhook/certs/tls.crt",
		TLSKeyPath:   "/etc/webhook/certs/tls.key",
		Port:         8443,
	}
}

// createInitContainer creates the CNI validator init container
func (c *WebhookConfig) createInitContainer() corev1.Container {
	return corev1.Container{
		Name:    InitContainerName,
		Image:   c.Image,
		Command: []string{"/gopher"},
		Args: []string{
			"init-validation",
		},
	}
}

// createCoreDNSConfigInitContainer creates an init container that writes the CoreDNS Corefile
// to a shared emptyDir volume at /etc/coredns/Corefile. The Corefile content is passed via
// the COREFILE environment variable so it can be generated per-pod at admission time.
func (c *WebhookConfig) createCoreDNSConfigInitContainer(corefile string) corev1.Container {
	return corev1.Container{
		Name:    CoreDNSConfigContainerName,
		Image:   c.Image,
		Command: []string{"/gopher"},
		Args:    []string{"write-coredns-config"},
		Env: []corev1.EnvVar{{
			Name:  "COREFILE",
			Value: corefile,
		}},
		VolumeMounts: []corev1.VolumeMount{{
			Name:      CoreDNSVolumeName,
			MountPath: "/etc/coredns",
		}},
	}
}

// createCoreDNSSidecarContainer creates the CoreDNS sidecar container for split-DNS.
func (c *WebhookConfig) createCoreDNSSidecarContainer() corev1.Container {
	return corev1.Container{
		Name:  CoreDNSContainerName,
		Image: c.CoreDNSImage,
		Args:  []string{"-conf", "/etc/coredns/Corefile"},
		Ports: []corev1.ContainerPort{{
			ContainerPort: 53,
			Protocol:      corev1.ProtocolUDP,
		}},
		VolumeMounts: []corev1.VolumeMount{{
			Name:      CoreDNSVolumeName,
			MountPath: "/etc/coredns",
			ReadOnly:  true,
		}},
	}
}

// fetchWGConfig retrieves and parses the WireGuard config from the named secret.
func (c *WebhookConfig) fetchWGConfig(ctx context.Context, namespace, secretName string) (*wireguard.Config, error) {
	if c.KubeClient == nil {
		return nil, fmt.Errorf("kubernetes client not configured")
	}
	data, err := c.KubeClient.FetchSecretKey(ctx, namespace, secretName, cni.SecretKeyWGConf)
	if err != nil {
		return nil, fmt.Errorf("failed to get wireguard config secret: %w", err)
	}
	cfg, err := wireguard.ParseConfig(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse wireguard config: %w", err)
	}
	return cfg, nil
}

// hasContainer checks if a container with the given name exists
func hasContainer(containers []corev1.Container, name string) bool {
	for _, c := range containers {
		if c.Name == name {
			return true
		}
	}
	return false
}

// shouldInject checks if injection is enabled for the pod
func shouldInject(pod *corev1.Pod) bool {
	if pod.Labels == nil {
		return false
	}
	return pod.Labels[cni.LabelEnabled] == "true"
}

// ValidationError represents a validation error
type ValidationError struct {
	Field   string
	Message string
}

// Error implements the error interface
func (v *ValidationError) Error() string {
	return v.Field + ": " + v.Message
}

// Status represents the webhook response status
type Status struct {
	metav1.Status
}
