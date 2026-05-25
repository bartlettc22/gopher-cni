package net

import (
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
)

func DefaultRoute(netNSPath string) (*netlink.Route, error) {
	_, netlinkHandle, err := NetNSHandles(netNSPath)
	if err != nil {
		return nil, err
	}

	routes, _ := netlinkHandle.RouteList(nil, netlink.FAMILY_V4)
	for _, r := range routes {
		if r.Dst == nil || r.Dst.String() == "0.0.0.0/0" {
			return &r, nil
		}
	}
	return nil, fmt.Errorf("no default route found")
}

func ReplaceDefaultRoute(netNSPath string, device string) error {

	_, netlinkHandle, err := NetNSHandles(netNSPath)
	if err != nil {
		return err
	}

	// Delete the default route
	routes, _ := netlinkHandle.RouteList(nil, netlink.FAMILY_V4)
	for _, r := range routes {
		if r.Dst == nil || r.Dst.String() == "0.0.0.0/0" {
			if err := netlinkHandle.RouteDel(&r); err != nil {
				return err
			}
		}
	}

	return RouteAdd(netNSPath, "0.0.0.0/0", "", device)
}

func RouteAdd(netNSPath string, destination string, gateway string, device string) error {

	_, netlinkHandle, err := NetNSHandles(netNSPath)
	if err != nil {
		return err
	}

	link, err := netlinkHandle.LinkByName(device)
	if err != nil {
		return err
	}

	route := netlink.Route{
		LinkIndex: link.Attrs().Index,
	}

	if gateway != "" {
		gwIP := net.ParseIP(gateway)
		if gwIP == nil {
			return fmt.Errorf("could not parse gateway IP %q", gateway)
		}
		route.Gw = gwIP
	} else {
		route.Scope = netlink.SCOPE_LINK
	}

	if destination != "" {
		_, dst, err := net.ParseCIDR(destination)
		if err != nil {
			return fmt.Errorf("could not add route '%s via %s dev %s': %v", destination, gateway, device, err)
		}
		route.Dst = dst
	}

	err = netlinkHandle.RouteAdd(&route)
	return err
}
