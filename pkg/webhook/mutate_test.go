package webhook

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestMutateHandler_InitContainerInjection tests that init containers are injected correctly
func TestMutateHandler_InitContainerInjection(t *testing.T) {
	tests := []struct {
		name                string
		pod                 *corev1.Pod
		expectPatch         bool
		expectInitContainer bool
	}{
		{
			name: "pod with gopher.cni/wgconf-secret set should be mutated",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels: map[string]string{
						"gopher.cni/enabled": "true",
					},
					Annotations: map[string]string{
						"gopher.cni/wgconf-secret": "wgsecret",
						"gopher.cni/dns-tunneled":  "false",
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
			},
			expectPatch:         true,
			expectInitContainer: true,
		},
		{
			name: "pod without label should not be mutated",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx:latest",
						},
					},
				},
			},
			expectPatch:         false,
			expectInitContainer: false,
		},
		{
			name: "pod with gopher.cni/wgconf-secret='' should not be mutated",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels: map[string]string{
						"gopher.cni/enabled": "",
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
			},
			expectPatch:         false,
			expectInitContainer: false,
		},
		{
			name: "pod with existing init container with same name should not be mutated",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels: map[string]string{
						"gopher.cni/enabled": "true",
					},
					Annotations: map[string]string{
						"gopher.cni/wgconf-secret": "wgsecret",
						"gopher.cni/dns-tunneled":  "false",
					},
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:  InitContainerName,
							Image: "existing:latest",
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
			expectPatch:         false,
			expectInitContainer: false,
		},
		{
			name: "pod with no init containers should have init container array created",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels: map[string]string{
						"gopher.cni/enabled": "true",
					},
					Annotations: map[string]string{
						"gopher.cni/wgconf-secret": "wgsecret",
						"gopher.cni/dns-tunneled":  "false",
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
			},
			expectPatch:         true,
			expectInitContainer: true,
		},
		{
			name: "pod with existing init containers should append to array",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels: map[string]string{
						"gopher.cni/enabled": "true",
					},
					Annotations: map[string]string{
						"gopher.cni/wgconf-secret": "wgsecret",
						"gopher.cni/dns-tunneled":  "false",
					},
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:  "other-init",
							Image: "other:latest",
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
			expectPatch:         true,
			expectInitContainer: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultWebhookConfig()
			handler := NewMutateHandler(config)
			ar := createAdmissionReview(tt.pod)
			response := handler.mutate(ar)

			if !response.Allowed {
				t.Errorf("expected mutation to be allowed, got denied with message: %v", response.Result)
			}

			if tt.expectPatch {
				if response.Patch == nil {
					t.Errorf("expected patch to be present, got nil")
				} else {
					// Verify the patch contains the init container
					var patches []PatchOperation
					if err := json.Unmarshal(response.Patch, &patches); err != nil {
						t.Fatalf("failed to unmarshal patches: %v", err)
					}

					if len(patches) == 0 {
						t.Errorf("expected patches to be non-empty")
					}

					// Verify at least one patch is for init containers
					foundInitContainerPatch := false
					for _, patch := range patches {
						if patch.Path == "/spec/initContainers" || patch.Path == "/spec/initContainers/-" {
							foundInitContainerPatch = true
							break
						}
					}

					if !foundInitContainerPatch {
						t.Errorf("expected to find init container patch, got patches: %+v", patches)
					}
				}

				if response.PatchType == nil {
					t.Errorf("expected PatchType to be set")
				} else if *response.PatchType != admissionv1.PatchTypeJSONPatch {
					t.Errorf("expected PatchType to be JSONPatch, got %v", *response.PatchType)
				}
			} else {
				if response.Patch != nil {
					t.Errorf("expected no patch, got patch: %s", string(response.Patch))
				}
			}
		})
	}
}

