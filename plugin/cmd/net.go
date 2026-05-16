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
func setupWGViaHost(netNSName, wgIfaceName string, wgConfig *wgtypes.Config, wgAddr *netlink.Addr, log *slog.Logger) error {

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

	// _, err = net.AddNSLinkFromCurrentNS(&net.LinkConfig{
	// 	Name:      wgIface,
	// 	LinkType:  net.LINK_TYPE_WIREGUARD,
	// 	NetNSPath: args.Netns,
	// 	Addresses: []*netlink.Addr{addr},
	// 	// MTU set based on this blog: https://keremerkan.net/posts/wireguard-mtu-fixes/
	// 	// May need to be adjusted in the future
	// 	MTU: 1280,
	// 	ConfigureFunc: func(wglink *netlink.Link) error {
	// 		err := wireguard.ConfigureWireguard((*wglink).Attrs().Name, wgConfig)
	// 		if err != nil {
	// 			return fmt.Errorf("%s error configuring wireguard: %v", logPrefix, err)
	// 		}
	// 		return nil
	// 	},
	// })
	// if err != nil {
	// 	return fmt.Errorf("%s error creating network link: %v", logPrefix, err)
	// }

	// Perform remaining operations in the destination (container) network namespace
	return containerNS.Do(func(_ ns.NetNS) error {
		// linkConf := &net.LinkConfig{
		// 	Name:      wgIface,
		// 	LinkType:  net.LINK_TYPE_WIREGUARD,
		// 	NetNSPath: args.Netns,
		// 	Addresses: []*netlink.Addr{addr},
		// 	// MTU set based on this blog: https://keremerkan.net/posts/wireguard-mtu-fixes/
		// 	// May need to be adjusted in the future
		// 	MTU: 1280,
		// 	ConfigureFunc: func(wglink *netlink.Link) error {
		// 		err := wireguard.ConfigureWireguard((*wglink).Attrs().Name, wgConfig)
		// 		if err != nil {
		// 			return e("failed to configure wireguard", err)
		// 		}
		// 		return nil
		// 	},
		// }
		// netlink.LinkAdd(netlink.Ne)

		// wgLink, err := netlink.LinkByName(wgIfaceName)
		// if err != nil {
		// 	return fmt.Errorf("failed to get link: %v", err)
		// }

		// 4. Assign IP address to the wireguard interface
		if err := netlink.AddrAdd(wgLink, wgAddr); err != nil {
			return fmt.Errorf("failed to add addr %q: %v", wgAddr.String(), err)
		}

		// 5. Configure WireGuard specifics (keys, peers, etc.)
		client, err := wgctrl.New()
		if err != nil {
			return fmt.Errorf("failed to create wgctrl client: %v", err)
		}
		defer client.Close()

		if err := client.ConfigureDevice(wgIfaceName, *wgConfig); err != nil {
			return fmt.Errorf("failed to configure wireguard interface%q: %v", wgIfaceName, err)
		}

		// 6. Set MTU before bringing the interface up.
		if err := netlink.LinkSetMTU(wgLink, wgMTU); err != nil {
			return fmt.Errorf("failed to set MTU on wireguard interface: %v", err)
		}

		// 7. Bring the interface up
		if err := netlink.LinkSetUp(wgLink); err != nil {
			return fmt.Errorf("failed to set link up: %v", err)
		}

		// Obtain the current default interface

		// Obtain the current default route from the previous results
		// var defaultRoute *cnitypes.Route
		// for _, route := range conf.PrevResultV1.Routes {
		// 	if route.Dst.String() == "0.0.0.0/0" {
		// 		defaultRoute = route
		// 	}
		// }
		// if defaultRoute == nil {
		// 	return fmt.Errorf("failed to find default route in previous results")
		// }

		// defaultRoute, err := mynet.DefaultRoute(netNSName)
		// if err != nil {
		// 	return fmt.Errorf("failed to get default route: %w", err)
		// }
		// log.Debug("default route", "route", defaultRoute.LinkIndex, "gw", defaultRoute.Gw)

		// Add routes for peer endpoints through the current default interface
		// for _, peer := range wgConfig.Peers {
		// 	log.Debug("adding peer route", "peer", peer.Endpoint.IP.String())
		// 	_, peerIP, err := net.ParseCIDR(peer.Endpoint.IP.String() + "/32")
		// 	if err != nil {
		// 		return fmt.Errorf("failed to parse peer IP: %w", err)
		// 	}
		// 	err = netlink.RouteAdd(&netlink.Route{
		// 		LinkIndex: defaultRoute.LinkIndex,
		// 		Dst:       peerIP,
		// 		Gw:        defaultRoute.Gw,
		// 	})
		// 	if err != nil {
		// 		return fmt.Errorf("failed to add route for peer %q: %v", peer.Endpoint.IP.String(), err)
		// 	}
		// }

		// _, defaultDst, _ := net.ParseCIDR("10.2.0.1/32")
		// route := &netlink.Route{
		// 	LinkIndex: wgLink.Attrs().Index,
		// 	Dst:       defaultDst,
		// 	Scope:     netlink.SCOPE_LINK,
		// }

		// // Replace the default route
		// if err := netlink.RouteReplace(route); err != nil {
		// 	return e("failed to replace route", err)
		// }
		// netlink.RouteReplace()

		_, defaultDst, err := net.ParseCIDR("0.0.0.0/0")
		if err != nil {
			return fmt.Errorf("failed to parse default route CIDR: %v", err)
		}
		newDefaultRoute := netlink.Route{
			LinkIndex: wgLink.Attrs().Index,
			Dst:       defaultDst,
			Scope:     netlink.SCOPE_LINK,
		}
		if err := netlink.RouteReplace(&newDefaultRoute); err != nil {
			return fmt.Errorf("failed to replace default route: %v", err)
		}
		// // Replace the default route
		// if err := mynet.ReplaceDefaultRoute(args.Netns, ifaceName); err != nil {
		// 	return fmt.Errorf("failed to replace default route: %v", err)
		// }

		return nil
	})
	// return err
}

