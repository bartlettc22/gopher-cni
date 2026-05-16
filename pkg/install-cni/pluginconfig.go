package install

import (
	"encoding/json"
	"fmt"

	"github.com/bartlettc22/gopher-cni/pkg/cni"
	"github.com/bartlettc22/gopher-cni/pkg/version"
	cnitypes "github.com/containernetworking/cni/pkg/types"
)

func getPluginConfigMap(cfg *Config, kubeconfigFilepath string) (map[string]any, error) {
	cniConfig := getPluginConfig(cfg, kubeconfigFilepath)

	configBytes, err := json.Marshal(cniConfig)
	if err != nil {
		return nil, fmt.Errorf("error marshalling CNI config: %w", err)
	}

	var configMap map[string]any
	err = json.Unmarshal(configBytes, &configMap)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling CNI plugin config: %v", err)
	}

	return configMap, nil
}

func getPluginConfig(cfg *Config, kubeconfigFilepath string) cni.PluginConfig {
	pluginConfig := cni.PluginConfig{
		NetConf: cnitypes.NetConf{
			Type: version.AppName,
		},
		LogLevel:      cfg.LogLevel,
		LogUDSAddress: cfg.UDSSocketAddress,
		Kubernetes: &cni.PluginKubernetesConfig{
			Kubeconfig: kubeconfigFilepath,
		},
	}

	if len(cfg.SkipRouteCIDRs) > 0 {
		pluginConfig.SkipRouteCIDRs = cfg.SkipRouteCIDRs
	}
	if len(cfg.KubeExcludeNamespaces) > 0 {
		pluginConfig.Kubernetes.ExcludeNamespaces = cfg.KubeExcludeNamespaces
	}

	return pluginConfig
}
