package utils

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseIPOrCIDRList(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantNets  []string // expected String() of each *net.IPNet
		wantError string   // substring expected in error; empty means no error
	}{
		// Empty / whitespace
		{
			name:     "empty string returns nil",
			input:    "",
			wantNets: nil,
		},
		{
			name:     "only commas returns nil",
			input:    ",,,",
			wantNets: nil,
		},
		{
			name:     "only whitespace returns nil",
			input:    "   ",
			wantNets: nil,
		},

		// Plain IPv4 addresses → /32 host routes
		{
			name:     "single IPv4 address",
			input:    "10.0.0.1",
			wantNets: []string{"10.0.0.1/32"},
		},
		{
			name:     "multiple IPv4 addresses",
			input:    "10.0.0.1, 10.0.0.2",
			wantNets: []string{"10.0.0.1/32", "10.0.0.2/32"},
		},
		{
			name:     "IPv4 address with extra whitespace",
			input:    "  1.1.1.1  ,  8.8.8.8  ",
			wantNets: []string{"1.1.1.1/32", "8.8.8.8/32"},
		},

		// Plain IPv6 addresses → /128 host routes
		{
			name:     "single IPv6 address",
			input:    "2001:db8::1",
			wantNets: []string{"2001:db8::1/128"},
		},
		{
			name:     "multiple IPv6 addresses",
			input:    "2001:db8::1, ::1",
			wantNets: []string{"2001:db8::1/128", "::1/128"},
		},

		// CIDR blocks
		{
			name:     "single IPv4 CIDR",
			input:    "10.0.0.0/8",
			wantNets: []string{"10.0.0.0/8"},
		},
		{
			name:     "single IPv4 /32 CIDR",
			input:    "10.2.0.2/32",
			wantNets: []string{"10.2.0.2/32"},
		},
		{
			name:     "CIDR normalizes host bits",
			input:    "10.0.0.1/24",
			wantNets: []string{"10.0.0.0/24"},
		},
		{
			name:     "multiple IPv4 CIDRs",
			input:    "10.0.0.0/8, 192.168.0.0/16",
			wantNets: []string{"10.0.0.0/8", "192.168.0.0/16"},
		},
		{
			name:     "single IPv6 CIDR",
			input:    "fd00::/8",
			wantNets: []string{"fd00::/8"},
		},
		{
			name:     "IPv6 /128 CIDR",
			input:    "2001:db8::1/128",
			wantNets: []string{"2001:db8::1/128"},
		},

		// Mixed IPs and CIDRs
		{
			name:     "mixed IPv4 address and CIDR",
			input:    "10.2.0.1, 10.0.0.0/8",
			wantNets: []string{"10.2.0.1/32", "10.0.0.0/8"},
		},
		{
			name:     "mixed IPv4 and IPv6",
			input:    "10.0.0.1, 2001:db8::1, 192.168.0.0/16, fd00::/8",
			wantNets: []string{"10.0.0.1/32", "2001:db8::1/128", "192.168.0.0/16", "fd00::/8"},
		},

		// Trailing/leading commas and blank slots
		{
			name:     "leading comma is ignored",
			input:    ",10.0.0.1",
			wantNets: []string{"10.0.0.1/32"},
		},
		{
			name:     "trailing comma is ignored",
			input:    "10.0.0.1,",
			wantNets: []string{"10.0.0.1/32"},
		},
		{
			name:     "consecutive commas are ignored",
			input:    "10.0.0.1,,10.0.0.2",
			wantNets: []string{"10.0.0.1/32", "10.0.0.2/32"},
		},

		// Error cases
		{
			name:      "invalid token",
			input:     "not-a-cidr",
			wantError: "invalid IP address",
		},
		{
			name:      "invalid prefix length",
			input:     "10.0.0.0/99",
			wantError: "invalid CIDR",
		},
		{
			name:      "invalid IP in CIDR",
			input:     "999.999.999.999/24",
			wantError: "invalid CIDR",
		},
		{
			name:      "valid entry followed by invalid",
			input:     "10.0.0.1, bad",
			wantError: "invalid IP address",
		},
		{
			name:      "invalid entry followed by valid",
			input:     "bad, 10.0.0.1",
			wantError: "invalid IP address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseIPOrCIDRList(tt.input)

			if tt.wantError != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantError)
				return
			}

			require.NoError(t, err)
			require.Len(t, got, len(tt.wantNets))
			for i, want := range tt.wantNets {
				assert.Equal(t, want, got[i].String(), "index %d", i)
			}
		})
	}
}

// TestParseIPOrCIDRList_PlainIPMask verifies the mask size directly, not just
// the String() output, for plain IPv4 and IPv6 addresses.
func TestParseIPOrCIDRList_PlainIPMask(t *testing.T) {
	got, err := ParseIPOrCIDRList("10.0.0.1, 2001:db8::1")
	require.NoError(t, err)
	require.Len(t, got, 2)

	ones, bits := got[0].Mask.Size()
	assert.Equal(t, 32, ones, "IPv4 plain IP prefix length")
	assert.Equal(t, 32, bits, "IPv4 plain IP mask size")

	ones, bits = got[1].Mask.Size()
	assert.Equal(t, 128, ones, "IPv6 plain IP prefix length")
	assert.Equal(t, 128, bits, "IPv6 plain IP mask size")
}

// TestParseIPOrCIDRList_Containment verifies the returned *net.IPNet values
// work correctly for route and containment checks.
func TestParseIPOrCIDRList_Containment(t *testing.T) {
	got, err := ParseIPOrCIDRList("10.0.0.0/8")
	require.NoError(t, err)
	require.Len(t, got, 1)

	_, expected, _ := net.ParseCIDR("10.0.0.0/8")
	assert.Equal(t, expected.String(), got[0].String())
	assert.True(t, got[0].Contains(net.ParseIP("10.2.0.1")), "10.0.0.0/8 should contain 10.2.0.1")
	assert.False(t, got[0].Contains(net.ParseIP("192.168.0.1")), "10.0.0.0/8 should not contain 192.168.0.1")
}
