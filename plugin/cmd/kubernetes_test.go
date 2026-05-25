package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/bartlettc22/gopher-cni/internal/cni"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// mockClient implements the kubernetes.Client interface for testing
type mockClient struct {
	pods    map[string]*corev1.Pod
	secrets map[string]*corev1.Secret
	podErr  error
	secErr  error
}

func newMockClient() *mockClient {
	return &mockClient{
		pods:    make(map[string]*corev1.Pod),
		secrets: make(map[string]*corev1.Secret),
	}
}

func (m *mockClient) GetPod(ctx context.Context, namespace, name string) (*corev1.Pod, error) {
	if m.podErr != nil {
		return nil, m.podErr
	}
	key := namespace + "/" + name
	if pod, exists := m.pods[key]; exists {
		return pod, nil
	}
	return nil, fmt.Errorf("pod %q not found", key)
}

func (m *mockClient) FetchSecretKey(ctx context.Context, namespace, name, key string) ([]byte, error) {
	if m.secErr != nil {
		return nil, m.secErr
	}
	k := namespace + "/" + name
	secret, exists := m.secrets[k]
	if !exists {
		return nil, fmt.Errorf("secret %q not found", k)
	}
	val, ok := secret.Data[key]
	if !ok {
		return nil, fmt.Errorf("secret %q has no key %q", k, key)
	}
	return val, nil
}

func (m *mockClient) addPod(namespace, name string, labels, annotations map[string]string) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Labels:      labels,
			Annotations: annotations,
		},
	}
	key := namespace + "/" + name
	m.pods[key] = pod
}

func (m *mockClient) addSecret(namespace, name string, data map[string][]byte) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: data,
	}
	key := namespace + "/" + name
	m.secrets[key] = secret
}

// Helper to create CNI args
func makeCNIArgs(podName, podNamespace string) *skel.CmdArgs {
	return &skel.CmdArgs{
		Args: fmt.Sprintf("K8S_POD_NAME=%s;K8S_POD_NAMESPACE=%s", podName, podNamespace),
	}
}

// Helper to create kubernetes config
func makeKubeConfig() *cni.PluginKubernetesConfig {
	return &cni.PluginKubernetesConfig{
		Kubeconfig: "/tmp/kubeconfig",
	}
}

// TestNewKubeProviderFromCNI_Success tests successful provider creation
func TestNewKubeProviderFromCNI_Success(t *testing.T) {
	podName := "test-pod"
	podNamespace := "default"

	client := newMockClient()
	client.addPod(podNamespace, podName, map[string]string{}, map[string]string{})

	kubeConfig := makeKubeConfig()
	cniArgs := makeCNIArgs(podName, podNamespace)

	provider, err := newKubeProviderFromCNI(kubeConfig, cniArgs, client, slog.Default())

	require.NoError(t, err)
	require.NotNil(t, provider)
	assert.Equal(t, podName, provider.PodName())
	assert.Equal(t, podNamespace, provider.PodNamespace())
}

// TestNewKubeProviderFromCNI_NilArgs tests error when CNI args are nil
func TestNewKubeProviderFromCNI_NilArgs(t *testing.T) {
	client := newMockClient()
	kubeConfig := makeKubeConfig()

	provider, err := newKubeProviderFromCNI(kubeConfig, nil, client, slog.Default())

	assert.Error(t, err)
	assert.Nil(t, provider)
	assert.Contains(t, err.Error(), "CNI plugin args not provided")
}

// TestNewKubeProviderFromCNI_NilKubeSpec tests error when kubernetes spec is nil
func TestNewKubeProviderFromCNI_NilKubeSpec(t *testing.T) {
	client := newMockClient()
	cniArgs := makeCNIArgs("test-pod", "default")

	provider, err := newKubeProviderFromCNI(nil, cniArgs, client, slog.Default())

	assert.Error(t, err)
	assert.Nil(t, provider)
	assert.Contains(t, err.Error(), "CNI Kubernetes spec not provided")
}

