package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	post("/api/rtms/streams/stream-old/stop", `{"status":"stopped"}`)
	resumed := attach("runtime-old", "stream-resumed", "12345678901")
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
