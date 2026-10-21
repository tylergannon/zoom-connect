package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tylergannon/zoom-connect/internal/store"
)

func TestRuntimeRestartSupersessionAndShareAccess(t *testing.T) {
	dsn := testDatabase(t)
	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("AUTH_MODE", "")
	t.Setenv("RTMS_RECORDER_URL", "")
	h, err := New(http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	login := request(t, server.Client(), "POST", server.URL+"/api/demo/login", nil, strings.NewReader(`{"email":"dana@acme.co"}`))
	defer login.Body.Close()
	var cookie *http.Cookie
	for _, c := range login.Cookies() {
		if c.Name == "zc_session" {
			cookie = c
		}
	}
	post := func(path, body string) Call {
		t.Helper()
		resp := request(t, server.Client(), "POST", server.URL+path, cookie, strings.NewReader(body))
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode/100 != 2 {
			t.Fatalf("%s: %s %s", path, resp.Status, raw)
		}
		var call Call
		json.Unmarshal(raw, &call)
		return call
	}
	attach := func(uuid, stream, number string) Call {
		t.Helper()
		body, _ := json.Marshal(zoomStreamIdentity{MeetingID: number, MeetingUUID: uuid, StreamID: stream})
		return post("/api/rtms/streams", string(body))
	}
	old := attach("runtime-old", "stream-old", "12345678901")
	// The connection marker follows the global feed's initial snapshot, so
	// a client cannot lose a transition immediately after connecting.
	feedCtx, feedCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer feedCancel()
	feedReq, _ := http.NewRequestWithContext(feedCtx, "GET", server.URL+"/api/stream", nil)
	feedReq.AddCookie(cookie)
	feed, err := server.Client().Do(feedReq)
	if err != nil {
		t.Fatal(err)
	}
	defer feed.Body.Close()
	feedReader := bufio.NewReader(feed.Body)
	waitFor := func(want string) {
		t.Helper()
		for {
			line, err := feedReader.ReadString('\n')
			if err != nil {
				t.Fatalf("global stream waiting for %s: %v", want, err)
			}
			if strings.Contains(line, want) {
				return
			}
		}
	}
	waitFor(": connected")
	post("/api/rtms/streams/stream-old/stop", `{"status":"stopped"}`)
	waitFor("event: call.ended")
	resumed := attach("runtime-old", "stream-resumed", "12345678901")
	waitFor("event: call.started")
	feedCancel()
	feed.Body.Close()
	if resumed.ID != old.ID || resumed.Status != "live" || resumed.Ended != nil {
		t.Fatalf("resume: %+v", resumed)
	}
	post("/api/demo/calls/"+old.ID+"/events", `{"kind":"transcript","data":{"text":"words after RTMS restart"}}`)
	// A missed stop cannot block registering or streaming the next session.
	next := post("/api/calls", `{"invitation":"https://zoom.us/j/12345678901","topic":"next instance"}`)
	current := attach("runtime-new", "stream-new", "12345678901")
	if current.ID != next.ID || current.ID == old.ID {
		t.Fatal("new runtime lost its pending registration")
	}
	s, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	previous, err := s.Q.GetCall(context.Background(), old.ID)
	if err != nil || previous.Status != "interrupted" {
		t.Fatalf("stale runtime: %+v %v", previous, err)
	}
	// A delayed fresh stream on an already superseded UUID must not end the
	// current runtime or take its pending registration.
	late := attach("runtime-old", "late-old-runtime", "12345678901")
	if late.ID != old.ID || late.Status != "interrupted" {
		t.Fatalf("late runtime: %+v", late)
	}
	now, err := s.Q.GetCall(context.Background(), current.ID)
	if err != nil || now.Status != "live" {
		t.Fatal("late old runtime interrupted the current session")
	}
	// Missing meeting numbers still produce durable captures by runtime UUID.
	missing := attach("runtime-no-number", "no-number-one", "")
	again := attach("runtime-no-number", "no-number-two", "")
	if missing.ID == "" || missing.ID != again.ID || missing.Status != "live" {
		t.Fatalf("missing number capture: %+v %+v", missing, again)
	}
	post("/api/demo/calls/"+missing.ID+"/events", `{"kind":"transcript","data":{"text":"captured without meeting number"}}`)
	// Host/creator columns alone never grant read access after a share is revoked.
	if _, err = s.Pool.Exec(context.Background(), `DELETE FROM shares WHERE call_id=$1`, missing.ID); err != nil {
		t.Fatal(err)
	}
	denied := request(t, server.Client(), "GET", server.URL+"/api/calls/"+missing.ID, cookie, nil)
	denied.Body.Close()
	if denied.StatusCode != 403 {
		t.Fatalf("host bypassed participant/share access: %s", denied.Status)
	}
}

func TestInterruptedStreamKeepsClientAndExpiresDurably(t *testing.T) {
	dsn := testDatabase(t)
	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("AUTH_MODE", "")
	t.Setenv("RTMS_RECORDER_URL", "")
	h, err := New(http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	login := request(t, srv.Client(), "POST", srv.URL+"/api/demo/login", nil, strings.NewReader(`{"email":"dana@acme.co"}`))
	var cookie *http.Cookie
	for _, c := range login.Cookies() {
		if c.Name == "zc_session" {
			cookie = c
		}
	}
	login.Body.Close()
	post := func(path, body string) Call {
		t.Helper()
		resp := request(t, srv.Client(), "POST", srv.URL+path, cookie, strings.NewReader(body))
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("%s: %s %s", path, resp.Status, raw)
		}
		var c Call
		json.NewDecoder(resp.Body).Decode(&c)
		return c
	}
	call := post("/api/calls", `{"invitation":"https://zoom.us/j/55566677788"}`)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/calls/"+call.ID+"/stream", nil)
	req.AddCookie(cookie)
	feed, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer feed.Body.Close()
	reader := bufio.NewReader(feed.Body)
	until := func(want string) {
		t.Helper()
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("waiting for %s: %v", want, err)
			}
			if strings.Contains(line, "event: end") && want != "event: end" {
				t.Fatal("client ended during recoverable interruption")
			}
			if strings.Contains(line, want) {
				return
			}
		}
	}
	until(": connected")
	attach := func() Call {
		return post("/api/rtms/streams", `{"meeting_id":"55566677788","meeting_uuid":"recoverable","rtms_stream_id":"same-id"}`)
	}
	attach()
	until("event: stream.started")
	post("/api/rtms/streams/same-id/stop", `{"status":"interrupted"}`)
	until("event: stream.paused")
	resumed := attach()
	if resumed.ID != call.ID || resumed.Status != "live" {
		t.Fatalf("resume: %+v", resumed)
	}
	post("/api/demo/calls/"+call.ID+"/events", `{"kind":"transcript","data":{"text":"after same-ID recovery"}}`)
	until("after same-ID recovery")
	// A phantom second stream cannot pin the real call live beyond its persisted deadline.
	post("/api/rtms/streams", `{"meeting_uuid":"recoverable","rtms_stream_id":"failed-dial"}`)
	post("/api/rtms/streams/failed-dial/stop", `{"status":"interrupted"}`)
	post("/api/rtms/streams/same-id/stop", `{"status":"stopped"}`)
	s, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.Pool.Exec(context.Background(), `UPDATE rtms_streams SET interrupted_at=now()-interval '3 minutes' WHERE stream_id='failed-dial'`); err != nil {
		t.Fatal(err)
	}
	// A new handler shares only the database, not the old handler's memory.
	fresh, err := New(http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	restarted := httptest.NewServer(fresh)
	defer restarted.Close()
	resp := request(t, restarted.Client(), "GET", restarted.URL+"/api/calls/"+call.ID, cookie, nil)
	var ended Call
	json.NewDecoder(resp.Body).Decode(&ended)
	resp.Body.Close()
	if ended.Status != "interrupted" {
		t.Fatalf("expired call: %+v", ended)
	}
	until("event: end")
	// Final stopped stream IDs never reopen, even after the handler restarts.
	closed := attach()
	if closed.Status != "interrupted" {
		t.Fatalf("closed ID reopened: %+v", closed)
	}
}

