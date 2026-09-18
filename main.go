package main

import (
	"net/http"

	nested "github.com/antonfisher/nested-logrus-formatter"
	log "github.com/sirupsen/logrus"

	webhook "example.com/webhook-learn/pkg"
)

func init() {
	log.SetFormatter(&nested.Formatter{
		TimestampFormat: "01/02 15:04:05",
	})
}

func main() {
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	http.HandleFunc("/", webhook.PodServer)
	log.Println("Server is running at port 80")
	log.Fatal(http.ListenAndServe(":80", nil))
}
