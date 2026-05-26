package webhook

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// serveAdmission handles the HTTP boilerplate shared by all admission webhook endpoints:
// body reading, Content-Type validation, AdmissionReview decoding, nil-request guard,
// response encoding, and UID propagation. handler is called with the decoded review
// and its response is sent back to the API server.
func serveAdmission(
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
	handler func(*admissionv1.AdmissionReview) *admissionv1.AdmissionResponse,
) {
	logger.Debug("handling admission request", "method", r.Method, "path", r.URL.Path)

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

	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "invalid Content-Type, expected application/json", http.StatusUnsupportedMediaType)
		return
	}

	admissionReview := admissionv1.AdmissionReview{}
	if _, _, err := deserializer.Decode(body, nil, &admissionReview); err != nil {
		http.Error(w, fmt.Sprintf("could not decode body: %v", err), http.StatusBadRequest)
		return
	}
	if admissionReview.Request == nil {
		http.Error(w, "admission request is nil", http.StatusBadRequest)
		return
	}

	response := handler(&admissionReview)

	responseReview := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Response: response,
	}
	responseReview.Response.UID = admissionReview.Request.UID

	respBytes, err := json.Marshal(responseReview)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(respBytes); err != nil {
		http.Error(w, fmt.Sprintf("could not write response: %v", err), http.StatusInternalServerError)
	}
}

// preparePod decodes and normalizes the pod from the admission request, and checks whether
// gopher-cni injection is enabled. Returns (pod, nil) when the pod should be processed, or
// (nil, earlyResponse) when the caller should return immediately (non-Pod kind, decode error,
// or injection not enabled).
func preparePod(req *admissionv1.AdmissionRequest) (*corev1.Pod, *admissionv1.AdmissionResponse) {
	allow := &admissionv1.AdmissionResponse{Allowed: true}

	if req.Kind.Kind != "Pod" {
		return nil, allow
	}

	pod := &corev1.Pod{}
	if err := json.Unmarshal(req.Object.Raw, pod); err != nil {
		return nil, &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Status:  "Failure",
				Message: fmt.Sprintf("could not unmarshal pod: %v", err),
				Code:    http.StatusBadRequest,
			},
		}
	}
	if pod.Namespace == "" {
		pod.Namespace = req.Namespace
	}

	if !shouldInject(pod) {
		return nil, allow
	}

	return pod, nil
}
