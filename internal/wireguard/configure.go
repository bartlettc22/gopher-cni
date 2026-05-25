package wireguard

import (
	"fmt"
	"net"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type Config struct {
	WGConfig  *wgtypes.Config
	Addresses []*net.IPNet // all IPv4 addresses from the [Interface] Address field
	DNS       []net.IP     // IPv4 DNS servers from the [Interface] DNS field
}

// ProtectedNets returns the networks that must route via the WireGuard interface
// regardless of split-tunnel config: all interface addresses plus a /32 host
// route for each IPv4 DNS server.
func (c *Config) ProtectedNets() []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(c.Addresses)+len(c.DNS))
	nets = append(nets, c.Addresses...)
	for _, ip := range c.DNS {
		if ip.To4() != nil {
			nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(32, 32)})
		}
	}
	return nets
}

func ConfigureWireguard(linkName string, cfg *Config) error {
	wgClient, err := wgctrl.New()
	if err != nil {
		return fmt.Errorf("could not create wgctrl client: %v", err)
	}
	defer wgClient.Close()

	if err := wgClient.ConfigureDevice(linkName, *cfg.WGConfig); err != nil {
		return fmt.Errorf("could not configure wireguard link: %v", err)
	}

	return nil
}
