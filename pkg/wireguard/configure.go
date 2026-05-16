package wireguard

import (
	"fmt"
	"net"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type Config struct {
	WGConfig *wgtypes.Config
	Address  *net.IPNet
	DNS      []net.IP // DNS servers from the Interface section
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
