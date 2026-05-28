package cmd

import (
	"fmt"
	"log/slog"
	"net"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// wgMTU is the MTU set on every WireGuard interface.
// WireGuard adds 80 bytes of overhead over IPv4 UDP (20 IP + 8 UDP + 32 WireGuard
// header + 16 auth tag + 4 type), so the maximum safe payload MTU is 1420.
const wgMTU = 1420

// Sets up a Wireguard interface inside the container by adding it to the host network namespace
// then moving it to the desired namespace.  This creates an isolated Wireguard interface inside the container
// that is not reliant/connected to the container's original interface.  The original interface is left intact
// but could safely be removed if not needed.
func setupWGViaHost(netNSName, wgIfaceName string, wgConfig *wgtypes.Config, wgAddrs []*netlink.Addr, protectedNets []*net.IPNet, splitTunnelCIDRs []*net.IPNet, log *slog.Logger) error {

	// 1. Add the wireguard link (interface) to the current (host) network namespace
	wgLink := &netlink.Wireguard{
		LinkAttrs: netlink.LinkAttrs{
			Name: wgIfaceName,
		},
	}
	if err := netlink.LinkAdd(wgLink); err != nil {
		return fmt.Errorf("failed to add wireguard link to host network namespace: %v", err)
	}

	// 2. Obtain handle to the destination (container) network namespace
	containerNS, err := ns.GetNS(netNSName)
	if err != nil {
		return fmt.Errorf("failed to get container network namespace: %w", err)
	}

	// 3. Move the link into the destination network namespace
	if err := netlink.LinkSetNsFd(wgLink, (int)(containerNS.Fd())); err != nil {
		return fmt.Errorf("could not move network link %q into network namespace %q: %v", wgLink.Attrs().Name, netNSName, err)
	}

	// Perform remaining operations in the destination (container) network namespace
	return containerNS.Do(func(_ ns.NetNS) error {
		// 4. Capture the original default route before WireGuard replaces it
		origDefault, err := defaultRoute()
		if err != nil {
			return fmt.Errorf("failed to get default route: %w", err)
		}

		// 5. Configure the WireGuard interface and set it as the default route
		if err := setupWG(wgLink, wgIfaceName, wgConfig, wgAddrs); err != nil {
			return err
		}
		if err := replaceDefaultRoute(wgLink); err != nil {
			return err
		}
		if err := addProtectedRoutes(wgLink, protectedNets); err != nil {
			return err
		}

		// 6. Policy route: replies to eth0-addressed traffic go back out eth0, not wg0.
		if err := addEth0ReturnRoute(origDefault); err != nil {
			return err
		}

		return addSplitTunnelRoutes(origDefault, splitTunnelCIDRs)
	})
}

// Sets up a Wireguard interface directly inside the container network namespace.
// This creates a Wireguard interface that routes through the container's original interface.
// The original interface must be left intact for connectivity.
func setupWGViaContainer(netNSName, wgIfaceName string, wgConfig *wgtypes.Config, wgAddrs []*netlink.Addr, protectedNets []*net.IPNet, splitTunnelCIDRs []*net.IPNet, log *slog.Logger) error {

	// 1. Obtain handle to the destination (container) network namespace
	containerNS, err := ns.GetNS(netNSName)
	if err != nil {
		return fmt.Errorf("failed to get container network namespace: %w", err)
	}

	// Perform all operations in the destination (container) network namespace
	return containerNS.Do(func(_ ns.NetNS) error {

		// 2. Add the wireguard link (interface) to the container network namespace
		wgLink := &netlink.Wireguard{
			LinkAttrs: netlink.LinkAttrs{
				Name: wgIfaceName,
			},
		}
		if err := netlink.LinkAdd(wgLink); err != nil {
			return fmt.Errorf("failed to add wireguard link to container network namespace: %v", err)
		}

		// 3. Add routes for peer endpoints through the current default interface
		// This ensure tunnel traffic can exit the container
		defaultRoute, err := defaultRoute()
		if err != nil {
			return fmt.Errorf("failed to get default route: %w", err)
		}
		for _, peer := range wgConfig.Peers {
			log.Debug("adding peer route", "peer", peer.Endpoint.IP.String())
			_, peerIP, err := net.ParseCIDR(peer.Endpoint.IP.String() + "/32")
			if err != nil {
				return fmt.Errorf("failed to parse peer IP: %w", err)
			}
			err = netlink.RouteAdd(&netlink.Route{
				LinkIndex: defaultRoute.LinkIndex,
				Dst:       peerIP,
				Gw:        defaultRoute.Gw,
			})
			if err != nil {
				return fmt.Errorf("failed to add route for peer %q: %v", peer.Endpoint.IP.String(), err)
			}
		}

		// 4. Configure the WireGuard interface and set it as the default route
		if err := setupWG(wgLink, wgIfaceName, wgConfig, wgAddrs); err != nil {
			return err
		}
		if err := replaceDefaultRoute(wgLink); err != nil {
			return err
		}
		if err := addProtectedRoutes(wgLink, protectedNets); err != nil {
			return err
		}

		// 5. Policy route: replies to eth0-addressed traffic go back out eth0, not wg0.
		if err := addEth0ReturnRoute(defaultRoute); err != nil {
			return err
		}

		return addSplitTunnelRoutes(defaultRoute, splitTunnelCIDRs)
	})
}

// setupWG configures a WireGuard interface: assigns the address, loads keys/peers,
// sets the MTU, and brings the link up. Route management is left to the caller.
// Must be called from within the target network namespace.
func setupWG(wgLink *netlink.Wireguard, wgIfaceName string, wgConfig *wgtypes.Config, wgAddrs []*netlink.Addr) error {

	// 1. Assign all IPv4 addresses to the wireguard interface
	for _, addr := range wgAddrs {
		if err := netlink.AddrAdd(wgLink, addr); err != nil {
			return fmt.Errorf("failed to add addr %q: %v", addr.String(), err)
		}
	}

	// 2. Configure WireGuard specifics (keys, peers, etc.)
	client, err := wgctrl.New()
	if err != nil {
		return fmt.Errorf("failed to create wgctrl client: %v", err)
	}
	defer client.Close()

	if err := client.ConfigureDevice(wgIfaceName, *wgConfig); err != nil {
		return fmt.Errorf("failed to configure wireguard interface%q: %v", wgIfaceName, err)
	}

	// 3. Set MTU before bringing the interface up.
	if err := netlink.LinkSetMTU(wgLink, wgMTU); err != nil {
		return fmt.Errorf("failed to set MTU on wireguard interface: %v", err)
	}

	// 4. Bring the interface up
	if err := netlink.LinkSetUp(wgLink); err != nil {
		return fmt.Errorf("failed to set link up: %v", err)
	}

	return nil
}

func replaceDefaultRoute(wgLink *netlink.Wireguard) error {
	_, defaultDst, err := net.ParseCIDR("0.0.0.0/0")
	if err != nil {
		return fmt.Errorf("failed to parse default route CIDR: %v", err)
	}
	if err := netlink.RouteReplace(&netlink.Route{
		LinkIndex: wgLink.Attrs().Index,
		Dst:       defaultDst,
		Scope:     netlink.SCOPE_LINK,
	}); err != nil {
		return fmt.Errorf("failed to replace default route: %v", err)
	}
	return nil
}

// addProtectedRoutes installs explicit routes for WireGuard addresses and DNS servers
// via the WireGuard interface, ensuring they are not captured by any split-tunnel routes.
func addProtectedRoutes(wgLink *netlink.Wireguard, nets []*net.IPNet) error {
	for _, n := range nets {
		if err := netlink.RouteAdd(&netlink.Route{
			LinkIndex: wgLink.Attrs().Index,
			Dst:       n,
			Scope:     netlink.SCOPE_LINK,
		}); err != nil {
			return fmt.Errorf("failed to add protected route for %s: %v", n, err)
		}
	}
	return nil
}

func addSplitTunnelRoutes(origDefault *netlink.Route, cidrs []*net.IPNet) error {
	for _, cidr := range cidrs {
		if err := netlink.RouteAdd(&netlink.Route{
			LinkIndex: origDefault.LinkIndex,
			Dst:       cidr,
			Gw:        origDefault.Gw,
		}); err != nil {
			return fmt.Errorf("failed to add split-tunnel route for %s: %v", cidr, err)
		}
	}
	return nil
}

func defaultRoute() (*netlink.Route, error) {
	routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		return nil, fmt.Errorf("failed to get default route: %w", err)
	}
	for _, r := range routes {
		if r.Dst == nil || r.Dst.String() == "0.0.0.0/0" {
			return &r, nil
		}
	}
	return nil, fmt.Errorf("no default route found")
}

