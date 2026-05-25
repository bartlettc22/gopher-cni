package cni

import (
	cnitypes "github.com/containernetworking/cni/pkg/types"
	cniv1 "github.com/containernetworking/cni/pkg/types/100"
)

const (

	// LabelPrefix is the prefix for all gopher-cni labels/annotations
	LabelPrefix = "gopher.cni/"

	// LabelEnabled is the label key that must be set to "true" for gopher-cni injection
	LabelEnabled = LabelPrefix + "enabled"

	// AnnotationWGConfSecret is the annotation key for the WireGuard config secret name
	AnnotationWGConfSecret = LabelPrefix + "wgconf-secret"

	// SecretKeyWGConf is the key in the Kubernetes secret that contains the wireguard configuration
	SecretKeyWGConf = "wg.conf"

	// AnnotationCNIMode is the annotation key for the CNI operation mode
	AnnotationCNIMode = LabelPrefix + "cni-mode"

	// AnnotationDNSTunneled is the annotation key for DNS tunneling configuration
	AnnotationDNSTunneled = LabelPrefix + "dns-tunneled"

	// AnnotationNATPMP is the annotation key for NAT-PMP port forwarding
	AnnotationNATPMP = LabelPrefix + "nat-pmp"

	// AnnotationSplitTunnelCIDRs is the annotation key for a comma-separated list of CIDRs
	// that should be routed via the default interface instead of the WireGuard tunnel.
	AnnotationSplitTunnelCIDRs = LabelPrefix + "split-tunnel-cidrs"

	// AnnotationSplitTunnelOverlap permits split-tunnel CIDRs that are less specific than
	// a WireGuard address or DNS server but still overlap. Set to "allow" to enable.
	AnnotationSplitTunnelOverlap = LabelPrefix + "split-tunnel-overlap"

	// CNIModePodOrigin is the pod-origin CNI mode
	CNIModePodOrigin = "pod-origin"

	// CNIModeHostOrigin is the host-origin CNI mode
	CNIModeHostOrigin = "host-origin"

	// DefaultCNIMode is the default CNI mode
	DefaultCNIMode = CNIModePodOrigin

	// PluginBinaryName is the name of the CNI plugin binary
	PluginBinaryName = "gopher-cni"

	// InterfaceName is the name of the WireGuard interface
	InterfaceName = "gcni0"
)

// PluginConfig is the configuration for the CNI plugin
// Used both for CNI config installation and for the CNI plugin itself
type PluginConfig struct {
	cnitypes.NetConf

	PrevResultV1   *cniv1.Result           `json:"-"`
	LogLevel       string                  `json:"log_level,omitempty"`
	LogUDSAddress  string                  `json:"log_uds_address,omitempty"`
	SkipRouteCIDRs []string                `json:"skip_route_cidrs,omitempty"`
	Kubernetes     *PluginKubernetesConfig `json:"kubernetes,omitempty"`
}

// PluginKubernetesConfig is the configuration for the Kubernetes integration
type PluginKubernetesConfig struct {
	Kubeconfig        string   `json:"kubeconfig,omitempty"`
	ExcludeNamespaces []string `json:"exclude_namespaces,omitempty"`
}
