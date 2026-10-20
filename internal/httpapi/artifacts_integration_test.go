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
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/tylergannon/zoom-connect/internal/store"
	"github.com/tylergannon/zoom-connect/internal/store/db"
)

// artifactTestServer is the real handler stack with a short access/version tick.
func artifactTestServer(t *testing.T, dsn string) (*Server, *httptest.Server) {
	return artifactTestServerTick(t, dsn, 100*time.Millisecond)
}

func artifactTestServerTick(t *testing.T, dsn string, tick time.Duration) (*Server, *httptest.Server) {
	t.Helper()
	s, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	a := &Server{s: s, h: newHub(s), artifactTick: tick}
	if err = a.seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	a.routes(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return a, server
}

// cred is either a browser session or an agent API key.
type cred struct {
	cookie *http.Cookie
	key    string
}

func artifactDo(t *testing.T, s *httptest.Server, method, path string, c cred, body string) (int, []byte, http.Header) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, s.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	resp, err := s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, resp.Header
}

func artifactLogin(t *testing.T, s *httptest.Server, email string) cred {
	t.Helper()
	r := request(t, s.Client(), "POST", s.URL+"/api/demo/login", nil, strings.NewReader(`{"email":"`+email+`"}`))
	defer r.Body.Close()
	for _, c := range r.Cookies() {
		if c.Name == "zc_session" {
			return cred{cookie: c}
		}
	}
	t.Fatalf("login %s: %s", email, r.Status)
	return cred{}
}

// artifactKey creates an API key through the existing key endpoint.
func artifactKey(t *testing.T, s *httptest.Server, owner cred) (cred, string) {
	t.Helper()
	code, b, _ := artifactDo(t, s, "POST", "/api/keys", owner, `{"name":"meeting-agent"}`)
	var out struct {
		Key struct {
			ID string `json:"id"`
		} `json:"key"`
		Secret string `json:"secret"`
	}
	if code != 201 || json.Unmarshal(b, &out) != nil || out.Secret == "" {
		t.Fatalf("create key: %d %s", code, b)
	}
	return cred{key: out.Secret}, out.Key.ID
}

func createTestArtifact(t *testing.T, s *httptest.Server, c cred, callID, title, html string) ArtifactSnapshot {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"title": title, "html": html})
	code, b, _ := artifactDo(t, s, "POST", "/api/calls/"+callID+"/artifacts", c, string(body))
	var out ArtifactSnapshot
	if code != 201 || json.Unmarshal(b, &out) != nil {
		t.Fatalf("create artifact: %d %s", code, b)
	}
	return out
}

func updateTestArtifact(t *testing.T, s *httptest.Server, c cred, callID, id string, base int64, html string) (int, []byte) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"base_version": base, "html": html})
	code, b, _ := artifactDo(t, s, "PUT", "/api/calls/"+callID+"/artifacts/"+id, c, string(body))
	return code, b
}

