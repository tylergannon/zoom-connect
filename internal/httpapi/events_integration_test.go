package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/tylergannon/zoom-connect/internal/store"
	"github.com/tylergannon/zoom-connect/internal/store/db"
)

func eventTestServer(t *testing.T, dsn string) (*Server, *httptest.Server) {
	t.Helper()
	s, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	a := &Server{s: s, h: newHub(s)}
	if err = a.seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = a.importArchives(context.Background()); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	a.routes(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return a, server
}
func eventLogin(t *testing.T, s *httptest.Server) *http.Cookie {
	t.Helper()
	r := request(t, s.Client(), "POST", s.URL+"/api/demo/login", nil, strings.NewReader(`{"email":"dana@acme.co"}`))
	defer r.Body.Close()
	for _, c := range r.Cookies() {
		if c.Name == "zc_session" {
			return c
		}
	}
	t.Fatal("missing login cookie")
	return nil
}
func eventTestCall(t *testing.T, s *httptest.Server, cookie *http.Cookie) Call {
	t.Helper()
	r := request(t, s.Client(), "POST", s.URL+"/api/demo/calls", cookie, strings.NewReader(`{"topic":"Postgres streaming proof","source":"zoom"}`))
	defer r.Body.Close()
	var call Call
	if r.StatusCode != 201 {
		t.Fatalf("create: %s", r.Status)
	}
	if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
		t.Fatal(err)
	}
	return call
}
func TestPostgresEventsReplayLiveFailureAndRestart(t *testing.T) {
	dsn := testDatabase(t)
	a, s := eventTestServer(t, dsn)
	cookie := eventLogin(t, s)
	call := eventTestCall(t, s, cookie)
	// Both readers catch up and then follow the same concurrently appended events.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	read := func() <-chan []Event {
		req, _ := http.NewRequestWithContext(ctx, "GET", s.URL+"/api/calls/"+call.ID+"/stream", nil)
		req.AddCookie(cookie)
		resp, err := s.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("stream: %s", resp.Status)
		}
		result := make(chan []Event, 1)
		go func() {
			defer resp.Body.Close()
			var events []Event
			scan := bufio.NewScanner(resp.Body)
			for scan.Scan() {
				if line := scan.Text(); strings.HasPrefix(line, "data: ") {
					var ev Event
					if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev) == nil && ev.Seq > 0 {
						events = append(events, ev)
					}
					if len(events) == 14 {
						result <- events
						return
					}
				}
			}
			result <- events
		}()
		return result
	}
	one, two := read(), read()
	// An actual database rejection must be a failed request and must consume no ID.
	_, err := a.s.Pool.Exec(ctx, `ALTER TABLE call_events ADD CONSTRAINT reject_test_event CHECK (kind<>'reject-test')`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		a.s.Pool.Exec(context.Background(), `ALTER TABLE call_events DROP CONSTRAINT IF EXISTS reject_test_event`)
	})
	resp := request(t, s.Client(), "POST", s.URL+"/api/demo/calls/"+call.ID+"/events", cookie, strings.NewReader(`{"kind":"reject-test"}`))
	resp.Body.Close()
	if resp.StatusCode != 500 {
		t.Fatalf("failed persistence acknowledged: %s", resp.Status)
	}
	var last int64
	if err = a.s.Pool.QueryRow(ctx, `SELECT last_seq FROM calls WHERE id=$1`, call.ID).Scan(&last); err != nil || last != 1 {
		t.Fatalf("failed write advanced seq: %d %v", last, err)
	}
	var wg sync.WaitGroup
	errors := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			payload := fmt.Sprintf(`{"kind":"transcript","participant":{"id":"tyler","name":"Tyler"},"data":{"text":"sentence %d","final":true}}`, i)
			req, _ := http.NewRequestWithContext(ctx, "POST", s.URL+"/api/demo/calls/"+call.ID+"/events", strings.NewReader(payload))
			req.AddCookie(cookie)
			r, err := s.Client().Do(req)
			if err == nil {
				r.Body.Close()
				if r.StatusCode != 201 {
					err = fmt.Errorf("append: %s", r.Status)
				}
			}
			errors <- err
		}(i)
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	resp = request(t, s.Client(), "POST", s.URL+"/api/demo/calls/"+call.ID+"/stop", cookie, strings.NewReader(`{}`))
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("stop: %s", resp.Status)
	}
	first, second := <-one, <-two
	if len(first) != 14 || !reflect.DeepEqual(first, second) {
		t.Fatalf("readers differ: %d versus %d", len(first), len(second))
	}
	for i, e := range first {
		if e.Seq != int64(i+1) {
			t.Fatalf("sequence gap: %+v", e)
		}
	}
	var count int
	if err = a.s.Pool.QueryRow(ctx, `SELECT count(*) FROM call_events WHERE call_id=$1`, call.ID).Scan(&count); err != nil || count != 14 {
		t.Fatalf("durable rows: %d %v", count, err)
	}
	// A fresh handler and hub replay committed rows without any archive directory.
	s.Close()
	_, restarted := eventTestServer(t, dsn)
	resp = request(t, restarted.Client(), "GET", restarted.URL+"/api/calls/"+call.ID+"/events?from=7", cookie, nil)
	var replay []Event
	err = json.NewDecoder(resp.Body).Decode(&replay)
	resp.Body.Close()
	if err != nil || !reflect.DeepEqual(replay, first[7:]) {
		t.Fatalf("restart replay differs: %v", err)
	}
	req, _ := http.NewRequest("GET", restarted.URL+"/api/calls/"+call.ID+"/stream", nil)
	req.AddCookie(cookie)
	req.Header.Set("Last-Event-ID", "12")
	resp, err = restarted.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(body), "id: 12\n") || !strings.Contains(string(body), "id: 13\n") || !strings.Contains(string(body), "id: 14\n") {
		t.Fatalf("SSE cursor: %s", body)
	}
	resp = request(t, restarted.Client(), "GET", restarted.URL+"/api/calls/"+call.ID+"/transcript?format=txt", cookie, nil)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "Tyler: sentence 11") {
		t.Fatalf("download lost content: %s", body)
	}
}

