package webhook

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/bartlettc22/gopher-cni/internal/cni"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
	mutateLogger  = slog.With("component", "webhook-mutate")
)

// MutateHandler handles mutating admission webhook requests
type MutateHandler struct {
	Config *WebhookConfig
}

// NewMutateHandler creates a new mutate handler
func NewMutateHandler(config *WebhookConfig) *MutateHandler {
	return &MutateHandler{
		Config: config,
	}
}

// ServeHTTP implements the http.Handler interface
func (h *MutateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	serveAdmission(w, r, mutateLogger, h.mutate)
}

// mutate processes the admission review and returns a response
func (h *MutateHandler) mutate(ar *admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	pod, early := preparePod(ar.Request)
	if early != nil {
		return early
	}

	if pod.Annotations == nil || pod.Annotations[cni.AnnotationWGConfSecret] == "" {
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Status:  "Failure",
				Message: fmt.Sprintf("missing required annotation: %s must specify a secret name", cni.AnnotationWGConfSecret),
				Code:    http.StatusUnprocessableEntity,
				Reason:  metav1.StatusReasonInvalid,
				Details: &metav1.StatusDetails{
					Causes: []metav1.StatusCause{{
						Type:    metav1.CauseTypeFieldValueRequired,
						Message: "must specify a secret name",
						Field:   cni.AnnotationWGConfSecret,
					}},
				},
			},
		}
	}

	patches, err := h.createPatches(pod)
	if err != nil {
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Status:  "Failure",
				Message: fmt.Sprintf("could not create patches: %v", err),
				Code:    http.StatusInternalServerError,
			},
		}
	}

	if len(patches) == 0 {
		return &admissionv1.AdmissionResponse{Allowed: true}
	}

	patchBytes, err := json.Marshal(patches)
	if err != nil {
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Status:  "Failure",
				Message: fmt.Sprintf("could not marshal patches: %v", err),
				Code:    http.StatusInternalServerError,
			},
		}
	}

	patchType := admissionv1.PatchTypeJSONPatch
	return &admissionv1.AdmissionResponse{
		Allowed:   true,
		PatchType: &patchType,
		Patch:     patchBytes,
	}
}

// createPatches creates JSON patches for the pod
func (h *MutateHandler) createPatches(pod *corev1.Pod) ([]PatchOperation, error) {
	var patches []PatchOperation

	mutateLogger.Debug("mutating pod", "namespace", pod.Namespace, "name", pod.Name)

	initPatches, err := h.createInitContainerPatches(pod)
	if err != nil {
		return nil, fmt.Errorf("failed to create init container patches: %w", err)
	}
	patches = append(patches, initPatches...)

	dnsPatches, err := h.createDNSPatches(pod)
	if err != nil {
		return nil, fmt.Errorf("failed to create DNS patches: %w", err)
	}
	patches = append(patches, dnsPatches...)

	return patches, nil
}

// createInitContainerPatches creates patches for the validator init container
func (h *MutateHandler) createInitContainerPatches(pod *corev1.Pod) ([]PatchOperation, error) {
	if hasContainer(pod.Spec.InitContainers, InitContainerName) {
		return nil, nil
	}

	initContainer := h.Config.createInitContainer()

	if len(pod.Spec.InitContainers) == 0 {
		return []PatchOperation{{
			Op:    "add",
			Path:  "/spec/initContainers",
			Value: []corev1.Container{initContainer},
		}}, nil
	}
	return []PatchOperation{{
		Op:    "add",
		Path:  "/spec/initContainers/-",
		Value: initContainer,
	}}, nil
}
