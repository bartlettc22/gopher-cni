package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/bartlettc22/gopher-cni/pkg/cni"
	"github.com/bartlettc22/gopher-cni/pkg/wireguard"
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

	mutateLogger.Debug("validating webhook request", "method", r.Method, "path", r.URL.Path)

	var body []byte
	if r.Body != nil {
		if data, err := io.ReadAll(r.Body); err == nil {
			body = data
		}
	}

	if len(body) == 0 {
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}

	// Verify content type
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		http.Error(w, "invalid Content-Type, expected application/json", http.StatusUnsupportedMediaType)
		return
	}

	// Decode admission review request
	admissionReview := admissionv1.AdmissionReview{}
	if _, _, err := deserializer.Decode(body, nil, &admissionReview); err != nil {
		http.Error(w, fmt.Sprintf("could not decode body: %v", err), http.StatusBadRequest)
		return
	}

	// Process the admission request
	if admissionReview.Request == nil {
		http.Error(w, "admission request is nil", http.StatusBadRequest)
		return
	}
	admissionResponse := h.mutate(&admissionReview)

	// Create the response
	responseAdmissionReview := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Response: admissionResponse,
	}

	// Set the UID from the request
	if admissionReview.Request != nil {
		responseAdmissionReview.Response.UID = admissionReview.Request.UID
	}

	// Marshal and send response
	respBytes, err := json.Marshal(responseAdmissionReview)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(respBytes); err != nil {
		http.Error(w, fmt.Sprintf("could not write response: %v", err), http.StatusInternalServerError)
	}
}

// mutate processes the admission review and returns a response
func (h *MutateHandler) mutate(ar *admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	req := ar.Request

	// Default response
	response := &admissionv1.AdmissionResponse{
		Allowed: true,
	}

	// Decode the pod
	pod := corev1.Pod{}
	if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
		response.Allowed = false
		response.Result = &metav1.Status{
			Status:  "Failure",
			Message: fmt.Sprintf("could not unmarshal pod: %v", err),
			Code:    http.StatusBadRequest,
		}
		return response
	}
	// For new pod creates the namespace is in req.Namespace, not the pod object.
	if pod.Namespace == "" {
		pod.Namespace = req.Namespace
	}

	// Check if injection is needed
	if !shouldInject(&pod) {
		// No injection needed, allow the pod as-is
		mutateLogger.Debug("skipping mutation, no injection needed", "namespace", pod.Namespace, "pod", pod.Name)
		return response
	}

	// Fail early with a clear message if the required wgconf-secret annotation is missing
	if pod.Annotations == nil || pod.Annotations[cni.AnnotationWGConfSecret] == "" {
		response.Allowed = false
		response.Result = &metav1.Status{
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
		}
		return response
	}

	// Create patches for the pod
	patches, err := h.createPatches(&pod)
	if err != nil {
		response.Allowed = false
		response.Result = &metav1.Status{
			Status:  "Failure",
			Message: fmt.Sprintf("could not create patches: %v", err),
			Code:    http.StatusInternalServerError,
		}
		return response
	}

	// If no patches needed, return
	if len(patches) == 0 {
		return response
	}

	// Marshal patches
	patchBytes, err := json.Marshal(patches)
	if err != nil {
		response.Allowed = false
		response.Result = &metav1.Status{
			Status:  "Failure",
			Message: fmt.Sprintf("could not marshal patches: %v", err),
			Code:    http.StatusInternalServerError,
		}
		return response
	}

	// Set patch type and data
	patchType := admissionv1.PatchTypeJSONPatch
	response.PatchType = &patchType
	response.Patch = patchBytes

	return response
}

// createPatches creates JSON patches for the pod
func (h *MutateHandler) createPatches(pod *corev1.Pod) ([]PatchOperation, error) {
	var patches []PatchOperation

	mutateLogger.Debug("mutating pod", "namespace", pod.Namespace, "pod", pod.Name)

	// Add init container patches
	initPatches, err := h.createInitContainerPatches(pod)
	if err != nil {
		return nil, fmt.Errorf("failed to create init container patches: %w", err)
	}
	patches = append(patches, initPatches...)

	// Check if NAT-PMP annotation is set to true
	if pod.Annotations != nil && pod.Annotations[cni.AnnotationNATPMP] == "true" {
		mutateLogger.Debug("NAT-PMP enabled, injecting sidecar", "namespace", pod.Namespace, "pod", pod.Name)
		// Add sidecar container patches
		sidecarPatches, err := h.createSidecarPatches(pod)
		if err != nil {
			return nil, fmt.Errorf("failed to create sidecar patches: %w", err)
		}
		patches = append(patches, sidecarPatches...)
	}

	// Check if DNS tunneling is enabled (defaults to true if not set)
	dnsTunneled := "true" // default value
	if pod.Annotations != nil {
		if val, ok := pod.Annotations[cni.AnnotationDNSTunneled]; ok {
			dnsTunneled = val
		}
	}

	if dnsTunneled == "true" {
		mutateLogger.Debug("DNS tunneling enabled, configuring DNS", "namespace", pod.Namespace, "pod", pod.Name)
		// Add DNS configuration patches
		dnsPatches, err := h.createDNSPatches(pod)
		if err != nil {
			return nil, fmt.Errorf("failed to create DNS patches: %w", err)
		}
		patches = append(patches, dnsPatches...)
	}

	return patches, nil
}

