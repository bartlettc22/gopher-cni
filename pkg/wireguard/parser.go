package wireguard

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/bartlettc22/gopher-cni/pkg/utils"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)


// ParseConfig parses a WireGuard INI-style configuration file and returns a Config
func ParseConfig(data []byte) (*Config, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))

	wgConfig := &wgtypes.Config{}
	var addresses []*net.IPNet
	var dns []net.IP
	var currentPeer *wgtypes.PeerConfig
	var section string

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		// Check for section headers
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			// Save current peer if we were building one
			if currentPeer != nil {
				wgConfig.Peers = append(wgConfig.Peers, *currentPeer)
				currentPeer = nil
			}

			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))

			if section == "peer" {
				currentPeer = &wgtypes.PeerConfig{}
			}
			continue
		}

		// Parse key-value pairs
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("line %d: invalid format, expected key=value", lineNum)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch section {
		case "interface":
			if err := parseInterfaceKey(wgConfig, &addresses, &dns, key, value, lineNum); err != nil {
				return nil, err
			}
		case "peer":
			if currentPeer == nil {
				return nil, fmt.Errorf("line %d: peer configuration outside [Peer] section", lineNum)
			}
			if err := parsePeerKey(currentPeer, key, value, lineNum); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("line %d: unknown section [%s]", lineNum, section)
		}
	}

	// Add the last peer if there is one
	if currentPeer != nil {
		wgConfig.Peers = append(wgConfig.Peers, *currentPeer)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading config: %w", err)
	}

	return &Config{
		WGConfig:  wgConfig,
		Addresses: addresses,
		DNS:       dns,
	}, nil
}

func parseInterfaceKey(config *wgtypes.Config, addresses *[]*net.IPNet, dns *[]net.IP, key, value string, lineNum int) error {
	switch strings.ToLower(key) {
	case "privatekey":
		privateKey, err := wgtypes.ParseKey(value)
		if err != nil {
			return fmt.Errorf("line %d: invalid private key: %w", lineNum, err)
		}
		config.PrivateKey = &privateKey

	case "listenport":
		port, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("line %d: invalid listen port: %w", lineNum, err)
		}
		if port < 0 || port > 65535 {
			return fmt.Errorf("line %d: listen port out of range: %d", lineNum, port)
		}
		config.ListenPort = &port

	case "fwmark":
		fwmark, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("line %d: invalid fwmark: %w", lineNum, err)
		}
		config.FirewallMark = &fwmark

	case "address":
		// TODO: support IPv6 addresses
		parsed, err := utils.ParseIPOrCIDRList(value)
		if err != nil {
			return fmt.Errorf("line %d: invalid address: %w", lineNum, err)
		}
		for _, n := range parsed {
			if n.IP.To4() != nil {
				*addresses = append(*addresses, n)
			}
		}

	case "dns":
		parsed, err := utils.ParseIPOrCIDRList(value)
		if err != nil {
			return fmt.Errorf("line %d: invalid DNS server: %w", lineNum, err)
		}
		for _, n := range parsed {
			*dns = append(*dns, n.IP)
		}

	default:
		// Ignore unknown keys for forward compatibility
	}

	return nil
}

func parsePeerKey(peer *wgtypes.PeerConfig, key, value string, lineNum int) error {
	switch strings.ToLower(key) {
	case "publickey":
		publicKey, err := wgtypes.ParseKey(value)
		if err != nil {
			return fmt.Errorf("line %d: invalid public key: %w", lineNum, err)
		}
		peer.PublicKey = publicKey

	case "presharedkey":
		presharedKey, err := wgtypes.ParseKey(value)
		if err != nil {
			return fmt.Errorf("line %d: invalid preshared key: %w", lineNum, err)
		}
		peer.PresharedKey = &presharedKey

	case "endpoint":
		endpoint, err := net.ResolveUDPAddr("udp", value)
		if err != nil {
			return fmt.Errorf("line %d: invalid endpoint: %w", lineNum, err)
		}
		peer.Endpoint = endpoint

	case "persistentkeepalive":
		keepalive, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("line %d: invalid persistent keepalive: %w", lineNum, err)
		}
		if keepalive < 0 {
			return fmt.Errorf("line %d: persistent keepalive cannot be negative", lineNum)
		}
		duration := time.Duration(keepalive) * time.Second
		peer.PersistentKeepaliveInterval = &duration

	case "allowedips":
		parsed, err := utils.ParseIPOrCIDRList(value)
		if err != nil {
			return fmt.Errorf("line %d: invalid allowed IP: %w", lineNum, err)
		}
		for _, n := range parsed {
			peer.AllowedIPs = append(peer.AllowedIPs, *n)
		}

	default:
		// Ignore unknown keys for forward compatibility
	}

	return nil
}
