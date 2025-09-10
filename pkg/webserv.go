package webhook

import (
	"fmt"
	"io"
	"net/http"

	log "github.com/sirupsen/logrus"
	admissionv1 "k8s.io/api/admission/v1"

	// "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	// jsonpatch "github.com/evanphx/json-patch"
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
		log.Warnln("Could not unmarshal raw object: %v", err)
		return &admissionv1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	}

	log.Infoln("AdmissionReview for Kind=%v, Namespace=%v Name=%v (%v) UID=%v patchOperation=%v UserInfo=%v", req.Kind, req.Namespace, req.Name, pod.Name, req.UID, req.Operation, req.UserInfo)

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

	// body, err := io.ReadAll(r.Body)
	// if err != nil {
	//     log.Errorln("Error reading body: %v\n", err)
	//     return
	// }
	// log.Infoln("Method: \n", r.Method)
	// log.Infoln("URL: \n", r.URL.String())
	// log.Infoln("Header: \n", r.Header)
	// log.Infoln("Body: ", string(body))

	// w.Write([]byte("Hello, HTTPS world!"))
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
			log.Warnln("Content-Type=%s, expect application/json", contentType)
			http.Error(w, "invalid Content-Type, expect `application/json`", http.StatusUnsupportedMediaType)
			return
		}

		var admissionResponse *admissionv1.AdmissionResponse
		ar := admissionv1.AdmissionReview{}
		if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
			log.Warnln("Can't decode body: ", err)
			admissionResponse = &admissionv1.AdmissionResponse{
				Result: &metav1.Status{
					Message: err.Error(),
				},
			}
		} else {
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
			log.Warnln("Can't encode response: %v", err)
			http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
		}
		log.Infoln("Ready to write reponse ...")
		if _, err := w.Write(resp); err != nil {
			log.Warnln("Can't write response: %v", err)
			http.Error(w, fmt.Sprintf("could not write response: %v", err), http.StatusInternalServerError)
		}
	}

}