// TestNewKubeProviderFromCNI_MissingPodName tests error when pod name is missing
func TestNewKubeProviderFromCNI_MissingPodName(t *testing.T) {
	client := newMockClient()
	kubeConfig := makeKubeConfig()
	cniArgs := &skel.CmdArgs{
		Args: "K8S_POD_NAMESPACE=default",
	}

	provider, err := newKubeProviderFromCNI(kubeConfig, cniArgs, client, slog.Default())

	assert.Error(t, err)
	assert.Nil(t, provider)
	assert.Contains(t, err.Error(), "CNI runtime did not provide namespace/pod info")
}

// TestNewKubeProviderFromCNI_MissingNamespace tests error when namespace is missing
func TestNewKubeProviderFromCNI_MissingNamespace(t *testing.T) {
	client := newMockClient()
	kubeConfig := makeKubeConfig()
	cniArgs := &skel.CmdArgs{
		Args: "K8S_POD_NAME=test-pod",
	}

	provider, err := newKubeProviderFromCNI(kubeConfig, cniArgs, client, slog.Default())

	assert.Error(t, err)
	assert.Nil(t, provider)
	assert.Contains(t, err.Error(), "CNI runtime did not provide namespace/pod info")
}

// TestNewKubeProviderFromCNI_PodNotFound tests error when pod doesn't exist
func TestNewKubeProviderFromCNI_PodNotFound(t *testing.T) {
	client := newMockClient()
	kubeConfig := makeKubeConfig()
	cniArgs := makeCNIArgs("nonexistent-pod", "default")

	provider, err := newKubeProviderFromCNI(kubeConfig, cniArgs, client, slog.Default())

	assert.Error(t, err)
	assert.Nil(t, provider)
	assert.Contains(t, err.Error(), "failed getting pod")
}

// TestNewKubeProviderFromCNI_GetPodError tests error during GetPod call
func TestNewKubeProviderFromCNI_GetPodError(t *testing.T) {
	client := newMockClient()
	client.podErr = fmt.Errorf("kubernetes API error")

	kubeConfig := makeKubeConfig()
	cniArgs := makeCNIArgs("test-pod", "default")

	provider, err := newKubeProviderFromCNI(kubeConfig, cniArgs, client, slog.Default())

	assert.Error(t, err)
	assert.Nil(t, provider)
	assert.Contains(t, err.Error(), "failed getting pod")
}

// TestPodNameAndNamespace tests the PodName and PodNamespace methods
func TestPodNameAndNamespace(t *testing.T) {
	podName := "my-pod"
	podNamespace := "kube-system"

	client := newMockClient()
	client.addPod(podNamespace, podName, map[string]string{}, map[string]string{})

	kubeConfig := makeKubeConfig()
	cniArgs := makeCNIArgs(podName, podNamespace)

	provider, err := newKubeProviderFromCNI(kubeConfig, cniArgs, client, slog.Default())
	require.NoError(t, err)

	assert.Equal(t, podName, provider.PodName())
	assert.Equal(t, podNamespace, provider.PodNamespace())
}

// TestFetchRawWireguardConfig_WithLabel tests fetching wireguard config when label is present
func TestFetchRawWireguardConfig_WithLabel(t *testing.T) {
	podName := "test-pod"
	podNamespace := "default"
	secretName := "wg-secret"
	wgConfig := []byte("wg config data")

	client := newMockClient()
	client.addPod(podNamespace, podName, map[string]string{
		cni.LabelEnabled: "true",
	}, map[string]string{
		cni.AnnotationWGConfSecret: secretName,
	})
	client.addSecret(podNamespace, secretName, map[string][]byte{
		cni.SecretKeyWGConf: wgConfig,
	})

	kubeConfig := makeKubeConfig()
	cniArgs := makeCNIArgs(podName, podNamespace)

	provider, err := newKubeProviderFromCNI(kubeConfig, cniArgs, client, slog.Default())
	require.NoError(t, err)

	config, err := provider.FetchRawWireguardConfig()

	assert.NoError(t, err)
	assert.Equal(t, wgConfig, config)
}

