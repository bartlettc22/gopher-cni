package controller

import "fmt"

func proxyPodName(proxyName string) string {
	return fmt.Sprintf("%s-proxy", proxyName)
}

func proxySvcName(proxyName string) string {
	return fmt.Sprintf("%s-proxy", proxyName)
}

func internalWGSecretName(proxyName string) string {
	return fmt.Sprintf("%s-internal-wg", proxyName)
}

func peersSecretName(proxyName string) string {
	return fmt.Sprintf("%s-peers", proxyName)
}

// peerClientSecretName returns the name of the WireGuard client config Secret
// generated for a peer pod. Pods should reference this name in their
// gopher.cni/wgconf-secret annotation.
func peerClientSecretName(proxyName, podName string) string {
	return fmt.Sprintf("%s-peer-%s-wg", proxyName, podName)
}
