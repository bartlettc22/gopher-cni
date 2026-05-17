//go:build integration

package integration

import (
	"context"
	"fmt"
)

const (
	dnsTunneledNamespace = "gopher-cni-test-dns-tunneled-false"
)

// TestDNSTunneledFalse verifies that when dns-tunneled=false is set on a pod,
// the mutating webhook does NOT inject DNS configuration into the pod spec:
// dnsPolicy is left at its default ("ClusterFirst"), spec.dnsConfig.nameservers
// is absent, and /etc/resolv.conf inside the running pod is unmodified (i.e.
// does not contain the WireGuard config's DNS server).
//
// The init container is still injected because dns-tunneled only gates DNS
// mutation, not the WireGuard validator init container.
func (s *IntegrationSuite) TestDNSTunneledFalse() {
	ctx := context.Background()

	s.createNamespace(ctx, dnsTunneledNamespace)
	s.Require().NoError(createWGSecret(ctx, dnsTunneledNamespace, wgSecretName, s.podWGConf), "create wg secret")

	fmt.Printf("Deploying pod with dns-tunneled=false in namespace %q...\n", dnsTunneledNamespace)

	overrides := fmt.Sprintf(`{`+
		`"metadata":{"annotations":{`+
		`"gopher.cni/wgconf-secret":%q,`+
		`"gopher.cni/cni-mode":"pod-origin",`+
		`"gopher.cni/dns-tunneled":"false"`+
		`}},`+
		`"spec":{"terminationGracePeriodSeconds":0}`+
		`}`, wgSecretName)

	_, err := kubectl(ctx,
		"run", wgPodName,
		"--namespace", dnsTunneledNamespace,
		"--image", wgPodImage,
		"--labels", "gopher.cni/enabled=true",
		"--restart", "Never",
		"--overrides", overrides,
		"--", "sleep", "3600",
	)
	s.Require().NoError(err, "deploy pod with dns-tunneled=false")
	s.Require().NoError(
		awaitPodReady(ctx, dnsTunneledNamespace, wgPodName, wgPodTimeout),
		"pod not ready",
	)

	// Init container must still be injected — dns-tunneled only gates DNS mutation.
	initContainers, err := kubectl(ctx,
		"get", "pod/"+wgPodName, "--namespace", dnsTunneledNamespace,
		"-o", `jsonpath={.spec.initContainers[*].name}`,
	)
	s.Require().NoError(err, "get pod init containers")
	s.Require().Contains(initContainers, "gopher-cni-validator",
		"mutating webhook must still inject gopher-cni-validator init container when dns-tunneled=false")

	// dnsPolicy must remain at the default — webhook must not have set it to "None".
	dnsPolicy, err := kubectl(ctx,
		"get", "pod/"+wgPodName, "--namespace", dnsTunneledNamespace,
		"-o", `jsonpath={.spec.dnsPolicy}`,
	)
	s.Require().NoError(err, "get pod dnsPolicy")
	s.Require().NotEqual("None", dnsPolicy,
		"webhook must NOT set dnsPolicy to None when dns-tunneled=false")

	// spec.dnsConfig.nameservers must be absent (empty jsonpath output).
	dnsNameservers, err := kubectl(ctx,
		"get", "pod/"+wgPodName, "--namespace", dnsTunneledNamespace,
		"-o", `jsonpath={.spec.dnsConfig.nameservers}`,
	)
	s.Require().NoError(err, "get pod dnsConfig.nameservers")
	s.Require().NotContains(dnsNameservers, wgDNSNameserver,
		"webhook must NOT inject DNS nameserver into pod spec when dns-tunneled=false")

	// /etc/resolv.conf inside the pod must not contain the WireGuard DNS server.
	resolv, err := kubectl(ctx,
		"exec", wgPodName, "--namespace", dnsTunneledNamespace,
		"--", "cat", "/etc/resolv.conf",
	)
	s.Require().NoError(err, "cat /etc/resolv.conf")
	s.Require().NotContains(resolv, "nameserver "+wgDNSNameserver,
		"/etc/resolv.conf must NOT contain WireGuard DNS nameserver when dns-tunneled=false")

	fmt.Println("dns-tunneled=false: DNS injection correctly suppressed")
}
