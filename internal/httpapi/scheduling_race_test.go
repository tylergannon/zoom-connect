package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// raceFixture runs two application instances against one database. The
// process-local hub mutex serializes a single instance, so only instances that
// share nothing but Postgres (a deploy overlap) can interleave a stop with an
// attach. Interleaving is forced with a row lock held by the test, never with
// sleeps: the attach transaction waits on the pending call row after it has
// already looked up its stream and before it inserts the stream mapping.
type raceFixture struct {
	t      *testing.T
	a, b   *httptest.Server
	cookie *http.Cookie
	pool   *pgxpool.Pool
	call   Call
}

const raceMeeting = "55566677788"

func newRaceFixture(t *testing.T) *raceFixture {
	t.Helper()
	dsn := testDatabase(t)
	// Handlers have no Close, so keep the connections they leak to the process small.
	t.Setenv("DATABASE_URL", dsn+"&pool_max_conns=3")
	t.Setenv("AUTH_MODE", "")
	t.Setenv("RTMS_RECORDER_URL", "")
	f := &raceFixture{t: t}
	for _, dst := range []**httptest.Server{&f.a, &f.b} {
		h, err := New(http.NotFoundHandler())
		if err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewServer(h)
		t.Cleanup(srv.Close)
		*dst = srv
	}
	login := request(t, f.a.Client(), "POST", f.a.URL+"/api/demo/login", nil, strings.NewReader(`{"email":"dana@acme.co"}`))
	for _, c := range login.Cookies() {
		if c.Name == "zc_session" {
			f.cookie = c
		}
	}
	login.Body.Close()
	var err error
	if f.pool, err = pgxpool.New(context.Background(), dsn); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.pool.Close)
	raw, code := f.post(f.a, "/api/calls", `{"invitation":"https://zoom.us/j/`+raceMeeting+`"}`)
	if code != 201 {
		t.Fatalf("register pending call: %d %s", code, raw)
	}
	json.Unmarshal(raw, &f.call)
	return f
}

func (f *raceFixture) post(srv *httptest.Server, path, body string) ([]byte, int) {
	f.t.Helper()
	resp := request(f.t, srv.Client(), "POST", srv.URL+path, f.cookie, strings.NewReader(body))
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return raw, resp.StatusCode
}

func (f *raceFixture) attachBody(uuid, stream string) string {
	b, _ := json.Marshal(zoomStreamIdentity{MeetingID: raceMeeting, MeetingUUID: uuid, StreamID: stream})
	return string(b)
}

type reply struct {
	raw  []byte
	code int
}

// async posts in the background and returns a function that waits for the reply.
func (f *raceFixture) async(srv *httptest.Server, path, body string) func() reply {
	done := make(chan reply, 1)
	go func() {
		raw, code := f.post(srv, path, body)
		done <- reply{raw, code}
	}()
	return func() reply {
		f.t.Helper()
		select {
		case r := <-done:
			return r
		case <-time.After(15 * time.Second):
			f.t.Fatalf("POST %s did not finish", path)
			return reply{}
		}
	}
}

