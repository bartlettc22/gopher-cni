//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

const (
	clusterName     = "gopher-cni-integration"
	kubectlContext  = "k3d-" + clusterName
	kubeconfigFile  = "/tmp/gopher-cni-integration.kubeconfig"
	setupTimeout    = 5 * time.Minute
	teardownTimeout = 2 * time.Minute

	imageRef           = "gopher-cni:integration"
	helmRelease        = "gopher-cni"
	helmNamespace      = "gopher-cni-system"
	helmChart          = "../../chart/gopher-cni"
	webhookServiceName = helmRelease + "-webhook"
	webhookCertSecret  = helmRelease + "-webhook-certs"
	buildTimeout       = 5 * time.Minute
	importTimeout      = 2 * time.Minute
	installTimeout     = 3 * time.Minute

	calicoVersion = "v3.28.2"
	calicoTimeout = 6 * time.Minute
	k3sPodCIDR    = "10.42.0.0/16"
)

// createCluster creates a k3d cluster for integration testing.
func createCluster(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()

	fmt.Printf("Creating k3d cluster %q...\n", clusterName)

	cmd := exec.CommandContext(ctx, "k3d", "cluster", "create", clusterName,
		"--no-lb",
		"--wait",
		"--k3s-arg", "--disable=traefik@server:*",
		// Disable Flannel so Calico can manage pod networking and NetworkPolicy.
		"--k3s-arg", "--flannel-backend=none@server:*",
		"--k3s-arg", "--disable-network-policy@server:*",
	)
	cmd.Env = append(os.Environ(), "KUBECONFIG="+kubeconfigFile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("k3d cluster create: %w", err)
	}

	fmt.Printf("Cluster %q created\n", clusterName)
	return nil
}

// waitForNodes blocks until all nodes report Ready.
// kubectl wait --all fails immediately when no resources exist yet, so we poll
// for node presence before handing off to kubectl wait.
func waitForNodes(ctx context.Context) error {
	fmt.Println("Waiting for nodes to be ready...")

	for {
		if ctx.Err() != nil {
			return fmt.Errorf("timed out waiting for nodes to appear: %w", ctx.Err())
		}
		out, _ := kubectl(ctx, "get", "nodes", "--no-headers")
		if out != "" {
			break
		}
		time.Sleep(2 * time.Second)
	}

	_, err := kubectl(ctx,
		"wait", "--for=condition=Ready", "nodes", "--all", "--timeout=120s",
	)
	return err
}

// clusterExists reports whether the named k3d cluster is already running.
func clusterExists(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "k3d", "cluster", "get", clusterName)
	cmd.Env = append(os.Environ(), "KUBECONFIG="+kubeconfigFile)
	return cmd.Run() == nil
}

// deleteCluster deletes the k3d cluster.
func deleteCluster(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, teardownTimeout)
	defer cancel()

	fmt.Printf("Deleting k3d cluster %q...\n", clusterName)

	cmd := exec.CommandContext(ctx, "k3d", "cluster", "delete", clusterName)
	cmd.Env = append(os.Environ(), "KUBECONFIG="+kubeconfigFile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("k3d cluster delete: %w", err)
	}

	fmt.Printf("Cluster %q deleted\n", clusterName)
	return nil
}

// kubectl runs a kubectl command against the integration cluster and returns
// stdout. Stderr is passed through to os.Stderr.
func kubectl(ctx context.Context, args ...string) (string, error) {
	allArgs := append([]string{"--kubeconfig", kubeconfigFile, "--context", kubectlContext}, args...)

	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, "kubectl", allArgs...)
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("kubectl %s: %w", strings.Join(args, " "), err)
	}

	return strings.TrimSpace(out.String()), nil
}

// buildImage builds the gopher-cni Docker image tagged for integration testing.
func buildImage(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()

	fmt.Printf("Building image %q...\n", imageRef)

	cmd := exec.CommandContext(ctx, "docker", "build", "-f", "../../build/Dockerfile", "-t", imageRef, "../..")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build: %w", err)
	}

	fmt.Printf("Image %q built\n", imageRef)
	return nil
}

// importImage loads the Docker image into the k3d cluster nodes so pods can
// use it without a registry.
func importImage(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, importTimeout)
	defer cancel()

	fmt.Printf("Importing image %q into cluster %q...\n", imageRef, clusterName)

	cmd := exec.CommandContext(ctx, "k3d", "image", "import", imageRef, "--cluster", clusterName)
	cmd.Env = append(os.Environ(), "KUBECONFIG="+kubeconfigFile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("k3d image import: %w", err)
	}

	fmt.Printf("Image %q imported\n", imageRef)
	return nil
}