// Sets up a Wireguard interface directly inside the container network namespace.
// This creates a Wireguard interface that routes through the container's original interface.
// The original interface must be left intact for connectivity.
func setupWGViaContainer(netNSName, wgIfaceName string, wgConfig *wgtypes.Config, wgAddr *netlink.Addr, log *slog.Logger) error {

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

		// wgLink, err := netlink.LinkByName(wgIfaceName)
		// if err != nil {
		// 	return fmt.Errorf("failed to get link: %v", err)
		// }

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

		// 4. Finish setting up the wireguard interface
		return setupWG(wgLink, wgIfaceName, wgConfig, wgAddr)

		// _, defaultDst, _ := net.ParseCIDR("10.2.0.1/32")
		// route := &netlink.Route{
		// 	LinkIndex: wgLink.Attrs().Index,
		// 	Dst:       defaultDst,
		// 	Scope:     netlink.SCOPE_LINK,
		// }

		// // Replace the default route
		// if err := netlink.RouteReplace(route); err != nil {
		// 	return e("failed to replace route", err)
		// }
		// netlink.RouteReplace()

		// // Replace the default route
		// if err := mynet.ReplaceDefaultRoute(args.Netns, ifaceName); err != nil {
		// 	return fmt.Errorf("failed to replace default route: %v", err)
		// }

		// return nil
	})
	// return err
}

// setupWG sets up wireguard interface in the container network namespace.
// This is always run in containerNS.Do()
func setupWG(wgLink *netlink.Wireguard, wgIfaceName string, wgConfig *wgtypes.Config, wgAddr *netlink.Addr) error {

	// 1. Assign IP address to the wireguard interface
	if err := netlink.AddrAdd(wgLink, wgAddr); err != nil {
		return fmt.Errorf("failed to add addr %q: %v", wgAddr.String(), err)
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

	// 5. Set wireguard interface as default route
	_, defaultDst, err := net.ParseCIDR("0.0.0.0/0")
	if err != nil {
		return fmt.Errorf("failed to parse default route CIDR: %v", err)
	}
	newDefaultRoute := netlink.Route{
		LinkIndex: wgLink.Attrs().Index,
		Dst:       defaultDst,
		Scope:     netlink.SCOPE_LINK,
	}
	if err := netlink.RouteReplace(&newDefaultRoute); err != nil {
		return fmt.Errorf("failed to replace default route: %v", err)
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
