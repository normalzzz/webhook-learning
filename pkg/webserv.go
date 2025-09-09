package webhook
import(
	"net/http"
	log "github.com/sirupsen/logrus"
	"io"
)

func PodServer(w http.ResponseWriter, r *http.Request){

	body, err := io.ReadAll(r.Body)
    if err != nil {
        log.Errorln("Error reading body: %v\n", err)
        return
    }
    log.Infoln("Method: %s\n", r.Method)
    log.Infoln("URL: %s\n", r.URL.String())
    log.Infoln("Header: %v\n", r.Header)
    log.Infoln("Body: ", string(body))

	w.Write([]byte("Hello, HTTPS world!"))
}