func TestArtifactScopesAndWrongCallIsolation(t *testing.T) {
	a, s := artifactTestServer(t, testDatabase(t))
	dana := artifactLogin(t, s, "dana@acme.co")
	call := eventTestCall(t, s, dana.cookie)
	other := eventTestCall(t, s, dana.cookie)
	danaKey, danaKeyID := artifactKey(t, s, dana)
	jordan := artifactLogin(t, s, "jordan@acme.co")
	jordanKey, _ := artifactKey(t, s, jordan)
	base := "/api/calls/" + call.ID + "/artifacts"

	if code, _, _ := artifactDo(t, s, "POST", base, cred{}, `{"title":"x","html":"x"}`); code != 401 {
		t.Fatalf("anonymous create: %d", code)
	}
	if code, _, _ := artifactDo(t, s, "POST", base, cred{key: "zc_not-a-key"}, `{"title":"x","html":"x"}`); code != 401 {
		t.Fatalf("unknown key create: %d", code)
	}
	if code, _, _ := artifactDo(t, s, "POST", base, jordanKey, `{"title":"x","html":"x"}`); code != 403 {
		t.Fatalf("outsider key create: %d", code)
	}
	if code, _, _ := artifactDo(t, s, "GET", base, jordanKey, ""); code != 403 {
		t.Fatalf("outsider list: %d", code)
	}

	// The call owner's agent publishes with its existing API key.
	art := createTestArtifact(t, s, danaKey, call.ID, "Decisions", "<h1>v1</h1>")
	if art.Version != 1 || art.HTML != "<h1>v1</h1>" || art.UpdatedVia != "meeting-agent" || art.UpdatedBy.ID != "u-dana" || art.ViewerURL != "/calls/"+call.ID+"/artifacts/"+art.ID {
		t.Fatalf("created artifact: %+v", art)
	}
	one := base + "/" + art.ID
	for _, path := range []string{one, one + "/stream", one + "/versions/1"} {
		if code, _, _ := artifactDo(t, s, "GET", path, jordan, ""); code != 403 {
			t.Fatalf("outsider read %s: %d", path, code)
		}
	}
	// HTML is only ever delivered as escaped JSON data, never as a document.
	code, b, h := artifactDo(t, s, "GET", "/api/v1/calls/"+call.ID+"/artifacts/"+art.ID, danaKey, "")
	var read ArtifactSnapshot
	if code != 200 || json.Unmarshal(b, &read) != nil || read.HTML != "<h1>v1</h1>" || strings.Contains(string(b), "<h1>") || h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Cache-Control") != "no-store" || !strings.HasPrefix(h.Get("Content-Type"), "application/json") {
		t.Fatalf("v1 read: %d %v %s", code, h, b)
	}

	// A share makes Jordan (and Jordan's key) a reader, not a writer.
	if code, b, _ := artifactDo(t, s, "POST", "/api/calls/"+call.ID+"/shares", dana, `{"email":"jordan@acme.co"}`); code != 201 {
		t.Fatalf("share: %d %s", code, b)
	}
	if code, _, _ := artifactDo(t, s, "GET", one, jordanKey, ""); code != 200 {
		t.Fatalf("shared reader: %d", code)
	}
	if code, _ := updateTestArtifact(t, s, jordanKey, call.ID, art.ID, 1, "<p>reader edit</p>"); code != 403 {
		t.Fatalf("shared reader update: %d", code)
	}
	if code, _, _ := artifactDo(t, s, "POST", base, jordan, `{"title":"x","html":"x"}`); code != 403 {
		t.Fatalf("shared reader create: %d", code)
	}

	// A participant who is not the host can write through their own key.
	if err := a.s.Q.AddParticipant(context.Background(), db.AddParticipantParams{CallID: call.ID, ParticipantID: "u-priya", UserID: pgtype.Text{String: "u-priya", Valid: true}, Name: "Priya Shah"}); err != nil {
		t.Fatal(err)
	}
	priyaKey, _ := artifactKey(t, s, artifactLogin(t, s, "priya@acme.co"))
	code, b = updateTestArtifact(t, s, priyaKey, call.ID, art.ID, 1, "<p>v2</p>")
	var v2 ArtifactSnapshot
	if code != 200 || json.Unmarshal(b, &v2) != nil || v2.Version != 2 || v2.Title != "Decisions" || v2.UpdatedBy.ID != "u-priya" || v2.CreatedBy.ID != "u-dana" {
		t.Fatalf("participant update: %d %s", code, b)
	}

	// The artifact is invisible through any other call, even to its owner.
	wrong := "/api/calls/" + other.ID + "/artifacts/" + art.ID
	for _, path := range []string{wrong, wrong + "/stream", wrong + "/versions/1"} {
		if code, b, _ := artifactDo(t, s, "GET", path, danaKey, ""); code != 404 {
			t.Fatalf("wrong-call read %s: %d %s", path, code, b)
		}
	}
	if code, _ := updateTestArtifact(t, s, danaKey, other.ID, art.ID, 2, "<p>hijack</p>"); code != 404 {
		t.Fatalf("wrong-call update: %d", code)
	}
	if code, b, _ := artifactDo(t, s, "GET", "/api/calls/"+other.ID+"/artifacts", danaKey, ""); code != 200 || strings.TrimSpace(string(b)) != "[]" {
		t.Fatalf("wrong-call list: %d %s", code, b)
	}
	if code, b, _ := artifactDo(t, s, "GET", one, danaKey, ""); code != 200 || !strings.Contains(string(b), `"version":2`) {
		t.Fatalf("wrong-call update changed artifact: %d %s", code, b)
	}

	// Revoking the key immediately ends its access.
	if code, _, _ := artifactDo(t, s, "DELETE", "/api/keys/"+danaKeyID, dana, ""); code != 204 {
		t.Fatalf("revoke: %d", code)
	}
	if code, _, _ := artifactDo(t, s, "GET", one, danaKey, ""); code != 401 {
		t.Fatalf("revoked key read: %d", code)
	}
}