// A signaling exit can be reported before the stream was ever attached to a
// call, and Zoom's stop can follow it. Both deliveries must be acknowledged and
// leave the stream closed without claiming the pending registration.
func TestStopBeforeAttachTombstoneAcceptsInterruptedThenStopped(t *testing.T) {
	t.Setenv("DATABASE_URL", testDatabase(t))
	t.Setenv("AUTH_MODE", "")
	t.Setenv("RTMS_RECORDER_URL", "")
	h, err := New(http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	login := request(t, srv.Client(), "POST", srv.URL+"/api/demo/login", nil, strings.NewReader(`{"email":"dana@acme.co"}`))
	var cookie *http.Cookie
	for _, c := range login.Cookies() {
		if c.Name == "zc_session" {
			cookie = c
		}
	}
	login.Body.Close()
	post := func(path, body string, status int) []byte {
		t.Helper()
		resp := request(t, srv.Client(), "POST", srv.URL+path, cookie, strings.NewReader(body))
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != status {
			t.Fatalf("POST %s: %s %s", path, resp.Status, raw)
		}
		return raw
	}
	var pending Call
	json.Unmarshal(post("/api/calls", `{"invitation":"https://zoom.us/j/44455566677"}`, 201), &pending)
	post("/api/rtms/streams/never-attached/stop", `{"status":"interrupted"}`, 200)
	post("/api/rtms/streams/never-attached/stop", `{"status":"stopped"}`, 200)
	var closed struct {
		Status string `json:"status"`
	}
	json.Unmarshal(post("/api/rtms/streams", `{"meeting_id":"44455566677","meeting_uuid":"late-start","rtms_stream_id":"never-attached"}`, 200), &closed)
	if closed.Status != "stopped" {
		t.Fatalf("closed stream attached: %+v", closed)
	}
	resp := request(t, srv.Client(), "GET", srv.URL+"/api/calls/"+pending.ID, cookie, nil)
	var still Call
	json.NewDecoder(resp.Body).Decode(&still)
	resp.Body.Close()
	if still.Status != "scheduled" {
		t.Fatalf("tombstoned stream claimed the pending call: %+v", still)
	}
}
