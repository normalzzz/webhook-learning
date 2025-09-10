package main

import (
	// "fmt"
	"net/http"
	log "github.com/sirupsen/logrus"
	nested "github.com/antonfisher/nested-logrus-formatter"

	webhook "xingzhan.io/webhook-learn/pkg"
)


func init() {
	log.SetFormatter(&nested.Formatter{
		TimestampFormat: "01/02 15:04:05",
	})
}

func main() {
    http.HandleFunc("/", webhook.PodServer)
    log.Println("Server is running at https://52.81.226.43:8443")
    // http.ListenAndServe(":8080", nil)
    http.ListenAndServeTLS(":8443", "server.crt", "server.key", nil)
}
