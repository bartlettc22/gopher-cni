package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bartlettc22/gopher-cni/pkg/utils"
	"github.com/bartlettc22/gopher-cni/pkg/version"
	"github.com/containernetworking/cni/libcni"
)

func createCNIConfigFile(cfg *Config, kubeconfigFilepath string) (string, error) {
	hostKubeconfigFilePath := strings.TrimPrefix(kubeconfigFilepath, cfg.MountedHostDir)
	pluginConfigMap, err := getPluginConfigMap(cfg, hostKubeconfigFilePath)
	if err != nil {
		return "", err
	}

	return installCNIConfig(pluginConfigMap, filepath.Join(cfg.MountedHostDir, cfg.CNINetDir))
}

func installCNIConfig(pluginConfigMap map[string]any, CNINetDir string) (string, error) {

	cniConfigFilepath, existingCNIConfig, err := getCNIConfig(CNINetDir)
	if err != nil {
		return "", err
	}

	// cniConfigFilepath, err := getCNIConfigFilepath(CNINetDir)
	// if err != nil {
	// 	return "", err
	// }
	// log.Info("CNI config file located", "path", cniConfigFilepath)

	// if !utils.FileExists(cniConfigFilepath) {
	// 	return "", fmt.Errorf("CNI config file %s was removed during configuration", cniConfigFilepath)
	// }

	// existinCNIConfig, err := os.ReadFile(cniConfigFilepath)
	// if err != nil {
	// 	return "", fmt.Errorf("error reading existing CNI config file: %v", err)
	// }

	newCNIConfigMap, err := insertCNIPluginConfig(pluginConfigMap, existingCNIConfig)
	if err != nil {
		return "", fmt.Errorf("error inserting CNI plugin config into existing CNI config: %v", err)
	}

	if err = writeCNIConfigMap(cniConfigFilepath, newCNIConfigMap); err != nil {
		return "", err
	}
	// if err = os.WriteFile(cniConfigFilepath, newCNIConfigBytes, os.FileMode(0o644)); err != nil {
	// 	return cniConfigFilepath, fmt.Errorf("failed to write CNI config file %v: %w", cniConfigFilepath, err)
	// }

	return cniConfigFilepath, nil
}

func uninstallCNIConfig(CNINetDir string) error {
	cniConfigFilepath, existingCNIConfig, err := getCNIConfig(CNINetDir)
	if err != nil {
		return err
	}

	newCNIConfigMap, err := removeCNIPluginConfig(existingCNIConfig)
	if err != nil {
		return err
	}

	if err = writeCNIConfigMap(cniConfigFilepath, newCNIConfigMap); err != nil {
		return err
	}

	return nil
}

// Waits indefinitely for a main CNI config file to exist before returning
// Or until cancelled by parent context
// TODO: create a proper file watcher for this
func getCNIConfig(CNINetDir string) (string, map[string]any, error) {

	var (
		filename string
		err      error
	)
	i := 0
	for {
		filename, err = getDefaultCNINetwork(CNINetDir)
		if err != nil {
			return "", nil, err
		}

		if filename != "" {
			break
		} else {
			// Only log every 30 iterations
			if i%30 == 0 {
				log.Warn("cannot find existing CNI network config, waiting for config file to be written", "cniNetDir", CNINetDir)
			}
			time.Sleep(1 * time.Second)
		}

		i++
	}

	log.Info("CNI config file located", "path", filename)

	if !utils.FileExists(filename) {
		return "", nil, fmt.Errorf("CNI config file %s was removed during configuration", filename)
	}

	configBytes, err := os.ReadFile(filename)
	if err != nil {
		return "", nil, fmt.Errorf("error reading existing CNI config file: %v", err)
	}

	var configMap map[string]any
	err = json.Unmarshal(configBytes, &configMap)
	if err != nil {
		return "", nil, fmt.Errorf("error unmarshaling existing CNI config: %v", err)
	}

	return filename, configMap, nil
}