// TestFetchRawWireguardConfig_NoLabel tests when no wireguard label is present
func TestFetchRawWireguardConfig_NoLabel(t *testing.T) {
	podName := "test-pod"
	podNamespace := "default"

	client := newMockClient()
	client.addPod(podNamespace, podName, map[string]string{}, map[string]string{})

	kubeConfig := makeKubeConfig()
	cniArgs := makeCNIArgs(podName, podNamespace)

	provider, err := newKubeProviderFromCNI(kubeConfig, cniArgs, client, slog.Default())
	require.NoError(t, err)

	config, err := provider.FetchRawWireguardConfig()

	assert.NoError(t, err)
	assert.Nil(t, config, "should return nil when label is not present")
}

// TestFetchRawWireguardConfig_EmptyLabel tests when label exists but is empty
func TestFetchRawWireguardConfig_EmptyLabel(t *testing.T) {
	podName := "test-pod"
	podNamespace := "default"

	client := newMockClient()
	client.addPod(podNamespace, podName, map[string]string{
		cni.LabelEnabled: "",
	}, map[string]string{})

	kubeConfig := makeKubeConfig()
	cniArgs := makeCNIArgs(podName, podNamespace)

	provider, err := newKubeProviderFromCNI(kubeConfig, cniArgs, client, slog.Default())
	require.NoError(t, err)

	config, err := provider.FetchRawWireguardConfig()

	assert.NoError(t, err)
	assert.Nil(t, config, "should return nil when label is empty")
}

// TestFetchRawWireguardConfig_ExcludedNamespace tests that excluded namespaces are skipped
func TestFetchRawWireguardConfig_ExcludedNamespace(t *testing.T) {
	podName := "test-pod"
	podNamespace := "kube-system"
	secretName := "wg-secret"

	client := newMockClient()
	client.addPod(podNamespace, podName, map[string]string{
		cni.LabelEnabled: "true",
	}, map[string]string{
		cni.AnnotationWGConfSecret: secretName,
	})

	kubeConfig := &cni.PluginKubernetesConfig{
		Kubeconfig:        "/tmp/kubeconfig",
		ExcludeNamespaces: []string{"kube-system", "kube-public"},
	}
	cniArgs := makeCNIArgs(podName, podNamespace)

	provider, err := newKubeProviderFromCNI(kubeConfig, cniArgs, client, slog.Default())
	require.NoError(t, err)

	config, err := provider.FetchRawWireguardConfig()

	assert.NoError(t, err)
	assert.Nil(t, config, "should return nil for excluded namespace")
}

// TestFetchRawWireguardConfig_SecretNotFound tests error when secret doesn't exist
func TestFetchRawWireguardConfig_SecretNotFound(t *testing.T) {
	podName := "test-pod"
	podNamespace := "default"
	secretName := "nonexistent-secret"

	client := newMockClient()
	client.addPod(podNamespace, podName, map[string]string{
		cni.LabelEnabled: "true",
	}, map[string]string{
		cni.AnnotationWGConfSecret: secretName,
	})

	kubeConfig := makeKubeConfig()
	cniArgs := makeCNIArgs(podName, podNamespace)

	provider, err := newKubeProviderFromCNI(kubeConfig, cniArgs, client, slog.Default())
	require.NoError(t, err)

	config, err := provider.FetchRawWireguardConfig()

	require.Error(t, err)
	assert.Nil(t, config)
	assert.Contains(t, err.Error(), "not found")
}

// TestFetchRawWireguardConfig_KeyNotInSecret tests error when secret key doesn't exist
func TestFetchRawWireguardConfig_KeyNotInSecret(t *testing.T) {
	podName := "test-pod"
	podNamespace := "default"
	secretName := "wg-secret"

	client := newMockClient()
	client.addPod(podNamespace, podName, map[string]string{
		cni.LabelEnabled: "true",
	}, map[string]string{
		cni.AnnotationWGConfSecret: secretName,
	})
	client.addSecret(podNamespace, secretName, map[string][]byte{
		"other-key": []byte("data"),
	})

	kubeConfig := makeKubeConfig()
	cniArgs := makeCNIArgs(podName, podNamespace)

	provider, err := newKubeProviderFromCNI(kubeConfig, cniArgs, client, slog.Default())
	require.NoError(t, err)

	config, err := provider.FetchRawWireguardConfig()

	require.Error(t, err)
	assert.Nil(t, config)
	assert.Contains(t, err.Error(), "no key")
}

