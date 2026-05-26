//go:build integration

package integration

import (
	"context"
	"fmt"
)

const (
	dnsTunneledNamespace = "gopher-cni-test-dns-tunneled"
)

// TestDNSTunneledAlwaysOn verifies that the mutating webhook always injects DNS
// configuration into pods — DNS tunneling cannot be disabled. Specifically:
//   - dnsPolicy is set to "None"
//   - spec.dnsConfig.nameservers contains the WireGuard config's DNS server
//   - /etc/resolv.conf inside the running pod reflects the injected nameserver
func (s *IntegrationSuite) TestDNSTunneledAlwaysOn() {
	ctx := context.Background()

	s.createNamespace(ctx, dnsTunneledNamespace)
	s.Require().NoError(createWGSecret(ctx, dnsTunneledNamespace, wgSecretName, s.podWGConf), "create wg secret")

	fmt.Printf("Deploying pod in namespace %q to verify DNS is always injected...\n", dnsTunneledNamespace)

	s.Require().NoError(
		deployWGPod(ctx, dnsTunneledNamespace, wgPodName, wgPodImage, wgSecretName, "pod-origin"),
		"deploy pod",
	)
	s.Require().NoError(
		awaitPodReady(ctx, dnsTunneledNamespace, wgPodName, wgPodTimeout),
		"pod not ready",
	)

	s.assertWebhookInjected(ctx, dnsTunneledNamespace, wgPodName)

	fmt.Println("DNS tunneling always-on: DNS injection confirmed")
}
