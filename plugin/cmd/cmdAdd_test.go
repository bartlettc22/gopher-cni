package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/bartlettc22/gopher-cni/internal/cni"
	"github.com/containernetworking/cni/pkg/skel"
	cnitypes "github.com/containernetworking/cni/pkg/types"
	cniv1 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdd(t *testing.T) {
	tests := []struct {
		name           string
		stdinData      string
		expectedError  string
		validateOutput func(t *testing.T, output string)
	}{
		{
			name: "config without previous result should fail",
			stdinData: `{
				"cniVersion": "1.0.0",
				"name": "test-network",
				"type": "gopher-cni"
			}`,
			expectedError: "does not work without a previous result",
			validateOutput: func(t *testing.T, output string) {
				// No output expected on error
			},
		},
		{
			name: "config with previous result but no kubernetes should fail",
			stdinData: `{
				"cniVersion": "1.0.0",
				"name": "test-network",
				"type": "gopher-cni",
				"prevResult": {
					"cniVersion": "1.0.0",
					"interfaces": [
						{
							"name": "eth0",
							"mac": "00:11:22:33:44:55"
						}
					],
					"ips": [
						{
							"address": "10.0.0.2/24",
							"gateway": "10.0.0.1",
							"interface": 0
						}
					]
				}
			}`,
			expectedError: "CNI Kubernetes config not provided",
			validateOutput: func(t *testing.T, output string) {
				// No output expected on error
			},
		},
		{
			name: "config with log level but no prevResult should fail",
			stdinData: `{
				"cniVersion": "1.0.0",
				"name": "test-network",
				"type": "gopher-cni",
				"log_level": "debug"
			}`,
			expectedError: "does not work without a previous result",
			validateOutput: func(t *testing.T, output string) {
				// No output expected on error
			},
		},
		{
			name: "config with kubernetes but no prevResult should fail",
			stdinData: `{
				"cniVersion": "1.0.0",
				"name": "test-network",
				"type": "gopher-cni",
				"kubernetes": {
					"kubeconfig": "/etc/kubernetes/config"
				}
			}`,
			expectedError: "does not work without a previous result",
			validateOutput: func(t *testing.T, output string) {
				// No output expected on error
			},
		},
		{
			name:          "invalid JSON",
			stdinData:     `{invalid json}`,
			expectedError: "unable to parse CNI ADD configuration",
			validateOutput: func(t *testing.T, output string) {
				// No output expected on error
			},
		},
		{
			name:          "empty config",
			stdinData:     ``,
			expectedError: "unable to parse CNI ADD configuration",
			validateOutput: func(t *testing.T, output string) {
				// No output expected on error
			},
		},
		{
			name: "config with malformed previous result",
			stdinData: `{
				"cniVersion": "1.0.0",
				"name": "test-network",
				"type": "gopher-cni",
				"prevResult": "invalid-result"
			}`,
			expectedError: "unable to parse CNI ADD configuration",
			validateOutput: func(t *testing.T, output string) {
				// No output expected on error
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test arguments
			args := &skel.CmdArgs{
				ContainerID: "test-container-123",
				Netns:       "/var/run/netns/test",
				IfName:      "eth0",
				Args:        "K8S_POD_NAME=test-pod",
				Path:        "/opt/cni/bin",
				StdinData:   []byte(tt.stdinData),
			}

			// Capture stdout to verify PrintResult output
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			// Execute the Add function
			err := Add(args)

			// Restore stdout
			w.Close()
			os.Stdout = oldStdout

			// Read captured output
			var buf strings.Builder
			_, err_c := io.Copy(&buf, r)
			require.NoError(t, err_c)
			output := buf.String()

			// Check error expectations
			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				require.NoError(t, err)
			}

			// Validate output if provided
			if tt.validateOutput != nil {
				tt.validateOutput(t, output)
			}
		})
	}
}