func TestArtifactVersionConflictsBoundsAndReadOnlyAfterEnd(t *testing.T) {
	_, s := artifactTestServer(t, testDatabase(t))
	dana := artifactLogin(t, s, "dana@acme.co")
	call := eventTestCall(t, s, dana.cookie)
	art := createTestArtifact(t, s, dana, call.ID, "Plan", "<p>one</p>")
	one := "/api/calls/" + call.ID + "/artifacts/" + art.ID

	if code, b := updateTestArtifact(t, s, dana, call.ID, art.ID, 1, "<p>two</p>"); code != 200 || !strings.Contains(string(b), `"version":2`) {
		t.Fatalf("update: %d %s", code, b)
	}
	// A writer that read version 1 cannot overwrite version 2.
	code, b := updateTestArtifact(t, s, dana, call.ID, art.ID, 1, "<p>stale</p>")
	var conflict struct {
		Error   string `json:"error"`
		Current int64  `json:"current_version"`
	}
	if code != 409 || json.Unmarshal(b, &conflict) != nil || conflict.Current != 2 {
		t.Fatalf("stale update: %d %s", code, b)
	}
	if _, b, _ := artifactDo(t, s, "GET", one, dana, ""); !strings.Contains(string(b), "two") || strings.Contains(string(b), "stale") {
		t.Fatalf("stale write leaked: %s", b)
	}

	// Concurrent writers from the same base: exactly one wins.
	var wg sync.WaitGroup
	results := make(chan int, 5)
	for i := range 5 {
		wg.Go(func() {
			code, _ := updateTestArtifact(t, s, dana, call.ID, art.ID, 2, fmt.Sprintf("<p>race %d</p>", i))
			results <- code
		})
	}
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for code := range results {
		switch code {
		case 200:
			wins++
		case 409:
			conflicts++
		}
	}
	if wins != 1 || conflicts != 4 {
		t.Fatalf("concurrent writers: %d wins, %d conflicts", wins, conflicts)
	}

	for name, tc := range map[string]struct {
		body string
		code int
	}{
		"missing base_version": {`{"html":"<p>x</p>"}`, 400},
		"missing html":         {`{"base_version":3}`, 400},
		"unknown field":        {`{"base_version":3,"html":"x","script":"x"}`, 400},
		"NUL":                  {`{"base_version":3,"html":"a\u0000b"}`, 400},
		"long title":           {`{"base_version":3,"html":"x","title":"` + strings.Repeat("t", 121) + `"}`, 400},
		"html over 256 KiB":    {`{"base_version":3,"html":"` + strings.Repeat("a", maxArtifactHTML+1) + `"}`, 413},
		"body over 1 MiB":      {`{"base_version":3,"html":"` + strings.Repeat("a", maxArtifactBody) + `"}`, 413},
	} {
		if code, b, _ := artifactDo(t, s, "PUT", one, dana, tc.body); code != tc.code {
			t.Fatalf("%s: got %d %s, want %d", name, code, b, tc.code)
		}
	}
	if code, b, _ := artifactDo(t, s, "GET", one, dana, ""); code != 200 || !strings.Contains(string(b), `"version":3`) {
		t.Fatalf("rejected input changed artifact: %s", b)
	}
	if code, b, _ := artifactDo(t, s, "GET", one+"/versions/1", dana, ""); code != 200 || !strings.Contains(string(b), "one") {
		t.Fatalf("version 1 snapshot: %d %s", code, b)
	}

	// Every version stays in the call's history and the page keeps its id.
	last := int64(33)
	for v := int64(3); v < last; v++ {
		if code, b := updateTestArtifact(t, s, dana, call.ID, art.ID, v, fmt.Sprintf("<p>%d</p>", v+1)); code != 200 {
			t.Fatalf("update %d: %d %s", v, code, b)
		}
	}
	for _, v := range []int64{1, 2, 3, last} {
		if code, b, _ := artifactDo(t, s, "GET", fmt.Sprintf("%s/versions/%d", one, v), dana, ""); code != 200 {
			t.Fatalf("version %d after %d updates: %d %s", v, last-1, code, b)
		}
	}
	var pages []Artifact
	if code, b, _ := artifactDo(t, s, "GET", "/api/calls/"+call.ID+"/artifacts", dana, ""); code != 200 || json.Unmarshal(b, &pages) != nil || len(pages) != 1 || pages[0].ID != art.ID || pages[0].Version != last {
		t.Fatalf("one page at version %d: %d %s", last, code, b)
	}

	for i := 1; i < maxArtifactsPerCall; i++ {
		createTestArtifact(t, s, dana, call.ID, fmt.Sprintf("Extra %d", i), "")
	}
	if code, b, _ := artifactDo(t, s, "POST", "/api/calls/"+call.ID+"/artifacts", dana, `{"title":"one too many","html":""}`); code != 409 {
		t.Fatalf("artifact limit: %d %s", code, b)
	}

	// After the call ends, artifacts stay readable and stop accepting writes.
	if code, _, _ := artifactDo(t, s, "POST", "/api/demo/calls/"+call.ID+"/stop", dana, ""); code != 200 {
		t.Fatalf("stop: %d", code)
	}
	if code, b := updateTestArtifact(t, s, dana, call.ID, art.ID, last, "<p>late</p>"); code != 409 || !strings.Contains(string(b), "read-only") {
		t.Fatalf("update after end: %d %s", code, b)
	}
	if code, b, _ := artifactDo(t, s, "GET", one, dana, ""); code != 200 || !strings.Contains(string(b), fmt.Sprintf(`"version":%d`, last)) {
		t.Fatalf("read after end: %d %s", code, b)
	}
}

