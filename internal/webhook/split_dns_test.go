package webhook

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/bartlettc22/gopher-cni/internal/cni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// unmarshalPatches unmarshals a JSON patch list from raw bytes.
func unmarshalPatches(t *testing.T, raw []byte) []PatchOperation {
	t.Helper()
	var patches []PatchOperation
	require.NoError(t, json.Unmarshal(raw, &patches), "failed to unmarshal patches")
	return patches
}

// findPatch returns the first patch whose Path matches, or nil.
func findPatch(patches []PatchOperation, path string) *PatchOperation {
	for i := range patches {
		if patches[i].Path == path {
			return &patches[i]
		}
	}
	return nil
}

// unmarshalPatchValue re-marshals a patch Value (interface{}) into T.
func unmarshalPatchValue[T any](t *testing.T, v any) T {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	var out T
	require.NoError(t, json.Unmarshal(b, &out))
	return out
}

// wgConfWithDNS returns a minimal WireGuard config that includes the given DNS lines.
func wgConfWithDNS(dns string) []byte {
	const validKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	conf := "[Interface]\nPrivateKey = " + validKey + "\nAddress = 10.0.0.1/24\n"
	if dns != "" {
		conf += "DNS = " + dns + "\n"
	}
	return []byte(conf)
}

// splitDNSPod builds a pod annotated for split-DNS with the given zones string.
func splitDNSPod(namespace, secretName, zones string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: namespace,
			Labels:    map[string]string{"gopher.cni/enabled": "true"},
			Annotations: map[string]string{
				cni.AnnotationWGConfSecret:        secretName,
				cni.AnnotationSplitTunnelDNSZones: zones,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "nginx:latest"}},
		},
	}
}

// tunnelDNSPod builds a pod annotated for standard tunnel DNS (no split).
func tunnelDNSPod(namespace, secretName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: namespace,
			Labels:    map[string]string{"gopher.cni/enabled": "true"},
			Annotations: map[string]string{
				cni.AnnotationWGConfSecret: secretName,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "nginx:latest"}},
		},
	}
}

