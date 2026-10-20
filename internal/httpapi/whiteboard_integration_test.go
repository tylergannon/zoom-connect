package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/tylergannon/zoom-connect/internal/store/db"
)

const fakeZoomToken = "fake-user-token-not-a-secret"

type fakeZoomCall struct {
	Method, Path, Auth, ContentType string
	Body                            map[string]any
}

// fakeZoom stands in for https://api.zoom.us/v2 with the documented status
// codes and response fields. mode "ok" accepts everything; "content403" and
// "create401" reject the named step the way Zoom documents.
func fakeZoom(t *testing.T, mode string) (*httptest.Server, func() []fakeZoomCall) {
	t.Helper()
	var mu sync.Mutex
	var calls []fakeZoomCall
	var saved []any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, fakeZoomCall{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), body})
		w.Header().Set("Content-Type", "application/json")
		reply := func(status int, v any) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(v)
		}
		switch {
		case r.Method == "POST" && r.URL.Path == "/v2/whiteboards" && mode == "create401":
			reply(401, map[string]any{"code": 124, "message": "fake: invalid access token"})
		case r.Method == "POST" && r.URL.Path == "/v2/whiteboards":
			reply(201, map[string]any{"whiteboard_id": "wb_fake_1", "whiteboard_name": body["name"], "created_at": "2026-10-20T00:00:00Z"})
		case r.Method == "POST" && r.URL.Path == "/v2/whiteboards/wb_fake_1/content" && mode == "content403":
			reply(403, map[string]any{"code": 111804, "message": "fake: no permission to edit this whiteboard"})
		case r.Method == "POST" && r.URL.Path == "/v2/whiteboards/wb_fake_1/content":
			objects, _ := body["content_objects"].([]any)
			for i, o := range objects {
				// A renderer may add its own fields; read-back must not depend on them.
				m := map[string]any{"id": fmt.Sprintf("fake-object-%d", i)}
				for k, v := range asMap(o) {
					m[k] = v
				}
				saved = append(saved, m)
			}
			reply(201, map[string]any{"page_id": "page_fake_1", "added": len(objects), "updated": 0})
		case r.Method == "GET" && r.URL.Path == "/v2/whiteboards/wb_fake_1/content":
			reply(200, map[string]any{"whiteboard_id": "wb_fake_1", "page_id": "page_fake_1", "content_objects": saved})
		default:
			reply(404, map[string]any{"code": 404, "message": "fake: unexpected request"})
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []fakeZoomCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]fakeZoomCall(nil), calls...)
	}
}

func nativeBoards(base string) *zoomWhiteboards {
	scopes := map[string]bool{}
	for _, s := range whiteboardScopes {
		scopes[s] = true
	}
	return &zoomWhiteboards{base: base + "/v2", token: fakeZoomToken, enabled: true, scopes: scopes}
}

func boardRequest(t *testing.T, ts *httptest.Server, method, url string, cookie *http.Cookie, apiKey, body string) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(raw), fakeZoomToken) {
		t.Fatalf("%s %s leaked the Zoom token: %s", method, url, raw)
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func demoLogin(t *testing.T, ts *httptest.Server, email string) *http.Cookie {
	t.Helper()
	resp := request(t, ts.Client(), "POST", ts.URL+"/api/demo/login", nil, strings.NewReader(`{"email":"`+email+`"}`))
	defer resp.Body.Close()
	for _, c := range resp.Cookies() {
		if c.Name == "zc_session" {
			return c
		}
	}
	t.Fatalf("login %s: %s", email, resp.Status)
	return nil
}

func eventKinds(t *testing.T, a *Server, callID, kind string) []map[string]any {
	t.Helper()
	rows, err := a.s.Pool.Query(context.Background(), `SELECT data FROM call_events WHERE call_id=$1 AND kind=$2 ORDER BY seq`, callID, kind)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), fakeZoomToken) {
			t.Fatalf("%s event stored the Zoom token", kind)
		}
		m := map[string]any{}
		_ = json.Unmarshal(raw, &m)
		out = append(out, m)
	}
	return out
}

