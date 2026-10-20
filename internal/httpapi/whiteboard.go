package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// Prototype: agents draft boards for a call; the call host may publish a
// reviewed draft to Zoom Whiteboard through the documented REST API.
//
// Zoom documents content_objects only as "a free-form JSON map whose schema is
// determined by the whiteboard renderer", with one example: {"type":"rect",
// "x":100,"y":200}. Native requests therefore carry positioned rectangles only.
// Card text, lane headings, card size and links stay in the local preview.
// Nothing here opens a board inside a meeting: no documented API does that.

const defaultWhiteboardAPI = "https://api.zoom.us/v2"

var whiteboardScopes = []string{"whiteboard:write:whiteboard", "whiteboard:write:content", "whiteboard:read:content"}

var whiteboardNotSent = []string{"card text", "lane headings", "card size", "links"}

const whiteboardNotSentReason = `Zoom documents content objects only by the example {"type":"rect","x":100,"y":200}; other renderer fields are undocumented, so they are not sent.`

const whiteboardOpenSteps = "In the Zoom desktop client the host selects Whiteboards → Existing Whiteboards, chooses the board by name, then Open Board."

type boardConcept struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Lanes []string `json:"lanes,omitempty"`
	Use   string   `json:"use"`
}

var boardConcepts = []boardConcept{
	{ID: "decision_board", Name: "Decision board", Lanes: []string{"Decided", "Open questions", "Parking lot"}, Use: "Decisions as they are made, each citing the transcript event that made it."},
	{ID: "action_swimlanes", Name: "Action swimlanes", Use: "One lane per owner; each card is an action item."},
	{ID: "evolving_diagram", Name: "Evolving diagram", Use: "Nodes and links an agent redrafts as the discussion changes."},
}

const (
	boardMargin    = 40
	boardHeader    = 64
	cardWidth      = 240
	cardHeight     = 120
	cardGap        = 24
	maxBoardCards  = 48
	maxBoardLanes  = 6
	maxBoardLinks  = 96
	maxCardText    = 280
	maxBoardTitle  = 120
	maxLaneName    = 60
	diagramColumns = 4
)

type boardCard struct {
	Lane      string `json:"lane,omitempty"`
	Text      string `json:"text"`
	SourceSeq int64  `json:"source_seq,omitempty"`
	X         int    `json:"x"`
	Y         int    `json:"y"`
	W         int    `json:"w"`
	H         int    `json:"h"`
}
type boardLink struct {
	From int `json:"from"`
	To   int `json:"to"`
}
type boardDraft struct {
	Concept string      `json:"concept"`
	Title   string      `json:"title"`
	Lanes   []string    `json:"lanes,omitempty"`
	Cards   []boardCard `json:"cards"`
	Links   []boardLink `json:"links,omitempty"`
	Agent   string      `json:"agent,omitempty"`
}
type cardInput struct {
	Lane      string `json:"lane"`
	Text      string `json:"text"`
	SourceSeq int64  `json:"source_seq"`
}

