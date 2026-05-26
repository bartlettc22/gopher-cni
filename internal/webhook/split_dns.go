package webhook

import (
	"context"
	"fmt"
	"strings"

	"github.com/bartlettc22/gopher-cni/internal/cni"
	corev1 "k8s.io/api/core/v1"
)

// createDNSPatches configures pod DNS based on the WireGuard config and split-DNS annotation.
//
// Always-on tunnel DNS: if the WireGuard secret contains a DNS server, dnsPolicy is set to
// None and nameservers are pointed directly at the tunnel resolver.
//
// Split DNS: if split-tunnel-dns-zones is also set, a CoreDNS sidecar is injected instead.
// CoreDNS forwards listed zones to the cluster DNS server (kube-dns) and everything else to
// the tunnel resolver. Pod nameservers are set to 127.0.0.1 (the sidecar).
func (h *MutateHandler) createDNSPatches(pod *corev1.Pod) ([]PatchOperation, error) {
	secretName := pod.Annotations[cni.AnnotationWGConfSecret]

	tunnelDNSServers, err := h.getTunnelDNSServers(pod.Namespace, secretName)
	if err != nil {
		return nil, fmt.Errorf("failed to read DNS from WireGuard secret %s: %w", secretName, err)
	}

	splitZonesRaw := pod.Annotations[cni.AnnotationSplitTunnelDNSZones]

	if splitZonesRaw != "" {
		return h.createSplitDNSPatches(pod, splitZonesRaw, tunnelDNSServers)
	}

	// Standard tunnel DNS: point pod nameservers directly at the tunnel resolver.
	if len(tunnelDNSServers) == 0 {
		return nil, nil
	}
	mutateLogger.Debug("configuring tunnel DNS", "namespace", pod.Namespace, "pod", pod.Name, "servers", tunnelDNSServers)
	return []PatchOperation{
		{Op: "replace", Path: "/spec/dnsPolicy", Value: "None"},
		{Op: "add", Path: "/spec/dnsConfig", Value: corev1.PodDNSConfig{Nameservers: tunnelDNSServers}},
	}, nil
}

// createSplitDNSPatches injects a CoreDNS sidecar that routes listed zones to the cluster DNS
// server and all other queries to the WireGuard tunnel DNS resolver.
func (h *MutateHandler) createSplitDNSPatches(pod *corev1.Pod, splitZonesRaw string, tunnelDNSServers []string) ([]PatchOperation, error) {
	clusterDNSIP, err := h.Config.KubeClient.GetServiceClusterIP(context.TODO(), "kube-system", "kube-dns")
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster DNS IP from kube-dns service: %w", err)
	}

	zones := parseDNSZones(splitZonesRaw)
	tunnelDNSIP := ""
	if len(tunnelDNSServers) > 0 {
		tunnelDNSIP = tunnelDNSServers[0]
	}
	corefile := generateCorefile(zones, clusterDNSIP, tunnelDNSIP)

	mutateLogger.Debug("configuring split DNS", "namespace", pod.Namespace, "pod", pod.Name,
		"zones", zones, "clusterDNS", clusterDNSIP, "tunnelDNS", tunnelDNSIP)

	var patches []PatchOperation

	// emptyDir volume shared between the config init container and the CoreDNS sidecar
	coreDNSVolume := corev1.Volume{
		Name:         CoreDNSVolumeName,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}
	if len(pod.Spec.Volumes) == 0 {
		patches = append(patches, PatchOperation{Op: "add", Path: "/spec/volumes", Value: []corev1.Volume{coreDNSVolume}})
	} else {
		patches = append(patches, PatchOperation{Op: "add", Path: "/spec/volumes/-", Value: coreDNSVolume})
	}

	// Init container that writes the Corefile into the shared volume
	configInitContainer := h.Config.createCoreDNSConfigInitContainer(corefile)
	patches = append(patches, PatchOperation{Op: "add", Path: "/spec/initContainers/-", Value: configInitContainer})

	// CoreDNS sidecar
	if hasContainer(pod.Spec.Containers, CoreDNSContainerName) {
		return nil, fmt.Errorf("container named %q already exists in pod", CoreDNSContainerName)
	}
	patches = append(patches, PatchOperation{Op: "add", Path: "/spec/containers/-", Value: h.Config.createCoreDNSSidecarContainer()})

	// Route all pod DNS through the CoreDNS sidecar
	patches = append(patches,
		PatchOperation{Op: "replace", Path: "/spec/dnsPolicy", Value: "None"},
		PatchOperation{Op: "add", Path: "/spec/dnsConfig", Value: corev1.PodDNSConfig{Nameservers: []string{"127.0.0.1"}}},
	)

	return patches, nil
}

// getTunnelDNSServers reads the WireGuard config from a secret and returns the IPv4 DNS servers.
func (h *MutateHandler) getTunnelDNSServers(namespace, secretName string) ([]string, error) {
	wgConfig, err := h.Config.fetchWGConfig(context.TODO(), namespace, secretName)
	if err != nil {
		return nil, err
	}

	var dnsServers []string
	for _, ip := range wgConfig.DNS {
		if ip.To4() != nil {
			dnsServers = append(dnsServers, ip.String())
		}
	}
	return dnsServers, nil
}

// parseDNSZones splits a comma-separated zone annotation value, normalising each zone by
// stripping any trailing dot (CoreDNS zone blocks use the bare name).
func parseDNSZones(raw string) []string {
	var zones []string
	for _, z := range strings.Split(raw, ",") {
		z = strings.TrimSpace(z)
		z = strings.TrimSuffix(z, ".")
		if z != "" {
			zones = append(zones, z)
		}
	}
	return zones
}

// generateCorefile builds a CoreDNS Corefile that forwards the given zones to clusterDNSIP
// and all other queries to tunnelDNSIP. If tunnelDNSIP is empty the catch-all block is omitted.
func generateCorefile(zones []string, clusterDNSIP, tunnelDNSIP string) string {
	var sb strings.Builder
	for _, zone := range zones {
		fmt.Fprintf(&sb, "%s {\n    forward . %s\n    log\n}\n\n", zone, clusterDNSIP)
	}
	if tunnelDNSIP != "" {
		fmt.Fprintf(&sb, ". {\n    forward . %s\n    log\n}\n", tunnelDNSIP)
	}
	return sb.String()
}
