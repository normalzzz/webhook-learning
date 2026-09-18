package webhook

import (
	"fmt"
	"io"
	"net/http"

	log "github.com/sirupsen/logrus"
	admissionv1 "k8s.io/api/admission/v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type patchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
)

func mutate(ar *admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	req := ar.Request

	var pod corev1.Pod
	if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
		log.Warn("Could not unmarshal Pod object")
		return &admissionv1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	}

	log.Info("Processing Pod admission request")

	annotationMap := make(map[string]string)
	annotationMap["annotation-injected-by"] = "webhook"

	var patch []patchOperation

	patch = append(patch, patchOperation{
		Op:    "add",
		Path:  "/metadata/annotations",
		Value: annotationMap,
	})

	patchstruct, err := json.Marshal(patch)
	if err != nil {
		log.Warnln("failed to Marshal patch struct ")
		return &admissionv1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	}

	return &admissionv1.AdmissionResponse{
		Allowed: true,
		Patch:   patchstruct,
		PatchType: func() *admissionv1.PatchType {
			pt := admissionv1.PatchTypeJSONPatch
			return &pt
		}(),
	}

}

func PodServer(w http.ResponseWriter, r *http.Request) {
	log.Info("PodServer request received")

	var body []byte

	if r.Body != nil {
		if data, err := io.ReadAll(r.Body); err == nil {
			body = data
		}
		if len(body) == 0 {
			log.Warnln("empty body")
			http.Error(w, "empty body", http.StatusBadRequest)
			return
		}
		// verify the content type is accurate
		contentType := r.Header.Get("Content-Type")
		if contentType != "application/json" {
			log.Warn("Invalid Content-Type, expect application/json")
			http.Error(w, "invalid Content-Type, expect `application/json`", http.StatusUnsupportedMediaType)
			return
		}

		var admissionResponse *admissionv1.AdmissionResponse
		ar := admissionv1.AdmissionReview{}
		if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
			log.Warn("Cannot decode AdmissionReview")
			admissionResponse = &admissionv1.AdmissionResponse{
				Result: &metav1.Status{
					Message: err.Error(),
				},
			}
		} else {
			if ar.Request == nil {
				log.Warn("Missing AdmissionReview request")
				http.Error(w, "missing AdmissionReview request", http.StatusBadRequest)
				return
			}
			admissionResponse = mutate(&ar)
		}

		admissionReview := admissionv1.AdmissionReview{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "admission.k8s.io/v1",
				Kind:       "AdmissionReview",
			},
		}

		if admissionResponse != nil {

			// Assign the Response object to AdmissionReview
			admissionReview.Response = admissionResponse
			if ar.Request != nil {
				admissionReview.Response.UID = ar.Request.UID
			}
		}
		resp, err := json.Marshal(admissionReview)
		if err != nil {
			log.Warn("Cannot encode response")
			http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
		}
		log.Infoln("Ready to write reponse ...")
		if _, err := w.Write(resp); err != nil {
			log.Warn("Cannot write response")
			http.Error(w, fmt.Sprintf("could not write response: %v", err), http.StatusInternalServerError)
		}
	}

}