func TestParseDNSZones(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "empty string",
			input: "",
			want:  nil,
		},
		{
			name:  "single zone",
			input: "cluster.local",
			want:  []string{"cluster.local"},
		},
		{
			name:  "multiple zones",
			input: "cluster.local,example.com",
			want:  []string{"cluster.local", "example.com"},
		},
		{
			name:  "zones with spaces",
			input: "cluster.local, example.com , corp.internal",
			want:  []string{"cluster.local", "example.com", "corp.internal"},
		},
		{
			name:  "trailing dots stripped",
			input: "cluster.local.,example.com.",
			want:  []string{"cluster.local", "example.com"},
		},
		{
			name:  "empty segments filtered",
			input: "cluster.local,,example.com",
			want:  []string{"cluster.local", "example.com"},
		},
		{
			name:  "only commas",
			input: ",,,",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDNSZones(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGenerateCorefile(t *testing.T) {
	tests := []struct {
		name         string
		zones        []string
		clusterDNSIP string
		tunnelDNSIP  string
		wantContains []string
		wantAbsent   []string
	}{
		{
			name:         "zones with tunnel DNS",
			zones:        []string{"cluster.local", "corp.internal"},
			clusterDNSIP: "10.96.0.10",
			tunnelDNSIP:  "10.8.0.1",
			wantContains: []string{
				"cluster.local {\n    forward . 10.96.0.10\n    log\n}",
				"corp.internal {\n    forward . 10.96.0.10\n    log\n}",
				". {\n    forward . 10.8.0.1\n    log\n}",
			},
		},
		{
			name:         "zones without tunnel DNS — no catch-all",
			zones:        []string{"cluster.local"},
			clusterDNSIP: "10.96.0.10",
			tunnelDNSIP:  "",
			wantContains: []string{
				"cluster.local {\n    forward . 10.96.0.10\n    log\n}",
			},
			wantAbsent: []string{". {"},
		},
		{
			name:         "no zones with tunnel DNS — only catch-all",
			zones:        nil,
			clusterDNSIP: "10.96.0.10",
			tunnelDNSIP:  "10.8.0.1",
			wantContains: []string{". {\n    forward . 10.8.0.1\n    log\n}"},
			wantAbsent:   []string{"cluster.local"},
		},
		{
			name:         "no zones and no tunnel DNS — empty output",
			zones:        nil,
			clusterDNSIP: "10.96.0.10",
			tunnelDNSIP:  "",
			wantAbsent:   []string{"{"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateCorefile(tt.zones, tt.clusterDNSIP, tt.tunnelDNSIP)
			for _, want := range tt.wantContains {
				assert.Contains(t, got, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, got, absent)
			}
		})
	}
}

func TestGetTunnelDNSServers(t *testing.T) {
	tests := []struct {
		name      string
		wgConf    []byte
		expectDNS []string
	}{
		{
			name:      "IPv4 only — all returned",
			wgConf:    wgConfWithDNS("1.1.1.1, 8.8.8.8"),
			expectDNS: []string{"1.1.1.1", "8.8.8.8"},
		},
		{
			name:      "IPv6 only — none returned",
			wgConf:    wgConfWithDNS("2606:4700:4700::1111"),
			expectDNS: nil,
		},
		{
			name:      "mixed IPv4 and IPv6 — only IPv4 returned",
			wgConf:    wgConfWithDNS("1.1.1.1, 2606:4700:4700::1111, 8.8.8.8"),
			expectDNS: []string{"1.1.1.1", "8.8.8.8"},
		},
		{
			name:      "no DNS field — returns nil",
			wgConf:    wgConfWithDNS(""),
			expectDNS: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newWebhookMockClient("default", "wg-secret", map[string][]byte{
				cni.SecretKeyWGConf: tt.wgConf,
			})
			config := DefaultWebhookConfig()
			config.KubeClient = client
			handler := NewMutateHandler(config)

			got, err := handler.getTunnelDNSServers("default", "wg-secret")
			require.NoError(t, err)
			assert.Equal(t, tt.expectDNS, got)
		})
	}
}

func TestCreateDNSPatches_StandardTunnel(t *testing.T) {
	t.Run("WG conf with DNS — sets dnsPolicy=None and nameservers", func(t *testing.T) {
		client := newWebhookMockClient("default", "wg-secret", map[string][]byte{
			cni.SecretKeyWGConf: wgConfWithDNS("10.8.0.1, 10.8.0.2"),
		})
		config := DefaultWebhookConfig()
		config.KubeClient = client
		handler := NewMutateHandler(config)

		pod := tunnelDNSPod("default", "wg-secret")
		patches, err := handler.createDNSPatches(pod)
		require.NoError(t, err)
		require.Len(t, patches, 2)

		policyPatch := findPatch(patches, "/spec/dnsPolicy")
		require.NotNil(t, policyPatch, "expected dnsPolicy patch")
		assert.Equal(t, "replace", policyPatch.Op)
		assert.Equal(t, "None", policyPatch.Value)

		cfgPatch := findPatch(patches, "/spec/dnsConfig")
		require.NotNil(t, cfgPatch, "expected dnsConfig patch")
		dnsConfig := unmarshalPatchValue[corev1.PodDNSConfig](t, cfgPatch.Value)
		assert.Equal(t, []string{"10.8.0.1", "10.8.0.2"}, dnsConfig.Nameservers)
	})

	t.Run("WG conf without DNS — no patches", func(t *testing.T) {
		config := defaultTestConfig()
		handler := NewMutateHandler(config)

		pod := tunnelDNSPod("default", "wgsecret")
		patches, err := handler.createDNSPatches(pod)
		require.NoError(t, err)
		assert.Empty(t, patches)
	})

	t.Run("secret not found — returns error", func(t *testing.T) {
		config := DefaultWebhookConfig()
		config.KubeClient = &webhookMockClient{secrets: map[string]*corev1.Secret{}}
		handler := NewMutateHandler(config)

		pod := tunnelDNSPod("default", "missing-secret")
		_, err := handler.createDNSPatches(pod)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing-secret")
	})
}

func TestCreateDNSPatches_SplitDNS(t *testing.T) {
	t.Run("split DNS injects CoreDNS sidecar and routes DNS through 127.0.0.1", func(t *testing.T) {
		client := newWebhookMockClient("default", "wg-secret", map[string][]byte{
			cni.SecretKeyWGConf: wgConfWithDNS("10.8.0.1"),
		})
		config := DefaultWebhookConfig()
		config.KubeClient = client
		handler := NewMutateHandler(config)

		pod := splitDNSPod("default", "wg-secret", "cluster.local, corp.internal")
		patches, err := handler.createDNSPatches(pod)
		require.NoError(t, err)

		// volume, config init container, coredns sidecar, dnsPolicy, dnsConfig
		require.Len(t, patches, 5)

		volumePatch := findPatch(patches, "/spec/volumes")
		require.NotNil(t, volumePatch, "expected volumes patch")
		volumes := unmarshalPatchValue[[]corev1.Volume](t, volumePatch.Value)
		require.Len(t, volumes, 1)
		assert.Equal(t, CoreDNSVolumeName, volumes[0].Name)
		assert.NotNil(t, volumes[0].VolumeSource.EmptyDir)

		initPatch := findPatch(patches, "/spec/initContainers/-")
		require.NotNil(t, initPatch, "expected initContainers patch")
		initCtr := unmarshalPatchValue[corev1.Container](t, initPatch.Value)
		assert.Equal(t, CoreDNSConfigContainerName, initCtr.Name)
		require.Len(t, initCtr.Env, 1)
		assert.Equal(t, "COREFILE", initCtr.Env[0].Name)
		assert.Contains(t, initCtr.Env[0].Value, "cluster.local")
		assert.Contains(t, initCtr.Env[0].Value, "corp.internal")
		assert.Contains(t, initCtr.Env[0].Value, "10.96.0.10") // default mock cluster DNS
		assert.Contains(t, initCtr.Env[0].Value, "10.8.0.1")   // tunnel DNS

		sidecarPatch := findPatch(patches, "/spec/containers/-")
		require.NotNil(t, sidecarPatch, "expected containers patch")
		sidecarCtr := unmarshalPatchValue[corev1.Container](t, sidecarPatch.Value)
		assert.Equal(t, CoreDNSContainerName, sidecarCtr.Name)
		assert.Equal(t, config.CoreDNSImage, sidecarCtr.Image)

		policyPatch := findPatch(patches, "/spec/dnsPolicy")
		require.NotNil(t, policyPatch)
		assert.Equal(t, "None", policyPatch.Value)

		cfgPatch := findPatch(patches, "/spec/dnsConfig")
		require.NotNil(t, cfgPatch)
		dnsConfig := unmarshalPatchValue[corev1.PodDNSConfig](t, cfgPatch.Value)
		assert.Equal(t, []string{"127.0.0.1"}, dnsConfig.Nameservers)
	})

	t.Run("split DNS with existing volumes appends to array", func(t *testing.T) {
		client := newWebhookMockClient("default", "wg-secret", map[string][]byte{
			cni.SecretKeyWGConf: wgConfWithDNS("10.8.0.1"),
		})
		config := DefaultWebhookConfig()
		config.KubeClient = client
		handler := NewMutateHandler(config)

		pod := splitDNSPod("default", "wg-secret", "cluster.local")
		pod.Spec.Volumes = []corev1.Volume{{Name: "existing-vol"}}
		patches, err := handler.createDNSPatches(pod)
		require.NoError(t, err)

		volumePatch := findPatch(patches, "/spec/volumes/-")
		require.NotNil(t, volumePatch, "expected volumes append patch")
		vol := unmarshalPatchValue[corev1.Volume](t, volumePatch.Value)
		assert.Equal(t, CoreDNSVolumeName, vol.Name)
	})

	t.Run("split DNS without WG tunnel DNS — no catch-all in Corefile", func(t *testing.T) {
		config := defaultTestConfig() // minimalWGConf has no DNS
		handler := NewMutateHandler(config)

		pod := splitDNSPod("default", "wgsecret", "cluster.local")
		patches, err := handler.createDNSPatches(pod)
		require.NoError(t, err)

		initPatch := findPatch(patches, "/spec/initContainers/-")
		require.NotNil(t, initPatch)
		initCtr := unmarshalPatchValue[corev1.Container](t, initPatch.Value)
		corefile := initCtr.Env[0].Value
		assert.Contains(t, corefile, "cluster.local")
		assert.NotContains(t, corefile, ". {", "catch-all block should be absent when there is no tunnel DNS")
	})

	t.Run("cluster DNS lookup failure returns error", func(t *testing.T) {
		client := newWebhookMockClient("default", "wg-secret", map[string][]byte{
			cni.SecretKeyWGConf: wgConfWithDNS("10.8.0.1"),
		})
		client.svcErr = fmt.Errorf("service not found")
		config := DefaultWebhookConfig()
		config.KubeClient = client
		handler := NewMutateHandler(config)

		pod := splitDNSPod("default", "wg-secret", "cluster.local")
		_, err := handler.createDNSPatches(pod)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "kube-dns")
	})

	t.Run("existing CoreDNS container name conflict returns error", func(t *testing.T) {
		client := newWebhookMockClient("default", "wg-secret", map[string][]byte{
			cni.SecretKeyWGConf: wgConfWithDNS("10.8.0.1"),
		})
		config := DefaultWebhookConfig()
		config.KubeClient = client
		handler := NewMutateHandler(config)

		pod := splitDNSPod("default", "wg-secret", "cluster.local")
		pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Name: CoreDNSContainerName})
		_, err := handler.createDNSPatches(pod)
		require.Error(t, err)
		assert.Contains(t, err.Error(), CoreDNSContainerName)
	})
}
