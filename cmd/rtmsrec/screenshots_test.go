package main

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestScreenBridgeUploadsBytesAndMetadata(t *testing.T) {
	image := []byte{0xff, 0xd8, 0xff, 0xd9}
	count := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		data, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/api/demo/calls/call-one/screenshots" || !bytes.Equal(data, image) || r.Header.Get("X-Zoom-Timestamp") != "1792184000123" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("incorrect image transport")
		}
		w.WriteHeader(201)
	}))
	defer srv.Close()
	b := &liveBridge{mu: sync.Mutex{}, base: srv.URL, key: "test-key", client: srv.Client(), calls: map[string]string{"stream-one": "call-one"}, closed: map[string]bool{}}
	b.screen("stream-one", map[string]any{"data": base64.StdEncoding.EncodeToString(image), "timestamp": float64(1792184000123)})
	if count != 1 {
		t.Fatal("no upload")
	}
	b.closed["stream-one"] = true
	b.screen("stream-one", map[string]any{"data": base64.StdEncoding.EncodeToString(image)})
	if count != 1 {
		t.Fatal("late frame uploaded after stop")
	}
}
