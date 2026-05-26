package webhook

import (
	"github.com/bartlettc22/gopher-cni/internal/cni"
	"github.com/bartlettc22/gopher-cni/internal/kubernetes"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// InitContainerName is the name of the injected init container
	InitContainerName = "gopher-cni-validator"

<<<<<<< Updated upstream
	// SidecarContainerName is the name of the injected sidecar container
	SidecarContainerName = "gopher-cni-sidecar"
=======
	// CoreDNSConfigContainerName is the name of the init container that writes the CoreDNS Corefile
	CoreDNSConfigContainerName = "gopher-cni-coredns-config"

	// CoreDNSContainerName is the name of the injected CoreDNS sidecar container
	CoreDNSContainerName = "gopher-cni-coredns"

	// CoreDNSVolumeName is the emptyDir volume shared between the CoreDNS config init container and sidecar
	CoreDNSVolumeName = "gopher-cni-coredns"
>>>>>>> Stashed changes
)

// PatchOperation represents a JSON patch operation
type PatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value any `json:"value,omitempty"`
}

// WebhookConfig holds the webhook configuration
type WebhookConfig struct {
	// Image is the container image for init and sidecar containers
	Image string

	// TLSDisable disables TLS for the webhook server
	TLSDisable bool

	// TLSCertPath is the path to the TLS certificate
	TLSCertPath string

	// TLSKeyPath is the path to the TLS private key
	TLSKeyPath string

	// Port is the webhook server port
	Port int

	// KubeClient is the Kubernetes client for reading secrets
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

// createSidecarContainer creates the NAT-PMP sidecar container
func (c *WebhookConfig) createSidecarContainer() corev1.Container {
	return corev1.Container{
		Name:  SidecarContainerName,
		Image: c.Image,
		Args: []string{
			"sidecar",
		},
	}
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