type sseMessage struct {
	event, id string
	data      string
}

// openArtifactStream parses SSE frames until EOF, then closes the channel.
func openArtifactStream(t *testing.T, s *httptest.Server, path string, c cred, lastID string) (<-chan sseMessage, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	req, _ := http.NewRequestWithContext(ctx, "GET", s.URL+path, nil)
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	resp, err := s.Client().Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" {
		cancel()
		t.Fatalf("stream %s: %s", path, resp.Status)
	}
	out := make(chan sseMessage, 32)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		r := bufio.NewReader(resp.Body)
		var m sseMessage
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSuffix(line, "\n")
			switch {
			case line == "":
				if m.event != "" {
					out <- m
				}
				m = sseMessage{}
			case strings.HasPrefix(line, "event: "):
				m.event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "id: "):
				m.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "data: "):
				m.data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	return out, cancel
}

func nextSnapshot(t *testing.T, events <-chan sseMessage) ArtifactSnapshot {
	t.Helper()
	select {
	case m, ok := <-events:
		if !ok {
			t.Fatal("stream closed before the next snapshot")
		}
		var snap ArtifactSnapshot
		if m.event != "artifact" || json.Unmarshal([]byte(m.data), &snap) != nil || m.id != fmt.Sprint(snap.Version) {
			t.Fatalf("unexpected stream message: %+v", m)
		}
		return snap
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a snapshot")
	}
	return ArtifactSnapshot{}
}

func waitClosed(t *testing.T, events <-chan sseMessage, want string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case m, ok := <-events:
			if !ok {
				if want != "" {
					t.Fatalf("stream closed without %q", want)
				}
				return
			}
			if want != "" && m.event == want {
				return
			}
			if m.event == "artifact" {
				t.Fatalf("snapshot delivered after access should have ended: %+v", m)
			}
		case <-deadline:
			t.Fatal("stream stayed open")
		}
	}
}