// getDefaultCNINetwork returns the full file path of the first CNI config file found in the given directory
// In the case that no valid config files are found, and empty string is returned with no error
func getDefaultCNINetwork(confDir string) (string, error) {

	files, err := libcni.ConfFiles(confDir, []string{".conf", ".conflist", ".json"})

	switch {
	case err != nil:
		return "", err
	case len(files) == 0:
		return "", nil
	}

	// CRI should choose the first config file in the sorted list
	slices.Sort(files)
	defaultConfFile := files[0]

	if strings.HasSuffix(defaultConfFile, ".conflist") {

		confList, err := libcni.ConfListFromFile(defaultConfFile)
		if err != nil {
			return "", fmt.Errorf("error loading CNI config list %s: %v", defaultConfFile, err)
		}

		if len(confList.Plugins) == 0 {
			return "", fmt.Errorf("CNI config list %s has no networks; only config lists with networks are supported", defaultConfFile)
		}

		return defaultConfFile, nil
	}

	log.Warn("default CNI config file is not a .conflist file, only .conflist files are supported", "file", defaultConfFile)
	return "", nil
}

// insertCNIPluginConfig inserts the CNI plugin config into the existing CNI config
// Requires the existing CNI config to be in the format of a CNI configlist file
func insertCNIPluginConfig(pluginConfigMap, existingCNIConfig map[string]any) (map[string]any, error) {

	// var existingMap map[string]any
	// err := json.Unmarshal(existingCNIConfig, &existingMap)
	// if err != nil {
	// 	return nil, fmt.Errorf("error unmarshaling existing CNI config: %v", err)
	// }

	// newMap := existingMap
	// plugins, ok := newMap["plugins"].([]any)
	// if !ok {
	// 	return nil, fmt.Errorf("error reading plugin list from CNI config")
	// }

	// // Search for and remove our plugin from the existing CNI config (if it exists)
	// for i, rawPlugin := range plugins {
	// 	plugin, ok := rawPlugin.(map[string]any)
	// 	if !ok {
	// 		return nil, fmt.Errorf("error reading plugin from CNI config plugin list")
	// 	}
	// 	if plugin["type"] == version.CNI_PLUGIN_TYPE {
	// 		plugins = append(plugins[:i], plugins[i+1:]...)
	// 		break
	// 	}
	// }
	newMap, err := removeCNIPluginConfig(existingCNIConfig)
	if err != nil {
		return nil, err
	}

	// var pluginMap map[string]any
	// err = json.Unmarshal(cniPluginConfig, &pluginMap)
	// if err != nil {
	// 	return nil, fmt.Errorf("error unmarshaling CNI plugin config: %v", err)
	// }

	// Finally, add our plugin to the bottom of the existing CNI config plugin list
	plugins := newMap["plugins"].([]any)
	newMap["plugins"] = append(plugins, pluginConfigMap)

	// Format the new CNI config
	// cniConfig, err := json.MarshalIndent(newMap, "", "  ")
	// if err != nil {
	// 	return nil, err
	// }
	// cniConfig = append(cniConfig, "\n"...)

	return newMap, nil
}

func removeCNIPluginConfig(cniConfigMap map[string]any) (map[string]any, error) {

	newCNIConfigMap := cniConfigMap
	plugins, ok := newCNIConfigMap["plugins"].([]any)
	if !ok {
		return nil, fmt.Errorf("error reading plugin list from CNI config")
	}

	// Search for and remove our plugin from the existing CNI config (if it exists)
	for i, rawPlugin := range plugins {
		plugin, ok := rawPlugin.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("error reading plugin from CNI config plugin list")
		}
		if plugin["type"] == version.CNI_PLUGIN_TYPE {
			plugins = append(plugins[:i], plugins[i+1:]...)
			break
		}
	}
	newCNIConfigMap["plugins"] = plugins

	return newCNIConfigMap, nil
}

func writeCNIConfigMap(cniConfigFilepath string, cfg map[string]any) error {
	cniConfig, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	cniConfig = append(cniConfig, "\n"...)

	log.Debug("new CNI config", "config", string(cniConfig))
	if err = os.WriteFile(cniConfigFilepath, cniConfig, os.FileMode(0o644)); err != nil {
		return fmt.Errorf("failed to write CNI config file %v: %w", cniConfigFilepath, err)
	}

	log.Info("wrote CNI config file", "path", cniConfigFilepath)
	return nil
}
