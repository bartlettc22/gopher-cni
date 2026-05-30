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

	// AnnotationSplitTunnelCIDRs is the annotation key for a comma-separated list of CIDRs
	// that should be routed via the default interface instead of the WireGuard tunnel.
	AnnotationSplitTunnelCIDRs = LabelPrefix + "split-tunnel-cidrs"

	// AnnotationSplitTunnelOverlap permits split-tunnel CIDRs that are less specific than
	// a WireGuard address or DNS server but still overlap. Set to "allow" to enable.
	AnnotationSplitTunnelOverlap = LabelPrefix + "split-tunnel-overlap"

	// AnnotationSplitTunnelDNSZones is a comma-separated list of DNS zone suffixes that
	// should be resolved via the cluster DNS server instead of the WireGuard tunnel DNS.
	// Requires a CoreDNS sidecar to be injected; the cluster DNS IP is discovered from
	// the kube-dns service in kube-system at admission time.
	AnnotationSplitTunnelDNSZones = LabelPrefix + "split-tunnel-dns-zones"

	// CNIModePodOrigin is the pod-origin CNI mode
	CNIModePodOrigin = "pod-origin"

	// CNIModeHostOrigin is the host-origin CNI mode
	CNIModeHostOrigin = "host-origin"

	// CNIModeProxy configures the pod as a WireGuard proxy: a wg-internal server
	// (for peer pods) bridged to a wg-vpn client (external VPN). ip_forward and
	// MASQUERADE are enabled so peer traffic is forwarded through the VPN.
	CNIModeProxy = "proxy"

	// DefaultCNIMode is the default CNI mode
	DefaultCNIMode = CNIModePodOrigin

	// AnnotationProxyInternalWGConfSecret is the annotation key for the Secret that holds
	// the proxy's internal WireGuard interface config (Interface section only; no peers).
	// Used only when cni-mode=proxy.
	AnnotationProxyInternalWGConfSecret = LabelPrefix + "proxy-internal-wgconf-secret"

	// AnnotationProxyPeersSecret is the annotation key for the Secret whose "peers.conf"
	// key contains the WireGuard [Peer] entries for the internal interface.
	// Used only when cni-mode=proxy.
	AnnotationProxyPeersSecret = LabelPrefix + "proxy-peers-secret"

	// SecretKeyPeersConf is the key in the peers Secret that holds the WireGuard peer list.
	SecretKeyPeersConf = "peers.conf"

	// SecretKeyPublicKey is the key in the internal WG Secret that holds the public key,
	// used by the controller when generating peer client configs.
	SecretKeyPublicKey = "publicKey"

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
