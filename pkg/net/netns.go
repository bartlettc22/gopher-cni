package net

import (
	"fmt"
	"strings"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

func NetNSHandles(NetNSPath string) (netns.NsHandle, *netlink.Handle, error) {

	netnsHandle, err := netns.GetFromPath(NetNSPath)
	if err != nil {
		// Makes the error a little more friendly if the network namespace doesn't exist
		if strings.Contains(err.Error(), "no such file or directory") {
			return -1, nil, fmt.Errorf("could not get network namespace handle for %q: namespace does not exist", NetNSPath)
		}
		return -1, nil, fmt.Errorf("could not get network namespace handle for %q: %v", NetNSPath, err)
	}

	netlinkHandle, err := netlink.NewHandleAt(netnsHandle)
	if err != nil {
		return -1, nil, fmt.Errorf("could not get container net ns netlink handle: %v", err)
	}

	return netnsHandle, netlinkHandle, nil
}