// installCalico installs Calico using the standalone calico.yaml manifest.
// Unlike the Tigera operator, calico-node runs with hostNetwork:true so it
// can start before any CNI exists — avoiding the chicken-and-egg problem on
// a fresh cluster. Once calico-node is Ready, nodes become Ready too.
func installCalico(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, calicoTimeout)
	defer cancel()

	fmt.Printf("Installing Calico %s...\n", calicoVersion)

	manifestURL := fmt.Sprintf(
		"https://raw.githubusercontent.com/projectcalico/calico/%s/manifests/calico.yaml",
		calicoVersion,
	)
	if _, err := kubectl(ctx, "apply", "-f", manifestURL); err != nil {
		return fmt.Errorf("apply calico manifest: %w", err)
	}

	// Set the IP pool CIDR to match k3s's default pod CIDR before any
	// calico-node pod starts and creates the pool with the wrong default.
	if _, err := kubectl(ctx, "set", "env", "daemonset/calico-node",
		"--namespace", "kube-system",
		"CALICO_IPV4POOL_CIDR="+k3sPodCIDR,
	); err != nil {
		return fmt.Errorf("set calico pod CIDR: %w", err)
	}

	// Poll until calico-node pods are created, then wait for Ready.
	fmt.Println("Waiting for calico-node pods to be ready...")
	for {
		if ctx.Err() != nil {
			return fmt.Errorf("timed out waiting for calico-node pods: %w", ctx.Err())
		}
		out, _ := kubectl(ctx, "get", "pods",
			"--namespace", "kube-system",
			"--selector", "k8s-app=calico-node",
			"--no-headers",
		)
		if out != "" {
			break
		}
		time.Sleep(5 * time.Second)
	}
	if _, err := kubectl(ctx, "wait",
		"--for=condition=Ready", "pods",
		"--namespace", "kube-system",
		"--selector", "k8s-app=calico-node",
		"--timeout", "240s",
	); err != nil {
		return fmt.Errorf("calico-node not ready: %w", err)
	}

	// Nodes reach Ready only after Calico configures pod networking.
	if err := waitForNodes(ctx); err != nil {
		return fmt.Errorf("nodes not ready after calico install: %w", err)
	}

	fmt.Println("Calico installed and ready")
	return nil
}

