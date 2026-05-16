//go:build integration

package integration

import (
	"context"
	"fmt"
)

const (
	podOriginFullNamespace   = "gopher-cni-test-pod-origin-full"
	podOriginPolicyNamespace = "gopher-cni-test-pod-origin-policy"
)

// TestPodOriginFull verifies the full connectivity path in pod-origin mode:
// the WireGuard interface is created inside the pod's network namespace and all
// traffic exits as WireGuard-encapsulated UDP through the pod's eth0 interface.
// Confirms the mutating webhook injected the init container and DNS config, and
// that the tunnel carries traffic end-to-end to nginx on the backend network.
func (s *IntegrationSuite) TestPodOriginFull() {
	ctx := context.Background()

	s.createNamespace(ctx, podOriginFullNamespace)
	s.Require().NoError(createWGSecret(ctx, podOriginFullNamespace, wgSecretName, s.podWGConf), "create wg secret")

	fmt.Printf("Deploying pod-origin pod in namespace %q...\n", podOriginFullNamespace)
	s.Require().NoError(
		deployWGPod(ctx, podOriginFullNamespace, wgPodName, wgPodImage, wgSecretName, "pod-origin"),
		"deploy pod-origin pod",
	)
	s.Require().NoError(
		awaitPodReady(ctx, podOriginFullNamespace, wgPodName, wgPodTimeout),
		"pod not ready — WireGuard interface may not have come up",
	)

	s.assertWebhookInjected(ctx, podOriginFullNamespace, wgPodName)

	fmt.Printf("Pod ready, testing connectivity to nginx at %s...\n", s.nginxBackendIP)
	s.Require().NoError(
		curlNginx(ctx, podOriginFullNamespace, wgPodName, s.nginxBackendIP, 10),
		"curl nginx via pod-origin WireGuard tunnel: pod → wg-server → nginx",
	)
	fmt.Println("pod-origin VPN connectivity confirmed")
}

// TestPodOriginPolicyBlocks verifies that a deny-all-egress NetworkPolicy
// prevents a pod-origin WireGuard pod from reaching the backend network.
//
// In pod-origin mode, WireGuard UDP exits through the pod's eth0 interface where
// Kubernetes NetworkPolicy (enforced by Calico) applies. The policy is applied
// before the pod is created to avoid pre-existing conntrack ESTABLISHED state
// that would otherwise let UDP through. The init container only validates the
// local interface (no network required), so the pod reaches Ready — but the
// WireGuard UDP handshake is a new flow that Calico drops, blackholing the tunnel.
func (s *IntegrationSuite) TestPodOriginPolicyBlocks() {
	ctx := context.Background()

	s.createNamespace(ctx, podOriginPolicyNamespace)

	fmt.Println("Applying deny-all-egress NetworkPolicy before pod creation...")
	s.Require().NoError(applyDenyAllEgress(ctx, podOriginPolicyNamespace), "apply deny-egress NetworkPolicy")

	s.Require().NoError(createWGSecret(ctx, podOriginPolicyNamespace, wgSecretName, s.podWGConf), "create wg secret")

	fmt.Printf("Deploying pod-origin pod in deny-all-egress namespace %q...\n", podOriginPolicyNamespace)
	s.Require().NoError(
		deployWGPod(ctx, podOriginPolicyNamespace, wgPodName, wgPodImage, wgSecretName, "pod-origin"),
		"deploy pod-origin pod",
	)
	s.Require().NoError(
		awaitPodReady(ctx, podOriginPolicyNamespace, wgPodName, wgPodTimeout),
		"pod not ready",
	)

	s.Require().Error(
		curlNginx(ctx, podOriginPolicyNamespace, wgPodName, s.nginxBackendIP, 5),
		"expected curl to fail — deny-all-egress must block WireGuard UDP handshake on eth0",
	)
	fmt.Println("NetworkPolicy correctly blocks pod-origin WireGuard egress")
}
