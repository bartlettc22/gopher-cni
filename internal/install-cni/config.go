package install

// Config defines the configuration options for CNI plugin installation.
// It contains all settings needed to install the CNI binary, generate
// kubeconfig, and configure the CNI plugin behavior on the host node.
type Config struct {
	// MountedHostDir is the base directory in the container which is mounted from the host machine.
	MountedHostDir string

	// CNINetDir is the location of the host CNI config files mounted into the container.
	// Typically /etc/cni/net.d where CNI configuration files are stored.
	CNINetDir string

	// LogLevel specifies the logging verbosity level (e.g., "debug", "info", "warn", "error").
	LogLevel string

	// K8sServiceHost is the hostname or IP address of the Kubernetes API server.
	// Typically populated from the KUBERNETES_SERVICE_HOST environment variable.
	K8sServiceHost string

	// K8sServicePort is the port number of the Kubernetes API server.
	// Typically populated from the KUBERNETES_SERVICE_PORT environment variable.
	K8sServicePort string

	// K8sServiceProtocol specifies the protocol to use when connecting to the Kubernetes API server.
	// Typically "https".
	K8sServiceProtocol string

	// K8sCAFile is the path to the Kubernetes cluster CA certificate file.
	// Defaults to the standard in-cluster location (/var/run/secrets/kubernetes.io/serviceaccount/ca.crt).
	K8sCAFile string

	// K8sSkipTLSVerify controls whether to skip TLS certificate verification when connecting
	// to the Kubernetes API server. Should only be true for testing/development.
	// Defaults to false.
	K8sSkipTLSVerify bool

	// KubeconfigFilename is the name of the kubeconfig file used by the CNI plugin
	// to authenticate with the Kubernetes API server.
	KubeconfigFilename string

	// KubeconfigMode specifies the file permissions to set when creating the kubeconfig file
	// (e.g., 0600 for read/write by owner only).
	KubeconfigMode int

	// KubeExcludeNamespaces is a list of namespace names to exclude from CNI plugin processing.
	// Pods in these namespaces will not have gopher-cni applied even if labeled.
	KubeExcludeNamespaces []string

	// SkipRouteCIDRs is a list of CIDR ranges that should not be routed through the WireGuard tunnel.
	// Traffic to these destinations will use the default network path instead of being tunneled.
	SkipRouteCIDRs []string

	// CNIBinSourceDir is the directory path containing the source CNI plugin binaries to be copied.
	// Typically the location in the container image where binaries are stored.
	CNIBinSourceDir string

	// CNIBinTargetDir is the destination directory path on the host where CNI plugin binaries
	// should be installed. Typically /opt/cni/bin on the host node.
	CNIBinTargetDir string

	// UDSSocketAddress is the Unix Domain Socket address that the CNI plugin will use
	// to send logs to the centralized logging service.
	UDSSocketAddress string
}