// createWebhookCertSecret generates a self-signed TLS certificate and stores it
// as the secret the DaemonSet mounts for webhook TLS. It returns the CA PEM so
// the caller can inject it into the MutatingWebhookConfiguration.
func createWebhookCertSecret(ctx context.Context) ([]byte, error) {
	fmt.Printf("Creating webhook cert secret %q...\n", webhookCertSecret)

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"gopher-cni-integration"}},
		DNSNames: []string{
			webhookServiceName,
			webhookServiceName + "." + helmNamespace,
			webhookServiceName + "." + helmNamespace + ".svc",
			webhookServiceName + "." + helmNamespace + ".svc.cluster.local",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("create certificate: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	certFile, err := os.CreateTemp("", "gopher-cni-tls-cert-*.pem")
	if err != nil {
		return nil, fmt.Errorf("create cert temp file: %w", err)
	}
	defer os.Remove(certFile.Name())
	certFile.Write(certPEM)
	certFile.Close()

	keyFile, err := os.CreateTemp("", "gopher-cni-tls-key-*.pem")
	if err != nil {
		return nil, fmt.Errorf("create key temp file: %w", err)
	}
	defer os.Remove(keyFile.Name())
	keyFile.Write(keyPEM)
	keyFile.Close()

	// Ensure the namespace exists before creating the secret.
	kubectl(ctx, "create", "namespace", helmNamespace) //nolint:errcheck // ok if already exists

	if _, err := kubectl(ctx, "create", "secret", "tls", webhookCertSecret,
		"--namespace", helmNamespace,
		"--cert", certFile.Name(),
		"--key", keyFile.Name(),
	); err != nil {
		return nil, fmt.Errorf("create secret: %w", err)
	}

	fmt.Printf("Secret %q created\n", webhookCertSecret)
	return certPEM, nil
}

// patchWebhookCABundle sets caBundle on both the MutatingWebhookConfiguration
// and ValidatingWebhookConfiguration so the API server can verify the webhook
// server's self-signed certificate for both admission phases.
// Must be called after Helm install (the resources must already exist).
func patchWebhookCABundle(ctx context.Context, caPEM []byte) error {
	caB64 := base64.StdEncoding.EncodeToString(caPEM)

	mutatingPatch := fmt.Sprintf(`[{"op":"replace","path":"/webhooks/0/clientConfig/caBundle","value":%q}]`, caB64)
	if _, err := kubectl(ctx,
		"patch", "mutatingwebhookconfiguration", helmRelease+"-mutating",
		"--type=json",
		"-p", mutatingPatch,
	); err != nil {
		return fmt.Errorf("patch mutatingwebhookconfiguration caBundle: %w", err)
	}
	fmt.Printf("Patched caBundle on MutatingWebhookConfiguration %q\n", helmRelease+"-mutating")

	// The validating config has two webhooks (pods and controllers).
	validatingPatch := fmt.Sprintf(`[`+
		`{"op":"replace","path":"/webhooks/0/clientConfig/caBundle","value":%q},`+
		`{"op":"replace","path":"/webhooks/1/clientConfig/caBundle","value":%q}`+
		`]`, caB64, caB64)
	if _, err := kubectl(ctx,
		"patch", "validatingwebhookconfiguration", helmRelease+"-validating",
		"--type=json",
		"-p", validatingPatch,
	); err != nil {
		return fmt.Errorf("patch validatingwebhookconfiguration caBundle: %w", err)
	}
	fmt.Printf("Patched caBundle on ValidatingWebhookConfiguration %q\n", helmRelease+"-validating")

	return nil
}

// waitForWebhook polls until the mutating webhook is reachable by issuing a
// dry-run pod create. The regular kubectl helper returns only "exit status 1"
// on failure, so we capture stderr with kubectlWithStderr to distinguish
// "connection refused" (server not up yet) from any real admission response
// (webhook is up, even if it denies the request).
func waitForWebhook(ctx context.Context) error {
	fmt.Println("Waiting for webhook to be ready...")
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		stderr, err := kubectlWithStderr(ctx,
			"run", "webhook-probe",
			"--namespace", helmNamespace,
			"--image", "registry.k8s.io/pause:3.9",
			"--labels", "gopher.cni/enabled=true",
			"--restart", "Never",
			"--dry-run=server",
		)
		if err == nil || !strings.Contains(stderr, "connection refused") {
			fmt.Println("Webhook is ready")
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out waiting for webhook to become ready")
}

// installApp installs gopher-cni into the cluster via the Helm chart.
// cert-manager and the webhook are disabled since the integration cluster is
// minimal and does not run cert-manager.
func installApp(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()

	fmt.Printf("Installing Helm release %q...\n", helmRelease)

	cmd := exec.CommandContext(ctx, "helm", "install", helmRelease, helmChart,
		"--kubeconfig", kubeconfigFile,
		"--namespace", helmNamespace,
		"--create-namespace",
		"--set", "image.repository=gopher-cni",
		"--set", "image.tag=integration",
		"--set", "image.pullPolicy=Never",
		"--set", "certificate.enabled=false",
		"--wait",
		"--timeout", "2m",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("helm install: %w", err)
	}

	fmt.Printf("Release %q installed\n", helmRelease)
	return nil
}

// IntegrationSuite is the testify suite for all integration tests.
type IntegrationSuite struct {
	suite.Suite

	// VPN infra — populated during SetupSuite, used by TestVPNConnectivity.
	nginxBackendIP string // nginx's IP on the backend Docker network
	podWGConf      string // wg.conf content for the test pod's Kubernetes secret
	wgConfDir      string // temp dir holding the server's wg0.conf (kept for container lifetime)
}

// TestIntegrationSuite is the entry point that runs the suite.
func TestIntegrationSuite(t *testing.T) {
	suite.Run(t, new(IntegrationSuite))
}

// SetupSuite creates the cluster, builds and imports the image, and installs
// the app via Helm — once before all tests run.
//
// Note: testify registers TearDownSuite's defer AFTER SetupSuite returns, so
// it won't run if SetupSuite exits via FailNow. The defer below handles cleanup
// on setup failure; runtime.Goexit (called by s.Require) does execute defers.
func (s *IntegrationSuite) SetupSuite() {
	ctx := context.Background()

	_ = os.Remove(kubeconfigFile)

	setupOK := false
	defer func() {
		if !setupOK {
			bg := context.Background()
			stopContainer(bg, wgServerContainerName) //nolint:errcheck
			stopContainer(bg, nginxContainerName)    //nolint:errcheck
			removeBackendNetwork(bg)                 //nolint:errcheck
			if s.wgConfDir != "" {
				os.RemoveAll(s.wgConfDir)
			}
			if err := deleteCluster(bg); err != nil {
				fmt.Fprintf(os.Stderr, "warning: cleanup after failed setup: %v\n", err)
			}
		}
	}()

	// Clean up any leftover infra from a previous run.
	bg := context.Background()
	stopContainer(bg, wgServerContainerName) //nolint:errcheck
	stopContainer(bg, nginxContainerName)    //nolint:errcheck
	removeBackendNetwork(bg)                 //nolint:errcheck

	if clusterExists(ctx) {
		fmt.Printf("Found existing cluster %q, cleaning up...\n", clusterName)
		s.Require().NoError(deleteCluster(ctx), "delete existing cluster")
	}

	s.Require().NoError(createCluster(ctx), "create cluster")
	s.Require().NoError(installCalico(ctx), "install calico")
	s.Require().NoError(buildImage(ctx), "build image")
	s.Require().NoError(importImage(ctx), "import image")

	// Generate WireGuard key pairs for the server and the test pod.
	serverPrivKey, err := wgtypes.GeneratePrivateKey()
	s.Require().NoError(err, "generate server WireGuard private key")
	podPrivKey, err := wgtypes.GeneratePrivateKey()
	s.Require().NoError(err, "generate pod WireGuard private key")

	// Create the backend Docker network and start infrastructure containers.
	s.Require().NoError(createBackendNetwork(ctx), "create backend network")

	nginxIP, err := startNginx(ctx)
	s.Require().NoError(err, "start nginx")
	s.nginxBackendIP = nginxIP

	// Write the server WireGuard config to a temp dir that the container will mount.
	confDir, err := os.MkdirTemp("", "gopher-cni-wg-server-*")
	s.Require().NoError(err, "create wg server conf dir")
	s.wgConfDir = confDir
	serverConf := generateServerWGConf(serverPrivKey, podPrivKey.PublicKey())
	s.Require().NoError(
		os.WriteFile(confDir+"/wg0.conf", []byte(serverConf), 0600),
		"write server wg0.conf",
	)

	wgServerIP, err := startWGServer(ctx, confDir)
	s.Require().NoError(err, "start WireGuard server")

	// Pre-generate the pod's wg.conf — stored in the suite so TestVPNConnectivity
	// can create the Kubernetes secret without needing to know the key material.
	s.podWGConf = generatePodWGConf(podPrivKey, serverPrivKey.PublicKey(), wgServerIP)

	caPEM, err := createWebhookCertSecret(ctx)
	s.Require().NoError(err, "create webhook cert secret")

	s.Require().NoError(installApp(ctx), "install app")
	s.Require().NoError(patchWebhookCABundle(ctx, caPEM), "patch webhook CA bundle")
	s.Require().NoError(waitForWebhook(ctx), "wait for webhook")

	setupOK = true
}

// TearDownSuite deletes the cluster and Docker infra after all tests finish.
// Set KEEP_CLUSTER=1 to leave everything running for debugging.
func (s *IntegrationSuite) TearDownSuite() {
	if os.Getenv("KEEP_CLUSTER") == "1" {
		fmt.Fprintf(os.Stderr, "KEEP_CLUSTER=1: leaving cluster %q and infra containers running\n", clusterName)
		fmt.Fprintf(os.Stderr, "  debug: docker exec %s wg show\n", wgServerContainerName)
		fmt.Fprintf(os.Stderr, "  debug: kubectl exec vpn-test-pod -n gopher-cni-test-vpn -- ip route\n")
		return
	}
	bg := context.Background()
	if err := stopContainer(bg, wgServerContainerName); err != nil {
		fmt.Fprintf(os.Stderr, "warning: stop wg-server: %v\n", err)
	}
	if err := stopContainer(bg, nginxContainerName); err != nil {
		fmt.Fprintf(os.Stderr, "warning: stop nginx: %v\n", err)
	}
	if err := removeBackendNetwork(bg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: remove backend network: %v\n", err)
	}
	if s.wgConfDir != "" {
		os.RemoveAll(s.wgConfDir)
	}
	if err := deleteCluster(bg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to delete cluster: %v\n", err)
	}
	_ = os.Remove(kubeconfigFile)
}

// TestDaemonSetReady verifies the gopher-cni DaemonSet is fully rolled out
// with all desired pods running and ready.
func (s *IntegrationSuite) TestDaemonSetReady() {
	ctx := context.Background()

	_, err := kubectl(ctx,
		"rollout", "status", "daemonset/"+helmRelease,
		"--namespace", helmNamespace,
		"--timeout", "60s",
	)
	s.Require().NoError(err, "daemonset rollout status")
}

// TestClusterReady verifies the cluster has at least one Ready node.
func (s *IntegrationSuite) TestClusterReady() {
	ctx := context.Background()

	out, err := kubectl(ctx, "get", "nodes", "--no-headers")
	s.Require().NoError(err, "get nodes")
	s.Require().NotEmpty(out, "no nodes found in cluster")
	s.T().Logf("nodes:\n%s", out)

	_, err = kubectl(ctx, "wait", "--for=condition=Ready", "nodes", "--all", "--timeout=30s")
	s.Require().NoError(err, "nodes not ready")
}