// TestFetchLabel tests the fetchLabel helper function
func TestFetchLabel(t *testing.T) {
	tests := []struct {
		name           string
		podLabels      map[string]string
		labelKey       string
		expectedResult string
	}{
		{
			name: "label exists with value",
			podLabels: map[string]string{
				cni.AnnotationWGConfSecret: "my-secret",
				"app":                      "myapp",
			},
			labelKey:       cni.AnnotationWGConfSecret,
			expectedResult: "my-secret",
		},
		{
			name: "label exists but empty",
			podLabels: map[string]string{
				cni.AnnotationWGConfSecret: "",
			},
			labelKey:       cni.AnnotationWGConfSecret,
			expectedResult: "",
		},
		{
			name: "label does not exist",
			podLabels: map[string]string{
				"app": "myapp",
			},
			labelKey:       cni.AnnotationWGConfSecret,
			expectedResult: "",
		},
		{
			name:           "empty labels map",
			podLabels:      map[string]string{},
			labelKey:       cni.AnnotationWGConfSecret,
			expectedResult: "",
		},
		{
			name:           "nil labels map",
			podLabels:      nil,
			labelKey:       cni.AnnotationWGConfSecret,
			expectedResult: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: tt.podLabels,
				},
			}

			result := fetchLabel(pod, tt.labelKey)

			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

// TestFetchSecretKey tests the Client.FetchSecretKey method
func TestFetchSecretKey(t *testing.T) {
	secretNamespace := "default"
	secretName := "test-secret"
	secretKey := "config"
	secretValue := []byte("secret data")

	client := newMockClient()
	client.addSecret(secretNamespace, secretName, map[string][]byte{
		secretKey: secretValue,
	})

	result, err := client.FetchSecretKey(context.Background(), secretNamespace, secretName, secretKey)

	assert.NoError(t, err)
	assert.Equal(t, secretValue, result)
}

// TestFetchSecretKey_SecretNotFound tests error when secret doesn't exist
func TestFetchSecretKey_SecretNotFound(t *testing.T) {
	client := newMockClient()

	result, err := client.FetchSecretKey(context.Background(), "default", "nonexistent-secret", "config")

	assert.Error(t, err)
	assert.Nil(t, result)
}

// TestFetchSecretKey_KeyNotFound tests error when key doesn't exist in secret
func TestFetchSecretKey_KeyNotFound(t *testing.T) {
	client := newMockClient()
	client.addSecret("default", "test-secret", map[string][]byte{
		"other-key": []byte("data"),
	})

	result, err := client.FetchSecretKey(context.Background(), "default", "test-secret", "missing-key")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no key")
}

// TestK8sArgs_Parsing tests that K8s args are parsed correctly
func TestK8sArgs_Parsing(t *testing.T) {
	tests := []struct {
		name              string
		args              string
		expectedPodName   string
		expectedNamespace string
		expectError       bool
	}{
		{
			name:              "valid args",
			args:              "K8S_POD_NAME=my-pod;K8S_POD_NAMESPACE=default",
			expectedPodName:   "my-pod",
			expectedNamespace: "default",
			expectError:       false,
		},
		{
			name:              "args with extra fields",
			args:              "K8S_POD_NAME=my-pod;K8S_POD_NAMESPACE=default;K8S_POD_INFRA_CONTAINER_ID=abc123",
			expectedPodName:   "my-pod",
			expectedNamespace: "default",
			expectError:       false,
		},
		{
			name:        "missing pod name",
			args:        "K8S_POD_NAMESPACE=default",
			expectError: true,
		},
		{
			name:        "missing namespace",
			args:        "K8S_POD_NAME=my-pod",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newMockClient()
			if !tt.expectError {
				client.addPod(tt.expectedNamespace, tt.expectedPodName, map[string]string{}, map[string]string{})
			}

			kubeConfig := makeKubeConfig()
			cniArgs := &skel.CmdArgs{Args: tt.args}

			provider, err := newKubeProviderFromCNI(kubeConfig, cniArgs, client, slog.Default())

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, provider)
			} else {
				require.NoError(t, err)
				require.NotNil(t, provider)
				assert.Equal(t, tt.expectedPodName, provider.PodName())
				assert.Equal(t, tt.expectedNamespace, provider.PodNamespace())
			}
		})
	}
}