func TestAdd_WithRealPrevResult(t *testing.T) {
	// Test with a more complex previous result structure
	// NOTE: This test will fail at kubernetes provider creation since we don't have k8s config
	prevResult := &cniv1.Result{
		CNIVersion: "1.0.0",
		Interfaces: []*cniv1.Interface{
			{
				Name:    "eth0",
				Mac:     "aa:bb:cc:dd:ee:ff",
				Sandbox: "/var/run/netns/test",
			},
			{
				Name: "veth0",
				Mac:  "ff:ee:dd:cc:bb:aa",
			},
		},
		IPs: []*cniv1.IPConfig{
			{
				Address: mustParseCIDR(t, "192.168.1.10/24"),
				Gateway: mustParseIP(t, "192.168.1.1"),
			},
			{
				Address: mustParseCIDR(t, "fd00::10/64"),
				Gateway: mustParseIP(t, "fd00::1"),
			},
		},
		Routes: []*cnitypes.Route{
			{
				Dst: mustParseCIDR(t, "0.0.0.0/0"),
				GW:  mustParseIP(t, "192.168.1.1"),
			},
		},
		DNS: cnitypes.DNS{
			Nameservers: []string{"8.8.8.8", "8.8.4.4"},
			Domain:      "example.com",
			Search:      []string{"example.com", "test.example.com"},
			Options:     []string{"ndots:5"},
		},
	}

	// Marshal the result
	prevResultBytes, err := json.Marshal(prevResult)
	require.NoError(t, err)

	// Create config with the previous result
	config := map[string]interface{}{
		"cniVersion": "1.0.0",
		"name":       "test-network",
		"type":       "gopher-cni",
		"prevResult": json.RawMessage(prevResultBytes),
	}

	configBytes, err := json.Marshal(config)
	require.NoError(t, err)

	args := &skel.CmdArgs{
		ContainerID: "test-container-456",
		Netns:       "/var/run/netns/test",
		IfName:      "eth0",
		Args:        "K8S_POD_NAME=test-pod",
		Path:        "/opt/cni/bin",
		StdinData:   configBytes,
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = Add(args)

	w.Close()
	os.Stdout = oldStdout

	var buf strings.Builder
	_, err_c := io.Copy(&buf, r)
	require.NoError(t, err_c)

	// Should fail because kubernetes config is not provided
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CNI Kubernetes config not provided")
}

func TestAdd_WithUDSLogging(t *testing.T) {
	// This test verifies that the Add function handles UDS logging configuration
	// Note: This will fail because prevResult is required

	config := `{
		"cniVersion": "1.0.0",
		"name": "test-network",
		"type": "gopher-cni",
		"log_level": "debug",
		"log_uds_address": "/tmp/nonexistent.sock"
	}`

	args := &skel.CmdArgs{
		ContainerID: "test-container-789",
		Netns:       "/var/run/netns/test",
		IfName:      "eth0",
		Args:        "K8S_POD_NAME=test-pod",
		Path:        "/opt/cni/bin",
		StdinData:   []byte(config),
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := Add(args)

	w.Close()
	os.Stdout = oldStdout

	var buf strings.Builder
	_, err_c := io.Copy(&buf, r)
	require.NoError(t, err_c)

	// Should fail because prevResult is not provided
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not work without a previous result")
}

func TestAdd_PrintResultError(t *testing.T) {
	// Test scenario where prevResult has incompatible version
	// This tests error handling in the config parsing flow

	config := `{
		"cniVersion": "0.3.0",
		"name": "test-network",
		"type": "gopher-cni",
		"prevResult": {
			"cniVersion": "invalid.version",
			"ips": []
		}
	}`

	args := &skel.CmdArgs{
		ContainerID: "test-container-error",
		Netns:       "/var/run/netns/test",
		IfName:      "eth0",
		StdinData:   []byte(config),
	}

	err := Add(args)

	// Should fail during config parsing due to invalid version
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unable to parse CNI ADD configuration")
}

// Helper functions for creating test data

func mustParseCIDR(t *testing.T, s string) net.IPNet {
	ipnet, err := cnitypes.ParseCIDR(s)
	require.NoError(t, err)
	return *ipnet
}

func mustParseIP(t *testing.T, s string) net.IP {
	ip := net.ParseIP(s)
	if ip == nil {
		require.FailNow(t, "invalid IP address", s)
	}
	return ip
}

// TestLoadNetConf tests the LoadNetConf function used by Add
func TestLoadNetConf(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectError  bool
		validateConf func(t *testing.T, conf *cni.PluginConfig)
	}{
		{
			name: "minimal valid config",
			input: `{
				"cniVersion": "1.0.0",
				"name": "test",
				"type": "gopher-cni"
			}`,
			expectError: false,
			validateConf: func(t *testing.T, conf *cni.PluginConfig) {
				assert.Equal(t, "1.0.0", conf.CNIVersion)
				assert.Equal(t, "test", conf.Name)
				assert.Equal(t, "gopher-cni", conf.Type)
			},
		},
		{
			name: "config with all fields",
			input: `{
				"cniVersion": "1.0.0",
				"name": "test",
				"type": "gopher-cni",
				"log_level": "debug",
				"log_uds_address": "/tmp/test.sock",
				"kubernetes": {
					"kubeconfig": "/etc/kube/config"
				}
			}`,
			expectError: false,
			validateConf: func(t *testing.T, conf *cni.PluginConfig) {
				assert.Equal(t, "debug", conf.LogLevel)
				assert.Equal(t, "/tmp/test.sock", conf.LogUDSAddress)
				assert.Equal(t, "/etc/kube/config", conf.Kubernetes.Kubeconfig)
			},
		},
		{
			name:        "invalid JSON",
			input:       `{invalid}`,
			expectError: true,
		},
		{
			name:        "empty input",
			input:       "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf, err := LoadNetConf([]byte(tt.input))

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, conf)
				if tt.validateConf != nil {
					tt.validateConf(t, conf)
				}
			}
		})
	}
}

