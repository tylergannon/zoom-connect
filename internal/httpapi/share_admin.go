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
	"net/mail"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/tylergannon/zoom-connect/internal/store/db"
)

type webhookEntry struct {
	At      time.Time `json:"at"`
	Event   string    `json:"event"`
	Topic   string    `json:"topic"`
	Outcome string    `json:"outcome"`
}

var webhookMu sync.Mutex
var webhookLog []webhookEntry

func recentWebhooks() []webhookEntry {
	webhookMu.Lock()
	defer webhookMu.Unlock()
	return append([]webhookEntry{}, webhookLog...)
}

func addWebhook(e webhookEntry) {
	webhookMu.Lock()
	defer webhookMu.Unlock()
	webhookLog = append([]webhookEntry{e}, webhookLog...)
	if len(webhookLog) > 12 {
		webhookLog = webhookLog[:12]
	}
}

func (a *Server) shares(w http.ResponseWriter, r *http.Request) {
	c, ok := a.authorizedCall(w, r)
	if !ok {
		return
	}
	rows, e := a.s.Q.ListShares(r.Context(), c.ID)
	if e != nil {
		jsonError(w, 500, "database error")
		return
	}
	p := principal(r)
	part, e := a.s.Q.ListParticipants(r.Context(), c.ID)
	if e != nil {
		jsonError(w, 500, "database error")
		return
	}
	participants := make([]Person, 0, len(part))
	for _, x := range part {
		participants = append(participants, Person{ID: x.ParticipantID, Name: x.Name, Email: x.Email.String})
	}
	shares := make([]map[string]any, 0, len(rows))
	for _, x := range rows {
		v := map[string]any{"id": x.ID, "email": x.Email, "granted_by": x.GrantedBy, "granted_by_name": x.GrantedByName, "created_at": x.CreatedAt.Time, "can_remove": x.GrantedBy == p.User.ID}
		if x.Name.Valid {
			v["name"] = x.Name.String
		}
		shares = append(shares, v)
	}
	writeJSON(w, 200, map[string]any{"participants": participants, "shares": shares, "can_share": a.canShareCall(r.Context(), c.ID, p.User.ID)})
}
func (a *Server) addShare(w http.ResponseWriter, r *http.Request) {
	c, ok := a.authorizedCall(w, r)
	if !ok {
		return
	}
	p := principal(r)
	if !a.canShareCall(r.Context(), c.ID, p.User.ID) {
		jsonError(w, 403, "only the call owner or participants can share")
		return
	}
	var in struct {
		Email string `json:"email"`
	}
	if decode(r, &in) != nil {
		jsonError(w, 400, "invalid body")
		return
	}
	addr, e := mail.ParseAddress(strings.TrimSpace(in.Email))
	if e != nil {
		jsonError(w, 400, "enter a valid email address")
		return
	}
	email := strings.ToLower(addr.Address)
	users, e := a.s.Q.SearchPeople(r.Context(), email)
	if e == nil {
		for _, u := range users {
			if strings.EqualFold(u.Email, email) && a.isParticipant(r.Context(), c.ID, u.ID) {
				jsonError(w, 400, "call participants already have access")
				return
			}
		}
	}
	_, e = a.s.Q.CreateShare(r.Context(), db.CreateShareParams{ID: newID(), CallID: c.ID, Lower: email, GrantedBy: p.User.ID})
	if e != nil {
		if e == pgx.ErrNoRows {
			jsonError(w, 409, "already shared")
		} else {
			jsonError(w, 500, "database error")
		}
		return
	}
	writeJSON(w, 201, map[string]any{"email": email, "granted_by": p.User.ID})
}
func (a *Server) removeShare(w http.ResponseWriter, r *http.Request) {
	c, ok := a.authorizedCall(w, r)
	if !ok {
		return
	}
	p := principal(r)
	_ = a.s.Q.DeleteShare(r.Context(), db.DeleteShareParams{ID: r.PathValue("shareId"), CallID: c.ID, GrantedBy: p.User.ID})
	w.WriteHeader(204)
}