func signedWebhook(t *testing.T, ts *httptest.Server, secret, body string, sign bool) int {
	t.Helper()
	req, _ := http.NewRequest("POST", ts.URL+"/zoom/webhook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if sign {
		stamp := strconv.FormatInt(time.Now().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = fmt.Fprintf(mac, "v0:%s:%s", stamp, body)
		req.Header.Set("x-zm-request-timestamp", stamp)
		req.Header.Set("x-zm-signature", "v0="+hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func asList(v any) []any         { l, _ := v.([]any); return l }
func asNumber(v any) int64       { f, _ := v.(float64); return int64(f) }
func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

const decisionDraft = `{"concept":"decision_board","title":"Launch decisions","cards":[
 {"lane":"Decided","text":"Ship the beta on Friday","source_seq":1},
 {"lane":"open questions","text":"Who owns the migration runbook?"},
 {"text":"Pricing page copy is final"}]}`

func TestWhiteboardDraftAccessAndPreview(t *testing.T) {
	dsn := testDatabase(t)
	a, ts := eventTestServer(t, dsn)
	dana := eventLogin(t, ts)
	call := eventTestCall(t, ts, dana)
	board := ts.URL + "/api/calls/" + call.ID + "/whiteboard"
	agentBoard := ts.URL + "/api/v1/calls/" + call.ID + "/whiteboard"

	if code, _ := boardRequest(t, ts, "GET", board, nil, "", ""); code != 401 {
		t.Fatalf("anonymous status: got %d, want 401", code)
	}
	code, made := boardRequest(t, ts, "POST", ts.URL+"/api/keys", dana, "", `{"name":"board-agent"}`)
	secret, _ := made["secret"].(string)
	if code != 201 || secret == "" {
		t.Fatalf("create API key: %d", code)
	}

	// An existing listening agent drafts with its API key on the agent route.
	code, out := boardRequest(t, ts, "POST", agentBoard, nil, secret, decisionDraft)
	if code != 201 {
		t.Fatalf("agent draft: got %d %v", code, out)
	}
	ev := asMap(out["event"])
	data := asMap(ev["data"])
	draftSeq := asNumber(ev["seq"])
	cards := asList(data["cards"])
	if ev["kind"] != "board.draft" || data["agent"] != "board-agent" || len(cards) != 3 {
		t.Fatalf("draft event: %v", ev)
	}
	if lane := asMap(cards[1])["lane"]; lane != "Open questions" {
		t.Fatalf("lane names must normalize to the concept's lanes, got %v", lane)
	}
	if lane := asMap(cards[2])["lane"]; lane != "Decided" {
		t.Fatalf("decision cards default to Decided, got %v", lane)
	}
	reqs := asList(out["native_requests"])
	if len(reqs) != 3 {
		t.Fatalf("native requests: %v", reqs)
	}
	create, content, verify := asMap(reqs[0]), asMap(reqs[1]), asMap(reqs[2])
	if create["method"] != "POST" || create["url"] != "https://api.zoom.us/v2/whiteboards" || create["scope"] != "whiteboard:write:whiteboard" || !reflect.DeepEqual(create["body"], map[string]any{"name": "Launch decisions"}) {
		t.Fatalf("create request: %v", create)
	}
	if content["url"] != "https://api.zoom.us/v2/whiteboards/{whiteboardId}/content" || content["scope"] != "whiteboard:write:content" || verify["method"] != "GET" || verify["scope"] != "whiteboard:read:content" {
		t.Fatalf("content requests: %v %v", content, verify)
	}
	objects := asList(asMap(content["body"])["content_objects"])
	wantFirst := map[string]any{"type": "rect", "x": float64(64), "y": float64(104)}
	if len(objects) != 3 || !reflect.DeepEqual(objects[0], wantFirst) {
		t.Fatalf("content objects must carry only documented fields: %v", objects)
	}
	rawReqs, _ := json.Marshal(reqs)
	if strings.Contains(string(rawReqs), "Ship the beta") || strings.Contains(string(rawReqs), "Decided") {
		t.Fatalf("native requests must not carry undocumented text fields: %s", rawReqs)
	}
	if notSent := fmt.Sprint(out["not_sent"]); !strings.Contains(notSent, "card text") {
		t.Fatalf("draft response must say what is not sent: %v", out["not_sent"])
	}

	// The draft is an ordinary call event, so listening agents see it.
	resp := request(t, ts.Client(), "GET", ts.URL+"/api/calls/"+call.ID+"/events", dana, nil)
	var events []Event
	_ = json.NewDecoder(resp.Body).Decode(&events)
	resp.Body.Close()
	if len(events) == 0 || events[len(events)-1].Kind != "board.draft" || events[len(events)-1].Seq != draftSeq {
		t.Fatalf("draft missing from call events: %v", events)
	}

	code, status := boardRequest(t, ts, "GET", board, dana, "", "")
	native := asMap(status["native"])
	missing := fmt.Sprint(native["missing"])
	if code != 200 || status["mode"] != "preview" || status["can_draft"] != true || status["can_publish"] != false || native["token"] != "missing" {
		t.Fatalf("preview status: %d %v", code, status)
	}
	for _, want := range []string{"ZOOM_WHITEBOARD_NATIVE=enabled", "ZOOM_WHITEBOARD_ACCESS_TOKEN", "whiteboard:write:whiteboard", "whiteboard:write:content", "whiteboard:read:content"} {
		if !strings.Contains(missing, want) {
			t.Fatalf("status must name missing %q: %s", want, missing)
		}
	}
	if latest := asMap(status["latest"]); asNumber(asMap(latest["draft"])["seq"]) != draftSeq || len(asList(latest["native_requests"])) != 3 {
		t.Fatalf("status latest draft: %v", latest)
	}
	if meeting := asMap(status["meeting"]); meeting["automatic"] != false || !strings.Contains(fmt.Sprint(meeting["open"]), "Existing Whiteboards") {
		t.Fatalf("status must say in-meeting opening is manual: %v", meeting)
	}

	for name, body := range map[string]string{
		"unknown concept": `{"concept":"miro","title":"x","cards":[{"text":"a"}]}`,
		"unknown field":   `{"concept":"decision_board","title":"x","cards":[{"text":"a"}],"color":"red"}`,
		"empty title":     `{"concept":"decision_board","title":" ","cards":[{"text":"a"}]}`,
		"no cards":        `{"concept":"decision_board","title":"x","cards":[]}`,
		"empty text":      `{"concept":"decision_board","title":"x","cards":[{"text":"  "}]}`,
		"unknown lane":    `{"concept":"decision_board","title":"x","cards":[{"lane":"Later","text":"a"}]}`,
		"future source":   `{"concept":"decision_board","title":"x","cards":[{"text":"a","source_seq":999999}]}`,
		"links on lanes":  `{"concept":"decision_board","title":"x","cards":[{"text":"a"},{"text":"b"}],"links":[{"from":0,"to":1}]}`,
		"self link":       `{"concept":"evolving_diagram","title":"x","cards":[{"text":"a"}],"links":[{"from":0,"to":0}]}`,
		"too many lanes":  `{"concept":"action_swimlanes","title":"x","cards":[{"lane":"a","text":"1"},{"lane":"b","text":"2"},{"lane":"c","text":"3"},{"lane":"d","text":"4"},{"lane":"e","text":"5"},{"lane":"f","text":"6"},{"lane":"g","text":"7"}]}`,
		"too many cards":  `{"concept":"evolving_diagram","title":"x","cards":[` + strings.Repeat(`{"text":"a"},`, maxBoardCards) + `{"text":"a"}]}`,
	} {
		if code, out := boardRequest(t, ts, "POST", board, dana, "", body); code != 400 {
			t.Fatalf("%s: got %d %v, want 400", name, code, out)
		}
	}

	// Call ACL: no access, share-only viewer, and a non-host participant.
	jordan := demoLogin(t, ts, "jordan@acme.co")
	if code, out := boardRequest(t, ts, "GET", board, jordan, "", ""); code != 403 || out["error"] != "forbidden" {
		t.Fatalf("unshared status: got %d %v", code, out)
	}
	if code, _ := boardRequest(t, ts, "POST", board, jordan, "", decisionDraft); code != 403 {
		t.Fatalf("unshared draft: got %d", code)
	}
	if code, _ := boardRequest(t, ts, "POST", ts.URL+"/api/calls/"+call.ID+"/shares", dana, "", `{"email":"priya@acme.co"}`); code != 201 {
		t.Fatalf("share with priya: %d", code)
	}
	priya := demoLogin(t, ts, "priya@acme.co")
	if code, out := boardRequest(t, ts, "GET", board, priya, "", ""); code != 200 || out["can_draft"] != false || out["can_publish"] != false {
		t.Fatalf("share-only status: got %d %v", code, out)
	}
	if code, _ := boardRequest(t, ts, "POST", board, priya, "", decisionDraft); code != 403 {
		t.Fatalf("share-only draft: got %d, want 403", code)
	}
	marcus, err := a.s.Q.GetUserByEmail(context.Background(), "marcus@acme.co")
	if err != nil {
		t.Fatal(err)
	}
	if err = a.s.Q.AddParticipant(context.Background(), db.AddParticipantParams{CallID: call.ID, ParticipantID: marcus.ID, UserID: pgtype.Text{String: marcus.ID, Valid: true}, Name: marcus.Name, Email: pgtype.Text{String: marcus.Email, Valid: true}}); err != nil {
		t.Fatal(err)
	}
	marcusCookie := demoLogin(t, ts, "marcus@acme.co")
	if code, _ := boardRequest(t, ts, "POST", board, marcusCookie, "", decisionDraft); code != 201 {
		t.Fatalf("participant draft: got %d, want 201", code)
	}
	publish := board + "/publish"
	body := fmt.Sprintf(`{"draft_seq":%d}`, draftSeq)
	if code, _ := boardRequest(t, ts, "POST", publish, marcusCookie, "", body); code != 403 {
		t.Fatalf("non-host publish: got %d, want 403", code)
	}
	if code, _ := boardRequest(t, ts, "POST", publish, priya, "", body); code != 403 {
		t.Fatalf("share-only publish: got %d, want 403", code)
	}

	// Native publishing is off by default; nothing is sent anywhere.
	code, out = boardRequest(t, ts, "POST", publish, dana, "", body)
	if code != 503 || !strings.Contains(fmt.Sprint(out["missing"]), "whiteboard:write:content") {
		t.Fatalf("disabled publish: got %d %v, want 503 naming missing scopes", code, out)
	}
	if got := eventKinds(t, a, call.ID, "board.published"); len(got) != 0 {
		t.Fatalf("disabled publish recorded %v", got)
	}

	// With native publishing off, a signed whiteboard.opened webhook is only logged.
	a.zoomSecret = "fake-webhook-secret"
	hook := `{"event":"whiteboard.opened","payload":{"account_id":"fake","operator_id":"fake","object":{"whiteboard_id":"wb_fake_1","meeting_uuid":"fake-meeting-uuid"}}}`
	if code := signedWebhook(t, ts, a.zoomSecret, hook, true); code != 200 {
		t.Fatalf("webhook: %d", code)
	}
	if got := eventKinds(t, a, call.ID, "board.in_meeting"); len(got) != 0 {
		t.Fatalf("disabled webhook recorded %v", got)
	}

	if code, _ := boardRequest(t, ts, "POST", ts.URL+"/api/demo/calls/"+call.ID+"/stop", dana, "", "{}"); code != 200 {
		t.Fatalf("stop call: %d", code)
	}
	if code, _ := boardRequest(t, ts, "POST", board, dana, "", decisionDraft); code != 409 {
		t.Fatalf("draft on ended call: got %d, want 409", code)
	}
}

func TestWhiteboardPublishAgainstFakeZoom(t *testing.T) {
	dsn := testDatabase(t)
	a, ts := eventTestServer(t, dsn)
	fake, calls := fakeZoom(t, "ok")
	a.boards = nativeBoards(fake.URL)
	a.zoomSecret = "fake-webhook-secret"
	dana := eventLogin(t, ts)
	call := eventTestCall(t, ts, dana)
	board := ts.URL + "/api/calls/" + call.ID + "/whiteboard"

	code, out := boardRequest(t, ts, "POST", board, dana, "", `{"concept":"action_swimlanes","title":"Follow-ups","cards":[
	 {"lane":"Dana","text":"Send the renewal quote"},{"lane":"Marcus","text":"Draft the runbook"},{"lane":"Dana","text":"Book the design review"}]}`)
	if code != 201 {
		t.Fatalf("draft: %d %v", code, out)
	}
	draftSeq := asNumber(asMap(out["event"])["seq"])
	if url := asMap(asList(out["native_requests"])[0])["url"]; url != fake.URL+"/v2/whiteboards" {
		t.Fatalf("native request must target the configured API base: %v", url)
	}
	code, status := boardRequest(t, ts, "GET", board, dana, "", "")
	native := asMap(status["native"])
	if code != 200 || status["mode"] != "native" || status["can_publish"] != true || native["token"] != "supplied" || len(asList(native["missing"])) != 0 {
		t.Fatalf("native status: %d %v", code, status)
	}
	publish := board + "/publish"
	if code, _ := boardRequest(t, ts, "POST", publish, dana, "", `{}`); code != 400 {
		t.Fatalf("publish without draft_seq: %d", code)
	}
	if code, _ := boardRequest(t, ts, "POST", publish, dana, "", `{"draft_seq":1}`); code != 404 {
		t.Fatalf("publish of a non-draft event: %d", code)
	}
	if n := len(calls()); n != 0 {
		t.Fatalf("Zoom was called %d times before a valid publish", n)
	}

	code, out = boardRequest(t, ts, "POST", publish, dana, "", fmt.Sprintf(`{"draft_seq":%d}`, draftSeq))
	if code != 201 {
		t.Fatalf("publish: %d %v", code, out)
	}
	ev := asMap(out["event"])
	data := asMap(ev["data"])
	if ev["kind"] != "board.published" || data["whiteboard_id"] != "wb_fake_1" || data["page_id"] != "page_fake_1" || asNumber(data["added"]) != 3 || asNumber(data["rects_sent"]) != 3 || asNumber(data["rects_read_back"]) != 3 || asNumber(data["draft_seq"]) != draftSeq {
		t.Fatalf("published event: %v", ev)
	}
	if asMap(out["meeting"])["automatic"] != false {
		t.Fatalf("publish must say the board is not opened in the meeting: %v", out["meeting"])
	}

	got := calls()
	if len(got) != 3 {
		t.Fatalf("want create, content and read-back requests, got %d: %v", len(got), got)
	}
	for _, c := range got {
		if c.Auth != "Bearer "+fakeZoomToken {
			t.Fatalf("%s %s sent Authorization %q", c.Method, c.Path, c.Auth)
		}
	}
	if got[0].Method != "POST" || got[0].Path != "/v2/whiteboards" || got[0].ContentType != "application/json" || !reflect.DeepEqual(got[0].Body, map[string]any{"name": "Follow-ups"}) {
		t.Fatalf("create request: %+v", got[0])
	}
	if got[1].Method != "POST" || got[1].Path != "/v2/whiteboards/wb_fake_1/content" || !reflect.DeepEqual(keysOf(got[1].Body), []string{"content_objects"}) {
		t.Fatalf("content request: %+v", got[1])
	}
	sent := asList(got[1].Body["content_objects"])
	wantObjects := []any{
		map[string]any{"type": "rect", "x": float64(64), "y": float64(104)},
		map[string]any{"type": "rect", "x": float64(352), "y": float64(104)},
		map[string]any{"type": "rect", "x": float64(64), "y": float64(248)},
	}
	if !reflect.DeepEqual(sent, wantObjects) {
		t.Fatalf("content objects: got %v, want %v", sent, wantObjects)
	}
	if got[2].Method != "GET" || got[2].Path != "/v2/whiteboards/wb_fake_1/content" || got[2].Body != nil {
		t.Fatalf("read-back request: %+v", got[2])
	}

	if code, _ := boardRequest(t, ts, "POST", publish, dana, "", fmt.Sprintf(`{"draft_seq":%d}`, draftSeq)); code != 409 {
		t.Fatalf("republish: got %d, want 409", code)
	}
	if n := len(calls()); n != 3 {
		t.Fatalf("republish reached Zoom: %d requests", n)
	}

	// whiteboard.opened is the only documented in-meeting signal.
	opened := `{"event":"whiteboard.opened","event_ts":1760990000000,"payload":{"account_id":"fake","operator_id":"fake-operator","object":{"whiteboard_id":"wb_fake_1","whiteboard_name":"Follow-ups","meeting_id":85012345678,"meeting_uuid":"fake-meeting-uuid=="}}}`
	if code := signedWebhook(t, ts, a.zoomSecret, opened, false); code != 401 {
		t.Fatalf("unsigned webhook: got %d, want 401", code)
	}
	if code := signedWebhook(t, ts, a.zoomSecret, strings.Replace(opened, `,"meeting_id":85012345678,"meeting_uuid":"fake-meeting-uuid=="`, "", 1), true); code != 200 {
		t.Fatalf("outside-meeting webhook: %d", code)
	}
	if code := signedWebhook(t, ts, a.zoomSecret, strings.Replace(opened, "wb_fake_1", "wb_someone_else", 1), true); code != 200 {
		t.Fatalf("unknown-board webhook: %d", code)
	}
	if got := eventKinds(t, a, call.ID, "board.in_meeting"); len(got) != 0 {
		t.Fatalf("unsigned, outside-meeting or unknown-board webhooks recorded %v", got)
	}
	if code := signedWebhook(t, ts, a.zoomSecret, opened, true); code != 200 {
		t.Fatalf("signed webhook: %d", code)
	}
	inMeeting := eventKinds(t, a, call.ID, "board.in_meeting")
	if len(inMeeting) != 1 || inMeeting[0]["whiteboard_id"] != "wb_fake_1" || inMeeting[0]["meeting_uuid"] != "fake-meeting-uuid==" || inMeeting[0]["meeting_id"] != "85012345678" {
		t.Fatalf("in-meeting event: %v", inMeeting)
	}
	code, status = boardRequest(t, ts, "GET", board, dana, "", "")
	latest := asMap(status["latest"])
	if code != 200 || latest["published"] == nil || latest["opened"] == nil {
		t.Fatalf("status after publish and open: %v", latest)
	}
}

func TestWhiteboardPublishFailures(t *testing.T) {
	dsn := testDatabase(t)
	a, ts := eventTestServer(t, dsn)
	dana := eventLogin(t, ts)
	call := eventTestCall(t, ts, dana)
	board := ts.URL + "/api/calls/" + call.ID + "/whiteboard"
	draft := func() int64 {
		code, out := boardRequest(t, ts, "POST", board, dana, "", decisionDraft)
		if code != 201 {
			t.Fatalf("draft: %d", code)
		}
		return asNumber(asMap(out["event"])["seq"])
	}

	fake, calls := fakeZoom(t, "create401")
	a.boards = nativeBoards(fake.URL)
	code, out := boardRequest(t, ts, "POST", board+"/publish", dana, "", fmt.Sprintf(`{"draft_seq":%d}`, draft()))
	zerr := asMap(out["zoom"])
	if code != 502 || zerr["stage"] != "create" || asNumber(zerr["status"]) != 401 || asNumber(zerr["code"]) != 124 || out["whiteboard_id"] != nil || len(calls()) != 1 {
		t.Fatalf("create failure: %d %v", code, out)
	}
	if got := eventKinds(t, a, call.ID, "board.publish_failed"); len(got) != 0 {
		t.Fatalf("no board exists after a failed create, got %v", got)
	}

	fake, calls = fakeZoom(t, "content403")
	a.boards = nativeBoards(fake.URL)
	seq := draft()
	code, out = boardRequest(t, ts, "POST", board+"/publish", dana, "", fmt.Sprintf(`{"draft_seq":%d}`, seq))
	zerr = asMap(out["zoom"])
	if code != 502 || zerr["stage"] != "content" || asNumber(zerr["status"]) != 403 || asNumber(zerr["code"]) != 111804 || out["whiteboard_id"] != "wb_fake_1" || len(calls()) != 2 {
		t.Fatalf("content failure: %d %v", code, out)
	}
	failed := eventKinds(t, a, call.ID, "board.publish_failed")
	if len(failed) != 1 || failed[0]["whiteboard_id"] != "wb_fake_1" || asNumber(failed[0]["draft_seq"]) != seq {
		t.Fatalf("a created board must be recorded after a later failure: %v", failed)
	}
	if got := eventKinds(t, a, call.ID, "board.published"); len(got) != 0 {
		t.Fatalf("failed publish recorded success: %v", got)
	}
}

func TestLayoutBoardCoordinates(t *testing.T) {
	d, err := layoutBoard("decision_board", " Decisions ", []cardInput{{Text: "a"}, {Lane: "parking lot", Text: "b"}, {Text: "c"}}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := [][2]int{}
	for _, c := range d.Cards {
		got = append(got, [2]int{c.X, c.Y})
	}
	if d.Title != "Decisions" || !reflect.DeepEqual(got, [][2]int{{64, 104}, {640, 104}, {64, 248}}) || d.Cards[1].Lane != "Parking lot" {
		t.Fatalf("decision layout: %+v", d)
	}
	d, err = layoutBoard("evolving_diagram", "Flow", []cardInput{{Text: "1"}, {Text: "2"}, {Text: "3"}, {Text: "4"}, {Text: "5"}}, []boardLink{{From: 0, To: 4}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if c := d.Cards[4]; c.X != 40 || c.Y != 232 || c.Lane != "" || len(d.Lanes) != 0 {
		t.Fatalf("diagram layout: %+v", d)
	}
	if _, err = layoutBoard("evolving_diagram", "Flow", []cardInput{{Text: "1"}}, []boardLink{{From: 0, To: 1}}, 0); err == nil {
		t.Fatal("a link to a missing card must fail")
	}
	objects := d.nativeObjects()
	for _, o := range objects {
		if !reflect.DeepEqual(keysOf(o), []string{"type", "x", "y"}) {
			t.Fatalf("native object has undocumented fields: %v", o)
		}
	}
	read := []map[string]any{{"type": "rect", "x": float64(40), "y": float64(40), "id": "z"}, {"type": "text", "x": float64(352), "y": float64(40)}}
	if n := matchedRects(objects, read); n != 1 {
		t.Fatalf("matchedRects: got %d, want 1", n)
	}
}

func TestConfigureWhiteboard(t *testing.T) {
	for _, k := range []string{"ZOOM_WHITEBOARD_NATIVE", "ZOOM_WHITEBOARD_ACCESS_TOKEN", "ZOOM_WHITEBOARD_SCOPES", "ZOOM_WHITEBOARD_API_URL"} {
		t.Setenv(k, "")
	}
	a := &Server{}
	if err := a.configureWhiteboard(); err != nil {
		t.Fatal(err)
	}
	if a.boards.base != defaultWhiteboardAPI || a.boards.ready() || len(a.boards.missing()) != 5 {
		t.Fatalf("default must be disabled with every precondition missing: %+v %v", a.boards, a.boards.missing())
	}
	for _, bad := range []string{"http://api.zoom.us/v2", "ftp://127.0.0.1/v2", "not a url"} {
		t.Setenv("ZOOM_WHITEBOARD_API_URL", bad)
		if err := a.configureWhiteboard(); err == nil {
			t.Fatalf("%q must be rejected", bad)
		}
	}
	t.Setenv("ZOOM_WHITEBOARD_API_URL", "http://127.0.0.1:9/v2/")
	t.Setenv("ZOOM_WHITEBOARD_NATIVE", "enabled")
	t.Setenv("ZOOM_WHITEBOARD_ACCESS_TOKEN", fakeZoomToken)
	t.Setenv("ZOOM_WHITEBOARD_SCOPES", "whiteboard:write:whiteboard,whiteboard:write:content")
	if err := a.configureWhiteboard(); err != nil {
		t.Fatal(err)
	}
	if a.boards.base != "http://127.0.0.1:9/v2" || a.boards.ready() || !reflect.DeepEqual(a.boards.missing(), []string{"granted scope whiteboard:read:content"}) {
		t.Fatalf("partial grant must stay disabled: %v", a.boards.missing())
	}
	t.Setenv("ZOOM_WHITEBOARD_SCOPES", "whiteboard:write:whiteboard whiteboard:write:content whiteboard:read:content")
	if err := a.configureWhiteboard(); err != nil || !a.boards.ready() {
		t.Fatalf("full grant must enable native publishing: %v %v", err, a.boards.missing())
	}
}
