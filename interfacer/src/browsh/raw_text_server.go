package browsh

import (
	"fmt"
	"strings"
	"net/http"
	"net/url"
	"crypto/rand"
	"io"
	"time"
)

// In order to communicate between the incoming HTTP request and the websocket request to the
// real browser to render the webpage, we keep track of requests in a map.
var rawTextRequests = make(map[string]string)

// HTTPServerStart starts the HTTP server is a seperate service from the usual interactive TTY
// app. It accepts normal HTTP requests and uses the path portion of the URL as the entry to the
// Browsh URL bar. It then returns a simple line-broken text version of whatever the browser
// loads. So for example, if you request `curl browsh-http-service.com/http://something.com`,
// it will return:
// `Something                                                                    `
func HTTPServerStart() {
	initialise()
	startFirefox()
	go startWebSocketServer()
	Log("Starting Browsh HTTP server")
	serverMux := http.NewServeMux()
	serverMux.HandleFunc("/", handleHTTPServerRequest)
	if err := http.ListenAndServe(":" + *HTTPServerPort, &slashFix{serverMux}); err != nil {
		Shutdown(err)
	}
}

func pseudoUUID() (uuid string) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		fmt.Println("Error: ", err)
		return
	}
	uuid = fmt.Sprintf("%X-%X-%X-%X-%X", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
	return uuid
}

type slashFix struct {
	mux http.Handler
}

// The default router from net/http collapses double slashes to a single slash in URL paths.
// This is obviously a problem for putting URLs in the path part of a URL, eg;
// https://domain.com/http://anotherdomain.com
// So here is a little hack that simply escapes the entire path portion to make sure it gets
// through the router unchanged.
func (h *slashFix) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.URL.Path = "/" + url.PathEscape(strings.TrimPrefix(r.URL.RequestURI(), "/"))
	h.mux.ServeHTTP(w, r)
}

func handleHTTPServerRequest(w http.ResponseWriter, r *http.Request) {
	var message string
	urlForBrowsh, _ := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/"))
	w.Header().Set("Cache-Control", "public, max-age=600")
	if strings.TrimSpace(urlForBrowsh) == "" {
		if (strings.Contains(r.Host, "text.")) {
			message = "Welcome to the Browsh plain text client.\n" +
				"You can use it by appending URLs like this;\n" +
				"http://html.brow.sh/https://www.brow.sh"
		} else {
			message = "<html>" +
				"Welcome to the Browsh HTML web client.<br />" +
				"Type a URL after 'html.brow.sh' in your URL bar, eg;<br />" +
				"<a href=\"http://html.brow.sh/https://www.brow.sh\">http://html.brow.sh/https://www.brow.sh</a><br />" +
				"</html>"
		}
		io.WriteString(w, message)
		return
	}
	if urlForBrowsh == "robots.txt" {
		message = "User-agent: *\nAllow: /$\nDisallow: /\n"
		io.WriteString(w, message)
		return
	}
	rawTextRequestID := pseudoUUID()
	mode := getRawTextMode(r)
	sendMessageToWebExtension(
		"/raw_text_request," + rawTextRequestID + "," +
		mode + "," +
		urlForBrowsh)
	waitForResponse(rawTextRequestID, w)
}

// 'PLAIN' mode returns raw text without any HTML whatsoever.
// 'HTML' mode returns some basic HTML tags for things like anchor links.
func getRawTextMode(r *http.Request) string {
	var mode = "HTML"
	if (strings.Contains(r.Host, "text.")) { mode = "PLAIN" }
	if (r.Header.Get("X-Browsh-Raw-Mode") == "PLAIN") { mode = "PLAIN" }
	return mode
}

func waitForResponse(rawTextRequestID string, w http.ResponseWriter) {
	var rawTextRequestResponse string
	var ok bool
	for {
		if rawTextRequestResponse, ok = rawTextRequests[rawTextRequestID]; ok {
			io.WriteString(w, rawTextRequestResponse)
			delete(rawTextRequests, rawTextRequestID)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}
