package webhook

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bartlettc22/gopher-cni/internal/cni"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// TestValidateHandler_ContainerConflicts tests that a pod carrying the injected
// init container (added by the mutating webhook before validation runs) is allowed.
func TestValidateHandler_ContainerConflicts(t *testing.T) {
	tests := []struct {
		name          string
		pod           *corev1.Pod
		expectedAllow bool
		errorContains string
	}{
		{
			name: "pod with injected init container is allowed",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels: map[string]string{
						cni.LabelEnabled: "true",
					},
					Annotations: map[string]string{
						cni.AnnotationWGConfSecret: "wgsecret",
					},
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:  InitContainerName,
							Image: "gopher-cni:latest",
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx:latest",
						},
					},
				},
			},
			expectedAllow: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultWebhookConfig()
			handler := NewValidateHandler(config)
			ar := createAdmissionReview(tt.pod)
			response := handler.validate(ar)

			if response.Allowed != tt.expectedAllow {
				t.Errorf("expected Allowed=%v, got %v", tt.expectedAllow, response.Allowed)
			}

			if !tt.expectedAllow && tt.errorContains != "" && response.Result != nil {
				if response.Result.Message == "" {
					t.Errorf("expected error message containing '%s', got empty message", tt.errorContains)
				}
			}
		})
	}
}

// TestValidateHandler_HostNetworkValidation tests host network validation
func TestValidateHandler_HostNetworkValidation(t *testing.T) {
	tests := []struct {
		name          string
		labels        map[string]string
		annotations   map[string]string
		expectedAllow bool
		description   string
	}{
		{
			name: "pod with gopher.cni/enabled set and hostNetwork should be rejected",
			labels: map[string]string{
				cni.LabelEnabled: "true",
			},
			annotations: map[string]string{
				cni.AnnotationWGConfSecret: "wgsecret",
			},
			expectedAllow: false,
			description:   "hostNetwork mode is not supported with gopher-cni",
		},
		{
			name:          "pod without gopher.cni/enabled label and hostNetwork should be allowed",
			labels:        map[string]string{},
			annotations:   map[string]string{},
			expectedAllow: true,
			description:   "validation should be skipped for pods without the label",
		},
		{
			name: "pod with gopher.cni/enabled=false and hostNetwork should be allowed",
			labels: map[string]string{
				cni.LabelEnabled: "false",
			},
			annotations:   map[string]string{},
			expectedAllow: true,
			description:   "validation should be skipped for pods with label set to false",
		},
		{
			name:          "pod with no labels and hostNetwork should be allowed",
			labels:        nil,
			annotations:   map[string]string{},
			expectedAllow: true,
			description:   "validation should be skipped for pods with no labels",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-pod",
					Namespace:   "default",
					Labels:      tt.labels,
					Annotations: tt.annotations,
				},
				Spec: corev1.PodSpec{
					HostNetwork: true,
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx:latest",
						},
					},
				},
			}

			config := DefaultWebhookConfig()
			handler := NewValidateHandler(config)
			ar := createAdmissionReview(pod)
			response := handler.validate(ar)

			if response.Allowed != tt.expectedAllow {
				t.Errorf("expected Allowed=%v, got %v. Description: %s", tt.expectedAllow, response.Allowed, tt.description)
				if response.Result != nil {
					t.Errorf("Response message: %s", response.Result.Message)
				}
			}

			if !tt.expectedAllow && (response.Result == nil || response.Result.Message == "") {
				t.Errorf("expected error message about hostNetwork")
			}
		})
	}
}

// TestValidateHandler_HTTPEndpoint tests the HTTP endpoint handling
func TestValidateHandler_HTTPEndpoint(t *testing.T) {
	config := DefaultWebhookConfig()
	handler := NewValidateHandler(config)

	t.Run("valid admission review", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
				Labels: map[string]string{
					cni.LabelEnabled: "true",
				},
				Annotations: map[string]string{
					cni.AnnotationWGConfSecret: "wgsecret",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "app",
						Image: "nginx:latest",
					},
				},
			},
		}

		ar := createAdmissionReview(pod)
		body, err := json.Marshal(ar)
		if err != nil {
			t.Fatalf("failed to marshal admission review: %v", err)
		}

		req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status code %d, got %d", http.StatusOK, w.Code)
		}

		var responseAR admissionv1.AdmissionReview
		if err := json.Unmarshal(w.Body.Bytes(), &responseAR); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if responseAR.Response == nil {
			t.Fatal("expected response to be non-nil")
		}
	})

	t.Run("empty body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader([]byte{}))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status code %d, got %d", http.StatusBadRequest, w.Code)
		}
	})

	t.Run("invalid content type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader([]byte("{}")))
		req.Header.Set("Content-Type", "text/plain")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnsupportedMediaType {
			t.Errorf("expected status code %d, got %d", http.StatusUnsupportedMediaType, w.Code)
		}
	})
}

