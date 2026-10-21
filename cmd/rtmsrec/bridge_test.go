package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
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
	b := &liveBridge{base: srv.URL, key: "test-key", client: srv.Client(), streams: map[string]*bridgeStream{}}
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
		b := &liveBridge{base: srv.URL, key: "key", client: srv.Client(), streams: map[string]*bridgeStream{}}
		b.start("stream-one", "12345678901", "runtime-one")
		b.text("stream-one", "transcript", map[string]any{"data": "opening words"})
		b.stop("stream-one", "stopped")
	}
	if starts != 2 || stops != 2 || events != 2 {
		t.Fatalf("starts=%d stops=%d events=%d", starts, stops, events)
	}
}

func TestBridgeFallsBackWhenAttachmentUnavailable(t *testing.T) {
	for _, status := range []int{400, 409, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			attaches, creates, events, stops := 0, 0, 0, 0
			app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/rtms/streams":
					attaches++
					w.WriteHeader(status)
				case "/api/demo/calls":
					creates++
					json.NewEncoder(w).Encode(map[string]string{"id": "standalone"})
				case "/api/demo/calls/standalone/events":
					events++
				case "/api/rtms/streams/stream/stop":
					// Retire ambiguous attachment before standalone fallback.
				case "/api/demo/calls/standalone/stop":
					stops++
				default:
					t.Errorf("unexpected route: %s", r.URL.Path)
				}
			}))
			defer app.Close()
			b := &liveBridge{base: app.URL, key: "key", client: app.Client(), streams: map[string]*bridgeStream{}, retryWindow: 2 * time.Millisecond, retryDelay: time.Millisecond}
			// Time-bounded attempts for server errors; client errors fall back on the first attempt.
			b.start("stream", "12345678901", "runtime")
			b.start("stream", "12345678901", "runtime")
			b.start("stream", "12345678901", "runtime")
			b.text("stream", "transcript", map[string]any{"data": "preserved text"})
			b.stop("stream", "stopped")
			if (status != 500 && attaches != 1) || (status == 500 && attaches < 2) || creates != 1 || events != 1 || stops != 1 {
				t.Fatalf("attach=%d create=%d text=%d stop=%d", attaches, creates, events, stops)
			}
		})
	}
}

func TestBridgeTransientAttachmentKeepsOpeningTextAndOtherStreams(t *testing.T) {
	firstAttempt := make(chan struct{})
	release := make(chan struct{})
	other := make(chan struct{}, 1)
	var mu sync.Mutex
	attempts, events := 0, 0
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/rtms/streams" {
			var in zoomIdentity
			json.NewDecoder(r.Body).Decode(&in)
			if in.StreamID == "blocked" {
				mu.Lock()
				attempts++
				n := attempts
				mu.Unlock()
				if n == 1 {
					close(firstAttempt)
					<-release
					w.WriteHeader(503)
					return
				}
			} else {
				other <- struct{}{}
			}
			json.NewEncoder(w).Encode(map[string]string{"id": in.StreamID, "status": "live", "stream_status": "live"})
			return
		}
		if r.URL.Path == "/api/rtms/streams/blocked/stop" {
			return
		}
		if r.URL.Path == "/api/demo/calls/blocked/events" {
			mu.Lock()
			events++
			mu.Unlock()
			return
		}
		t.Errorf("unexpected fallback/route: %s", r.URL.Path)
	}))
	defer app.Close()
	b := &liveBridge{base: app.URL, key: "key", client: app.Client(), retryWindow: time.Second, retryDelay: time.Millisecond}
	done := make(chan struct{})
	go func() { b.start("blocked", "12345678901", "one"); close(done) }()
	<-firstAttempt
	go b.start("other", "22345678901", "two")
	select {
	case <-other:
	case <-time.After(time.Second):
		t.Fatal("one meeting stalled another")
	}
	close(release)
	<-done
	b.text("blocked", "transcript", map[string]any{"data": "opening words"})
	b.stop("blocked", "interrupted")
	b.start("blocked", "12345678901", "one")
	b.text("blocked", "transcript", map[string]any{"data": "resumed words"})
	mu.Lock()
	defer mu.Unlock()
	if attempts != 3 || events != 2 {
		t.Fatalf("attempts=%d events=%d", attempts, events)
	}
}
