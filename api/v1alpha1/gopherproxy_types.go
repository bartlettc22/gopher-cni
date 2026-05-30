package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=gproxy
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Pod",type=string,JSONPath=`.status.podName`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// GopherProxy creates a WireGuard proxy pod that forwards traffic from internal peer pods
// to an external VPN, allowing multiple pods to share a single VPN connection.
type GopherProxy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GopherProxySpec   `json:"spec,omitempty"`
	Status GopherProxyStatus `json:"status,omitempty"`
}

// GopherProxySpec defines the desired state of GopherProxy.
type GopherProxySpec struct {
	// VPNWGSecret is the name of the Secret in the same namespace containing the external
	// VPN WireGuard configuration (key: wg.conf).
	// +kubebuilder:validation:Required
	VPNWGSecret string `json:"vpnWGSecret"`

	// InternalAddress is the WireGuard address (with prefix length) assigned to the proxy's
	// internal interface. Peer IPs are allocated from this subnet.
	// Example: "10.100.0.1/24"
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^(\d{1,3}\.){3}\d{1,3}/\d{1,2}$`
	InternalAddress string `json:"internalAddress"`

	// InternalListenPort is the UDP port the proxy's internal WireGuard interface listens on.
	// +kubebuilder:default=51820
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	InternalListenPort int32 `json:"internalListenPort,omitempty"`

	// PeerSelector selects pods in the same namespace that should receive an auto-generated
	// WireGuard client config Secret to connect through this proxy.
	// +optional
	PeerSelector *metav1.LabelSelector `json:"peerSelector,omitempty"`

	// PeerAllowedIPs is the list of CIDRs that peer pods will route via this proxy.
	// Defaults to 0.0.0.0/0 (all traffic through the proxy/VPN).
	// +optional
	PeerAllowedIPs []string `json:"peerAllowedIPs,omitempty"`

	// Image is the container image for the proxy pod.
	// +optional
	Image string `json:"image,omitempty"`
}

// ProxyPhase describes the current lifecycle phase of the proxy.
type ProxyPhase string

const (
	ProxyPhasePending ProxyPhase = "Pending"
	ProxyPhaseRunning ProxyPhase = "Running"
	ProxyPhaseFailed  ProxyPhase = "Failed"
)

// GopherProxyStatus defines the observed state of GopherProxy.
type GopherProxyStatus struct {
	// Phase is the current lifecycle phase of the proxy.
	// +optional
	Phase ProxyPhase `json:"phase,omitempty"`

	// PodName is the name of the managed proxy pod.
	// +optional
	PodName string `json:"podName,omitempty"`

	// ServiceName is the name of the ClusterIP Service fronting the proxy's internal WireGuard port.
	// +optional
	ServiceName string `json:"serviceName,omitempty"`

	// InternalPublicKey is the WireGuard public key for the proxy's internal interface.
	// Peer pods use this value in their client configs.
	// +optional
	InternalPublicKey string `json:"internalPublicKey,omitempty"`

	// PeersSecretName is the name of the Secret holding the current WireGuard peer list
	// that the proxy pod hot-reloads.
	// +optional
	PeersSecretName string `json:"peersSecretName,omitempty"`

	// Conditions represent the latest observations of the proxy's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true

// GopherProxyList contains a list of GopherProxy.
type GopherProxyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GopherProxy `json:"items"`
}
