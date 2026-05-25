package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/bartlettc22/gopher-cni/pkg/cni"
	"github.com/bartlettc22/gopher-cni/pkg/wireguard"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	validateLogger = slog.With("component", "webhook-validate")
)

// ValidateHandler handles validating admission webhook requests
type ValidateHandler struct {
	Config *WebhookConfig
}

// NewValidateHandler creates a new validate handler
func NewValidateHandler(config *WebhookConfig) *ValidateHandler {
	return &ValidateHandler{
		Config: config,
	}
}

// ServeHTTP implements the http.Handler interface
func (h *ValidateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	validateLogger.Debug("validating webhook request", "method", r.Method, "path", r.URL.Path)

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
	admissionResponse := h.validate(&admissionReview)

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

// validate processes the admission review and returns a response
func (h *ValidateHandler) validate(ar *admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	req := ar.Request

	// Default response
	response := &admissionv1.AdmissionResponse{
		Allowed: true,
	}

	// Only pods are validated
	if req.Kind.Kind != "Pod" {
		return response
	}

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

	if !shouldInjectForLabels(pod.Labels) {
		validateLogger.Debug("skipping validation, no injection label set",
			"namespace", pod.Namespace,
			"name", pod.Name)
		return response
	}

	validationErrors := h.validatePodSpec(pod.Namespace, &pod.Spec, pod.Labels, pod.Annotations)
	if len(validationErrors) > 0 {
		response.Allowed = false
		causes := make([]metav1.StatusCause, len(validationErrors))
		errorMessages := make([]string, len(validationErrors))
		for i, ve := range validationErrors {
			errorMessages[i] = ve.Error()
			causes[i] = metav1.StatusCause{
				Type:    metav1.CauseTypeFieldValueInvalid,
				Message: ve.Message,
				Field:   ve.Field,
			}
		}
		response.Result = &metav1.Status{
			Status:  "Failure",
			Message: fmt.Sprintf("pod validation failed: %s", strings.Join(errorMessages, "; ")),
			Code:    http.StatusUnprocessableEntity,
			Reason:  metav1.StatusReasonInvalid,
			Details: &metav1.StatusDetails{
				Causes: causes,
			},
		}
		return response
	}

	return response
}

// shouldInjectForLabels checks if injection is enabled based on labels
func shouldInjectForLabels(labels map[string]string) bool {
	if labels == nil {
		return false
	}
	return labels[cni.LabelEnabled] == "true"
}

// validatePodSpec validates the pod specification
func (h *ValidateHandler) validatePodSpec(namespace string, podSpec *corev1.PodSpec, labels map[string]string, annotations map[string]string) []ValidationError {
	var errors []ValidationError

	validateLogger.Debug("validating pod spec")

	// Validate required annotation: wgconf-secret
	if annotations == nil || annotations[cni.AnnotationWGConfSecret] == "" {
		errors = append(errors, ValidationError{
			Field:   cni.AnnotationWGConfSecret,
			Message: "missing required annotation: " + cni.AnnotationWGConfSecret + " must specify a secret name",
		})
	}

	// Validate cni-mode annotation if present
	if cniMode, ok := annotations[cni.AnnotationCNIMode]; ok && cniMode != "" {
		if cniMode != cni.CNIModePodOrigin && cniMode != cni.CNIModeHostOrigin {
			errors = append(errors, ValidationError{
				Field:   cni.AnnotationCNIMode,
				Message: fmt.Sprintf("invalid value '%s': must be '%s' or '%s'", cniMode, cni.CNIModePodOrigin, cni.CNIModeHostOrigin),
			})
		}
	}

	// Validate dns-tunneled annotation if present
	if dnsTunneled, ok := annotations[cni.AnnotationDNSTunneled]; ok && dnsTunneled != "" {
		if !isValidBoolString(dnsTunneled) {
			errors = append(errors, ValidationError{
				Field:   cni.AnnotationDNSTunneled,
				Message: fmt.Sprintf("invalid value '%s': must be 'true' or 'false'", dnsTunneled),
			})
		}
	}

	// Validate nat-pmp annotation if present
	if natPMP, ok := annotations[cni.AnnotationNATPMP]; ok && natPMP != "" {
		if !isValidBoolString(natPMP) {
			errors = append(errors, ValidationError{
				Field:   cni.AnnotationNATPMP,
				Message: fmt.Sprintf("invalid value '%s': must be 'true' or 'false'", natPMP),
			})
		}
	}

	// Validate split-tunnel-overlap annotation if present
	if overlap, ok := annotations[cni.AnnotationSplitTunnelOverlap]; ok && overlap != "" {
		if overlap != "allow" {
			errors = append(errors, ValidationError{
				Field:   cni.AnnotationSplitTunnelOverlap,
				Message: fmt.Sprintf("invalid value '%s': must be 'allow'", overlap),
			})
		}
	}

	// Validate split-tunnel CIDR overlap against WireGuard addresses and DNS servers
	if splitCIDRs, ok := annotations[cni.AnnotationSplitTunnelCIDRs]; ok && splitCIDRs != "" {
		if secretName := annotations[cni.AnnotationWGConfSecret]; secretName != "" && h.Config.KubeClient != nil {
			overlapErrors := h.validateSplitTunnelOverlap(namespace, splitCIDRs, secretName, annotations)
			errors = append(errors, overlapErrors...)
		}
	}

	// Validate host network mode
	if podSpec.HostNetwork {
		errors = append(errors, ValidationError{
			Field:   "spec.template.spec.hostNetwork",
			Message: "hostNetwork mode is not supported with gopher-cni",
		})
	}

	return errors
}

// isValidBoolString checks if a string is a valid boolean representation
func isValidBoolString(s string) bool {
	return s == "true" || s == "false"
}

// validateSplitTunnelOverlap checks split-tunnel CIDRs against WireGuard addresses and DNS servers.
// Two-tier rules:
//   - Same or more specific than a protected net → always reject (explicit route would lose)
//   - Less specific but overlapping → reject unless split-tunnel-overlap=allow
func (h *ValidateHandler) validateSplitTunnelOverlap(namespace, splitCIDRsRaw, secretName string, annotations map[string]string) []ValidationError {
	var errors []ValidationError

	// Parse split-tunnel CIDRs from annotation
	var splitNets []*net.IPNet
	for _, s := range strings.Split(splitCIDRsRaw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		_, cidr, err := net.ParseCIDR(s)
		if err != nil {
			errors = append(errors, ValidationError{
				Field:   cni.AnnotationSplitTunnelCIDRs,
				Message: fmt.Sprintf("invalid CIDR %q: %v", s, err),
			})
			continue
		}
		splitNets = append(splitNets, cidr)
	}
	if len(errors) > 0 {
		return errors
	}

	// Fetch WireGuard config from secret
	wgConfData, err := h.Config.KubeClient.FetchSecretKey(context.TODO(), namespace, secretName, cni.SecretKeyWGConf)
	if err != nil {
		// Secret may not exist yet at admission time; skip overlap check
		validateLogger.Debug("could not fetch wgconf secret for overlap validation", "error", err)
		return nil
	}
	wgConfig, err := wireguard.ParseConfig(wgConfData)
	if err != nil {
		validateLogger.Debug("could not parse wireguard config for overlap validation", "error", err)
		return nil
	}

	protectedNets := wgConfig.ProtectedNets()

	overlapAllowed := annotations[cni.AnnotationSplitTunnelOverlap] == "allow"

	for _, split := range splitNets {
		splitOnes, _ := split.Mask.Size()
		for _, prot := range protectedNets {
			protOnes, _ := prot.Mask.Size()

			// Check for any overlap
			if !split.Contains(prot.IP) && !prot.Contains(split.IP) {
				continue
			}

			if splitOnes >= protOnes {
				// Same or more specific: explicit route via gcni0 would lose; always reject
				errors = append(errors, ValidationError{
					Field: cni.AnnotationSplitTunnelCIDRs,
					Message: fmt.Sprintf(
						"split-tunnel CIDR %s overlaps with protected net %s and is equally or more specific; traffic to %s would bypass WireGuard",
						split, prot, prot.IP,
					),
				})
			} else if !overlapAllowed {
				// Less specific: explicit route wins via longest-prefix-match, but reject by default
				errors = append(errors, ValidationError{
					Field: cni.AnnotationSplitTunnelCIDRs,
					Message: fmt.Sprintf(
						"split-tunnel CIDR %s overlaps with protected net %s; set %s=allow to permit this overlap",
						split, prot, cni.AnnotationSplitTunnelOverlap,
					),
				})
			}
		}
	}

	return errors
}
