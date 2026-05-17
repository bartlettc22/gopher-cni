package wireguard

import (
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

var testPrivateKey string
var testPublicKey string

func init() {
	// To avoid having secret-looking keys in the tests, generate a new key pair
	privateKey, _ := wgtypes.GeneratePrivateKey()
	testPrivateKey = privateKey.String()
	testPublicKey = privateKey.PublicKey().String()
}

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(*testing.T, *Config)
	}{
		{
			name: "basic interface config",
			input: `[Interface]
PrivateKey = ` + testPrivateKey + `
ListenPort = 51820`,
			wantErr: false,
			check: func(t *testing.T, cfg *Config) {
				if cfg.WGConfig.PrivateKey == nil {
					t.Error("expected private key to be set")
				}
				if cfg.WGConfig.ListenPort == nil || *cfg.WGConfig.ListenPort != 51820 {
					t.Error("expected listen port to be 51820")
				}
			},
		},
		{
			name: "interface with address",
			input: `[Interface]
PrivateKey = ` + testPrivateKey + `
Address = 10.0.0.1/24
ListenPort = 51820`,
			wantErr: false,
			check: func(t *testing.T, cfg *Config) {
				if cfg.WGConfig.PrivateKey == nil {
					t.Error("expected private key to be set")
				}
				if cfg.Address == nil {
					t.Error("expected address to be set")
				}
				// net.ParseCIDR normalizes the IP to the network address
				if cfg.Address != nil && cfg.Address.String() != "10.0.0.0/24" {
					t.Errorf("expected address 10.0.0.0/24, got %s", cfg.Address.String())
				}
			},
		},
		{
			name: "single peer",
			input: `[Interface]
PrivateKey = ` + testPrivateKey + `
ListenPort = 51820

[Peer]
PublicKey = ` + testPublicKey + `
AllowedIPs = 10.0.0.2/32
Endpoint = 192.168.1.1:51820`,
			wantErr: false,
			check: func(t *testing.T, cfg *Config) {
				if len(cfg.WGConfig.Peers) != 1 {
					t.Fatalf("expected 1 peer, got %d", len(cfg.WGConfig.Peers))
				}
				peer := cfg.WGConfig.Peers[0]
				if peer.Endpoint == nil {
					t.Error("expected endpoint to be set")
				}
				if len(peer.AllowedIPs) != 1 {
					t.Errorf("expected 1 allowed IP, got %d", len(peer.AllowedIPs))
				}
			},
		},
		{
			name: "multiple peers",
			input: `[Interface]
PrivateKey = ` + testPrivateKey + `

[Peer]
PublicKey = ` + testPublicKey + `
AllowedIPs = 10.0.0.2/32

[Peer]
PublicKey = ` + testPublicKey + `
AllowedIPs = 10.0.0.3/32`,
			wantErr: false,
			check: func(t *testing.T, cfg *Config) {
				if len(cfg.WGConfig.Peers) != 2 {
					t.Fatalf("expected 2 peers, got %d", len(cfg.WGConfig.Peers))
				}
			},
		},
		{
			name: "peer with persistent keepalive",
			input: `[Interface]
PrivateKey = ` + testPrivateKey + `

[Peer]
PublicKey = ` + testPublicKey + `
AllowedIPs = 10.0.0.2/32
PersistentKeepalive = 25`,
			wantErr: false,
			check: func(t *testing.T, cfg *Config) {
				if len(cfg.WGConfig.Peers) != 1 {
					t.Fatalf("expected 1 peer, got %d", len(cfg.WGConfig.Peers))
				}
				peer := cfg.WGConfig.Peers[0]
				if peer.PersistentKeepaliveInterval == nil {
					t.Fatal("expected keepalive to be set")
				}
				expected := 25 * time.Second
				if *peer.PersistentKeepaliveInterval != expected {
					t.Errorf("expected keepalive %v, got %v", expected, *peer.PersistentKeepaliveInterval)
				}
			},
		},
		{
			name: "multiple allowed IPs",
			input: `[Interface]
PrivateKey = ` + testPrivateKey + `

[Peer]
PublicKey = ` + testPublicKey + `
AllowedIPs = 10.0.0.2/32, 10.0.0.3/32, 192.168.1.0/24`,
			wantErr: false,
			check: func(t *testing.T, cfg *Config) {
				if len(cfg.WGConfig.Peers) != 1 {
					t.Fatalf("expected 1 peer, got %d", len(cfg.WGConfig.Peers))
				}
				peer := cfg.WGConfig.Peers[0]
				if len(peer.AllowedIPs) != 3 {
					t.Errorf("expected 3 allowed IPs, got %d", len(peer.AllowedIPs))
				}
			},
		},
		{
			name: "config with comments",
			input: `# This is a comment
[Interface]
# Private key for the interface
PrivateKey = ` + testPrivateKey + `
ListenPort = 51820

# Peer configuration
[Peer]
PublicKey = ` + testPublicKey + `
AllowedIPs = 10.0.0.2/32`,
			wantErr: false,
			check: func(t *testing.T, cfg *Config) {
				if cfg.WGConfig.PrivateKey == nil {
					t.Error("expected private key to be set")
				}
				if len(cfg.WGConfig.Peers) != 1 {
					t.Fatalf("expected 1 peer, got %d", len(cfg.WGConfig.Peers))
				}
			},
		},
		{
			name: "multiple comma-separated addresses picks first ipv4",
			input: `[Interface]
PrivateKey = ` + testPrivateKey + `
Address = 10.0.0.1/24, 10.0.0.2/32`,
			wantErr: false,
			check: func(t *testing.T, cfg *Config) {
				if cfg.Address == nil {
					t.Fatal("expected address to be set")
				}
				if cfg.Address.String() != "10.0.0.0/24" {
					t.Errorf("expected first address 10.0.0.0/24, got %s", cfg.Address.String())
				}
			},
		},
		{
			name: "ipv6 address before ipv4 picks first ipv4",
			input: `[Interface]
PrivateKey = ` + testPrivateKey + `
Address = fd00::1/128, 10.0.0.1/24`,
			wantErr: false,
			check: func(t *testing.T, cfg *Config) {
				if cfg.Address == nil {
					t.Fatal("expected address to be set")
				}
				if cfg.Address.String() != "10.0.0.0/24" {
					t.Errorf("expected first IPv4 address 10.0.0.0/24, got %s", cfg.Address.String())
				}
			},
		},
		{
			name: "invalid private key",
			input: `[Interface]
PrivateKey = invalid-key`,
			wantErr: true,
		},
		{
			name: "invalid listen port",
			input: `[Interface]
PrivateKey = ` + testPrivateKey + `
ListenPort = not-a-number`,
			wantErr: true,
		},
		{
			name: "invalid allowed IP",
			input: `[Interface]
PrivateKey = ` + testPrivateKey + `

[Peer]
PublicKey = ` + testPublicKey + `
AllowedIPs = invalid-ip`,
			wantErr: true,
		},
		{
			name: "port out of range",
			input: `[Interface]
PrivateKey = ` + testPrivateKey + `
ListenPort = 70000`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseConfig([]byte(tt.input))

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestParseConfigEmpty(t *testing.T) {
	cfg, err := ParseConfig([]byte(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config, got nil")
	}
}

func TestParseConfigWhitespaceAndEmptyLines(t *testing.T) {
	input := `

[Interface]

PrivateKey = ` + testPrivateKey + `


[Peer]

PublicKey = ` + testPublicKey + `
AllowedIPs = 10.0.0.2/32

`
	cfg, err := ParseConfig([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.WGConfig.PrivateKey == nil {
		t.Error("expected private key to be set")
	}
	if len(cfg.WGConfig.Peers) != 1 {
		t.Errorf("expected 1 peer, got %d", len(cfg.WGConfig.Peers))
	}
}
