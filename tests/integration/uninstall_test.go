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
	k3dServerNode    = "k3d-" + clusterName + "-server-0"
	uninstallTimeout = 2 * time.Minute
)

// TestZZZCNIUninstall verifies that when the Helm release is uninstalled the
// deferred Uninstall() path in the install-cni container runs correctly and
// removes all gopher-cni artifacts from the host node filesystem:
//   - the CNI plugin binary is gone from /opt/cni/bin/
//   - the kubeconfig is gone from /etc/cni/net.d/
//   - the underlying CNI conflist (e.g. Calico's) still exists
//   - no conflist file in /etc/cni/net.d/ still references the gopher-cni plugin type
//
// Named TestZZZ... so testify runs it last — all other tests require the Helm
// release to be present.
func (s *IntegrationSuite) TestZZZCNIUninstall() {
	ctx := context.Background()

	fmt.Printf("Uninstalling Helm release %q from namespace %q...\n", helmRelease, helmNamespace)
	cmd := exec.CommandContext(ctx, "helm", "uninstall", helmRelease,
		"--kubeconfig", kubeconfigFile,
		"--namespace", helmNamespace,
		"--wait",
		"--timeout", "2m",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	s.Require().NoError(cmd.Run(), "helm uninstall %s", helmRelease)

	// Helm --wait removes the DaemonSet resource but its child pods may still
	// be terminating. Wait for them to fully disappear so Uninstall() has run.
	fmt.Println("Waiting for gopher-cni pods to fully terminate...")
	waitCtx, cancel := context.WithTimeout(ctx, uninstallTimeout)
	defer cancel()
	if err := awaitPodsDeleted(waitCtx, helmNamespace, "app.kubernetes.io/name=gopher-cni", 90*time.Second); err != nil {
		s.Require().NoError(err, "gopher-cni pods did not terminate in time")
	}

	// Binary must be gone from the host CNI bin directory.
	s.assertFileAbsentOnNode(ctx,
		"/opt/cni/bin/gopher-cni",
		"CNI plugin binary must be removed from host node on uninstall",
	)

	// Kubeconfig must be gone from the host CNI net directory.
	s.assertFileAbsentOnNode(ctx,
		"/etc/cni/net.d/ZZZ-gopher-cni-kubeconfig",
		"CNI kubeconfig must be removed from host node on uninstall",
	)

	// The underlying CNI conflist (e.g. Calico's) must still exist — gopher-cni
	// only removes its own plugin entry, not the whole file.
	s.assertFileExistsOnNode(ctx,
		"/etc/cni/net.d/10-calico.conflist",
		"calico conflist must still exist after gopher-cni uninstall",
	)

	// The gopher-cni plugin entry must be stripped from every conflist file.
	// Poll for up to 30 s: in k3d the bind-mount write may not be visible to a
	// subsequent docker exec immediately after the pod's process exits.
	fmt.Println("Waiting for conflist cleanup to propagate...")
	if err := awaitConflistClean(ctx, 30*time.Second); err != nil {
		s.Require().NoError(err, "gopher-cni type must be removed from all conflist files on uninstall")
	}

	fmt.Println("CNI uninstall verified: all host artifacts removed from node")
}

// awaitPodsDeleted polls until no pods matching selector exist in namespace, or
// until timeout. Returns nil immediately if no pods are found on the first poll.
func awaitPodsDeleted(ctx context.Context, namespace, selector string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		out, _ := kubectl(ctx, "get", "pod",
			"--namespace", namespace,
			"--selector", selector,
			"--no-headers",
		)
		if strings.TrimSpace(out) == "" {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out after %s waiting for pods (selector=%s) to terminate in %s", timeout, selector, namespace)
}

// assertFileAbsentOnNode asserts that path does not exist on the k3d server
// node by running `test ! -f` inside the container via docker exec.
func (s *IntegrationSuite) assertFileAbsentOnNode(ctx context.Context, path, msg string) {
	cmd := exec.CommandContext(ctx, "docker", "exec", k3dServerNode,
		"sh", "-c", "test ! -f "+path,
	)
	s.Require().NoError(cmd.Run(), "%s (path: %s)", msg, path)
}

// assertFileExistsOnNode asserts that path exists on the k3d server node by
// running `test -f` inside the container via docker exec.
func (s *IntegrationSuite) assertFileExistsOnNode(ctx context.Context, path, msg string) {
	cmd := exec.CommandContext(ctx, "docker", "exec", k3dServerNode,
		"sh", "-c", "test -f "+path,
	)
	s.Require().NoError(cmd.Run(), "%s (path: %s)", msg, path)
}

// awaitConflistClean polls until no file under /etc/cni/net.d/ on the k3d
// server node contains a reference to the gopher-cni plugin type, or until
// timeout elapses.
func awaitConflistClean(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var out bytes.Buffer
		cmd := exec.CommandContext(ctx, "docker", "exec", k3dServerNode,
			"sh", "-c", `grep -rl '"type": "gopher-cni"' /etc/cni/net.d/ 2>/dev/null`,
		)
		cmd.Stdout = &out
		_ = cmd.Run() // grep exits 1 on no match — check output instead
		if strings.TrimSpace(out.String()) == "" {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out after %s waiting for conflist to be clean of gopher-cni references", timeout)
}