// layoutBoard validates an agent's draft and assigns the coordinates shared by
// the preview and the native request.
func layoutBoard(concept, title string, cards []cardInput, links []boardLink, lastSeq int64) (boardDraft, error) {
	d := boardDraft{Concept: concept, Title: strings.TrimSpace(title), Links: links}
	var lanes []string
	switch concept {
	case "decision_board":
		lanes = boardConcepts[0].Lanes
	case "action_swimlanes", "evolving_diagram":
	default:
		return d, errors.New("concept must be decision_board, action_swimlanes, or evolving_diagram")
	}
	if d.Title == "" || len([]rune(d.Title)) > maxBoardTitle {
		return d, fmt.Errorf("title must be 1 to %d characters", maxBoardTitle)
	}
	if len(cards) == 0 || len(cards) > maxBoardCards {
		return d, fmt.Errorf("a board needs 1 to %d cards", maxBoardCards)
	}
	if len(links) > 0 && concept != "evolving_diagram" {
		return d, errors.New("links are only supported by evolving_diagram")
	}
	if len(links) > maxBoardLinks {
		return d, fmt.Errorf("a diagram may have at most %d links", maxBoardLinks)
	}
	for _, l := range links {
		if l.From < 0 || l.To < 0 || l.From >= len(cards) || l.To >= len(cards) || l.From == l.To {
			return d, errors.New("links must join two different card indexes")
		}
	}
	rows := map[string]int{}
	for i, in := range cards {
		text := strings.TrimSpace(in.Text)
		if text == "" || len([]rune(text)) > maxCardText {
			return d, fmt.Errorf("card %d text must be 1 to %d characters", i, maxCardText)
		}
		if in.SourceSeq < 0 || in.SourceSeq > lastSeq {
			return d, fmt.Errorf("card %d source_seq must name an event already in this call", i)
		}
		card := boardCard{Text: text, SourceSeq: in.SourceSeq, W: cardWidth, H: cardHeight}
		lane := strings.TrimSpace(in.Lane)
		switch concept {
		case "decision_board":
			if lane == "" {
				lane = lanes[0]
			}
			found := false
			for _, name := range lanes {
				if strings.EqualFold(name, lane) {
					lane, found = name, true
				}
			}
			if !found {
				return d, fmt.Errorf("card %d lane must be one of %s", i, strings.Join(lanes, ", "))
			}
		case "action_swimlanes":
			if lane == "" {
				lane = "Unassigned"
			}
			if len([]rune(lane)) > maxLaneName {
				return d, fmt.Errorf("card %d lane must be at most %d characters", i, maxLaneName)
			}
			if _, seen := rows[lane]; !seen {
				if len(lanes) == maxBoardLanes {
					return d, fmt.Errorf("a board may have at most %d lanes", maxBoardLanes)
				}
				lanes = append(lanes, lane)
			}
		case "evolving_diagram":
			lane = ""
		}
		if concept == "evolving_diagram" {
			card.X = boardMargin + (i%diagramColumns)*(cardWidth+3*cardGap)
			card.Y = boardMargin + (i/diagramColumns)*(cardHeight+3*cardGap)
		} else {
			column := 0
			for j, name := range lanes {
				if name == lane {
					column = j
				}
			}
			card.Lane = lane
			card.X = boardMargin + column*(cardWidth+2*cardGap) + cardGap
			card.Y = boardMargin + boardHeader + rows[lane]*(cardHeight+cardGap)
		}
		rows[lane]++
		d.Cards = append(d.Cards, card)
	}
	d.Lanes = lanes
	return d, nil
}

// nativeObjects uses only the fields shown in Zoom's documented example.
func (d boardDraft) nativeObjects() []map[string]any {
	out := make([]map[string]any, 0, len(d.Cards))
	for _, c := range d.Cards {
		out = append(out, map[string]any{"type": "rect", "x": c.X, "y": c.Y})
	}
	return out
}

type nativeRequest struct {
	Method string `json:"method"`
	URL    string `json:"url"`
	Scope  string `json:"scope"`
	Body   any    `json:"body,omitempty"`
}

func (z *zoomWhiteboards) requests(d boardDraft) []nativeRequest {
	content := z.base + "/whiteboards/{whiteboardId}/content"
	return []nativeRequest{
		{Method: "POST", URL: z.base + "/whiteboards", Scope: whiteboardScopes[0], Body: map[string]string{"name": d.Title}},
		{Method: "POST", URL: content, Scope: whiteboardScopes[1], Body: map[string]any{"content_objects": d.nativeObjects()}},
		{Method: "GET", URL: content, Scope: whiteboardScopes[2]},
	}
}

type zoomWhiteboards struct {
	base, token string
	enabled     bool
	scopes      map[string]bool
	client      *http.Client
	// publishing serializes publishes so one draft never creates two boards;
	// the deployment is a single application instance.
	publishing sync.Mutex
}

