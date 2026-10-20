package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInvitationMeetingID(t *testing.T) {
	for _, test := range []struct{ invite, want string }{
		{"Join Zoom Meeting\nhttps://us02web.zoom.us/j/12345678901?pwd=secret\nPasscode: secret", "12345678901"},
		{"https://zoom.us/wc/join/1234567890", "1234567890"},
		{"https://app.zoom.us/wc/12345678901/join", "12345678901"},
		{"https://zoom.us/s/12345678901", "12345678901"},
		{"https://agency.zoomgov.com/j/123456789", "123456789"},
		{"https://zoom.us.evil.test/j/12345678901", ""},
		{"https://evilzoom.us/j/12345678901", ""},
		{"https://zoom.us@evil.test/j/12345678901", ""},
		{"http://zoom.us/j/12345678901", ""},
		{"https://zoom.us/j/12345678901 https://zoom.us/j/99999999999", ""},
	} {
		got, err := invitationMeetingID(test.invite)
		if got != test.want || ((err == nil) != (test.want != "")) {
			t.Errorf("invitation %q = %q, %v; want %q", test.invite, got, err, test.want)
		}
	}
}

func TestScheduledCallWaitStartReplayAndCancel(t *testing.T) {
	t.Setenv("DATABASE_URL", testDatabase(t))
	t.Setenv("AUTH_MODE", "")
	t.Setenv("RTMS_RECORDER_URL", "")
	h, err := New(http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	login := func(email string) *http.Cookie {
		resp := request(t, server.Client(), "POST", server.URL+"/api/demo/login", nil, strings.NewReader(`{"email":"`+email+`"}`))
		defer resp.Body.Close()
		for _, c := range resp.Cookies() {
			if c.Name == "zc_session" {
				return c
			}
		}
		t.Fatal("no session")
		return nil
	}
	dana, jordan := login("dana@acme.co"), login("jordan@acme.co")
	post := func(path, body string, cookie *http.Cookie, status int) []byte {
		t.Helper()
		resp := request(t, server.Client(), "POST", server.URL+path, cookie, strings.NewReader(body))
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != status {
			t.Fatalf("POST %s: %s %s", path, resp.Status, raw)
		}
		return raw
	}
	body := `{"invitation":"https://us02web.zoom.us/j/12345678901?pwd=must-not-be-stored","topic":"Scheduled integration","scheduled_at":"2099-10-19T20:00:00Z"}`
	// Only a trusted admin can register; access is an explicit share, not attendance.
	var call Call
	json.Unmarshal(post("/api/calls", body, dana, 201), &call)
	if call.Status != "scheduled" || call.Started != nil || len(call.Participants) != 0 || !call.CanShare || !call.IsOwner {
		t.Fatalf("scheduled metadata: %+v", call)
	}
	post("/api/calls", body, jordan, 403)
	post("/api/calls", body, dana, 409)
	unauthorized := request(t, server.Client(), "GET", server.URL+"/api/calls/"+call.ID, jordan, nil)
	unauthorized.Body.Close()
	if unauthorized.StatusCode != 403 {
		t.Fatalf("unshared call status: %s", unauthorized.Status)
	}
	// Sharing authorizes another user's API key before any meeting participants exist.
	post("/api/calls/"+call.ID+"/shares", `{"email":"jordan@acme.co"}`, dana, 201)
	var key struct {
		Secret string `json:"secret"`
	}
	json.Unmarshal(post("/api/keys", `{"name":"scheduled client"}`, jordan, 201), &key)
	// Recreate the app handler to prove the pending registration is durable.
	h, err = New(http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	restarted := httptest.NewServer(h)
	defer restarted.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", restarted.URL+"/api/calls/"+call.ID+"/stream", nil)
	req.Header.Set("Authorization", "Bearer "+key.Secret)
	resp, err := restarted.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	readUntil := func(want string) {
		t.Helper()
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("waiting for %q: %v", want, err)
			}
			if strings.Contains(line, want) {
				return
			}
			if strings.Contains(line, "event: end") {
				t.Fatalf("stream ended before %q", want)
			}
		}
	}
	readUntil("event: call.scheduled")
	readUntil(": keepalive") // Remains open beyond the terminal-status poll.
	// Use the restarted server for recorder mutations and duplicate deliveries.
	old := server
	server = restarted
	attach := `{"meeting_id":"12345678901","meeting_uuid":"runtime-one","rtms_stream_id":"stream-one"}`
	var live Call
	json.Unmarshal(post("/api/rtms/streams", attach, dana, 200), &live)
	if live.ID != call.ID || live.Status != "live" || live.Started == nil {
		t.Fatalf("identity not preserved: %+v", live)
	}
	post("/api/rtms/streams", attach, dana, 200)
	post("/api/rtms/streams", `{"meeting_id":"12345678901","meeting_uuid":"runtime-one","rtms_stream_id":"stream-two"}`, dana, 200)
	post("/api/rtms/streams", attach, jordan, 403)
	post("/api/demo/calls/"+call.ID+"/events", `{"kind":"transcript","participant":{"id":"speaker","name":"First speaker"},"data":{"text":"opening words"}}`, dana, 201)
	readUntil("event: stream.started")
	readUntil("opening words")
	post("/api/rtms/streams/stream-one/stop", `{"status":"stopped"}`, dana, 200)
	metadata := request(t, restarted.Client(), "GET", restarted.URL+"/api/calls/"+call.ID, jordan, nil)
	json.NewDecoder(metadata.Body).Decode(&live)
	metadata.Body.Close()
	if live.Status != "live" {
		t.Fatal("first stream ended a call that still has an active stream")
	}
	post("/api/rtms/streams/stream-two/stop", `{"status":"stopped"}`, dana, 200)
	readUntil("event: stream.stopped")
	_, err = io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	// Sequence replay returns each event once, skipping the initial scheduled event.
	replayReq, _ := http.NewRequest("GET", restarted.URL+"/api/calls/"+call.ID+"/stream?from=0", nil)
	replayReq.AddCookie(jordan)
	replayReq.Header.Set("Last-Event-ID", "1")
	replay, err := restarted.Client().Do(replayReq)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(replay.Body)
	replay.Body.Close()
	if strings.Count(string(raw), "event: stream.started") != 1 || strings.Contains(string(raw), "event: call.scheduled") || !strings.Contains(string(raw), "opening words") {
		t.Fatalf("replay: %s", raw)
	}
	// Reusing a link registers the next session; replaying the old runtime never claims it.
	var next Call
	json.Unmarshal(post("/api/calls", body, dana, 201), &next)
	var oldRuntime Call
	json.Unmarshal(post("/api/rtms/streams", attach, dana, 200), &oldRuntime)
	if oldRuntime.ID != call.ID {
		t.Fatal("old runtime claimed a new scheduled session")
	}
	post("/api/calls/"+next.ID+"/cancel", `{}`, jordan, 403)
	// A stop delivered ahead of its start must not claim a pending registration.
	post("/api/rtms/streams/late-stream/stop", `{"status":"stopped"}`, dana, 200)
	post("/api/rtms/streams", `{"meeting_id":"12345678901","meeting_uuid":"late-runtime","rtms_stream_id":"late-stream"}`, dana, 200)
	waitingCtx, waitingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer waitingCancel()
	waitingReq, _ := http.NewRequestWithContext(waitingCtx, "GET", restarted.URL+"/api/calls/"+next.ID+"/stream", nil)
	waitingReq.AddCookie(dana)
	waiting, err := restarted.Client().Do(waitingReq)
	if err != nil {
		t.Fatal(err)
	}
	defer waiting.Body.Close()
	post("/api/calls/"+next.ID+"/cancel", `{}`, dana, 200)
	cancelledEvents, err := io.ReadAll(waiting.Body)
	if err != nil || !strings.Contains(string(cancelledEvents), "event: call.cancelled") || !strings.Contains(string(cancelledEvents), "event: end") {
		t.Fatalf("waiting client cancellation: %s %v", cancelledEvents, err)
	}
	ended := request(t, restarted.Client(), "GET", restarted.URL+"/api/calls/"+next.ID+"/stream", dana, nil)
	raw, _ = io.ReadAll(ended.Body)
	ended.Body.Close()
	if !strings.Contains(string(raw), "event: call.cancelled") || !strings.Contains(string(raw), "event: end") {
		t.Fatalf("cancelled stream: %s", raw)
	}
	server = old
}

func TestParseFromPrefersReconnectCursor(t *testing.T) {
	req := httptest.NewRequest("GET", "/stream?from=2", nil)
	req.Header.Set("Last-Event-ID", "9")
	got, _ := parseFrom(req)
	if got != 9 {
		t.Fatal(fmt.Sprintf("got %d", got))
	}
}