func TestArtifactStreamLatestReconnectRevocationAndRestart(t *testing.T) {
	dsn := testDatabase(t)
	a, s := artifactTestServer(t, dsn)
	dana := artifactLogin(t, s, "dana@acme.co")
	call := eventTestCall(t, s, dana.cookie)
	art := createTestArtifact(t, s, dana, call.ID, "Live board", "<p>v1</p>")
	path := "/api/calls/" + call.ID + "/artifacts/" + art.ID + "/stream"
	if code, b, _ := artifactDo(t, s, "POST", "/api/calls/"+call.ID+"/shares", dana, `{"email":"jordan@acme.co"}`); code != 201 {
		t.Fatalf("share: %d %s", code, b)
	}
	jordan := artifactLogin(t, s, "jordan@acme.co")

	// A shared reader sees the current snapshot first, then live updates.
	events, cancel := openArtifactStream(t, s, path, jordan, "")
	if snap := nextSnapshot(t, events); snap.Version != 1 || snap.HTML != "<p>v1</p>" {
		t.Fatalf("initial snapshot: %+v", snap)
	}
	updateTestArtifact(t, s, dana, call.ID, art.ID, 1, "<p>v2</p>")
	if snap := nextSnapshot(t, events); snap.Version != 2 || snap.HTML != "<p>v2</p>" {
		t.Fatalf("live snapshot: %+v", snap)
	}
	cancel()

	// Updates missed while disconnected collapse to the latest on reconnect.
	updateTestArtifact(t, s, dana, call.ID, art.ID, 2, "<p>v3</p>")
	updateTestArtifact(t, s, dana, call.ID, art.ID, 3, "<p>v4</p>")
	events, cancel = openArtifactStream(t, s, path, jordan, "2")
	if snap := nextSnapshot(t, events); snap.Version != 4 || snap.HTML != "<p>v4</p>" {
		t.Fatalf("reconnect snapshot: %+v", snap)
	}
	cancel()
	// A client that already holds the latest gets only newer versions.
	events, cancel = openArtifactStream(t, s, path+"?from=4", jordan, "")
	updateTestArtifact(t, s, dana, call.ID, art.ID, 4, "<p>v5</p>")
	if snap := nextSnapshot(t, events); snap.Version != 5 {
		t.Fatalf("resume snapshot: %+v", snap)
	}

	// Removing the share closes the open stream and further reads fail.
	code, b, _ := artifactDo(t, s, "GET", "/api/calls/"+call.ID+"/shares", dana, "")
	var shares struct {
		Shares []struct {
			ID string `json:"id"`
		} `json:"shares"`
	}
	if code != 200 || json.Unmarshal(b, &shares) != nil || len(shares.Shares) != 1 {
		t.Fatalf("shares: %d %s", code, b)
	}
	if code, _, _ := artifactDo(t, s, "DELETE", "/api/calls/"+call.ID+"/shares/"+shares.Shares[0].ID, dana, ""); code != 204 {
		t.Fatalf("unshare: %d", code)
	}
	waitClosed(t, events, "")
	cancel()
	if code, _, _ := artifactDo(t, s, "GET", "/api/calls/"+call.ID+"/artifacts/"+art.ID, jordan, ""); code != 403 {
		t.Fatalf("read after unshare: %d", code)
	}

	// A revoked key's open stream closes too.
	key, keyID := artifactKey(t, s, dana)
	events, cancel = openArtifactStream(t, s, path, key, "")
	nextSnapshot(t, events)
	artifactDo(t, s, "DELETE", "/api/keys/"+keyID, dana, "")
	waitClosed(t, events, "")
	cancel()

	// A second instance (fresh hub) on the same database serves the latest
	// state and picks up writes made through the first instance by polling.
	_, s2 := artifactTestServer(t, dsn)
	dana2 := artifactLogin(t, s2, "dana@acme.co")
	events, cancel = openArtifactStream(t, s2, path, dana2, "")
	defer cancel()
	if snap := nextSnapshot(t, events); snap.Version != 5 || snap.HTML != "<p>v5</p>" {
		t.Fatalf("second instance snapshot: %+v", snap)
	}
	updateTestArtifact(t, s, dana, call.ID, art.ID, 5, "<p>v6</p>")
	if snap := nextSnapshot(t, events); snap.Version != 6 {
		t.Fatalf("cross-instance update: %+v", snap)
	}
	// The stream ends when the call ends.
	artifactDo(t, s, "POST", "/api/demo/calls/"+call.ID+"/stop", dana, "")
	waitClosed(t, events, "end")
	_ = a
}

