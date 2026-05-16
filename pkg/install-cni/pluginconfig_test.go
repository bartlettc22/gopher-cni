package install

import (
	"testing"

	"github.com/bartlettc22/gopher-cni/pkg/cni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetPluginConfig(t *testing.T) {
	tests := []struct {
		name               string
		cfg                *Config
		kubeconfigFilepath string
		validateConf       func(t *testing.T, conf cni.PluginConfig)
	}{
		{
			name: "valid config",
			cfg: &Config{
				LogLevel:              "info",
				UDSSocketAddress:      "/tmp/uds.sock",
				SkipRouteCIDRs:        []string{"10.2.0.0/16"},
				KubeExcludeNamespaces: []string{"kube-system"},
			},
			kubeconfigFilepath: "/tmp/kubeconfig",
			validateConf: func(t *testing.T, conf cni.PluginConfig) {
				assert.Equal(t, "gopher-cni", conf.Type)
				assert.Equal(t, "info", conf.LogLevel)
				assert.Equal(t, "/tmp/uds.sock", conf.LogUDSAddress)
				assert.Equal(t, "/tmp/kubeconfig", conf.Kubernetes.Kubeconfig)
				assert.Equal(t, []string{"kube-system"}, conf.Kubernetes.ExcludeNamespaces)
				assert.Equal(t, []string{"10.2.0.0/16"}, conf.SkipRouteCIDRs)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pluginConfig := getPluginConfig(tt.cfg, tt.kubeconfigFilepath)
			require.NotNil(t, pluginConfig)
			if tt.validateConf != nil {
				tt.validateConf(t, pluginConfig)
			}
		})
	}
}