// TestCNIProvider_WithExcludedNamespaces tests provider with multiple excluded namespaces
func TestCNIProvider_WithExcludedNamespaces(t *testing.T) {
	tests := []struct {
		name              string
		podNamespace      string
		excludeNamespaces []string
		hasLabel          bool
		shouldFetchConfig bool
	}{
		{
			name:              "pod in excluded namespace",
			podNamespace:      "kube-system",
			excludeNamespaces: []string{"kube-system", "kube-public"},
			hasLabel:          true,
			shouldFetchConfig: false,
		},
		{
			name:              "pod not in excluded namespace",
			podNamespace:      "default",
			excludeNamespaces: []string{"kube-system", "kube-public"},
			hasLabel:          true,
			shouldFetchConfig: true,
		},
		{
			name:              "no excluded namespaces",
			podNamespace:      "kube-system",
			excludeNamespaces: []string{},
			hasLabel:          true,
			shouldFetchConfig: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newMockClient()
			labels := map[string]string{}
			annotations := map[string]string{}
			if tt.hasLabel {
				labels[cni.LabelEnabled] = "true"
				annotations[cni.AnnotationWGConfSecret] = "wg-secret"
				client.addSecret(tt.podNamespace, "wg-secret", map[string][]byte{
					cni.SecretKeyWGConf: []byte("config"),
				})
			}
			client.addPod(tt.podNamespace, "test-pod", labels, annotations)

			kubeConfig := &cni.PluginKubernetesConfig{
				Kubeconfig:        "/tmp/kubeconfig",
				ExcludeNamespaces: tt.excludeNamespaces,
			}
			cniArgs := makeCNIArgs("test-pod", tt.podNamespace)

			provider, err := newKubeProviderFromCNI(kubeConfig, cniArgs, client, slog.Default())
			require.NoError(t, err)

			config, err := provider.FetchRawWireguardConfig()

			if tt.shouldFetchConfig {
				assert.NoError(t, err)
				assert.NotNil(t, config)
			} else {
				assert.NoError(t, err)
				assert.Nil(t, config)
			}
		})
	}
}

// TestCNIProvider_MultipleLabels tests pod with multiple labels
func TestCNIProvider_MultipleLabels(t *testing.T) {
	podName := "test-pod"
	podNamespace := "default"
	secretName := "wg-secret"
	wgConfig := []byte("wg config")

	client := newMockClient()
	client.addPod(podNamespace, podName, map[string]string{
		"app":            "myapp",
		"version":        "v1",
		cni.LabelEnabled: "true",
		"another-label":  "value",
	}, map[string]string{
		cni.AnnotationWGConfSecret: secretName,
	})
	client.addSecret(podNamespace, secretName, map[string][]byte{
		cni.SecretKeyWGConf: wgConfig,
	})

	kubeConfig := makeKubeConfig()
	cniArgs := makeCNIArgs(podName, podNamespace)

	provider, err := newKubeProviderFromCNI(kubeConfig, cniArgs, client, slog.Default())
	require.NoError(t, err)

	config, err := provider.FetchRawWireguardConfig()

	assert.NoError(t, err)
	assert.Equal(t, wgConfig, config)
}

// TestCNIMode tests the CNIMode method
func TestCNIMode(t *testing.T) {
	tests := []struct {
		name         string
		annotations  map[string]string
		expectedMode string
	}{
		{
			name:         "no annotation returns default",
			annotations:  map[string]string{},
			expectedMode: cni.CNIModePodOrigin,
		},
		{
			name:         "pod-origin annotation",
			annotations:  map[string]string{cni.AnnotationCNIMode: cni.CNIModePodOrigin},
			expectedMode: cni.CNIModePodOrigin,
		},
		{
			name:         "host-origin annotation",
			annotations:  map[string]string{cni.AnnotationCNIMode: cni.CNIModeHostOrigin},
			expectedMode: cni.CNIModeHostOrigin,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newMockClient()
			client.addPod("default", "test-pod", map[string]string{}, tt.annotations)
			provider, err := newKubeProviderFromCNI(makeKubeConfig(), makeCNIArgs("test-pod", "default"), client, slog.Default())
			require.NoError(t, err)
			assert.Equal(t, tt.expectedMode, provider.CNIMode())
		})
	}
}