func (a *Server) configureWhiteboard() error {
	z := &zoomWhiteboards{base: defaultWhiteboardAPI, token: strings.TrimSpace(os.Getenv("ZOOM_WHITEBOARD_ACCESS_TOKEN")), enabled: os.Getenv("ZOOM_WHITEBOARD_NATIVE") == "enabled", scopes: map[string]bool{}}
	if v := os.Getenv("ZOOM_WHITEBOARD_API_URL"); v != "" {
		u, err := url.Parse(v)
		if err != nil || u.Host == "" || !(u.Scheme == "https" || (u.Scheme == "http" && loopbackHost(u.Hostname()))) {
			return fmt.Errorf("ZOOM_WHITEBOARD_API_URL must be HTTPS, or HTTP on loopback for a local test server")
		}
		z.base = strings.TrimRight(v, "/")
	}
	for _, s := range strings.FieldsFunc(os.Getenv("ZOOM_WHITEBOARD_SCOPES"), func(r rune) bool { return r == ' ' || r == ',' }) {
		z.scopes[s] = true
	}
	a.boards = z
	return nil
}
func loopbackHost(host string) bool {
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}
func (a *Server) whiteboard() *zoomWhiteboards {
	if a.boards != nil {
		return a.boards
	}
	return &zoomWhiteboards{base: defaultWhiteboardAPI, scopes: map[string]bool{}}
}

// missing names every precondition for native requests. Scopes are those the
// operator declares from the token grant; Zoom remains the authority and a
// mismatch surfaces as a 401 or 403 from Zoom.
func (z *zoomWhiteboards) missing() []string {
	out := []string{}
	if !z.enabled {
		out = append(out, "feature gate ZOOM_WHITEBOARD_NATIVE=enabled")
	}
	if z.token == "" {
		out = append(out, "user OAuth access token in ZOOM_WHITEBOARD_ACCESS_TOKEN")
	}
	for _, s := range whiteboardScopes {
		if !z.scopes[s] {
			out = append(out, "granted scope "+s)
		}
	}
	return out
}
func (z *zoomWhiteboards) ready() bool { return len(z.missing()) == 0 }

type zoomError struct {
	Stage      string `json:"stage"`
	Status     int    `json:"status,omitempty"`
	Code       int    `json:"code,omitempty"`
	Message    string `json:"message,omitempty"`
	RetryAfter string `json:"retry_after,omitempty"`
}

func (e *zoomError) Error() string {
	return fmt.Sprintf("Zoom Whiteboard %s failed: %d %d %s", e.Stage, e.Status, e.Code, e.Message)
}

