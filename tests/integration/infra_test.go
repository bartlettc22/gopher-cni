//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

const (
	wgServerContainerName = "gopher-cni-wg-server"
	nginxContainerName    = "gopher-cni-nginx"
	backendNetworkName    = "gopher-cni-backend"
	k3dNetworkName        = "k3d-" + clusterName

	wgServerVPNAddr = "10.100.0.1/24"
	wgPodVPNAddr    = "10.100.0.2/32"
	wgListenPort    = "51820"
	wgServerImage   = "lscr.io/linuxserver/wireguard"

	wgStartTimeout = 60 * time.Second
)

func generateServerWGConf(serverPrivKey, podPubKey wgtypes.Key) string {
	return fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s
ListenPort = %s
PostUp = iptables -A FORWARD -i wg0 -j ACCEPT; iptables -t nat -A POSTROUTING -j MASQUERADE
PostDown = iptables -D FORWARD -i wg0 -j ACCEPT; iptables -t nat -D POSTROUTING -j MASQUERADE

[Peer]
PublicKey = %s
AllowedIPs = %s
`, serverPrivKey, wgServerVPNAddr, wgListenPort, podPubKey, wgPodVPNAddr)
}

func generatePodWGConf(podPrivKey, serverPubKey wgtypes.Key, serverIP string) string {
	return fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s
DNS = 1.1.1.1

[Peer]
PublicKey = %s
Endpoint = %s:%s
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 25
`, podPrivKey, wgPodVPNAddr, serverPubKey, serverIP, wgListenPort)
}

func createBackendNetwork(ctx context.Context) error {
	fmt.Printf("Creating Docker network %q...\n", backendNetworkName)
	cmd := exec.CommandContext(ctx, "docker", "network", "create", backendNetworkName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker network create %s: %w", backendNetworkName, err)
	}
	return nil
}

func removeBackendNetwork(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "network", "rm", backendNetworkName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func stopContainer(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// getContainerIP returns the container's IP address on the given Docker network.
// Uses index to handle network names containing hyphens.
func getContainerIP(ctx context.Context, containerName, networkName string) (string, error) {
	format := fmt.Sprintf(`{{index .NetworkSettings.Networks %q "IPAddress"}}`, networkName)
	out, err := exec.CommandContext(ctx, "docker", "inspect", "--format", format, containerName).Output()
	if err != nil {
		return "", fmt.Errorf("docker inspect %s on network %s: %w", containerName, networkName, err)
	}
	ip := strings.TrimSpace(string(out))
	if ip == "" {
		return "", fmt.Errorf("container %s has no IP on network %s", containerName, networkName)
	}
	return ip, nil
}

func startNginx(ctx context.Context) (string, error) {
	fmt.Println("Starting nginx container on backend network...")
	cmd := exec.CommandContext(ctx, "docker", "run", "-d",
		"--name", nginxContainerName,
		"--network", backendNetworkName,
		"nginx:alpine",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker run nginx: %w", err)
	}

	ip, err := getContainerIP(ctx, nginxContainerName, backendNetworkName)
	if err != nil {
		return "", err
	}
	fmt.Printf("nginx started, backend IP: %s\n", ip)
	return ip, nil
}

// startWGServer starts the WireGuard server container on the k3d network,
// then connects it to the backend network so it can NAT traffic to nginx.
// confDir must contain wg0.conf and will be mounted at /config/wg_confs.
func startWGServer(ctx context.Context, confDir string) (string, error) {
	fmt.Println("Starting WireGuard server container...")

	cmd := exec.CommandContext(ctx, "docker", "run", "-d",
		"--name", wgServerContainerName,
		"--cap-add", "NET_ADMIN",
		"--cap-add", "SYS_MODULE",
		"--sysctl", "net.ipv4.conf.all.src_valid_mark=1",
		"--network", k3dNetworkName,
		"-v", confDir+":/config/wg_confs",
		wgServerImage,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker run wg-server: %w", err)
	}

	// Connect to backend network so the server can reach nginx and NAT traffic back.
	connectCmd := exec.CommandContext(ctx, "docker", "network", "connect", backendNetworkName, wgServerContainerName)
	connectCmd.Stdout = os.Stdout
	connectCmd.Stderr = os.Stderr
	if err := connectCmd.Run(); err != nil {
		return "", fmt.Errorf("docker network connect %s to %s: %w", wgServerContainerName, backendNetworkName, err)
	}

	if err := waitForWGServer(ctx); err != nil {
		return "", err
	}

	ip, err := getContainerIP(ctx, wgServerContainerName, k3dNetworkName)
	if err != nil {
		return "", err
	}
	fmt.Printf("WireGuard server ready, k3d network IP: %s\n", ip)
	return ip, nil
}

func waitForWGServer(ctx context.Context) error {
	fmt.Println("Waiting for WireGuard server to be ready...")
	deadline := time.Now().Add(wgStartTimeout)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		cmd := exec.CommandContext(ctx, "docker", "exec", wgServerContainerName, "wg", "show", "wg0")
		if cmd.Run() == nil {
			fmt.Println("WireGuard server is ready")
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("WireGuard server did not become ready within %s", wgStartTimeout)
}
