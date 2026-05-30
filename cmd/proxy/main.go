// cmd/proxy is the entrypoint for the gopher-proxy pod. It sets up two WireGuard
// interfaces — an internal server for peer pods and an external VPN client — then
// hot-reloads peers as the controller updates the peers Secret.
package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/vishvananda/netlink"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/bartlettc22/gopher-cni/internal/logging"
	"github.com/bartlettc22/gopher-cni/internal/utils"
	"github.com/bartlettc22/gopher-cni/internal/wireguard"
)

const (
	ifaceInternal = "wg-internal"
	ifaceVPN      = "wg-vpn"
	wgMTU         = 1420
	peersFile     = "/etc/gopher-proxy/peers/peers.conf"
	peersPollInterval = 5 * time.Second
)

type config struct {
	InternalPrivKeyPath string
	InternalAddress     string
	InternalListenPort  int
	VPNWGConfPath       string
	LogLevel            string
	LogFormat           string
}

func loadConfig() (*config, error) {
	portStr := utils.GetEnv("INTERNAL_LISTEN_PORT", "51820")
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("INTERNAL_LISTEN_PORT must be a valid port number, got %q", portStr)
	}

	c := &config{
		InternalPrivKeyPath: utils.GetEnv("INTERNAL_PRIVKEY_PATH", "/etc/gopher-proxy/internal-wg/privateKey"),
		InternalAddress:     utils.GetEnv("INTERNAL_ADDRESS", ""),
		InternalListenPort:  port,
		VPNWGConfPath:       utils.GetEnv("VPN_WG_CONF_PATH", "/etc/gopher-proxy/vpn-wg/wg.conf"),
		LogLevel:            utils.GetEnv("LOG_LEVEL", "info"),
		LogFormat:           utils.GetEnv("LOG_FORMAT", "text"),
	}
	if c.InternalAddress == "" {
		return nil, fmt.Errorf("INTERNAL_ADDRESS is required")
	}
	return c, nil
}

var log = slog.Default()

func main() {
	cfg, err := loadConfig()
	if err != nil {
		logging.Fatal(fmt.Errorf("failed to load config: %w", err))
	}
	if err := logging.Configure(cfg.LogLevel, cfg.LogFormat); err != nil {
		logging.Fatal(fmt.Errorf("failed to configure logger: %w", err))
	}
	log = slog.With("component", "proxy")
	log.Info("starting gopher-proxy")

	if err := run(cfg); err != nil {
		logging.Fatal(err)
	}
}

func run(cfg *config) error {
	// 1. Read internal WireGuard private key.
	privKeyBytes, err := os.ReadFile(cfg.InternalPrivKeyPath)
	if err != nil {
		return fmt.Errorf("reading internal private key: %w", err)
	}
	internalPrivKey, err := wgtypes.ParseKey(string(privKeyBytes))
	if err != nil {
		return fmt.Errorf("parsing internal private key: %w", err)
	}

	// 2. Parse the proxy's internal address.
	internalIP, internalNet, err := net.ParseCIDR(cfg.InternalAddress)
	if err != nil {
		return fmt.Errorf("parsing INTERNAL_ADDRESS %q: %w", cfg.InternalAddress, err)
	}
	internalAddr := &netlink.Addr{IPNet: &net.IPNet{IP: internalIP, Mask: internalNet.Mask}}

	// 3. Set up wg-internal (server interface for peer pods).
	listenPort := cfg.InternalListenPort
	if err := createWGIface(ifaceInternal, &wgtypes.Config{
		PrivateKey: &internalPrivKey,
		ListenPort: &listenPort,
	}, []*netlink.Addr{internalAddr}); err != nil {
		return fmt.Errorf("creating %s: %w", ifaceInternal, err)
	}
	log.Info("internal WireGuard interface up", "iface", ifaceInternal, "addr", cfg.InternalAddress)

	// 4. Parse and set up the VPN WireGuard config (wg-vpn).
	vpnConf, err := readVPNConfig(cfg.VPNWGConfPath)
	if err != nil {
		return fmt.Errorf("reading VPN WireGuard config: %w", err)
	}
	vpnAddrs := netlinkAddrs(vpnConf.Addresses)
	if err := createWGIface(ifaceVPN, vpnConf.WGConfig, vpnAddrs); err != nil {
		return fmt.Errorf("creating %s: %w", ifaceVPN, err)
	}
	log.Info("VPN WireGuard interface up", "iface", ifaceVPN)

	// 5. Add routes for VPN AllowedIPs via wg-vpn.
	if err := addVPNRoutes(vpnConf); err != nil {
		return fmt.Errorf("adding VPN routes: %w", err)
	}

	// 6. NAT: masquerade traffic from the internal subnet going out wg-vpn so
	// the VPN server sees the proxy's VPN IP as the source, not the peer's internal IP.
	if err := addMasquerade(internalNet.String()); err != nil {
		return fmt.Errorf("adding MASQUERADE rule: %w", err)
	}
	log.Info("MASQUERADE rule added", "src", internalNet)

	// 7. Watch peers.conf and hot-reload peers on changes.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("watching peers file for changes", "path", peersFile)
	watchPeers(ctx, internalPrivKey)

	log.Info("shutdown complete")
	return nil
}