// createInitContainerPatches creates patches for the init container
func (h *MutateHandler) createInitContainerPatches(pod *corev1.Pod) ([]PatchOperation, error) {
	var patches []PatchOperation

	if hasContainer(pod.Spec.InitContainers, InitContainerName) {
		return nil, nil
	}

	initContainer := h.Config.createInitContainer()

	// Determine the path and operation
	if len(pod.Spec.InitContainers) == 0 {
		// No init containers exist, create the array
		patches = append(patches, PatchOperation{
			Op:    "add",
			Path:  "/spec/initContainers",
			Value: []corev1.Container{initContainer},
		})
	} else {
		// Add to existing init containers
		patches = append(patches, PatchOperation{
			Op:    "add",
			Path:  "/spec/initContainers/-",
			Value: initContainer,
		})
	}

	return patches, nil
}

// createSidecarPatches creates patches for the sidecar container
func (h *MutateHandler) createSidecarPatches(pod *corev1.Pod) ([]PatchOperation, error) {
	var patches []PatchOperation

	// Check if sidecar container already exists - this is an error condition
	if hasContainer(pod.Spec.Containers, SidecarContainerName) {
		return nil, fmt.Errorf("sidecar container with name '%s' already exists", SidecarContainerName)
	}

	sidecarContainer := h.Config.createSidecarContainer()

	// Add sidecar to containers array
	// Containers array always exists in a pod spec
	patches = append(patches, PatchOperation{
		Op:    "add",
		Path:  "/spec/containers/-",
		Value: sidecarContainer,
	})

	return patches, nil
}

// createDNSPatches creates patches for DNS configuration when dns-tunneled is enabled
func (h *MutateHandler) createDNSPatches(pod *corev1.Pod) ([]PatchOperation, error) {
	var patches []PatchOperation

	// Set DNS policy to None
	patches = append(patches, PatchOperation{
		Op:    "replace",
		Path:  "/spec/dnsPolicy",
		Value: "None",
	})

	// Get the WireGuard secret name from annotations
	secretName, ok := pod.Annotations[cni.AnnotationWGConfSecret]
	if !ok || secretName == "" {
		return nil, fmt.Errorf("dns-tunneled enabled but no wgconf-secret annotation found")
	}

	// Read the secret to get DNS servers
	dnsServers, err := h.getDNSServersFromSecret(pod.Namespace, secretName)
	if err != nil {
		return nil, fmt.Errorf("failed to read DNS from secret %s: %w", secretName, err)
	}

	// Create dnsConfig with nameservers from WireGuard config
	dnsConfig := corev1.PodDNSConfig{
		Nameservers: dnsServers,
	}

	patches = append(patches, PatchOperation{
		Op:    "add",
		Path:  "/spec/dnsConfig",
		Value: dnsConfig,
	})

	return patches, nil
}

// getDNSServersFromSecret reads the WireGuard config from a secret and extracts DNS servers
func (h *MutateHandler) getDNSServersFromSecret(namespace, secretName string) ([]string, error) {
	if h.Config.KubeClient == nil {
		return nil, fmt.Errorf("kubernetes client not configured")
	}

	// Read the secret
	secret, err := h.Config.KubeClient.GetSecret(context.TODO(), namespace, secretName)
	if err != nil {
		return nil, fmt.Errorf("failed to get secret: %w", err)
	}

	// Get the wg0.conf data from the secret
	wgConfData, ok := secret.Data[cni.SecretKeyWGConf]
	if !ok {
		return nil, fmt.Errorf("secret does not contain %s key", cni.SecretKeyWGConf)
	}

	// Parse the WireGuard config
	wgConfig, err := wireguard.ParseConfig(wgConfData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse wireguard config: %w", err)
	}

	// Convert DNS IPs to strings
	dnsServers := make([]string, len(wgConfig.DNS))
	for i, ip := range wgConfig.DNS {
		dnsServers[i] = ip.String()
	}

	return dnsServers, nil
}