func TestLegacyArchiveImportAtomicAndOnce(t *testing.T) {
	dsn := testDatabase(t)
	a, _ := eventTestServer(t, dsn)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "recording.jsonl")
	ev := Event{Seq: 1, TS: 123, Recv: 124, Kind: "transcript", Participant: &Person{ID: "tyler", Name: "Tyler"}, Data: map[string]any{"text": "original live words"}}
	content, _ := json.Marshal(ev)
	if err := os.WriteFile(path, append(content, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	id := newID()
	if err := a.s.Q.CreateCall(ctx, db.CreateCallParams{ID: id, Topic: "legacy", Status: "stopped", Started: pgtype.Timestamptz{Time: time.Now(), Valid: true}, HostUserID: "u-dana", LastSeq: 2, EventPath: path}); err != nil {
		t.Fatal(err)
	}
	// Refuse an incomplete file; no partial rows or migration marker may commit.
	if err := a.importArchives(ctx); err == nil {
		t.Fatal("incomplete archive accepted")
	}
	rows, err := a.h.snapshot(ctx, id, 0)
	if err != nil || len(rows) != 0 {
		t.Fatalf("partial import committed: %v %+v", err, rows)
	}
	c, err := a.s.Q.GetCall(ctx, id)
	if err != nil || c.EventPath != path {
		t.Fatal("failed import cleared path")
	}
	// Correct the fixture's stale expected count, then preserve all event fields.
	if err = a.s.Q.UpdateCallSeq(ctx, db.UpdateCallSeqParams{ID: id, LastSeq: 1}); err != nil {
		t.Fatal(err)
	}
	if err = a.importArchives(ctx); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = a.importArchives(ctx); err != nil {
		t.Fatalf("import depends on old file: %v", err)
	}
	rows, err = a.h.snapshot(ctx, id, 0)
	if err != nil || !reflect.DeepEqual(rows, []Event{ev}) {
		t.Fatalf("import changed event: %+v %v", rows, err)
	}
	next, err := a.emit(ctx, id, "chat", nil, map[string]any{"text": "next event"})
	if err != nil || next.Seq != 2 {
		t.Fatalf("post-import sequence: %+v %v", next, err)
	}
}
