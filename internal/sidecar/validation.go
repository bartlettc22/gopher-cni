package sidecar

import (
	"errors"
	"fmt"
	"log/slog"
	"net"

	"github.com/bartlettc22/gopher-cni/internal/cni"
	"github.com/bartlettc22/gopher-cni/internal/logging"
	pkgnet "github.com/bartlettc22/gopher-cni/internal/net"
	"github.com/vishvananda/netlink"
)

func RunInitValidation() {
	log := slog.With("component", "init-validation")
	log.Info("starting init validation")

	if err := validate(log, &netlink.Handle{}, cni.InterfaceName); err != nil {
		logging.Fatal(err)
	}

	log.Info("init validation passed")
}

func validate(log *slog.Logger, nl pkgnet.NetlinkClient, ifaceName string) error {
	log.Info("detecting interface", "name", ifaceName)
	link, err := nl.LinkByName(ifaceName)
	if err != nil {
		if errors.Is(err, netlink.LinkNotFoundError{}) {
			return fmt.Errorf("interface not found: %w", err)
		}
		return fmt.Errorf("error detecting interface %q: %w", ifaceName, err)
	}
	if link == nil {
		return fmt.Errorf("interface %q not found, no error returned", ifaceName)
	}
	log.Info("interface found", "name", ifaceName)

	if link.Attrs().Flags&net.FlagUp == 0 {
		return fmt.Errorf("interface %q is not up", ifaceName)
	}
	log.Info("interface is up", "name", ifaceName)

	addrs, err := nl.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return fmt.Errorf("error listing addresses for interface %q: %w", ifaceName, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("interface %q has no IP address assigned", ifaceName)
	}
	log.Info("interface has IP address", "name", ifaceName, "address", addrs[0].IP.String())

	return nil
}
