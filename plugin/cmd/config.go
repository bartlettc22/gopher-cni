package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/bartlettc22/gopher-cni/internal/cni"
	cniv1 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"
)

// LoadNetConf unmarshals a network configuration from JSON and returns
// a NetConf together with the CNI version
func LoadNetConf(bytes []byte) (*cni.PluginConfig, error) {
	n := &cni.PluginConfig{}
	if err := json.Unmarshal(bytes, n); err != nil {
		return nil, fmt.Errorf("failed to load netconf: %w", err)
	}

	if n.RawPrevResult != nil {
		resultBytes, err := json.Marshal(n.RawPrevResult)
		if err != nil {
			return nil, fmt.Errorf("could not serialize prevResult: %w", err)
		}
		res, err := version.NewResult(n.CNIVersion, resultBytes)
		if err != nil {
			return nil, fmt.Errorf("could not parse prevResult: %w", err)
		}
		n.PrevResultV1, err = cniv1.NewResultFromResult(res)
		if err != nil {
			return nil, fmt.Errorf("could not convert result to current version: %w", err)
		}
		n.PrevResult = n.PrevResultV1
	}

	return n, nil
}
