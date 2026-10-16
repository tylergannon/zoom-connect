package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T) *server {
	t.Helper()
	rec, err := newRecorder(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &server{
		cfg:     config{clientID: "cid", clientSecret: "csec", webhookSecret: "whsec"},
		streams: map[string]*stream{},
		hooks:   rec,
	}
}

func sign(secret, ts, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:" + ts + ":" + body))
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func TestURLValidation(t *testing.T) {
	sv := newTestServer(t)
	body := `{"event":"endpoint.url_validation","payload":{"plainToken":"abc123"}}`
	req := httptest.NewRequest("POST", "/zoom/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()
	sv.webhook(w, req)

	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	mac := hmac.New(sha256.New, []byte("whsec"))
	mac.Write([]byte("abc123"))
	if resp["plainToken"] != "abc123" || resp["encryptedToken"] != hex.EncodeToString(mac.Sum(nil)) {
		t.Fatalf("bad validation response: %s", w.Body.String())
	}
}

func TestSignatureVerification(t *testing.T) {
	sv := newTestServer(t)
	body := `{"event":"meeting.rtms_stopped","payload":{"meeting_uuid":"m","rtms_stream_id":"s"}}`
	now := strconv.FormatInt(time.Now().Unix(), 10)
	stale := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)

	cases := []struct {
		name string
		ts   string
		sig  string
		want int
	}{
		{"valid", now, sign("whsec", now, body), 200},
		{"wrong secret", now, sign("other", now, body), 401},
		{"stale timestamp", stale, sign("whsec", stale, body), 401},
		{"missing headers", "", "", 401},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/zoom/webhook", strings.NewReader(body))
			if c.ts != "" {
				req.Header.Set("x-zm-request-timestamp", c.ts)
				req.Header.Set("x-zm-signature", c.sig)
			}
			w := httptest.NewRecorder()
			sv.webhook(w, req)
			if w.Code != c.want {
				t.Fatalf("got %d want %d", w.Code, c.want)
			}
		})
	}
}

func TestStreamSignature(t *testing.T) {
	s := &stream{cfg: config{clientID: "cid", clientSecret: "csec"}, meetingUUID: "mu", streamID: "sid"}
	mac := hmac.New(sha256.New, []byte("csec"))
	mac.Write([]byte("cid,mu,sid"))
	if got, want := s.signature(), hex.EncodeToString(mac.Sum(nil)); got != want {
		t.Fatalf("signature %s want %s", got, want)
	}
}

var _ = http.StatusOK
