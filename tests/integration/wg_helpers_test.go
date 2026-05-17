//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"time"
)

const (
	wgPodName       = "wg-test-pod"
	wgPodImage      = "curlimages/curl:latest"
	wgSecretName    = "wg-secret"
	wgPodTimeout    = 90 * time.Second
	wgDNSNameserver = "1.1.1.1"
)

// createNamespace creates a namespace and registers a cleanup that deletes it
// when the test finishes (skipped when KEEP_CLUSTER=1).
func (s *IntegrationSuite) createNamespace(ctx context.Context, namespace string) {
	kubectl(ctx, "delete", "namespace", namespace, "--ignore-not-found") //nolint:errcheck
	_, err := kubectl(ctx, "create", "namespace", namespace)
	s.Require().NoError(err, "create namespace %s", namespace)
	s.T().Cleanup(func() {
		if os.Getenv("KEEP_CLUSTER") == "1" {
			return
		}
		kubectl(context.Background(), "delete", "namespace", namespace, "--ignore-not-found") //nolint:errcheck
	})
}

// createWGSecret writes wgConf to a temp file and creates a Kubernetes generic
// secret with a wg.conf key in the given namespace.
func createWGSecret(ctx context.Context, namespace, secretName, wgConf string) error {
	confFile, err := os.CreateTemp("", "wg-*.conf")
	if err != nil {
		return fmt.Errorf("create temp wg.conf: %w", err)
	}
	defer os.Remove(confFile.Name())
	if _, err := confFile.WriteString(wgConf); err != nil {
		return fmt.Errorf("write wg.conf: %w", err)
	}
	confFile.Close()

	_, err = kubectl(ctx, "create", "secret", "generic", secretName,
		"--namespace", namespace,
		"--from-file=wg.conf="+confFile.Name(),
	)
	return err
}

// deployWGPod deploys a pod with gopher-cni annotations for the given CNI mode.
func deployWGPod(ctx context.Context, namespace, podName, image, secretName, cniMode string) error {
	overrides := fmt.Sprintf(`{`+
		`"metadata":{"annotations":{`+
		`"gopher.cni/wgconf-secret":%q,`+
		`"gopher.cni/cni-mode":%q`+
		`}},`+
		`"spec":{"terminationGracePeriodSeconds":0}`+
		`}`, secretName, cniMode)

	_, err := kubectl(ctx,
		"run", podName,
		"--namespace", namespace,
		"--image", image,
		"--labels", "gopher.cni/enabled=true",
		"--restart", "Never",
		"--overrides", overrides,
		"--", "sleep", "3600",
	)
	return err
}

// awaitPodReady waits for the named pod to reach the Ready condition.
func awaitPodReady(ctx context.Context, namespace, podName string, timeout time.Duration) error {
	_, err := kubectl(ctx,
		"wait", "pod/"+podName,
		"--namespace", namespace,
		"--for=condition=Ready",
		fmt.Sprintf("--timeout=%s", timeout),
	)
	return err
}

// assertWebhookInjected verifies the mutating webhook fired correctly:
//   - gopher-cni-validator init container is present in the pod spec
//   - dnsPolicy is set to None
//   - wgDNSNameserver appears in spec.dnsConfig.nameservers
//   - wgDNSNameserver appears in /etc/resolv.conf inside the running pod
func (s *IntegrationSuite) assertWebhookInjected(ctx context.Context, namespace, podName string) {
	initContainers, err := kubectl(ctx,
		"get", "pod/"+podName, "--namespace", namespace,
		"-o", `jsonpath={.spec.initContainers[*].name}`,
	)
	s.Require().NoError(err, "get pod init containers")
	s.Require().Contains(initContainers, "gopher-cni-validator",
		"mutating webhook must inject gopher-cni-validator init container")

	dnsPolicy, err := kubectl(ctx,
		"get", "pod/"+podName, "--namespace", namespace,
		"-o", `jsonpath={.spec.dnsPolicy}`,
	)
	s.Require().NoError(err, "get pod dnsPolicy")
	s.Require().Equal("None", dnsPolicy, "webhook must set dnsPolicy to None")

	dnsNameservers, err := kubectl(ctx,
		"get", "pod/"+podName, "--namespace", namespace,
		"-o", `jsonpath={.spec.dnsConfig.nameservers}`,
	)
	s.Require().NoError(err, "get pod dnsConfig.nameservers")
	s.Require().Contains(dnsNameservers, wgDNSNameserver,
		"webhook must inject DNS nameserver from WireGuard config into pod spec")

	resolv, err := kubectl(ctx,
		"exec", podName, "--namespace", namespace,
		"--", "cat", "/etc/resolv.conf",
	)
	s.Require().NoError(err, "cat /etc/resolv.conf")
	s.Require().Contains(resolv, "nameserver "+wgDNSNameserver,
		"/etc/resolv.conf must contain nameserver injected from WireGuard config")
}

// curlNginx runs curl from inside the named pod to the nginx backend.
// Returns nil on HTTP success, non-nil on failure or timeout.
func curlNginx(ctx context.Context, namespace, podName, nginxIP string, maxTimeSecs int) error {
	_, err := kubectl(ctx,
		"exec", podName, "--namespace", namespace,
		"--", "curl", "-sf", "--max-time", fmt.Sprintf("%d", maxTimeSecs),
		fmt.Sprintf("http://%s/", nginxIP),
	)
	return err
}

// applyDenyAllEgress applies a deny-all-egress NetworkPolicy to namespace.
func applyDenyAllEgress(ctx context.Context, namespace string) error {
	netpol := fmt.Sprintf(`apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: deny-egress
  namespace: %s
spec:
  podSelector: {}
  policyTypes:
  - Egress
`, namespace)

	netpolFile, err := os.CreateTemp("", "deny-egress-*.yaml")
	if err != nil {
		return fmt.Errorf("create netpol temp file: %w", err)
	}
	defer os.Remove(netpolFile.Name())
	if _, err := netpolFile.WriteString(netpol); err != nil {
		return fmt.Errorf("write netpol: %w", err)
	}
	netpolFile.Close()

	_, err = kubectl(ctx, "apply", "-f", netpolFile.Name())
	return err
}