// TestValidateHandler_LabelValidation tests label validation
func TestValidateHandler_LabelValidation(t *testing.T) {
	tests := []struct {
		name          string
		labels        map[string]string
		annotations   map[string]string
		expectedAllow bool
		errorContains string
	}{
		{
			name:   "missing gopher.cni/enabled label - validation skipped",
			labels: map[string]string{
				// Label not set
			},
			annotations: map[string]string{
				cni.AnnotationWGConfSecret: "my-secret",
			},
			expectedAllow: true, // Validation is skipped, pod is allowed
			errorContains: "",
		},
		{
			name: "gopher.cni/enabled label set to false - validation skipped",
			labels: map[string]string{
				cni.LabelEnabled: "false",
			},
			annotations: map[string]string{
				cni.AnnotationWGConfSecret: "my-secret",
			},
			expectedAllow: true, // Validation is skipped, pod is allowed
			errorContains: "",
		},
		{
			name: "valid gopher.cni/enabled label",
			labels: map[string]string{
				cni.LabelEnabled: "true",
			},
			annotations: map[string]string{
				cni.AnnotationWGConfSecret: "my-secret",
			},
			expectedAllow: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-pod",
					Namespace:   "default",
					Labels:      tt.labels,
					Annotations: tt.annotations,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx:latest",
						},
					},
				},
			}

			config := DefaultWebhookConfig()
			handler := NewValidateHandler(config)
			ar := createAdmissionReview(pod)
			response := handler.validate(ar)

			if response.Allowed != tt.expectedAllow {
				t.Errorf("expected Allowed=%v, got %v", tt.expectedAllow, response.Allowed)
			}

			if !tt.expectedAllow && tt.errorContains != "" {
				if response.Result == nil || response.Result.Message == "" {
					t.Errorf("expected error message containing '%s', got empty message", tt.errorContains)
				} else if !contains(response.Result.Message, tt.errorContains) {
					t.Errorf("expected error message to contain '%s', got '%s'", tt.errorContains, response.Result.Message)
				}
			}
		})
	}
}

// TestValidateHandler_AnnotationValidation tests annotation validation
func TestValidateHandler_AnnotationValidation(t *testing.T) {
	tests := []struct {
		name          string
		annotations   map[string]string
		expectedAllow bool
		errorContains string
	}{
		{
			name:        "missing wgconf-secret annotation",
			annotations: map[string]string{
				// No wgconf-secret annotation
			},
			expectedAllow: false,
			errorContains: "missing required annotation: gopher.cni/wgconf-secret",
		},
		{
			name: "empty wgconf-secret annotation",
			annotations: map[string]string{
				cni.AnnotationWGConfSecret: "",
			},
			expectedAllow: false,
			errorContains: "missing required annotation: gopher.cni/wgconf-secret",
		},
		{
			name: "valid wgconf-secret annotation",
			annotations: map[string]string{
				cni.AnnotationWGConfSecret: "my-wireguard-secret",
			},
			expectedAllow: true,
		},
		{
			name: "invalid cni-mode value",
			annotations: map[string]string{
				cni.AnnotationWGConfSecret: "my-secret",
				cni.AnnotationCNIMode:      "invalid-mode",
			},
			expectedAllow: false,
			errorContains: "must be 'pod-origin' or 'host-origin'",
		},
		{
			name: "valid cni-mode pod-origin",
			annotations: map[string]string{
				cni.AnnotationWGConfSecret: "my-secret",
				cni.AnnotationCNIMode:      "pod-origin",
			},
			expectedAllow: true,
		},
		{
			name: "valid cni-mode host-origin",
			annotations: map[string]string{
				cni.AnnotationWGConfSecret: "my-secret",
				cni.AnnotationCNIMode:      "host-origin",
			},
			expectedAllow: true,
		},
		{
			name: "all annotations valid",
			annotations: map[string]string{
				cni.AnnotationWGConfSecret: "my-secret",
				cni.AnnotationCNIMode:      "host-origin",
			},
			expectedAllow: true,
		},
		{
			name: "multiple invalid annotations",
			annotations: map[string]string{
				cni.AnnotationWGConfSecret:     "my-secret",
				cni.AnnotationCNIMode:          "invalid-mode",
				cni.AnnotationSplitTunnelCIDRs: "not-a-cidr",
			},
			expectedAllow: false,
			errorContains: "invalid value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels: map[string]string{
						cni.LabelEnabled: "true",
					},
					Annotations: tt.annotations,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx:latest",
						},
					},
				},
			}

			config := DefaultWebhookConfig()
			handler := NewValidateHandler(config)
			ar := createAdmissionReview(pod)
			response := handler.validate(ar)

			if response.Allowed != tt.expectedAllow {
				t.Errorf("expected Allowed=%v, got %v", tt.expectedAllow, response.Allowed)
				if response.Result != nil {
					t.Errorf("Response message: %s", response.Result.Message)
				}
			}

			if !tt.expectedAllow && tt.errorContains != "" {
				if response.Result == nil || response.Result.Message == "" {
					t.Errorf("expected error message containing '%s', got empty message", tt.errorContains)
				} else if !contains(response.Result.Message, tt.errorContains) {
					t.Errorf("expected error message to contain '%s', got '%s'", tt.errorContains, response.Result.Message)
				}
			}
		})
	}
}