// An update that wakes a stream must not reach a key or share revoked since
// the last periodic check. The tick is an hour, so within this test only the
// check made at wake-up can close these streams.
func TestArtifactStreamRechecksAccessBeforeWakeSnapshot(t *testing.T) {
	_, s := artifactTestServerTick(t, testDatabase(t), time.Hour)
	dana := artifactLogin(t, s, "dana@acme.co")
	call := eventTestCall(t, s, dana.cookie)
	// revokeThenUpdate opens a stream as viewer, revokes its access, then
	// publishes a version, which wakes the stream.
	revokeThenUpdate := func(t *testing.T, viewer cred, revoke func()) {
		art := createTestArtifact(t, s, dana, call.ID, "Live board", "<p>v1</p>")
		events, cancel := openArtifactStream(t, s, "/api/calls/"+call.ID+"/artifacts/"+art.ID+"/stream", viewer, "")
		defer cancel()
		if snap := nextSnapshot(t, events); snap.Version != 1 {
			t.Fatalf("initial snapshot: %+v", snap)
		}
		revoke()
		if code, b := updateTestArtifact(t, s, dana, call.ID, art.ID, 1, "<p>v2 after revocation</p>"); code != 200 {
			t.Fatalf("update: %d %s", code, b)
		}
		waitClosed(t, events, "")
	}

	t.Run("revoked key", func(t *testing.T) {
		key, keyID := artifactKey(t, s, dana)
		revokeThenUpdate(t, key, func() {
			if code, _, _ := artifactDo(t, s, "DELETE", "/api/keys/"+keyID, dana, ""); code != 204 {
				t.Fatalf("revoke key: %d", code)
			}
		})
	})
	t.Run("removed share", func(t *testing.T) {
		if code, b, _ := artifactDo(t, s, "POST", "/api/calls/"+call.ID+"/shares", dana, `{"email":"jordan@acme.co"}`); code != 201 {
			t.Fatalf("share: %d %s", code, b)
		}
		revokeThenUpdate(t, artifactLogin(t, s, "jordan@acme.co"), func() {
			code, b, _ := artifactDo(t, s, "GET", "/api/calls/"+call.ID+"/shares", dana, "")
			var shares struct {
				Shares []struct {
					ID string `json:"id"`
				} `json:"shares"`
			}
			if code != 200 || json.Unmarshal(b, &shares) != nil || len(shares.Shares) != 1 {
				t.Fatalf("shares: %d %s", code, b)
			}
			if code, _, _ := artifactDo(t, s, "DELETE", "/api/calls/"+call.ID+"/shares/"+shares.Shares[0].ID, dana, ""); code != 204 {
				t.Fatalf("unshare: %d", code)
			}
		})
	})
}

type historyPage struct {
	CurrentVersion int64                 `json:"current_version"`
	Versions       []ArtifactVersionInfo `json:"versions"`
	NextBefore     *int64                `json:"next_before"`
}

func artifactHistory(t *testing.T, s *httptest.Server, c cred, path string) historyPage {
	t.Helper()
	code, b, _ := artifactDo(t, s, "GET", path, c, "")
	var out historyPage
	if code != 200 || json.Unmarshal(b, &out) != nil || strings.Contains(string(b), `"html"`) {
		t.Fatalf("history %s: %d %s", path, code, b)
	}
	return out
}

func historyVersions(p historyPage) []int64 {
	out := make([]int64, 0, len(p.Versions))
	for _, v := range p.Versions {
		out = append(out, v.Version)
	}
	return out
}