// TestMutateHandler_PatchOperations tests the patch operations are correct
func TestMutateHandler_PatchOperations(t *testing.T) {
	t.Run("patch for pod with no init containers creates array", func(t *testing.T) {
		config := DefaultWebhookConfig()
		handler := NewMutateHandler(config)

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
				Labels: map[string]string{
					"gopher.cni/enabled": "true",
				},
				Annotations: map[string]string{
					"gopher.cni/wgconf-secret": "wgsecret",
					"gopher.cni/dns-tunneled":  "false",
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
		response := handler.mutate(ar)

		if !response.Allowed {
			t.Fatalf("expected mutation to be allowed")
		}

		var patches []PatchOperation
		if err := json.Unmarshal(response.Patch, &patches); err != nil {
			t.Fatalf("failed to unmarshal patches: %v", err)
		}

		if len(patches) != 1 {
			t.Errorf("expected exactly 1 patch, got %d", len(patches))
		}

		patch := patches[0]
		if patch.Op != "add" {
			t.Errorf("expected Op to be 'add', got %s", patch.Op)
		}

		if patch.Path != "/spec/initContainers" {
			t.Errorf("expected Path to be '/spec/initContainers', got %s", patch.Path)
		}

		// Verify the value is an array - JSON unmarshaling gives us []interface{}
		valueArray, ok := patch.Value.([]interface{})
		if !ok {
			t.Fatalf("expected Value to be array, got %T", patch.Value)
		}

		if len(valueArray) != 1 {
			t.Errorf("expected 1 container in array, got %d", len(valueArray))
		}

		// Re-marshal and unmarshal to get the container structure
		containerBytes, err := json.Marshal(valueArray[0])
		if err != nil {
			t.Fatalf("failed to marshal container: %v", err)
		}

		var container corev1.Container
		if err := json.Unmarshal(containerBytes, &container); err != nil {
			t.Fatalf("failed to unmarshal container: %v", err)
		}

		if container.Name != InitContainerName {
			t.Errorf("expected container name to be %s, got %s", InitContainerName, container.Name)
		}

		if container.Image != config.Image {
			t.Errorf("expected container image to be %s, got %s", config.Image, container.Image)
		}
	})

	t.Run("patch for pod with existing init containers appends to array", func(t *testing.T) {
		config := DefaultWebhookConfig()
		handler := NewMutateHandler(config)

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
				Labels: map[string]string{
					"gopher.cni/enabled": "true",
				},
				Annotations: map[string]string{
					"gopher.cni/wgconf-secret": "wgsecret",
					"gopher.cni/dns-tunneled":  "false",
				},
			},
			Spec: corev1.PodSpec{
				InitContainers: []corev1.Container{
					{
						Name:  "existing-init",
						Image: "existing:latest",
					},
				},
				Containers: []corev1.Container{
					{
						Name:  "app",
						Image: "nginx:latest",
					},
				},
			},
		}

		ar := createAdmissionReview(pod)
		response := handler.mutate(ar)

		if !response.Allowed {
			t.Fatalf("expected mutation to be allowed")
		}

		var patches []PatchOperation
		if err := json.Unmarshal(response.Patch, &patches); err != nil {
			t.Fatalf("failed to unmarshal patches: %v", err)
		}

		if len(patches) != 1 {
			t.Errorf("expected exactly 1 patch, got %d", len(patches))
		}

		patch := patches[0]
		if patch.Op != "add" {
			t.Errorf("expected Op to be 'add', got %s", patch.Op)
		}

		if patch.Path != "/spec/initContainers/-" {
			t.Errorf("expected Path to be '/spec/initContainers/-', got %s", patch.Path)
		}

		// Verify the value is a single container - JSON unmarshaling gives us map[string]interface{}
		containerBytes, err := json.Marshal(patch.Value)
		if err != nil {
			t.Fatalf("failed to marshal container: %v", err)
		}

		var container corev1.Container
		if err := json.Unmarshal(containerBytes, &container); err != nil {
			t.Fatalf("failed to unmarshal container: %v", err)
		}

		if container.Name != InitContainerName {
			t.Errorf("expected container name to be %s, got %s", InitContainerName, container.Name)
		}

		if container.Image != config.Image {
			t.Errorf("expected container image to be %s, got %s", config.Image, container.Image)
		}
	})
}

// TestMutateHandler_HTTPEndpoint tests the HTTP endpoint handling
func TestMutateHandler_HTTPEndpoint(t *testing.T) {
	config := DefaultWebhookConfig()
	handler := NewMutateHandler(config)

	t.Run("valid admission review", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
				Labels: map[string]string{
					"gopher.cni/enabled": "true",
				},
				Annotations: map[string]string{
					"gopher.cni/wgconf-secret": "wgsecret",
					"gopher.cni/dns-tunneled":  "false",
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

		req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(body))
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

		if !responseAR.Response.Allowed {
			t.Errorf("expected response to be allowed")
		}
	})

	t.Run("empty body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader([]byte{}))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status code %d, got %d", http.StatusBadRequest, w.Code)
		}
	})

	t.Run("invalid content type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader([]byte("{}")))
		req.Header.Set("Content-Type", "text/plain")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnsupportedMediaType {
			t.Errorf("expected status code %d, got %d", http.StatusUnsupportedMediaType, w.Code)
		}
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader([]byte("invalid json")))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status code %d, got %d", http.StatusBadRequest, w.Code)
		}
	})
}

// TestMutateHandler_InitContainerConfiguration tests the init container configuration
func TestMutateHandler_InitContainerConfiguration(t *testing.T) {
	config := DefaultWebhookConfig()
	config.Image = "custom-image:v1.2.3"
	handler := NewMutateHandler(config)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels: map[string]string{
				"gopher.cni/enabled": "true",
			},
			Annotations: map[string]string{
				"gopher.cni/wgconf-secret": "wgsecret",
				"gopher.cni/dns-tunneled":  "false",
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
	response := handler.mutate(ar)

	if !response.Allowed {
		t.Fatalf("expected mutation to be allowed")
	}

	var patches []PatchOperation
	if err := json.Unmarshal(response.Patch, &patches); err != nil {
		t.Fatalf("failed to unmarshal patches: %v", err)
	}

	// Get the init container from the patch
	var initContainer corev1.Container
	if patches[0].Path == "/spec/initContainers" {
		// When creating the array, the value is an array
		valueArray, ok := patches[0].Value.([]interface{})
		if !ok {
			t.Fatalf("expected Value to be array, got %T", patches[0].Value)
		}
		containerBytes, err := json.Marshal(valueArray[0])
		if err != nil {
			t.Fatalf("failed to marshal container: %v", err)
		}
		if err := json.Unmarshal(containerBytes, &initContainer); err != nil {
			t.Fatalf("failed to unmarshal container: %v", err)
		}
	} else {
		// When appending, the value is a single container
		containerBytes, err := json.Marshal(patches[0].Value)
		if err != nil {
			t.Fatalf("failed to marshal container: %v", err)
		}
		if err := json.Unmarshal(containerBytes, &initContainer); err != nil {
			t.Fatalf("failed to unmarshal container: %v", err)
		}
	}

	// Verify the container configuration
	if initContainer.Name != InitContainerName {
		t.Errorf("expected init container name to be %s, got %s", InitContainerName, initContainer.Name)
	}

	if initContainer.Image != "custom-image:v1.2.3" {
		t.Errorf("expected init container image to be 'custom-image:v1.2.3', got %s", initContainer.Image)
	}

	if len(initContainer.Command) != 1 || initContainer.Command[0] != "/gopher" {
		t.Errorf("expected init container command to be ['/gopher'], got %v", initContainer.Command)
	}
}