// BenchmarkAdd measures the performance of the Add function
func BenchmarkAdd(b *testing.B) {
	config := `{
		"cniVersion": "1.0.0",
		"name": "test-network",
		"type": "gopher-cni",
		"prevResult": {
			"cniVersion": "1.0.0",
			"interfaces": [{"name": "eth0", "mac": "00:11:22:33:44:55"}],
			"ips": [{"address": "10.0.0.2/24", "gateway": "10.0.0.1", "interface": 0}]
		}
	}`

	args := &skel.CmdArgs{
		ContainerID: "benchmark-container",
		Netns:       "/var/run/netns/bench",
		IfName:      "eth0",
		StdinData:   []byte(config),
	}

	// Redirect stdout to discard
	oldStdout := os.Stdout
	os.Stdout, _ = os.Open(os.DevNull)
	defer func() { os.Stdout = oldStdout }()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Add(args)
	}
}

// TestAdd_ConcurrentCalls tests that Add can handle concurrent calls
func TestAdd_ConcurrentCalls(t *testing.T) {
	config := `{
		"cniVersion": "1.0.0",
		"name": "test-network",
		"type": "gopher-cni"
	}`

	// Redirect stdout
	oldStdout := os.Stdout
	os.Stdout, _ = os.Open(os.DevNull)
	defer func() { os.Stdout = oldStdout }()

	// Run multiple Add calls concurrently
	const numGoroutines = 10
	errChan := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			args := &skel.CmdArgs{
				ContainerID: string(rune(id)),
				Netns:       "/var/run/netns/test",
				IfName:      "eth0",
				StdinData:   []byte(config),
			}
			errChan <- Add(args)
		}(i)
	}

	// Collect results - all should fail because no prevResult
	for i := 0; i < numGoroutines; i++ {
		err := <-errChan
		require.Error(t, err, "concurrent call %d should have failed", i)
		assert.Contains(t, err.Error(), "does not work without a previous result")
	}
}

// MockError implements error interface for testing
type MockError struct {
	message string
}

func (e *MockError) Error() string {
	return e.message
}

func (e *MockError) Unwrap() error {
	return errors.New("underlying error")
}

// TestAdd_ErrorWrapping verifies error wrapping behavior
func TestAdd_ErrorWrapping(t *testing.T) {
	tests := []struct {
		name       string
		stdinData  string
		errorCheck func(t *testing.T, err error)
	}{
		{
			name:      "config parse error is wrapped",
			stdinData: `invalid json`,
			errorCheck: func(t *testing.T, err error) {
				assert.Contains(t, err.Error(), "unable to parse CNI ADD configuration")
				assert.True(t, errors.Unwrap(err) != nil, "error should be wrapped")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := &skel.CmdArgs{
				ContainerID: "error-test",
				Netns:       "/var/run/netns/test",
				IfName:      "eth0",
				StdinData:   []byte(tt.stdinData),
			}

			err := Add(args)
			require.Error(t, err)
			tt.errorCheck(t, err)
		})
	}
}