// createWGIface creates a WireGuard interface with the given name, configures it,
// assigns addresses, sets the MTU, and brings it up.
func createWGIface(name string, cfg *wgtypes.Config, addrs []*netlink.Addr) error {
	link := &netlink.Wireguard{LinkAttrs: netlink.LinkAttrs{Name: name}}
	if err := netlink.LinkAdd(link); err != nil {
		return fmt.Errorf("netlink.LinkAdd %s: %w", name, err)
	}

	for _, addr := range addrs {
		if err := netlink.AddrAdd(link, addr); err != nil {
			return fmt.Errorf("netlink.AddrAdd %s %s: %w", name, addr, err)
		}
	}

	wgClient, err := wgctrl.New()
	if err != nil {
		return fmt.Errorf("wgctrl.New: %w", err)
	}
	defer wgClient.Close()

	if err := wgClient.ConfigureDevice(name, *cfg); err != nil {
		return fmt.Errorf("wgctrl.ConfigureDevice %s: %w", name, err)
	}
	if err := netlink.LinkSetMTU(link, wgMTU); err != nil {
		return fmt.Errorf("netlink.LinkSetMTU %s: %w", name, err)
	}
	return netlink.LinkSetUp(link)
}

// readVPNConfig reads and parses the VPN WireGuard config file.
func readVPNConfig(path string) (*wireguard.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return wireguard.ParseConfig(data)
}

// addVPNRoutes adds a route for each AllowedIP in the VPN peer configs via wg-vpn.
func addVPNRoutes(cfg *wireguard.Config) error {
	vpnLink, err := netlink.LinkByName(ifaceVPN)
	if err != nil {
		return fmt.Errorf("looking up %s: %w", ifaceVPN, err)
	}
	for _, peer := range cfg.WGConfig.Peers {
		for _, allowed := range peer.AllowedIPs {
			allowed := allowed // avoid loop capture
			if err := netlink.RouteAdd(&netlink.Route{
				LinkIndex: vpnLink.Attrs().Index,
				Dst:       &allowed,
			}); err != nil {
				return fmt.Errorf("adding route %s via %s: %w", allowed.String(), ifaceVPN, err)
			}
		}
	}
	return nil
}

// addMasquerade adds an iptables MASQUERADE rule so traffic from the internal
// WireGuard subnet is SNATed to the proxy's VPN IP before leaving via wg-vpn.
func addMasquerade(internalSubnet string) error {
	return runIPTables("-t", "nat", "-A", "POSTROUTING",
		"-s", internalSubnet, "-o", ifaceVPN, "-j", "MASQUERADE")
}

func runIPTables(args ...string) error {
	out, err := exec.Command("iptables", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("iptables %v: %w: %s", args, err, string(out))
	}
	return nil
}

// watchPeers polls peers.conf and calls wg set to hot-reload the peer list
// whenever the file content changes.
func watchPeers(ctx context.Context, internalPrivKey wgtypes.Key) {
	var lastHash [32]byte
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(peersPollInterval):
		}

		data, err := os.ReadFile(filepath.Clean(peersFile))
		if err != nil {
			if !os.IsNotExist(err) {
				log.Error("reading peers file", "err", err)
			}
			continue
		}

		hash := sha256.Sum256(data)
		if hash == lastHash {
			continue
		}
		lastHash = hash

		if err := reloadPeers(internalPrivKey, data); err != nil {
			log.Error("reloading peers", "err", err)
			continue
		}
		log.Info("peers reloaded")
	}
}

// reloadPeers parses the peers.conf data and applies it to wg-internal via wgctrl.
func reloadPeers(privKey wgtypes.Key, data []byte) error {
	peers, err := parsePeersConf(data)
	if err != nil {
		return fmt.Errorf("parsing peers.conf: %w", err)
	}

	wgClient, err := wgctrl.New()
	if err != nil {
		return fmt.Errorf("wgctrl.New: %w", err)
	}
	defer wgClient.Close()

	// Replace the full peer list atomically.
	return wgClient.ConfigureDevice(ifaceInternal, wgtypes.Config{
		PrivateKey:   &privKey,
		ReplacePeers: true,
		Peers:        peers,
	})
}

// parsePeersConf parses the WireGuard INI-style peers.conf into wgtypes.PeerConfig entries.
func parsePeersConf(data []byte) ([]wgtypes.PeerConfig, error) {
	var peers []wgtypes.PeerConfig
	var current *wgtypes.PeerConfig

	for _, rawLine := range splitLines(string(data)) {
		line := strings.TrimSpace(rawLine)
		switch {
		case line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";"):
			continue
		case line == "[Peer]":
			if current != nil {
				peers = append(peers, *current)
			}
			current = &wgtypes.PeerConfig{}
		case current != nil && strings.HasPrefix(line, "PublicKey = "):
			key, err := wgtypes.ParseKey(strings.TrimPrefix(line, "PublicKey = "))
			if err != nil {
				return nil, fmt.Errorf("invalid peer public key: %w", err)
			}
			current.PublicKey = key
		case current != nil && strings.HasPrefix(line, "AllowedIPs = "):
			_, cidr, err := net.ParseCIDR(strings.TrimPrefix(line, "AllowedIPs = "))
			if err != nil {
				return nil, fmt.Errorf("invalid peer AllowedIPs: %w", err)
			}
			current.AllowedIPs = append(current.AllowedIPs, *cidr)
		}
	}
	if current != nil {
		peers = append(peers, *current)
	}
	return peers, nil
}

func splitLines(s string) []string {
	return strings.Split(s, "\n")
}

// netlinkAddrs converts []*net.IPNet to []*netlink.Addr for use with netlink.AddrAdd.
func netlinkAddrs(nets []*net.IPNet) []*netlink.Addr {
	addrs := make([]*netlink.Addr, len(nets))
	for i, n := range nets {
		addrs[i] = &netlink.Addr{IPNet: n}
	}
	return addrs
}
