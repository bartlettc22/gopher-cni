package net

import "github.com/vishvananda/netlink"

// NetlinkClient is the interface for netlink operations used by this package.
type NetlinkClient interface {
	LinkByName(name string) (netlink.Link, error)
	AddrList(link netlink.Link, family int) ([]netlink.Addr, error)
}