func (a *Server) zoomSettings(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !p.User.IsAdmin {
		jsonError(w, 404, "not found")
		return
	}
	writeJSON(w, 200, a.zoomView(r.Context()))
}

func (a *Server) zoomView(ctx context.Context) map[string]any {
	credentials := map[string]any{"client_id": os.Getenv("ZOOM_CLIENT_ID"), "client_secret_suffix": "", "webhook_secret_configured": a.zoomSecret != ""}
	capture := map[string]bool{"transcript": true, "chat": true}
	if secret := os.Getenv("ZOOM_CLIENT_SECRET"); secret != "" {
		credentials["client_secret_suffix"] = lastFour(secret)
	}
	if saved, e := a.s.Q.GetSetting(ctx, "zoom"); e == nil {
		var cfg struct {
			Credentials map[string]any  `json:"credentials"`
			Capture     map[string]bool `json:"capture"`
		}
		_ = json.Unmarshal(saved, &cfg)
		if os.Getenv("ZOOM_CLIENT_ID") == "" && cfg.Credentials["client_id"] != nil {
			credentials["client_id"] = cfg.Credentials["client_id"]
		}
		if os.Getenv("ZOOM_CLIENT_SECRET") == "" && cfg.Credentials["client_secret_suffix"] != nil {
			credentials["client_secret_suffix"] = cfg.Credentials["client_secret_suffix"]
		}
		for k, v := range cfg.Capture {
			capture[k] = v
		}
	}
	webhooks := recentWebhooks()
	var last any
	if len(webhooks) > 0 {
		last = webhooks[0].At
	}
	var live, week int
	_ = a.s.Pool.QueryRow(ctx, `SELECT count(*) FROM calls WHERE status='live'`).Scan(&live)
	_ = a.s.Pool.QueryRow(ctx, `SELECT count(*) FROM calls WHERE started>now()-interval '7 days'`).Scan(&week)
	return map[string]any{"mode": "demo", "connected": false, "webhook_url": env("PUBLIC_URL", "http://127.0.0.1:8080") + "/zoom/webhook", "last_webhook": last, "live_streams": live, "calls_this_week": week, "credentials": credentials, "capture": capture, "webhooks": webhooks}
}
func lastFour(v string) string {
	if len(v) > 4 {
		return v[len(v)-4:]
	}
	return v
}
func (a *Server) saveZoomSettings(w http.ResponseWriter, r *http.Request) {
	if !principal(r).User.IsAdmin {
		jsonError(w, 404, "not found")
		return
	}
	var in struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		Capture      struct {
			Transcript bool `json:"transcript"`
			Chat       bool `json:"chat"`
		} `json:"capture"`
	}
	if decode(r, &in) != nil {
		jsonError(w, 400, "invalid body")
		return
	}
	credentials := map[string]any{}
	capture := map[string]bool{"transcript": true, "chat": true}
	if b, e := a.s.Q.GetSetting(r.Context(), "zoom"); e == nil {
		var saved struct {
			Credentials map[string]any  `json:"credentials"`
			Capture     map[string]bool `json:"capture"`
		}
		_ = json.Unmarshal(b, &saved)
		credentials = saved.Credentials
		if credentials == nil {
			credentials = map[string]any{}
		}
		for k, v := range saved.Capture {
			capture[k] = v
		}
	}
	if v := os.Getenv("ZOOM_CLIENT_ID"); v != "" {
		in.ClientID = v
	}
	if v := os.Getenv("ZOOM_CLIENT_SECRET"); v != "" {
		in.ClientSecret = v
	}
	if in.ClientSecret != "" {
		credentials["client_secret_suffix"] = lastFour(in.ClientSecret)
	}
	if in.ClientID != "" {
		credentials["client_id"] = in.ClientID
	}
	capture["transcript"] = in.Capture.Transcript
	capture["chat"] = in.Capture.Chat
	payload, _ := json.Marshal(map[string]any{"credentials": credentials, "capture": capture})
	_ = a.s.Q.SetSetting(r.Context(), db.SetSettingParams{Key: "zoom", Value: payload})
	writeJSON(w, 200, a.zoomView(r.Context()))
}

