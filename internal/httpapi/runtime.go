package httpapi

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"
)

func (a *Server) configureRecorder() error {
	address := os.Getenv("RTMS_RECORDER_URL")
	if address == "" {
		return nil
	}
	u, err := url.Parse(address)
	if err != nil {
		return fmt.Errorf("invalid RTMS_RECORDER_URL")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "http" || (u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())) {
		return fmt.Errorf("RTMS_RECORDER_URL must be a loopback HTTP URL")
	}
	proxy := httputil.NewSingleHostReverseProxy(u)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) { jsonError(w, 502, "recorder unavailable") }
	a.recorder = proxy
	a.recorderHealth = address + "/healthz"
	return nil
}
func (a *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.s.Pool.Ping(ctx); err != nil {
		jsonError(w, 503, "database unavailable")
		return
	}
	if a.recorderHealth != "" {
		req, _ := http.NewRequestWithContext(ctx, "GET", a.recorderHealth, nil)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			jsonError(w, 503, "recorder unavailable")
			return
		}
		response.Body.Close()
		if response.StatusCode != 200 {
			jsonError(w, 503, "recorder unavailable")
			return
		}
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}