// The history listing pages through every stored version newest first, with
// the call ACL, and survives a server restart.
func TestArtifactHistoryPagesAccessAndRestart(t *testing.T) {
	dsn := testDatabase(t)
	_, s := artifactTestServer(t, dsn)
	dana := artifactLogin(t, s, "dana@acme.co")
	call := eventTestCall(t, s, dana.cookie)
	other := eventTestCall(t, s, dana.cookie)
	art := createTestArtifact(t, s, dana, call.ID, "Board", "<p>1</p>")
	key, _ := artifactKey(t, s, dana)
	for v := int64(1); v < 60; v++ {
		body, _ := json.Marshal(map[string]any{"base_version": v, "html": fmt.Sprintf("<p>%d</p>", v+1), "title": fmt.Sprintf("Board %d", v+1)})
		if code, b, _ := artifactDo(t, s, "PUT", "/api/calls/"+call.ID+"/artifacts/"+art.ID, key, string(body)); code != 200 {
			t.Fatalf("update %d: %d %s", v, code, b)
		}
	}
	versions := "/api/calls/" + call.ID + "/artifacts/" + art.ID + "/versions"

	first := artifactHistory(t, s, dana, versions)
	got := historyVersions(first)
	if first.CurrentVersion != 60 || len(got) != 50 || got[0] != 60 || got[49] != 11 || first.NextBefore == nil || *first.NextBefore != 11 {
		t.Fatalf("first page: current %d, %d versions %v, next %v", first.CurrentVersion, len(got), got, first.NextBefore)
	}
	if v := first.Versions[0]; v.Title != "Board 60" || v.Via != "meeting-agent" || v.Author.ID != "u-dana" || v.Bytes != int64(len("<p>60</p>")) {
		t.Fatalf("newest entry: %+v", v)
	}
	rest := artifactHistory(t, s, dana, fmt.Sprintf("%s?before=%d", versions, *first.NextBefore))
	if got := historyVersions(rest); len(got) != 10 || got[0] != 10 || got[9] != 1 || rest.NextBefore != nil {
		t.Fatalf("last page: %v next %v", got, rest.NextBefore)
	}
	if got := historyVersions(artifactHistory(t, s, dana, versions+"?before=4&limit=2")); fmt.Sprint(got) != "[3 2]" {
		t.Fatalf("limited page: %v", got)
	}
	for _, q := range []string{"?limit=0", "?limit=101", "?before=0", "?before=x"} {
		if code, b, _ := artifactDo(t, s, "GET", versions+q, dana, ""); code != 400 {
			t.Fatalf("%s: %d %s", q, code, b)
		}
	}

	// Same ACL as reads: anonymous 401, outsider 403, share recipient allowed,
	// and the artifact id under another call or an unknown id is 404.
	if code, _, _ := artifactDo(t, s, "GET", versions, cred{}, ""); code != 401 {
		t.Fatalf("anonymous history: %d", code)
	}
	jordan := artifactLogin(t, s, "jordan@acme.co")
	if code, _, _ := artifactDo(t, s, "GET", versions, jordan, ""); code != 403 {
		t.Fatalf("outsider history: %d", code)
	}
	if code, b, _ := artifactDo(t, s, "POST", "/api/calls/"+call.ID+"/shares", dana, `{"email":"jordan@acme.co"}`); code != 201 {
		t.Fatalf("share: %d %s", code, b)
	}
	if got := historyVersions(artifactHistory(t, s, jordan, versions+"?limit=1")); fmt.Sprint(got) != "[60]" {
		t.Fatalf("shared history: %v", got)
	}
	for _, path := range []string{"/api/calls/" + other.ID + "/artifacts/" + art.ID + "/versions", "/api/v1/calls/" + call.ID + "/artifacts/nope/versions"} {
		if code, _, _ := artifactDo(t, s, "GET", path, dana, ""); code != 404 {
			t.Fatalf("%s: %d", path, code)
		}
	}

	// A fresh server on the same database serves the latest version and the
	// whole history.
	_, s2 := artifactTestServer(t, dsn)
	dana2 := artifactLogin(t, s2, "dana@acme.co")
	code, b, _ := artifactDo(t, s2, "GET", "/api/calls/"+call.ID+"/artifacts/"+art.ID, dana2, "")
	var latest ArtifactSnapshot
	if code != 200 || json.Unmarshal(b, &latest) != nil || latest.ID != art.ID || latest.Version != 60 || latest.HTML != "<p>60</p>" {
		t.Fatalf("latest after restart: %d %s", code, b)
	}
	if got := historyVersions(artifactHistory(t, s2, dana2, versions+"?limit=100")); len(got) != 60 || got[59] != 1 {
		t.Fatalf("history after restart: %d versions", len(got))
	}
	if code, b, _ := artifactDo(t, s2, "GET", versions+"/1", dana2, ""); code != 200 || !strings.Contains(string(b), `"html":"\u003cp\u003e1\u003c/p\u003e"`) {
		t.Fatalf("version 1 after restart: %d %s", code, b)
	}
}
