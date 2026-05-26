package sidecar

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"

	"github.com/bartlettc22/gopher-cni/internal/net/mocks"
	"github.com/vishvananda/netlink"
)

type fakeLink struct {
	netlink.LinkAttrs
}

func (f *fakeLink) Attrs() *netlink.LinkAttrs { return &f.LinkAttrs }
func (f *fakeLink) Type() string              { return "fake" }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestValidate(t *testing.T) {
	upLink := &fakeLink{netlink.LinkAttrs{Flags: net.FlagUp}}
	downLink := &fakeLink{netlink.LinkAttrs{Flags: 0}}
	someAddr := []netlink.Addr{{IPNet: mustCIDR("10.0.0.1/24")}}

	tests := []struct {
		name      string
		mock      mocks.NetlinkClient
		wantErr   bool
		errSubstr string
	}{
		{
			name: "valid interface",
			mock: mocks.NetlinkClient{
				LinkByNameFn: func(name string) (netlink.Link, error) { return upLink, nil },
				AddrListFn:   func(link netlink.Link, family int) ([]netlink.Addr, error) { return someAddr, nil },
			},
		},
		{
			name: "interface not found",
			mock: mocks.NetlinkClient{
				LinkByNameFn: func(name string) (netlink.Link, error) {
					return nil, netlink.LinkNotFoundError{}
				},
			},
			wantErr:   true,
			errSubstr: "interface not found",
		},
		{
			name: "LinkByName error",
			mock: mocks.NetlinkClient{
				LinkByNameFn: func(name string) (netlink.Link, error) {
					return nil, fmt.Errorf("kernel error")
				},
			},
			wantErr:   true,
			errSubstr: "error detecting interface",
		},
		{
			name: "interface is down",
			mock: mocks.NetlinkClient{
				LinkByNameFn: func(name string) (netlink.Link, error) { return downLink, nil },
			},
			wantErr:   true,
			errSubstr: "is not up",
		},
		{
			name: "AddrList error",
			mock: mocks.NetlinkClient{
				LinkByNameFn: func(name string) (netlink.Link, error) { return upLink, nil },
				AddrListFn:   func(link netlink.Link, family int) ([]netlink.Addr, error) { return nil, fmt.Errorf("netlink error") },
			},
			wantErr:   true,
			errSubstr: "error listing addresses",
		},
		{
			name: "no IP address assigned",
			mock: mocks.NetlinkClient{
				LinkByNameFn: func(name string) (netlink.Link, error) { return upLink, nil },
				AddrListFn:   func(link netlink.Link, family int) ([]netlink.Addr, error) { return []netlink.Addr{}, nil },
			},
			wantErr:   true,
			errSubstr: "no IP address assigned",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate(discardLogger(), &tt.mock, "gcni0")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("expected error containing %q, got: %v", tt.errSubstr, err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func mustCIDR(s string) *net.IPNet {
	_, ipnet, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return ipnet
}