func (z *zoomWhiteboards) do(ctx context.Context, stage, method, path string, body any, want int, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return &zoomError{Stage: stage, Message: "request could not be encoded"}
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, z.base+path, reader)
	if err != nil {
		return &zoomError{Stage: stage, Message: "request could not be built"}
	}
	req.Header.Set("Authorization", "Bearer "+z.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := z.client
	if client == nil {
		// Never follow a redirect with the bearer token.
		client = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	resp, err := client.Do(req)
	if err != nil {
		return &zoomError{Stage: stage, Message: "Zoom Whiteboard unavailable"}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != want {
		e := &zoomError{Stage: stage, Status: resp.StatusCode, RetryAfter: resp.Header.Get("Retry-After")}
		var reply struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &reply) == nil {
			e.Code, e.Message = reply.Code, truncateText(reply.Message, 300)
		}
		return e
	}
	if out != nil && json.Unmarshal(data, out) != nil {
		return &zoomError{Stage: stage, Status: resp.StatusCode, Message: "unexpected Zoom response"}
	}
	return nil
}
func truncateText(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// publish creates a board, saves the rectangles and reads the first page back.
// It returns the created whiteboard ID even when a later step fails.
func (z *zoomWhiteboards) publish(ctx context.Context, d boardDraft) (map[string]any, string, error) {
	var board struct {
		ID   string `json:"whiteboard_id"`
		Name string `json:"whiteboard_name"`
	}
	if err := z.do(ctx, "create", "POST", "/whiteboards", map[string]string{"name": d.Title}, 201, &board); err != nil {
		return nil, "", err
	}
	if board.ID == "" {
		return nil, "", &zoomError{Stage: "create", Status: 201, Message: "Zoom returned no whiteboard_id"}
	}
	path := "/whiteboards/" + url.PathEscape(board.ID) + "/content"
	sent := d.nativeObjects()
	var saved struct {
		PageID  string `json:"page_id"`
		Added   int    `json:"added"`
		Updated int    `json:"updated"`
	}
	if err := z.do(ctx, "content", "POST", path, map[string]any{"content_objects": sent}, 201, &saved); err != nil {
		return nil, board.ID, err
	}
	var read struct {
		PageID  string           `json:"page_id"`
		Objects []map[string]any `json:"content_objects"`
	}
	if err := z.do(ctx, "verify", "GET", path, nil, 200, &read); err != nil {
		return nil, board.ID, err
	}
	return map[string]any{"provider": "zoom_whiteboard", "whiteboard_id": board.ID, "whiteboard_name": board.Name, "page_id": saved.PageID, "added": saved.Added, "updated": saved.Updated, "rects_sent": len(sent), "rects_read_back": matchedRects(sent, read.Objects), "objects_on_first_page": len(read.Objects)}, board.ID, nil
}

// matchedRects counts sent rectangles found again by documented fields only.
func matchedRects(sent, read []map[string]any) int {
	left := map[string]int{}
	for _, o := range read {
		if o["type"] == "rect" {
			left[fmt.Sprint(number(o["x"]), ",", number(o["y"]))]++
		}
	}
	n := 0
	for _, o := range sent {
		k := fmt.Sprint(number(o["x"]), ",", number(o["y"]))
		if left[k] > 0 {
			left[k]--
			n++
		}
	}
	return n
}
func number(v any) float64 {
	switch x := v.(type) {
	case int:
		return float64(x)
	case float64:
		return x
	}
	return -1
}

func (a *Server) whiteboardStatus(w http.ResponseWriter, r *http.Request) {
	c, ok := a.authorizedCall(w, r)
	if !ok {
		return
	}
	p := principal(r)
	z := a.whiteboard()
	latest, err := a.latestBoardEvents(r.Context(), c.ID)
	if err != nil {
		jsonError(w, 500, "database error")
		return
	}
	mode, token := "preview", "missing"
	if z.ready() {
		mode = "native"
	}
	if z.token != "" {
		token = "supplied"
	}
	view := map[string]any{"draft": latest["board.draft"], "published": latest["board.published"], "failed": latest["board.publish_failed"], "opened": latest["board.in_meeting"]}
	if ev := latest["board.draft"]; ev != nil {
		if d, err := draftFromEvent(*ev); err == nil {
			view["native_requests"], view["not_sent"] = z.requests(d), whiteboardNotSent
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{
		"provider":    "zoom_whiteboard",
		"mode":        mode,
		"can_draft":   callOpen(c.Status) && a.canShareCall(r.Context(), c.ID, p.User.ID),
		"can_publish": callOpen(c.Status) && c.HostUserID == p.User.ID && z.ready(),
		"concepts":    boardConcepts,
		"native":      map[string]any{"enabled": z.ready(), "api_base": z.base, "token": token, "required_scopes": whiteboardScopes, "missing": z.missing(), "not_sent_reason": whiteboardNotSentReason},
		"meeting":     map[string]any{"automatic": false, "open": whiteboardOpenSteps, "verification": "Zoom whiteboard.opened webhook with payload.object.meeting_uuid, recorded as board.in_meeting"},
		"latest":      view,
	})
}

func (a *Server) latestBoardEvents(ctx context.Context, id string) (map[string]*Event, error) {
	rows, err := a.s.Pool.Query(ctx, `SELECT DISTINCT ON (kind) seq,ts,recv,kind,participant,data FROM call_events WHERE call_id=$1 AND kind IN ('board.draft','board.published','board.publish_failed','board.in_meeting') ORDER BY kind, seq DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*Event{}
	for rows.Next() {
		var seq, ts, recv int64
		var kind string
		var participant, data []byte
		if err = rows.Scan(&seq, &ts, &recv, &kind, &participant, &data); err != nil {
			return nil, err
		}
		ev, err := decodeEvent(seq, ts, recv, kind, participant, data)
		if err != nil {
			return nil, err
		}
		out[kind] = &ev
	}
	return out, rows.Err()
}
func draftFromEvent(ev Event) (boardDraft, error) {
	var d boardDraft
	b, err := json.Marshal(ev.Data)
	if err == nil {
		err = json.Unmarshal(b, &d)
	}
	return d, err
}

func (a *Server) draftWhiteboard(w http.ResponseWriter, r *http.Request) {
	c, ok := a.authorizedCall(w, r)
	if !ok {
		return
	}
	p := principal(r)
	if !a.canShareCall(r.Context(), c.ID, p.User.ID) {
		jsonError(w, 403, "only the call owner or participants can draft a board")
		return
	}
	if !callOpen(c.Status) {
		jsonError(w, 409, "call has ended")
		return
	}
	var in struct {
		Concept string      `json:"concept"`
		Title   string      `json:"title"`
		Cards   []cardInput `json:"cards"`
		Links   []boardLink `json:"links"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		jsonError(w, 400, "invalid board: "+err.Error())
		return
	}
	d, err := layoutBoard(in.Concept, in.Title, in.Cards, in.Links, c.LastSeq)
	if err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	if p.IsKey {
		d.Agent = p.KeyName
	}
	var data map[string]any
	b, _ := json.Marshal(d)
	_ = json.Unmarshal(b, &data)
	ev, err := a.emit(r.Context(), c.ID, "board.draft", &Person{ID: p.User.ID, Name: p.User.Name, Email: p.User.Email}, data)
	if err != nil {
		jsonError(w, 500, "database error")
		return
	}
	writeJSON(w, 201, map[string]any{"event": ev, "native_requests": a.whiteboard().requests(d), "not_sent": whiteboardNotSent, "not_sent_reason": whiteboardNotSentReason})
}

func (a *Server) publishWhiteboard(w http.ResponseWriter, r *http.Request) {
	c, ok := a.authorizedCall(w, r)
	if !ok {
		return
	}
	p := principal(r)
	if c.HostUserID != p.User.ID {
		jsonError(w, 403, "call host only")
		return
	}
	if !callOpen(c.Status) {
		jsonError(w, 409, "call has ended")
		return
	}
	z := a.whiteboard()
	if !z.ready() {
		writeJSON(w, 503, map[string]any{"error": "native Zoom Whiteboard publishing is disabled", "missing": z.missing()})
		return
	}
	var in struct {
		DraftSeq int64 `json:"draft_seq"`
	}
	if decode(r, &in) != nil || in.DraftSeq <= 0 {
		jsonError(w, 400, "draft_seq of the reviewed draft is required")
		return
	}
	var raw []byte
	err := a.s.Pool.QueryRow(r.Context(), `SELECT data FROM call_events WHERE call_id=$1 AND seq=$2 AND kind='board.draft'`, c.ID, in.DraftSeq).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		jsonError(w, 404, "draft not found")
		return
	}
	if err != nil {
		jsonError(w, 500, "database error")
		return
	}
	var d boardDraft
	if json.Unmarshal(raw, &d) != nil || len(d.Cards) == 0 {
		jsonError(w, 500, "invalid draft")
		return
	}
	z.publishing.Lock()
	defer z.publishing.Unlock()
	var published bool
	if err = a.s.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM call_events WHERE call_id=$1 AND kind='board.published' AND data->>'draft_seq'=$2)`, c.ID, strconv.FormatInt(in.DraftSeq, 10)).Scan(&published); err != nil {
		jsonError(w, 500, "database error")
		return
	}
	if published {
		jsonError(w, 409, "draft already published")
		return
	}
	who := &Person{ID: p.User.ID, Name: p.User.Name, Email: p.User.Email}
	result, boardID, err := z.publish(r.Context(), d)
	if err != nil {
		var zerr *zoomError
		if !errors.As(err, &zerr) {
			zerr = &zoomError{Stage: "publish", Message: "Zoom Whiteboard unavailable"}
		}
		body := map[string]any{"error": "Zoom Whiteboard request failed", "zoom": zerr}
		if boardID != "" {
			// A board now exists in Zoom; record it so the host can find or remove it.
			body["whiteboard_id"] = boardID
			if _, e := a.emit(context.WithoutCancel(r.Context()), c.ID, "board.publish_failed", who, map[string]any{"draft_seq": in.DraftSeq, "provider": "zoom_whiteboard", "whiteboard_id": boardID, "zoom": zerr}); e != nil {
				jsonError(w, 500, "database error")
				return
			}
		}
		writeJSON(w, 502, body)
		return
	}
	result["draft_seq"] = in.DraftSeq
	ev, err := a.emit(context.WithoutCancel(r.Context()), c.ID, "board.published", who, result)
	if err != nil {
		jsonError(w, 500, "database error")
		return
	}
	writeJSON(w, 201, map[string]any{"event": ev, "meeting": map[string]any{"automatic": false, "open": whiteboardOpenSteps}})
}

// boardOpened records Zoom's report that a board this app published was opened
// inside a meeting. The webhook is the only documented in-meeting signal.
func (a *Server) boardOpened(ctx context.Context, body []byte) (string, error) {
	var hook struct {
		Payload struct {
			OperatorID string `json:"operator_id"`
			Object     struct {
				WhiteboardID   string `json:"whiteboard_id"`
				WhiteboardName string `json:"whiteboard_name"`
				MeetingID      any    `json:"meeting_id"`
				MeetingUUID    string `json:"meeting_uuid"`
			} `json:"object"`
		} `json:"payload"`
	}
	if json.Unmarshal(body, &hook) != nil || hook.Payload.Object.WhiteboardID == "" {
		return "ignored: no whiteboard", nil
	}
	o := hook.Payload.Object
	if o.MeetingUUID == "" {
		return "ignored: opened outside a meeting", nil
	}
	var callID string
	err := a.s.Pool.QueryRow(ctx, `SELECT call_id FROM call_events WHERE kind='board.published' AND data->>'whiteboard_id'=$1 LIMIT 1`, o.WhiteboardID).Scan(&callID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "ignored: board was not published by Zoom Connect", nil
	}
	if err != nil {
		return "", err
	}
	c, err := a.s.Q.GetCall(ctx, callID)
	if err != nil {
		return "", err
	}
	meetingID := ""
	switch v := o.MeetingID.(type) {
	case string:
		meetingID = v
	case float64:
		meetingID = strconv.FormatInt(int64(v), 10)
	}
	data := map[string]any{"provider": "zoom_whiteboard", "whiteboard_id": o.WhiteboardID, "whiteboard_name": o.WhiteboardName, "meeting_uuid": o.MeetingUUID, "meeting_id": meetingID, "opened_by": hook.Payload.OperatorID}
	if c.ZoomMeetingUuid.Valid {
		data["same_meeting"] = c.ZoomMeetingUuid.String == o.MeetingUUID
	} else if c.ZoomMeetingID.Valid && meetingID != "" {
		data["same_meeting"] = c.ZoomMeetingID.String == meetingID
	}
	if _, err = a.emit(ctx, callID, "board.in_meeting", nil, data); err != nil {
		return "", err
	}
	return "accepted; board opened in a meeting", nil
}