const eth0ReturnTable = 100

// addEth0ReturnRoute installs source-based policy routing to fix asymmetric routing
// after the default route is replaced by wg0. Without this, replies to traffic that
// arrived on eth0 are sent back out wg0, which the remote drops. We mirror the
// original default route into a private table and add an ip rule per eth0 address so
// those replies are looked up in that table and exit via eth0.
func addEth0ReturnRoute(origDefault *netlink.Route) error {
	eth0, err := netlink.LinkByIndex(origDefault.LinkIndex)
	if err != nil {
		return fmt.Errorf("failed to get eth0 link (index %d): %w", origDefault.LinkIndex, err)
	}

	addrs, err := netlink.AddrList(eth0, netlink.FAMILY_V4)
	if err != nil {
		return fmt.Errorf("failed to list eth0 addresses: %w", err)
	}

	// Preserve Gw and Scope from the original so the route is valid whether it
	// used a gateway IP or was link-scoped (on-link).
	_, defaultDst, _ := net.ParseCIDR("0.0.0.0/0")
	if err := netlink.RouteAdd(&netlink.Route{
		Table:     eth0ReturnTable,
		LinkIndex: origDefault.LinkIndex,
		Dst:       defaultDst,
		Gw:        origDefault.Gw,
		Scope:     origDefault.Scope,
	}); err != nil {
		return fmt.Errorf("failed to add eth0 return route to table %d: %w", eth0ReturnTable, err)
	}

	for _, addr := range addrs {
		ip := addr.IP.To4()
		if ip == nil {
			continue
		}
		rule := netlink.NewRule()
		rule.Table = eth0ReturnTable
		rule.Src = &net.IPNet{IP: ip, Mask: net.CIDRMask(32, 32)}
		if err := netlink.RuleAdd(rule); err != nil {
			return fmt.Errorf("failed to add ip rule for eth0 address %s: %w", ip, err)
		}
	}

	return nil
}
