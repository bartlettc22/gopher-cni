package mocks

import "github.com/vishvananda/netlink"

// NetlinkClient is a mock implementation of net.NetlinkClient for use in tests.
type NetlinkClient struct {
	LinkByNameFn func(name string) (netlink.Link, error)
	AddrListFn   func(link netlink.Link, family int) ([]netlink.Addr, error)
}

func (m *NetlinkClient) LinkByName(name string) (netlink.Link, error) {
	return m.LinkByNameFn(name)
}

func (m *NetlinkClient) AddrList(link netlink.Link, family int) ([]netlink.Addr, error) {
	return m.AddrListFn(link, family)
}