func (a *Server) createDemoCall(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !p.User.IsAdmin {
		jsonError(w, 403, "admin only")
		return
	}
	var in struct {
		Topic  string `json:"topic"`
		Source string `json:"source"`
	}
	_ = decode(r, &in)
	if strings.TrimSpace(in.Topic) == "" {
		in.Topic = "Demo meeting"
	}
	id := newID()
	now := time.Now().UTC()
	if e := a.s.Q.CreateCall(r.Context(), db.CreateCallParams{ID: id, Topic: in.Topic, Status: "live", Started: pgtype.Timestamptz{Time: now, Valid: true}, HostUserID: p.User.ID}); e != nil {
		jsonError(w, 500, "database error")
		return
	}
	_ = a.s.Q.AddParticipant(r.Context(), db.AddParticipantParams{CallID: id, ParticipantID: p.User.ID, UserID: pgtype.Text{String: p.User.ID, Valid: true}, Name: p.User.Name, Email: pgtype.Text{String: p.User.Email, Valid: true}})
	if _, err := a.emit(r.Context(), id, "stream.started", nil, map[string]any{"source": in.Source}); err != nil {
		jsonError(w, 500, "database error")
		return
	}
	// The RTMS recorder uses this existing local-demo API to supply real
	// meeting events. Never mix scripted transcript lines into those calls.
	if in.Source != "zoom" {
		go a.simulate(id)
	}
	c, e := a.s.Q.GetCall(r.Context(), id)
	if e != nil {
		jsonError(w, 500, "database error")
		return
	}
	writeJSON(w, 201, a.toCall(r.Context(), c, p))
}
func (a *Server) addDemoEvent(w http.ResponseWriter, r *http.Request) {
	if !principal(r).User.IsAdmin {
		jsonError(w, 403, "admin only")
		return
	}
	c, e := a.s.Q.GetCall(r.Context(), r.PathValue("id"))
	if e != nil {
		jsonError(w, 404, "call not found")
		return
	}
	if c.Status != "live" {
		jsonError(w, 409, "call is not live")
		return
	}
	var in struct {
		Kind        string         `json:"kind"`
		Participant Person         `json:"participant"`
		Data        map[string]any `json:"data"`
	}
	if decode(r, &in) != nil || in.Kind == "" {
		jsonError(w, 400, "invalid event")
		return
	}
	ev, err := a.emit(r.Context(), c.ID, in.Kind, &in.Participant, in.Data)
	if err != nil {
		jsonError(w, 500, "database error")
		return
	}
	writeJSON(w, 201, ev)
}
func (a *Server) stopDemoCall(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !p.User.IsAdmin {
		jsonError(w, 403, "admin only")
		return
	}
	id := r.PathValue("id")
	c, e := a.s.Q.GetCall(r.Context(), id)
	if e != nil {
		jsonError(w, 404, "call not found")
		return
	}
	if _, err := a.emit(r.Context(), id, "stream.stopped", nil, map[string]any{"reason": "demo stop"}); err != nil {
		jsonError(w, 500, "database error")
		return
	}
	c, e = a.s.Q.GetCall(r.Context(), id)
	if e != nil {
		jsonError(w, 500, "database error")
		return
	}
	writeJSON(w, 200, a.toCall(r.Context(), c, p))
}

