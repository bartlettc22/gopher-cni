package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bartlettc22/gopher-cni/internal/utils"
	"github.com/bartlettc22/gopher-cni/internal/version"
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

	newCNIConfigMap, err := insertCNIPluginConfig(pluginConfigMap, existingCNIConfig)
	if err != nil {
		return "", fmt.Errorf("error inserting CNI plugin config into existing CNI config: %v", err)
	}

	if err = writeCNIConfigMap(cniConfigFilepath, newCNIConfigMap); err != nil {
		return "", err
	}

	return cniConfigFilepath, nil
}

// uninstallCNIConfig removes the gopher-cni plugin entry from the conflist at
// the given file path. The path must be the full path to the conflist file
// (not a directory), which is the value stored in Installer.cniConfigFilepath.
func uninstallCNIConfig(cniConfigFilepath string) error {
	configBytes, err := os.ReadFile(cniConfigFilepath)
	if err != nil {
		return fmt.Errorf("error reading CNI config file: %v", err)
	}

	var existingCNIConfig map[string]any
	if err := json.Unmarshal(configBytes, &existingCNIConfig); err != nil {
		return fmt.Errorf("error unmarshaling CNI config: %v", err)
	}

	newCNIConfigMap, err := removeCNIPluginConfig(existingCNIConfig)
	if err != nil {
		return err
	}

	return writeCNIConfigMap(cniConfigFilepath, newCNIConfigMap)
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

	newMap, err := removeCNIPluginConfig(existingCNIConfig)
	if err != nil {
		return nil, err
	}

	plugins := newMap["plugins"].([]any)
	newMap["plugins"] = append(plugins, pluginConfigMap)

	return newMap, nil
}

func removeCNIPluginConfig(cniConfigMap map[string]any) (map[string]any, error) {

	newCNIConfigMap := cniConfigMap
	plugins, ok := newCNIConfigMap["plugins"].([]any)
	if !ok {
		return nil, fmt.Errorf("error reading plugin list from CNI config")
	}

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