func (f *raceFixture) count(query string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func (f *raceFixture) waitUntil(what string, ok func() bool) {
	f.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			f.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// holdPendingCall locks the pending registration row, parking any attach that
// is about to claim it, and returns the release function.
func (f *raceFixture) holdPendingCall() func() {
	f.t.Helper()
	ctx := context.Background()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	var id string
	if err = tx.QueryRow(ctx, `SELECT id FROM calls WHERE id=$1 FOR UPDATE`, f.call.ID).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	var once sync.Once
	release := func() { once.Do(func() { tx.Rollback(ctx) }) }
	f.t.Cleanup(release)
	return release
}

// parkedAttaches counts attach transactions waiting for the pending call row
// (the claim query is the one that follows the stream-identity lookup).
func (f *raceFixture) parkedAttaches() int {
	return f.count(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%zoom_meeting_id=$1 AND status=''scheduled'' FOR UPDATE%'`)
}

// queuedBehindStream counts requests waiting on a per-stream advisory lock.
func (f *raceFixture) queuedBehindStream() int {
	return f.count(`SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND NOT granted`)
}

func (f *raceFixture) rtmsStatus(stream string) string {
	f.t.Helper()
	var status string
	if err := f.pool.QueryRow(context.Background(), `SELECT status FROM rtms_streams WHERE stream_id=$1`, stream).Scan(&status); err != nil && err != pgx.ErrNoRows {
		f.t.Fatal(err)
	}
	return status
}

func (f *raceFixture) callStatus(id string) string {
	f.t.Helper()
	var status string
	if err := f.pool.QueryRow(context.Background(), `SELECT status FROM calls WHERE id=$1`, id).Scan(&status); err != nil {
		f.t.Fatal(err)
	}
	return status
}

// A stop delivered by one instance while another instance is attaching the
// same stream must not turn the attach into a 409. The recorder treats 409 as
// permanent and would capture a standalone call instead of the registered one.
func TestConcurrentStopAndAttachNeverConflict(t *testing.T) {
	for _, stop := range []string{"stopped", "interrupted"} {
		t.Run(stop, func(t *testing.T) {
			f := newRaceFixture(t)
			release := f.holdPendingCall()
			attach := f.async(f.a, "/api/rtms/streams", f.attachBody("race-runtime", "race-stream"))
			f.waitUntil("attach to park on the pending call", func() bool { return f.parkedAttaches() == 1 })
			stopped := f.async(f.b, "/api/rtms/streams/race-stream/stop", `{"status":"`+stop+`"}`)
			// Unfixed, the stop commits its tombstone while the attach is parked.
			// Fixed, the stop queues behind the attach's per-stream lock.
			f.waitUntil("stop to finish or queue behind the attach", func() bool {
				if f.rtmsStatus("race-stream") == stop && f.queuedBehindStream() == 0 {
					return true
				}
				return f.queuedBehindStream() == 1
			})
			release()
			a, s := attach(), stopped()
			if a.code != 200 {
				t.Fatalf("attach lost the race: %d %s", a.code, a.raw)
			}
			if s.code != 200 {
				t.Fatalf("stop: %d %s", s.code, s.raw)
			}
			if n := f.count(`SELECT count(*) FROM calls WHERE zoom_meeting_id=$1`, raceMeeting); n != 1 {
				t.Fatalf("race produced %d calls for the meeting number, want the one registration", n)
			}
			// The attach committed before the stop, so the registered call was claimed
			// and then closed (or paused) by that stream; the stream must not stay live.
			wantCall := map[string]string{"stopped": "stopped", "interrupted": "live"}[stop]
			if got := f.callStatus(f.call.ID); got != wantCall {
				t.Fatalf("registered call is %q, want %q", got, wantCall)
			}
			if got := f.rtmsStatus("race-stream"); got != stop {
				t.Fatalf("stream is %q, want %q", got, stop)
			}
		})
	}
}

// Two instances delivering the same attach must be idempotent, not 409.
func TestConcurrentDuplicateAttachIsIdempotent(t *testing.T) {
	f := newRaceFixture(t)
	release := f.holdPendingCall()
	first := f.async(f.a, "/api/rtms/streams", f.attachBody("dup-runtime", "dup-stream"))
	f.waitUntil("first attach to park", func() bool { return f.parkedAttaches() == 1 })
	second := f.async(f.b, "/api/rtms/streams", f.attachBody("dup-runtime", "dup-stream"))
	f.waitUntil("second attach to park or queue", func() bool { return f.parkedAttaches() == 2 || f.queuedBehindStream() == 1 })
	release()
	one, two := first(), second()
	if one.code != 200 || two.code != 200 {
		t.Fatalf("duplicate attach: %d %s / %d %s", one.code, one.raw, two.code, two.raw)
	}
	var x, y Call
	json.Unmarshal(one.raw, &x)
	json.Unmarshal(two.raw, &y)
	if x.ID != f.call.ID || y.ID != f.call.ID {
		t.Fatalf("duplicate attach left the registration: %q %q want %q", x.ID, y.ID, f.call.ID)
	}
	if n := f.count(`SELECT count(*) FROM rtms_streams WHERE stream_id='dup-stream'`); n != 1 {
		t.Fatalf("%d stream mappings", n)
	}
	if n := f.count(`SELECT count(*) FROM call_events WHERE call_id=$1 AND kind='stream.started'`, f.call.ID); n != 1 {
		t.Fatalf("%d stream.started events, want 1", n)
	}
}

// Ordinary sequential deliveries keep their meaning with the repair in place:
// attach, replayed attach, identity mismatch, final stop, stop-before-start,
// and a saved pending call that a later, different stream still claims.
func TestStreamLifecycleSequentialDeliveries(t *testing.T) {
	f := newRaceFixture(t)
	attach := func(uuid, stream string) ([]byte, int) {
		return f.post(f.a, "/api/rtms/streams", f.attachBody(uuid, stream))
	}
	// Stop before start: tombstone only; the saved registration is untouched.
	if raw, code := f.post(f.b, "/api/rtms/streams/early/stop", `{"status":"stopped"}`); code != 200 {
		t.Fatalf("early stop: %d %s", code, raw)
	}
	raw, code := attach("runtime-early", "early")
	var closed struct {
		Status string `json:"status"`
	}
	json.Unmarshal(raw, &closed)
	if code != 200 || closed.Status != "stopped" || f.callStatus(f.call.ID) != "scheduled" {
		t.Fatalf("closed stream claimed the pending call: %d %s call=%s", code, raw, f.callStatus(f.call.ID))
	}
	// A different, ordinary stream claims the same saved registration.
	raw, code = attach("runtime-one", "ordinary")
	var live Call
	json.Unmarshal(raw, &live)
	if code != 200 || live.ID != f.call.ID || live.Status != "live" {
		t.Fatalf("ordinary attach: %d %s", code, raw)
	}
	// Replay and duplicate delivery do not change anything.
	for range 2 {
		raw, code = attach("runtime-one", "ordinary")
		var again Call
		json.Unmarshal(raw, &again)
		if code != 200 || again.ID != f.call.ID {
			t.Fatalf("replayed attach: %d %s", code, raw)
		}
	}
	if n := f.count(`SELECT count(*) FROM call_events WHERE call_id=$1 AND kind='stream.started'`, f.call.ID); n != 1 {
		t.Fatalf("replay emitted %d stream.started events", n)
	}
	// Identity checks still hold: the stream cannot move to another runtime.
	if raw, code = attach("other-runtime", "ordinary"); code != 409 {
		t.Fatalf("mismatched runtime on an existing stream: %d %s", code, raw)
	}
	// A final stop closes the call, and the closed stream never reopens it.
	if raw, code = f.post(f.b, "/api/rtms/streams/ordinary/stop", `{"status":"stopped"}`); code != 200 {
		t.Fatalf("stop: %d %s", code, raw)
	}
	raw, code = attach("runtime-one", "ordinary")
	json.Unmarshal(raw, &live)
	if code != 200 || live.Status != "stopped" || f.rtmsStatus("ordinary") != "stopped" {
		t.Fatalf("closed stream reopened: %d %s", code, raw)
	}
}
