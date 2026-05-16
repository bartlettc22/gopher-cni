//go:build integration

package integration

import (
	"context"
	"fmt"
)

const (
	hostOriginFullNamespace   = "gopher-cni-test-host-origin-full"
	hostOriginPolicyNamespace = "gopher-cni-test-host-origin-policy"
)

// TestHostOriginFull verifies the full connectivity path in host-origin mode:
// the WireGuard interface is created in the host network namespace and traffic
// is routed through the host's network stack to nginx on the backend network.
// Confirms the mutating webhook injected the init container and DNS config, and
// that the tunnel carries traffic end-to-end.
func (s *IntegrationSuite) TestHostOriginFull() {
	ctx := context.Background()

	s.createNamespace(ctx, hostOriginFullNamespace)
	s.Require().NoError(createWGSecret(ctx, hostOriginFullNamespace, wgSecretName, s.podWGConf), "create wg secret")

	fmt.Printf("Deploying host-origin pod in namespace %q...\n", hostOriginFullNamespace)
	s.Require().NoError(
		deployWGPod(ctx, hostOriginFullNamespace, wgPodName, wgPodImage, wgSecretName, "host-origin"),
		"deploy host-origin pod",
	)
	s.Require().NoError(
		awaitPodReady(ctx, hostOriginFullNamespace, wgPodName, wgPodTimeout),
		"pod not ready — WireGuard interface may not have come up",
	)

	s.assertWebhookInjected(ctx, hostOriginFullNamespace, wgPodName)

	fmt.Printf("Pod ready, testing connectivity to nginx at %s...\n", s.nginxBackendIP)
	s.Require().NoError(
		curlNginx(ctx, hostOriginFullNamespace, wgPodName, s.nginxBackendIP, 10),
		"curl nginx via host-origin WireGuard tunnel: pod → host netns → wg-server → nginx",
	)
	fmt.Println("host-origin VPN connectivity confirmed")
}

// TestHostOriginPolicyNoEffect verifies that a deny-all-egress NetworkPolicy has
// NO effect on host-origin WireGuard traffic.
//
// In host-origin mode, the WireGuard interface lives in the host network namespace
// and encrypted packets exit through the host's network interface — not the pod's
// eth0. Kubernetes NetworkPolicy is enforced at the pod's veth pair and therefore
// does not intercept this traffic path. Traffic must flow freely even with a
// deny-all-egress policy applied to the namespace.
func (s *IntegrationSuite) TestHostOriginPolicyNoEffect() {
	ctx := context.Background()

	s.createNamespace(ctx, hostOriginPolicyNamespace)

	fmt.Println("Applying deny-all-egress NetworkPolicy before pod creation...")
	s.Require().NoError(applyDenyAllEgress(ctx, hostOriginPolicyNamespace), "apply deny-egress NetworkPolicy")

	s.Require().NoError(createWGSecret(ctx, hostOriginPolicyNamespace, wgSecretName, s.podWGConf), "create wg secret")

	fmt.Printf("Deploying host-origin pod in deny-all-egress namespace %q...\n", hostOriginPolicyNamespace)
	s.Require().NoError(
		deployWGPod(ctx, hostOriginPolicyNamespace, wgPodName, wgPodImage, wgSecretName, "host-origin"),
		"deploy host-origin pod",
	)
	s.Require().NoError(
		awaitPodReady(ctx, hostOriginPolicyNamespace, wgPodName, wgPodTimeout),
		"pod not ready",
	)

	// Traffic must still reach nginx: WireGuard packets exit via the host
	// network namespace and bypass pod-level NetworkPolicy enforcement entirely.
	s.Require().NoError(
		curlNginx(ctx, hostOriginPolicyNamespace, wgPodName, s.nginxBackendIP, 10),
		"curl must succeed — deny-all-egress must not affect host-origin traffic exiting via host netns",
	)
	fmt.Println("Confirmed: deny-all-egress has no effect on host-origin WireGuard traffic")
}