// TestSplitTunnelCIDRs tests the SplitTunnelCIDRs method
func TestSplitTunnelCIDRs(t *testing.T) {
	tests := []struct {
		name        string
		annotation  string
		expectCIDRs []string
		expectError string
	}{
		{
			name:        "no annotation returns nil",
			annotation:  "",
			expectCIDRs: nil,
		},
		{
			name:        "single CIDR",
			annotation:  "192.168.1.0/24",
			expectCIDRs: []string{"192.168.1.0/24"},
		},
		{
			name:        "multiple CIDRs",
			annotation:  "192.168.1.0/24,10.0.0.0/8",
			expectCIDRs: []string{"192.168.1.0/24", "10.0.0.0/8"},
		},
		{
			name:        "CIDRs with spaces",
			annotation:  "192.168.1.0/24, 10.0.0.0/8",
			expectCIDRs: []string{"192.168.1.0/24", "10.0.0.0/8"},
		},
		{
			name:        "host bits masked to network address",
			annotation:  "192.168.1.5/24",
			expectCIDRs: []string{"192.168.1.0/24"},
		},
		{
			name:        "invalid CIDR returns error",
			annotation:  "not-a-cidr",
			expectError: "invalid",
		},
		{
			name:        "invalid CIDR in list returns error",
			annotation:  "192.168.1.0/24,bad",
			expectError: "invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			annotations := map[string]string{}
			if tt.annotation != "" {
				annotations[cni.AnnotationSplitTunnelCIDRs] = tt.annotation
			}

			client := newMockClient()
			client.addPod("default", "test-pod", map[string]string{}, annotations)
			provider, err := newKubeProviderFromCNI(makeKubeConfig(), makeCNIArgs("test-pod", "default"), client, slog.Default())
			require.NoError(t, err)

			cidrs, err := provider.SplitTunnelCIDRs()

			if tt.expectError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectError)
				return
			}

			require.NoError(t, err)
			if tt.expectCIDRs == nil {
				assert.Nil(t, cidrs)
			} else {
				require.Len(t, cidrs, len(tt.expectCIDRs))
				for i, cidr := range cidrs {
					assert.Equal(t, tt.expectCIDRs[i], cidr.String())
				}
			}
		})
	}
}

// BenchmarkNewKubeProviderFromCNI benchmarks provider creation
func BenchmarkNewKubeProviderFromCNI(b *testing.B) {
	client := newMockClient()
	client.addPod("default", "test-pod", map[string]string{}, map[string]string{})

	kubeConfig := makeKubeConfig()
	cniArgs := makeCNIArgs("test-pod", "default")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = newKubeProviderFromCNI(kubeConfig, cniArgs, client, slog.Default())
	}
}

// BenchmarkFetchLabel benchmarks the fetchLabel function
func BenchmarkFetchLabel(b *testing.B) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app":                      "myapp",
				"version":                  "v1",
				cni.AnnotationWGConfSecret: "my-secret",
				"environment":              "prod",
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = fetchLabel(pod, cni.AnnotationWGConfSecret)
	}
}

// BenchmarkFetchRawWireguardConfig benchmarks the FetchRawWireguardConfig method
func BenchmarkFetchRawWireguardConfig(b *testing.B) {
	client := newMockClient()
	client.addPod("default", "test-pod", map[string]string{
		cni.LabelEnabled: "true",
	}, map[string]string{
		cni.AnnotationWGConfSecret: "wg-secret",
	})
	client.addSecret("default", "wg-secret", map[string][]byte{
		cni.SecretKeyWGConf: []byte("config"),
	})

	kubeConfig := makeKubeConfig()
	cniArgs := makeCNIArgs("test-pod", "default")

	provider, _ := newKubeProviderFromCNI(kubeConfig, cniArgs, client, slog.Default())

	for b.Loop() {
		_, _ = provider.FetchRawWireguardConfig()
	}
}