// TestValidateSplitTunnelOverlap tests overlap detection between split-tunnel CIDRs and protected nets
func TestValidateSplitTunnelOverlap(t *testing.T) {
	const validKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

	wgConf := func(address, dns string) []byte {
		conf := "[Interface]\nPrivateKey = " + validKey + "\nAddress = " + address + "\n"
		if dns != "" {
			conf += "DNS = " + dns + "\n"
		}
		return []byte(conf)
	}

	tests := []struct {
		name         string
		wgConf       []byte
		splitCIDRs   string
		overlapAnnot string
		expectErrors bool
		alwaysReject bool // true if overlap=allow should still fail
	}{
		{
			name:         "no overlap - allowed",
			wgConf:       wgConf("10.2.0.2/32", "10.2.0.1"),
			splitCIDRs:   "192.168.1.0/24",
			expectErrors: false,
		},
		{
			name:         "split CIDR same specificity as WG address - always reject",
			wgConf:       wgConf("10.2.0.2/32", ""),
			splitCIDRs:   "10.2.0.2/32",
			expectErrors: true,
			alwaysReject: true,
		},
		{
			name:         "split CIDR more specific than WG address subnet - always reject",
			wgConf:       wgConf("10.2.0.0/24", ""),
			splitCIDRs:   "10.2.0.0/25",
			expectErrors: true,
			alwaysReject: true,
		},
		{
			name:         "split CIDR less specific than WG address - reject by default",
			wgConf:       wgConf("10.2.0.2/32", ""),
			splitCIDRs:   "10.0.0.0/8",
			expectErrors: true,
			alwaysReject: false,
		},
		{
			name:         "split CIDR less specific but overlap=allow - permitted",
			wgConf:       wgConf("10.2.0.2/32", ""),
			splitCIDRs:   "10.0.0.0/8",
			overlapAnnot: "allow",
			expectErrors: false,
		},
		{
			name:         "split CIDR same specificity as DNS /32 - always reject",
			wgConf:       wgConf("10.2.0.2/32", "10.2.0.1"),
			splitCIDRs:   "10.2.0.1/32",
			expectErrors: true,
			alwaysReject: true,
		},
		{
			name:         "split CIDR less specific overlaps DNS - reject by default",
			wgConf:       wgConf("10.2.0.2/32", "10.2.0.1"),
			splitCIDRs:   "10.0.0.0/8",
			expectErrors: true,
			alwaysReject: false,
		},
		{
			name:         "split CIDR less specific overlaps DNS with overlap=allow - permitted",
			wgConf:       wgConf("192.168.100.1/32", "10.2.0.1"),
			splitCIDRs:   "10.0.0.0/8",
			overlapAnnot: "allow",
			expectErrors: false,
		},
		{
			name:         "split CIDR same specificity overlaps DNS with overlap=allow - still rejected",
			wgConf:       wgConf("192.168.100.1/32", "10.2.0.1"),
			splitCIDRs:   "10.2.0.1/32",
			overlapAnnot: "allow",
			expectErrors: true,
			alwaysReject: true,
		},
		{
			name:         "multiple split CIDRs one overlaps - rejected",
			wgConf:       wgConf("10.2.0.2/32", ""),
			splitCIDRs:   "192.168.1.0/24, 10.2.0.2/32",
			expectErrors: true,
			alwaysReject: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newWebhookMockClient("default", "wg-secret", map[string][]byte{
				cni.SecretKeyWGConf: tt.wgConf,
			})
			config := DefaultWebhookConfig()
			config.KubeClient = client
			handler := NewValidateHandler(config)

			annotations := map[string]string{}
			if tt.overlapAnnot != "" {
				annotations[cni.AnnotationSplitTunnelOverlap] = tt.overlapAnnot
			}

			errs := handler.validateSplitTunnelOverlap("default", tt.splitCIDRs, "wg-secret", annotations)

			if tt.expectErrors && len(errs) == 0 {
				t.Errorf("expected validation errors, got none")
			}
			if !tt.expectErrors && len(errs) > 0 {
				t.Errorf("expected no errors, got: %v", errs)
			}

			// Verify that alwaysReject cases are rejected even with overlap=allow
			if tt.alwaysReject {
				annotationsWithAllow := map[string]string{
					cni.AnnotationSplitTunnelOverlap: "allow",
				}
				errsWithAllow := handler.validateSplitTunnelOverlap("default", tt.splitCIDRs, "wg-secret", annotationsWithAllow)
				if len(errsWithAllow) == 0 {
					t.Errorf("expected rejection even with overlap=allow for same/more-specific overlap")
				}
			}
		})
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > 0 && len(substr) > 0 && stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Helper function to create an admission review request
func createAdmissionReview(pod *corev1.Pod) *admissionv1.AdmissionReview {
	podJSON, _ := json.Marshal(pod)

	return &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Request: &admissionv1.AdmissionRequest{
			UID: "test-uid",
			Kind: metav1.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
			Resource: metav1.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "pods",
			},
			Namespace: pod.Namespace,
			Operation: admissionv1.Create,
			Object: runtime.RawExtension{
				Raw: podJSON,
			},
		},
	}
}
