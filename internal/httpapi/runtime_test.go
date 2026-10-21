package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecorderProxyPreservesSignedRequest(t *testing.T) {
	const body = `{"event":"endpoint.url_validation","payload":{"plainToken":"probe"}}`
	var received bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/zoom/webhook" || string(data) != body || r.Header.Get("x-zm-signature") != "v0=test-signature" {
			t.Errorf("request changed in proxy")
		}
		received = true
		w.WriteHeader(202)
	}))
	defer upstream.Close()
	t.Setenv("RTMS_RECORDER_URL", upstream.URL)
	a := &Server{}
	if err := a.configureRecorder(); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	a.routes(mux)
	req := httptest.NewRequest("POST", "/zoom/webhook", strings.NewReader(body))
	req.Header.Set("x-zm-signature", "v0=test-signature")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, req)
	if response.Code != 202 || !received {
		t.Fatalf("proxy not reached: %d", response.Code)
	}
	t.Setenv("RTMS_RECORDER_URL", "http://example.com")
	if err := a.configureRecorder(); err == nil {
		t.Fatal("non-loopback recorder accepted")
	}
}
