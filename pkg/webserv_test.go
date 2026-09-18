package webhook

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPodServerKeepsRequestDataOutOfLogs(t *testing.T) {
	fixture, err := os.ReadFile("../examples/admission-review.json")
	if err != nil {
		t.Fatal(err)
	}
	var review admissionv1.AdmissionReview
	if err := json.Unmarshal(fixture, &review); err != nil {
		t.Fatal(err)
	}
	review.Request.UserInfo.Username = "private-user-marker"
	review.Request.UserInfo.Extra = nil
	review.Request.Namespace = "private-namespace-marker"
	review.Request.Name = "private-pod-marker"
	body, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer
	previousOutput := log.StandardLogger().Out
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previousOutput) })

	tests := []struct {
		name        string
		body        string
		contentType string
		status      int
	}{
		{"valid admission", string(body), "application/json", http.StatusOK},
		{"invalid content type", string(body), "private-content-type-marker", http.StatusUnsupportedMediaType},
		{"missing request", `{"apiVersion":"admission.k8s.io/v1","kind":"AdmissionReview"}`, "application/json", http.StatusBadRequest},
		{"invalid JSON", `{"private-json-marker": invalid}`, "application/json", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs.Reset()
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.contentType)
			req.Header.Set("Authorization", "Bearer private-token-marker")
			req.RemoteAddr = "192.0.2.10:12345"
			response := httptest.NewRecorder()
			PodServer(response, req)
			if response.Code != tt.status {
				t.Fatalf("status = %d, want %d", response.Code, tt.status)
			}
			for _, marker := range []string{"private-", "192.0.2.10", string(review.Request.UID)} {
				if strings.Contains(logs.String(), marker) {
					t.Fatalf("request data leaked into logs: %q", marker)
				}
			}
			if tt.name == "valid admission" {
				var result admissionv1.AdmissionReview
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.TypeMeta != (metav1.TypeMeta{APIVersion: "admission.k8s.io/v1", Kind: "AdmissionReview"}) || result.Response == nil {
					t.Fatal("missing AdmissionReview response")
				}
				if !result.Response.Allowed || result.Response.UID != review.Request.UID || result.Response.PatchType == nil || *result.Response.PatchType != admissionv1.PatchTypeJSONPatch {
					t.Fatalf("unexpected admission response: %+v", result.Response)
				}
				if string(result.Response.Patch) != `[{"op":"add","path":"/metadata/annotations","value":{"annotation-injected-by":"webhook"}}]` {
					t.Fatalf("unexpected patch: %s", result.Response.Patch)
				}
			}
		})
	}
}
