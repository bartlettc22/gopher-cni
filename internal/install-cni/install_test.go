package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bartlettc22/gopher-cni/internal/cni"
	"github.com/bartlettc22/gopher-cni/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUninstallRemovesFromHostPaths verifies that Uninstall:
//   - removes the kubeconfig from the MountedHostDir-prefixed path
//   - removes the binary from the MountedHostDir-prefixed path
//   - strips the gopher-cni entry from the CNI conflist using the directory
//     path (not the config file path)
//
// This test would have caught both bugs fixed in install.go: passing the
// config file path to uninstallCNIConfig instead of the directory, and
// omitting MountedHostDir from the binary removal path.
func TestUninstallRemovesFromHostPaths(t *testing.T) {
	hostRoot := t.TempDir()
	cniNetDir := "/etc/cni/net.d"
	cniBinDir := "/opt/cni/bin"

	require.NoError(t, os.MkdirAll(filepath.Join(hostRoot, cniNetDir), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(hostRoot, cniBinDir), 0755))

	// Write fake kubeconfig
	kubeconfigPath := filepath.Join(hostRoot, cniNetDir, "ZZZ-gopher-cni-kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, []byte("kubeconfig"), 0600))

	// Write fake binary at the host-prefixed path
	binaryPath := filepath.Join(hostRoot, cniBinDir, cni.PluginBinaryName)
	require.NoError(t, os.WriteFile(binaryPath, []byte("binary"), 0755))

	// Write a CNI conflist that includes gopher-cni as a chained plugin
	conflistPath := filepath.Join(hostRoot, cniNetDir, "10-calico.conflist")
	conflist := map[string]any{
		"name":       "k8s-pod-network",
		"cniVersion": "0.3.1",
		"plugins": []any{
			map[string]any{"type": "calico"},
			map[string]any{"type": version.CNI_PLUGIN_TYPE, "logLevel": "info"},
		},
	}
	conflistBytes, err := json.Marshal(conflist)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(conflistPath, conflistBytes, 0644))

	installer := &Installer{
		cfg: &Config{
			MountedHostDir:     hostRoot,
			CNINetDir:          cniNetDir,
			CNIBinTargetDir:    cniBinDir,
			KubeconfigFilename: "ZZZ-gopher-cni-kubeconfig",
		},
		kubeconfigFilepath: kubeconfigPath,
		cniConfigFilepath:  conflistPath,
	}

	installer.Uninstall()

	assert.NoFileExists(t, kubeconfigPath, "kubeconfig must be removed from host-prefixed path")
	assert.NoFileExists(t, binaryPath, "binary must be removed from host-prefixed path")

	// gopher-cni must be stripped from the conflist; other plugins must remain
	raw, err := os.ReadFile(conflistPath)
	require.NoError(t, err)
	var updated map[string]any
	require.NoError(t, json.Unmarshal(raw, &updated))
	plugins, ok := updated["plugins"].([]any)
	require.True(t, ok, "plugins must still be a list")
	assert.Len(t, plugins, 1, "only the primary CNI plugin should remain")
	remaining := plugins[0].(map[string]any)
	assert.Equal(t, "calico", remaining["type"], "calico plugin must not be removed")
}