func (a *Server) announcements(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	fl, ok := w.(http.Flusher)
	if !ok {
		return
	}
	_, _ = fmt.Fprint(w, ": connected\n\n")
	fl.Flush()
	seen := map[string]db.ListCallsRow{}
	send := func(kind string, c db.ListCallsRow) {
		payload, _ := json.Marshal(map[string]any{"id": c.ID, "topic": c.Topic, "status": c.Status, "url": "/calls/" + c.ID})
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, payload)
		fl.Flush()
	}
	poll := func(initial bool) {
		rows, e := a.s.Q.ListCalls(r.Context(), pgtype.Text{String: p.User.ID, Valid: true})
		if e != nil {
			return
		}
		present := map[string]bool{}
		for _, c := range rows {
			present[c.ID] = true
			old, exists := seen[c.ID]
			if initial {
				seen[c.ID] = c
				continue
			} else if !exists {
				if c.Status == "live" {
					send("call.started", c)
				} else if c.Status == "scheduled" {
					send("call.scheduled", c)
				} else if c.SharedBy != "" {
					send("call.shared", c)
				}
			} else if old.Status == "scheduled" && c.Status == "live" {
				send("call.started", c)
			} else if old.Status != c.Status && c.Status == "cancelled" {
				send("call.cancelled", c)
			} else if old.Status != c.Status && !callOpen(c.Status) {
				send("call.ended", c)
			} else if old.SharedBy == "" && c.SharedBy != "" {
				send("call.shared", c)
			}
			seen[c.ID] = c
		}
		for id := range seen {
			if !present[id] {
				delete(seen, id)
			}
		}
	}
	poll(true)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !a.stillValid(r.Context(), r, p) {
				return
			}
			poll(false)
		}
	}
}

func (a *Server) zoomWebhook(w http.ResponseWriter, r *http.Request) {
	body, e := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if e != nil {
		jsonError(w, 400, "invalid body")
		return
	}
	var payload struct {
		Event   string `json:"event"`
		Payload struct {
			PlainToken string `json:"plainToken"`
			Object     struct {
				UUID     string `json:"uuid"`
				Topic    string `json:"topic"`
				HostID   string `json:"host_id"`
				StreamID string `json:"rtms_stream_id"`
			} `json:"object"`
		} `json:"payload"`
	}
	_ = json.Unmarshal(body, &payload)
	now := time.Now().UTC()
	if payload.Event == "endpoint.url_validation" {
		sum := hmac.New(sha256.New, []byte(a.zoomSecret))
		sum.Write([]byte(payload.Payload.PlainToken))
		writeJSON(w, 200, map[string]any{"plainToken": payload.Payload.PlainToken, "encryptedToken": hex.EncodeToString(sum.Sum(nil))})
		addWebhook(webhookEntry{At: now, Event: payload.Event, Outcome: "validation answered"})
		return
	}
	stamp := r.Header.Get("x-zm-request-timestamp")
	sig := r.Header.Get("x-zm-signature")
	unix, e := strconv.ParseInt(stamp, 10, 64)
	if e != nil || time.Since(time.Unix(unix, 0)) > 5*time.Minute || time.Until(time.Unix(unix, 0)) > 5*time.Minute || a.zoomSecret == "" {
		addWebhook(webhookEntry{At: now, Event: payload.Event, Outcome: "rejected"})
		jsonError(w, 401, "invalid signature or timestamp")
		return
	}
	mac := hmac.New(sha256.New, []byte(a.zoomSecret))
	_, _ = fmt.Fprintf(mac, "v0:%s:%s", stamp, body)
	want := "v0=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(sig)) {
		addWebhook(webhookEntry{At: now, Event: payload.Event, Outcome: "bad signature"})
		jsonError(w, 401, "invalid signature")
		return
	}
	if payload.Event == "meeting.rtms_started" {
		addWebhook(webhookEntry{At: now, Event: payload.Event, Topic: payload.Payload.Object.Topic, Outcome: "accepted; live RTMS connection is simulated"})
	} else if payload.Event == "meeting.rtms_stopped" || payload.Event == "meeting.rtms_interrupted" {
		status := "stopped"
		if strings.HasSuffix(payload.Event, "interrupted") {
			status = "interrupted"
		}
		if _, err := a.emit(r.Context(), payload.Payload.Object.UUID, "stream."+status, nil, map[string]any{"reason": "Zoom webhook"}); err != nil {
			jsonError(w, 500, "database error")
			return
		}
		addWebhook(webhookEntry{At: now, Event: payload.Event, Topic: payload.Payload.Object.Topic, Outcome: "accepted"})
	} else {
		addWebhook(webhookEntry{At: now, Event: payload.Event, Outcome: "ignored"})
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

var _ = pgtype.Text{}
