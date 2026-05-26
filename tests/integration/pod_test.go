//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	podTestNamespace    = "gopher-cni-test-pod"
	cniPodTestNamespace = "gopher-cni-test-cni-pod"
	podReadyTimeout     = 60 * time.Second
)

// TestPodBasicStartup deploys a plain pod (no gopher-cni label) and verifies
// it reaches Ready. Runs after TestDaemonSetReady (alphabetically P > D).
func (s *IntegrationSuite) TestPodBasicStartup() {
	ctx := context.Background()

	kubectl(ctx, "delete", "namespace", podTestNamespace, "--ignore-not-found") //nolint:errcheck
	defer func() {
		if os.Getenv("KEEP_CLUSTER") == "1" {
			return
		}
		kubectl(context.Background(), "delete", "namespace", podTestNamespace, "--ignore-not-found") //nolint:errcheck
	}()

	_, err := kubectl(ctx, "create", "namespace", podTestNamespace)
	s.Require().NoError(err, "create namespace %s", podTestNamespace)

	fmt.Printf("Deploying test pod in namespace %q...\n", podTestNamespace)

	_, err = kubectl(ctx,
		"run", "test-pod",
		"--namespace", podTestNamespace,
		"--image", "registry.k8s.io/pause:3.9",
		"--restart", "Never",
	)
	s.Require().NoError(err, "kubectl run test-pod")

	_, err = kubectl(ctx,
		"wait", "pod/test-pod",
		"--namespace", podTestNamespace,
		"--for=condition=Ready",
		fmt.Sprintf("--timeout=%s", podReadyTimeout),
	)
	s.Require().NoError(err, "pod test-pod not ready within %s", podReadyTimeout)

	fmt.Printf("Pod test-pod is Ready in namespace %q\n", podTestNamespace)
}

// minimalNoopWGConf is a WireGuard config with no DNS entry, so the mutating
// webhook can fetch and parse it without injecting any DNS patches.
const minimalNoopWGConf = `[Interface]
PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
Address = 10.2.0.2/32

[Peer]
PublicKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
AllowedIPs = 0.0.0.0/0
Endpoint = 1.2.3.4:51820
`

// TestValidatingWebhookRejectsInvalidPod verifies that the validating webhook
// rejects a pod with an invalid cni-mode. A real WireGuard secret (no DNS) is
// created so the mutating webhook can complete, then the validating webhook fires
// and denies the invalid cni-mode value.
func (s *IntegrationSuite) TestValidatingWebhookRejectsInvalidPod() {
	ctx := context.Background()

	kubectl(ctx, "delete", "namespace", cniPodTestNamespace, "--ignore-not-found") //nolint:errcheck
	defer func() {
		if os.Getenv("KEEP_CLUSTER") == "1" {
			return
		}
		kubectl(context.Background(), "delete", "namespace", cniPodTestNamespace, "--ignore-not-found") //nolint:errcheck
	}()

	_, err := kubectl(ctx, "create", "namespace", cniPodTestNamespace)
	s.Require().NoError(err, "create namespace %s", cniPodTestNamespace)

	s.Require().NoError(createWGSecret(ctx, cniPodTestNamespace, wgSecretName, minimalNoopWGConf), "create wg secret")

	fmt.Printf("Deploying pod with invalid cni-mode in namespace %q (expecting validating webhook rejection)...\n", cniPodTestNamespace)

	overrides := fmt.Sprintf(`{"metadata":{"annotations":{`+
		`"gopher.cni/wgconf-secret":%q,`+
		`"gopher.cni/cni-mode":"invalid-mode"`+
		`}}}`, wgSecretName)

	errOutput, runErr := kubectlWithStderr(ctx,
		"run", "test-pod",
		"--namespace", cniPodTestNamespace,
		"--image", "registry.k8s.io/pause:3.9",
		"--labels", "gopher.cni/enabled=true",
		"--restart", "Never",
		"--overrides", overrides,
	)
	s.Require().Error(runErr, "expected validating webhook to reject pod with invalid cni-mode")
	s.Require().Contains(errOutput, "cni-mode", "expected rejection message to reference the invalid cni-mode annotation")
}

// kubectlWithStderr runs kubectl and returns combined stderr output along with any error.
func kubectlWithStderr(ctx context.Context, args ...string) (string, error) {
	allArgs := append([]string{"--kubeconfig", kubeconfigFile, "--context", kubectlContext}, args...)

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "kubectl", allArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	errOutput := strings.TrimSpace(stderr.String())
	if err != nil {
		return errOutput, fmt.Errorf("kubectl %s: %w", strings.Join(args, " "), err)
	}
	return errOutput, nil
}
