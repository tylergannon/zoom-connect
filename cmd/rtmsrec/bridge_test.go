package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestBridgePreservesRealTextAndSeparatesStreams(t *testing.T) {
	var mu sync.Mutex
	created, events, stopped := 0, 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing authentication")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		switch r.URL.Path {
		case "/api/demo/calls":
			created++
			if body["source"] != "zoom" {
				t.Error("real calls must disable scripted events")
			}
			id := "call-one"
			if created == 2 {
				id = "call-two"
			}
			json.NewEncoder(w).Encode(map[string]string{"id": id})
		case "/api/demo/calls/call-one/events":
			events++
			p := body["participant"].(map[string]any)
			d := body["data"].(map[string]any)
			if body["kind"] != "transcript" || p["name"] != "Actual Speaker" || d["text"] != "words received from Zoom" {
				t.Errorf("text altered: %#v", body)
			}
		case "/api/demo/calls/call-one/stop":
			stopped++
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	b := &liveBridge{base: srv.URL, key: "test-key", client: srv.Client(), calls: map[string]string{}, closed: map[string]bool{}}
	b.start("stream-one")
	b.start("stream-one")
	b.start("stream-two")
	b.text("stream-one", "transcript", map[string]any{"user_id": 123, "user_name": "Actual Speaker", "data": "words received from Zoom"})
	b.stop("stream-one")
	b.stop("stream-one")
	b.text("stream-one", "transcript", map[string]any{"data": "late packet"})
	mu.Lock()
	defer mu.Unlock()
	if created != 2 || events != 1 || stopped != 1 {
		t.Fatalf("created=%d events=%d stopped=%d", created, events, stopped)
	}
}

func TestBridgeUsesDurableZoomIdentity(t *testing.T) {
	starts, stops, events := 0, 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/rtms/streams":
			var in zoomIdentity
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Error(err)
			}
			if in.MeetingID != "12345678901" || in.MeetingUUID != "runtime-one" || in.StreamID != "stream-one" {
				t.Errorf("lost meeting identity: %+v", in)
			}
			starts++
			json.NewEncoder(w).Encode(map[string]string{"id": "scheduled-call", "status": "live", "stream_status": "live"})
		case "/api/demo/calls/scheduled-call/events":
			events++
		case "/api/rtms/streams/stream-one/stop":
			stops++
		default:
			t.Errorf("unexpected route: %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	for i := 0; i < 2; i++ { // Simulate recorder restart; both must obtain the same call.
		b := &liveBridge{base: srv.URL, key: "key", client: srv.Client(), calls: map[string]string{}, closed: map[string]bool{}}
		b.start("stream-one", "12345678901", "runtime-one")
		b.text("stream-one", "transcript", map[string]any{"data": "opening words"})
		b.stop("stream-one", "stopped")
	}
	if starts != 2 || stops != 2 || events != 2 {
		t.Fatalf("starts=%d stops=%d events=%d", starts, stops, events)
	}
}
