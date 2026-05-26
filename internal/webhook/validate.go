package webhook

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/bartlettc22/gopher-cni/internal/cni"
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
	serveAdmission(w, r, validateLogger, h.validate)
}

// validate processes the admission review and returns a response
func (h *ValidateHandler) validate(ar *admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	pod, early := preparePod(ar.Request)
	if early != nil {
		return early
	}

	validationErrors := h.validatePodSpec(pod.Namespace, &pod.Spec, pod.Annotations)
	if len(validationErrors) == 0 {
		return &admissionv1.AdmissionResponse{Allowed: true}
	}

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
	return &admissionv1.AdmissionResponse{
		Allowed: false,
		Result: &metav1.Status{
			Status:  "Failure",
			Message: fmt.Sprintf("pod validation failed: %s", strings.Join(errorMessages, "; ")),
			Code:    http.StatusUnprocessableEntity,
			Reason:  metav1.StatusReasonInvalid,
			Details: &metav1.StatusDetails{
				Causes: causes,
			},
		},
	}
}

// validatePodSpec validates the pod specification
func (h *ValidateHandler) validatePodSpec(namespace string, podSpec *corev1.PodSpec, annotations map[string]string) []ValidationError {
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

	wgConfig, err := h.Config.fetchWGConfig(context.TODO(), namespace, secretName)
	if err != nil {
		// Secret may not exist yet at admission time; skip overlap check
		validateLogger.Debug("could not fetch or parse wgconf secret for overlap validation", "error", err)
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
