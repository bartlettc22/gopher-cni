package webhook

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// allowHandler is a stub that always allows.
var allowHandler = func(ar *admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	return &admissionv1.AdmissionResponse{Allowed: true}
}

// admissionReviewBody marshals an AdmissionReview to JSON for use as a request body.
func admissionReviewBody(t *testing.T, ar *admissionv1.AdmissionReview) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(ar)
	require.NoError(t, err)
	return bytes.NewReader(b)
}

// reviewWithRequest returns a minimal AdmissionReview with a Request carrying the given UID.
func reviewWithRequest(uid string) *admissionv1.AdmissionReview {
	return &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{APIVersion: "admission.k8s.io/v1", Kind: "AdmissionReview"},
		Request:  &admissionv1.AdmissionRequest{UID: types.UID("test-uid-" + uid)},
	}
}

// reviewWithoutRequest returns a minimal AdmissionReview with no Request (nil after decode).
func reviewWithoutRequest() *admissionv1.AdmissionReview {
	return &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{APIVersion: "admission.k8s.io/v1", Kind: "AdmissionReview"},
	}
}

func doServeAdmission(t *testing.T, body *bytes.Reader, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	serveAdmission(w, req, slog.Default(), allowHandler)
	return w
}

func TestServeAdmission_InvalidRequests(t *testing.T) {
	t.Run("empty body returns 400", func(t *testing.T) {
		w := doServeAdmission(t, bytes.NewReader(nil), "application/json")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("wrong content-type returns 415", func(t *testing.T) {
		ar := reviewWithRequest("1")
		w := doServeAdmission(t, admissionReviewBody(t, ar), "text/plain")
		assert.Equal(t, http.StatusUnsupportedMediaType, w.Code)
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		w := doServeAdmission(t, bytes.NewReader([]byte("not json")), "application/json")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("nil admission request returns 400", func(t *testing.T) {
		ar := reviewWithoutRequest()
		w := doServeAdmission(t, admissionReviewBody(t, ar), "application/json")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestServeAdmission_ValidRequest(t *testing.T) {
	t.Run("returns 200 with application/json content-type", func(t *testing.T) {
		ar := reviewWithRequest("1")
		w := doServeAdmission(t, admissionReviewBody(t, ar), "application/json")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	})

	t.Run("response body is a valid AdmissionReview", func(t *testing.T) {
		ar := reviewWithRequest("1")
		w := doServeAdmission(t, admissionReviewBody(t, ar), "application/json")
		var responseAR admissionv1.AdmissionReview
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &responseAR))
		require.NotNil(t, responseAR.Response)
	})

	t.Run("UID from request is propagated to response", func(t *testing.T) {
		ar := reviewWithRequest("abc123")
		w := doServeAdmission(t, admissionReviewBody(t, ar), "application/json")
		var responseAR admissionv1.AdmissionReview
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &responseAR))
		assert.Equal(t, ar.Request.UID, responseAR.Response.UID)
	})

	t.Run("handler allow decision is reflected in response", func(t *testing.T) {
		ar := reviewWithRequest("1")
		req := httptest.NewRequest(http.MethodPost, "/", admissionReviewBody(t, ar))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		serveAdmission(w, req, slog.Default(), func(ar *admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
			return &admissionv1.AdmissionResponse{Allowed: true}
		})
		var responseAR admissionv1.AdmissionReview
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &responseAR))
		assert.True(t, responseAR.Response.Allowed)
	})

	t.Run("handler deny decision is reflected in response", func(t *testing.T) {
		ar := reviewWithRequest("1")
		req := httptest.NewRequest(http.MethodPost, "/", admissionReviewBody(t, ar))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		serveAdmission(w, req, slog.Default(), func(ar *admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
			return &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: "denied for test",
					Code:    http.StatusForbidden,
				},
			}
		})
		assert.Equal(t, http.StatusOK, w.Code, "HTTP status is always 200; admission decision is in the body")
		var responseAR admissionv1.AdmissionReview
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &responseAR))
		assert.False(t, responseAR.Response.Allowed)
		assert.Equal(t, "denied for test", responseAR.Response.Result.Message)
	})

	t.Run("handler receives the decoded AdmissionReview", func(t *testing.T) {
		ar := reviewWithRequest("sentinel")
		var received *admissionv1.AdmissionReview
		req := httptest.NewRequest(http.MethodPost, "/", admissionReviewBody(t, ar))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		serveAdmission(w, req, slog.Default(), func(got *admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
			received = got
			return &admissionv1.AdmissionResponse{Allowed: true}
		})
		require.NotNil(t, received)
		assert.Equal(t, ar.Request.UID, received.Request.UID)
	})
}
