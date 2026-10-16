package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oklog/ulid/v2"
	"github.com/tylergannon/zoom-connect/internal/store"
	"github.com/tylergannon/zoom-connect/internal/store/db"
)

type Server struct {
	s              *store.Store
	h              *hub
	domain         string
	zoomSecret     string
	login          *passwordLogin
	recorder       http.Handler
	recorderHealth string
}

func New(next http.Handler) (http.Handler, error) {
	dsn := env("DATABASE_URL", "postgres://zoom:zoom@127.0.0.1:5432/zoom_connect?sslmode=disable")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := store.Open(ctx, dsn)
	if err != nil {
		return nil, err
	}
	h := newHub(s)
	a := &Server{s: s, h: h, domain: env("ALLOWED_DOMAIN", "acme.co"), zoomSecret: os.Getenv("ZOOM_WEBHOOK_SECRET")}
	if err = a.seed(ctx); err != nil {
		s.Close()
		return nil, err
	}
	if err = a.importArchives(ctx); err != nil {
		s.Close()
		return nil, err
	}
	if err = a.configureLogin(ctx); err != nil {
		s.Close()
		return nil, err
	}
	if err = a.configureRecorder(); err != nil {
		s.Close()
		return nil, err
	}
	a.startSimulators()
	mux := http.NewServeMux()
	a.routes(mux)
	mux.Handle("/", next)
	return mux, nil
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func (a *Server) routes(m *http.ServeMux) {
	pub := func(pattern string, h http.HandlerFunc) { m.HandleFunc(pattern, h) }
	priv := func(pattern string, h http.HandlerFunc) { m.Handle(pattern, a.auth(http.HandlerFunc(h))) }
	pub("GET /healthz", a.health)
	pub("GET /api/auth/options", a.authOptions)
	pub("POST /api/login", a.passwordSignIn)
	pub("GET /api/demo/users", a.demoUsers)
	pub("POST /api/demo/login", a.demoLogin)
	priv("GET /api/me", a.me)
	priv("POST /api/logout", a.logout)
	priv("GET /api/calls", a.calls)
	priv("GET /api/calls/{id}", a.call)
	priv("GET /api/calls/{id}/events", a.events)
	priv("GET /api/calls/{id}/stream", a.stream)
	priv("GET /api/calls/{id}/transcript", a.transcript)
	priv("GET /api/calls/{id}/shares", a.shares)
	priv("POST /api/calls/{id}/shares", a.addShare)
	priv("DELETE /api/calls/{id}/shares/{shareId}", a.removeShare)
	priv("GET /api/people", a.people)
	priv("GET /api/keys", a.keys)
	priv("POST /api/keys", a.createKey)
	priv("DELETE /api/keys/{id}", a.revokeKey)
	priv("GET /api/admin/zoom", a.zoomSettings)
	priv("POST /api/admin/zoom", a.saveZoomSettings)
	priv("POST /api/demo/calls", a.createDemoCall)
	priv("POST /api/demo/calls/{id}/events", a.addDemoEvent)
	priv("POST /api/demo/calls/{id}/stop", a.stopDemoCall)
	if a.recorder != nil {
		m.Handle("POST /zoom/webhook", a.recorder)
	} else {
		pub("POST /zoom/webhook", a.zoomWebhook)
	}
	// The agent guide uses /api/v1; keep the same access and representation rules.
	priv("GET /api/v1/calls", a.calls)
	priv("GET /api/v1/calls/{id}", a.call)
	priv("GET /api/v1/calls/{id}/events", a.events)
	priv("GET /api/v1/calls/{id}/stream", a.stream)
	priv("GET /api/v1/calls/{id}/transcript", a.transcript)
	priv("GET /api/stream", a.announcements)
	priv("GET /api/v1/stream", a.announcements)
}

type principalKey struct{}

func principal(r *http.Request) Principal {
	p, _ := r.Context().Value(principalKey{}).(Principal)
	return p
}
func (a *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var p Principal
		var err error
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			p, err = a.keyPrincipal(ctx, strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")))
		} else if c, e := r.Cookie("zc_session"); e == nil {
			p, err = a.sessionPrincipal(ctx, c.Value)
		} else {
			err = pgx.ErrNoRows
		}
		if err != nil {
			jsonError(w, 401, "unauthorized")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, principalKey{}, p)))
	})
}
func (a *Server) keyPrincipal(ctx context.Context, secret string) (Principal, error) {
	hash := sha256.Sum256([]byte(secret))
	k, e := a.s.Q.FindKey(ctx, hex.EncodeToString(hash[:]))
	if e != nil {
		return Principal{}, e
	}
	u, e := a.s.Q.GetUserByID(ctx, k.UserID)
	if e != nil {
		return Principal{}, e
	}
	_ = a.s.Q.TouchKey(ctx, k.ID)
	return Principal{User: user(u), KeyID: k.ID, KeyName: k.Name, IsKey: true}, nil
}
func (a *Server) sessionPrincipal(ctx context.Context, id string) (Principal, error) {
	u, e := a.s.Q.GetSession(ctx, id)
	if e != nil {
		return Principal{}, e
	}
	return Principal{User: User{ID: u.ID, Email: u.Email, Name: u.Name, IsAdmin: u.IsAdmin}}, nil
}
func (a *Server) stillValid(ctx context.Context, r *http.Request, p Principal) bool {
	if p.IsKey {
		h := r.Header.Get("Authorization")
		secret := strings.TrimPrefix(h, "Bearer ")
		sum := sha256.Sum256([]byte(secret))
		_, e := a.s.Q.FindKey(ctx, hex.EncodeToString(sum[:]))
		return e == nil
	}
	c, e := r.Cookie("zc_session")
	if e != nil {
		return false
	}
	_, e = a.s.Q.GetSession(ctx, c.Value)
	return e == nil
}
func user(u db.User) User { return User{ID: u.ID, Email: u.Email, Name: u.Name, IsAdmin: u.IsAdmin} }
func newID() string       { return ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String() }
func randomSecret(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
func jsonError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}
func (a *Server) demoUsers(w http.ResponseWriter, r *http.Request) {
	if a.login != nil {
		jsonError(w, 404, "not found")
		return
	}
	us, e := a.s.Q.ListUsers(r.Context())
	if e != nil {
		jsonError(w, 500, "database error")
		return
	}
	out := make([]User, 0, len(us))
	for _, u := range us {
		out = append(out, user(u))
	}
	writeJSON(w, 200, out)
}
func (a *Server) demoLogin(w http.ResponseWriter, r *http.Request) {
	if a.login != nil {
		jsonError(w, 404, "not found")
		return
	}
	var in struct {
		Email string `json:"email"`
	}
	if decode(r, &in) != nil {
		jsonError(w, 400, "invalid body")
		return
	}
	u, e := a.s.Q.GetUserByEmail(r.Context(), strings.ToLower(strings.TrimSpace(in.Email)))
	if e != nil {
		jsonError(w, 403, "unknown demo user")
		return
	}
	a.createLoginSession(w, r, user(u))
}

func (a *Server) me(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, principal(r).User) }
func (a *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, e := r.Cookie("zc_session"); e == nil {
		_ = a.s.Q.DeleteSession(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "zc_session", Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
	w.WriteHeader(204)
}

func (a *Server) seed(ctx context.Context) error {
	if os.Getenv("AUTH_MODE") == "password" {
		return nil
	}
	admins := map[string]bool{}
	for _, email := range strings.Split(env("ADMIN_EMAILS", "dana@acme.co,marcus@acme.co"), ",") {
		admins[strings.ToLower(strings.TrimSpace(email))] = true
	}
	rows := []struct {
		id, email, name string
		admin           bool
	}{{"u-dana", "dana@acme.co", "Dana Kim", true}, {"u-marcus", "marcus@acme.co", "Marcus Reyes", true}, {"u-jordan", "jordan@acme.co", "Jordan Lee", false}, {"u-priya", "priya@acme.co", "Priya Shah", false}, {"u-leo", "leo@acme.co", "Leo Park", false}}
	for _, u := range rows {
		_, e := a.s.Pool.Exec(ctx, `INSERT INTO users(id,email,name,is_admin) VALUES($1,$2,$3,$4) ON CONFLICT(email) DO UPDATE SET name=EXCLUDED.name,is_admin=EXCLUDED.is_admin`, u.id, u.email, u.name, admins[strings.ToLower(u.email)])
		if e != nil {
			return e
		}
	}
	n, e := a.s.Q.CountCalls(ctx)
	if e != nil {
		return e
	}
	if n > 0 {
		return nil
	}
	today := time.Now().UTC()
	type demo struct {
		topic, host   string
		people        []string
		status        string
		age, duration time.Duration
	}
	demos := []demo{
		{"Weekly product sync", "u-dana", []string{"u-dana", "u-marcus", "u-priya", "u-jordan"}, "live", 23 * time.Minute, 42 * time.Minute},
		{"Acme × Northwind kickoff", "u-leo", []string{"u-leo", "u-jordan", "u-priya"}, "live", 4 * time.Minute, 35 * time.Minute},
		{"Design review: agent API", "u-marcus", []string{"u-marcus", "u-dana", "u-priya", "u-jordan"}, "stopped", 5 * time.Hour, 42 * time.Minute},
		{"Hiring sync — backend", "u-dana", []string{"u-dana", "u-jordan"}, "stopped", 7 * time.Hour, 18 * time.Minute},
		{"Customer call: Globex renewal", "u-leo", []string{"u-leo", "u-marcus"}, "stopped", 30 * time.Hour, 55 * time.Minute},
		{"Infra standup", "u-dana", []string{"u-dana", "u-marcus"}, "stopped", 54 * time.Hour, 12 * time.Minute},
		{"1:1 Dana / Marcus", "u-marcus", []string{"u-marcus", "u-dana"}, "interrupted", 72 * time.Hour, 30 * time.Minute},
		{"Q4 planning", "u-priya", []string{"u-priya", "u-jordan", "u-dana"}, "stopped", 96 * time.Hour, 50 * time.Minute},
		{"Support handoff", "u-jordan", []string{"u-jordan", "u-leo"}, "stopped", 120 * time.Hour, 26 * time.Minute},
		{"Sales weekly", "u-leo", []string{"u-leo", "u-marcus", "u-dana"}, "stopped", 144 * time.Hour, 38 * time.Minute},
	}
	lines := [][]string{
		{"Okay, let's get going. Main thing today is the agent API and whether we're ready to hand keys out.", "The stream endpoint replays from zero, then follows live with Last-Event-ID reconnects.", "The webhook may land before the socket opens; the sixty-second buffer should cover that gap.", "Priya, can you check that participant shares work for somebody who signs in later?", "Yes, the email share is stored now and resolves after the first sign-in.", "We should keep the transcript download available as txt, VTT, and JSON.", "The admin page will show which Zoom events were accepted or rejected.", "I will update the guide after this call and send a key to the integration team.", "The keys are shown once, and we only store their hashes.", "Let's make the first release demo-only for Zoom credentials.", "I'll send the notes with the open questions this afternoon.", "Great, let's call this ready for the next review."},
		{"Thanks for joining the kickoff. Today we'll connect your Northwind team to Acme.", "Our first step is getting your team into the shared workspace.", "Jordan will own the account setup and Priya will handle the integration check.", "We can start with the transcript feed, then add the CRM handoff.", "That makes sense. Please share a short sample call before we turn on the connector.", "We'll verify the permissions against the people who were actually on the call.", "Let's schedule the technical walkthrough for Thursday.", "I'll send a summary and the setup link after we wrap."},
		{"Goal for today is agreeing on the shape of the agent API.", "Agents can follow one call over SSE or pull the full event list after it ends.", "Can a single connection tell an agent whenever a new call starts?", "Yes, the user-scoped stream only announces calls that user can access.", "Each event has a sequence id, so reconnects resume without repeating lines.", "We should document the 401, 403, and 404 cases with examples.", "API keys should inherit exactly the owner's participant and share access.", "Revoke must also close any stream that's already open.", "Let's include a copyable curl example and a structured event sample.", "That gives Priya enough to finish the guide.", "I'll review the wording before we send it out.", "Agreed. We'll ship the first version with transcript and chat only."},
		{"We have two backend candidates and one final interview to schedule.", "Jordan, the take-home covered the storage and API boundaries well.", "The remaining question is how they approach debugging under pressure.", "Let's align on the rubric before we compare notes.", "I'll check everyone's availability and send the panel invite."},
		{"Thanks for making time to talk through the Globex renewal.", "The team is using the weekly export, but they'd like a live view too.", "Let's show them the transcript workflow and the access model.", "Their security team wants revocation to take effect immediately.", "We check permissions again while an SSE connection is open.", "Good. Can we send the trial terms by Friday?", "Yes, I'll include the two options and a short implementation plan.", "Let's confirm the renewal date and the support contact before we close."},
		{"Build is green on main and the migration is safe to rerun.", "The staging database has the new call and share tables.", "We saw one slow query on the people search; the index should address it.", "I'll watch the logs after deploy and report back in the channel."},
		{"I wanted to check in about the rollout workload.", "The next two weeks are busy, so let's narrow the first milestone.", "I'll take the partner calls and you can focus on the API guide.", "That works. We should block Friday afternoon for a proper break.", "Thanks, I'll update the plan after lunch."},
		{"Let's start with the Q4 target and work back to the launch date.", "The partner pilot is the main dependency for November.", "We need a clear owner for onboarding and a fallback if data is late.", "Priya can own the technical checklist; Jordan will coordinate the pilot.", "We'll review the numbers at the end of the month.", "I'll turn these into milestones and circulate the draft."},
		{"The support ticket is about an expired invite link.", "I can reproduce it with the old organization email.", "Let's issue a new link and verify the user can see the shared call.", "The participant still has access, so the transcript should load now.", "I'll add the fix to the handoff notes."},
		{"Pipeline is on track for the October target.", "The renewal conversation with Globex is moving well.", "We need the updated pricing sheet before the next call.", "Marcus will review the terms and I'll send them to Dana.", "Let's keep the follow-up on the calendar for Tuesday."},
	}
	for i, d := range demos {
		host, _ := a.s.Q.GetUserByID(ctx, d.host)
		id := newID()
		start := today.Add(-d.age)
		status := d.status
		var ended pgtype.Timestamptz
		if status != "live" {
			ended = pgtype.Timestamptz{Time: start.Add(d.duration), Valid: true}
		}
		chatCount := int64(0)
		if i == 0 || i == 2 {
			chatCount = 2
		}
		if err := a.s.Q.CreateCall(ctx, db.CreateCallParams{ID: id, Topic: d.topic, Status: status, Started: pgtype.Timestamptz{Time: start, Valid: true}, Ended: ended, HostUserID: d.host, LastSeq: 0}); err != nil {
			return err
		}
		people := make([]Person, 0, len(d.people))
		for _, uid := range d.people {
			u, _ := a.s.Q.GetUserByID(ctx, uid)
			_ = a.s.Q.AddParticipant(ctx, db.AddParticipantParams{CallID: id, ParticipantID: uid, UserID: pgtype.Text{String: uid, Valid: true}, Name: u.Name, Email: pgtype.Text{String: u.Email, Valid: true}})
			people = append(people, Person{ID: uid, Name: u.Name, Email: u.Email})
		}
		// Give every persona a useful set of calls while preserving private-call examples.
		if i == 1 || i == 4 || i == 8 {
			_, _ = a.s.Q.CreateShare(ctx, db.CreateShareParams{ID: newID(), CallID: id, Lower: "dana@acme.co", GrantedBy: host.ID})
		}
		eventDuration := d.duration
		if status == "live" {
			eventDuration = d.age
		}
		for j, text := range lines[i] {
			p := people[j%len(people)]
			offset := time.Duration(j) * eventDuration / time.Duration(len(lines[i]))
			ev := Event{Seq: int64(j + 1), TS: start.Add(offset).UnixMilli(), Recv: start.Add(offset).UnixMilli(), Kind: "transcript", Participant: &p, Data: map[string]any{"text": text, "lang": "en", "final": true}}
			if err := a.h.append(ctx, id, ev); err != nil {
				return err
			}
		}
		if chatCount > 0 {
			for j, text := range []string{"docs draft: the agent API page in the app has curl examples now", "I'll send the revised notes after this meeting."} {
				p := people[(j+1)%len(people)]
				offset := eventDuration - time.Duration(2-j)*time.Minute
				ev := Event{Seq: int64(len(lines[i]) + j + 1), TS: start.Add(offset).UnixMilli(), Recv: start.Add(offset).UnixMilli(), Kind: "chat", Participant: &p, Data: map[string]any{"text": text}}
				if err := a.h.append(ctx, id, ev); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (a *Server) startSimulators() {
	rows, e := a.s.Pool.Query(context.Background(), `SELECT id,status FROM calls WHERE status='live'`)
	if e != nil {
		return
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id, status string
		_ = rows.Scan(&id, &status)
		ids = append(ids, id)
	}
	for _, id := range ids {
		initial, err := a.h.snapshot(context.Background(), id, 0)
		if err != nil {
			log.Printf("read simulator source: %v", err)
			continue
		}
		if len(initial) > 0 && initial[0].Data["source"] == "zoom" {
			continue
		}
		go a.simulate(id)
	}
}
func (a *Server) simulate(id string) {
	phrases := []string{"I'll share the updated notes after this call.", "That gives us a clear path for the next milestone.", "Let's make sure the owner and date are in the summary."}
	ticker := time.NewTicker(12 * time.Second)
	defer ticker.Stop()
	for n := 0; n < 7200; n++ {
		<-ticker.C
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		c, e := a.s.Q.GetCall(ctx, id)
		if e != nil || c.Status != "live" {
			cancel()
			return
		}
		p := Person{ID: c.HostUserID, Name: c.HostName, Email: c.HostEmail}
		if _, err := a.emit(ctx, id, "transcript", &p, map[string]any{"text": phrases[n%len(phrases)], "lang": "en", "final": true}); err != nil {
			log.Printf("persist simulated event: %v", err)
		}
		cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	if _, err := a.emit(ctx, id, "stream.stopped", nil, map[string]any{"reason": "demo simulation limit reached"}); err != nil {
		log.Printf("persist stop: %v", err)
	}
	cancel()
}
func (a *Server) emit(ctx context.Context, id, kind string, p *Person, data map[string]any) (Event, error) {
	return a.h.appendNext(ctx, id, kind, p, data)
}
func (a *Server) admin(p Principal) bool { return p.User.IsAdmin }
func (a *Server) isParticipant(ctx context.Context, id, userID string) bool {
	rows, e := a.s.Q.ListParticipants(ctx, id)
	if e != nil {
		return false
	}
	for _, r := range rows {
		if r.UserID.Valid && r.UserID.String == userID {
			return true
		}
	}
	return false
}
func (a *Server) canAccess(ctx context.Context, id, userID string) bool {
	v, e := a.s.Q.CanAccessCall(ctx, db.CanAccessCallParams{CallID: id, UserID: pgtype.Text{String: userID, Valid: true}})
	return e == nil && v.Bool
}
func (a *Server) authorizedCall(w http.ResponseWriter, r *http.Request) (db.GetCallRow, bool) {
	c, e := a.s.Q.GetCall(r.Context(), r.PathValue("id"))
	if errors.Is(e, pgx.ErrNoRows) {
		jsonError(w, 404, "call not found")
		return c, false
	}
	if e != nil {
		jsonError(w, 500, "database error")
		return c, false
	}
	p := principal(r)
	if !a.canAccess(r.Context(), c.ID, p.User.ID) {
		ps, _ := a.s.Q.ListParticipants(r.Context(), c.ID)
		names := make([]Person, 0, len(ps))
		for _, q := range ps {
			names = append(names, Person{ID: q.ParticipantID, Name: q.Name})
		}
		writeJSON(w, 403, map[string]any{"error": "forbidden", "participants": names})
		return c, false
	}
	return c, true
}
func (a *Server) toCall(ctx context.Context, c db.GetCallRow, p Principal) Call {
	ps, _ := a.s.Q.ListParticipants(ctx, c.ID)
	people := make([]Person, 0, len(ps))
	for _, q := range ps {
		people = append(people, Person{ID: q.ParticipantID, Name: q.Name, Email: q.Email.String})
	}
	var end *time.Time
	if c.Ended.Valid {
		t := c.Ended.Time
		end = &t
	}
	return Call{ID: c.ID, Topic: c.Topic, Status: c.Status, Started: c.Started.Time, Ended: end, Host: Person{ID: c.HostUserID, Name: c.HostName, Email: c.HostEmail}, Participants: people, LastSeq: c.LastSeq, LatestEvent: a.latestEvent(ctx, c.ID), IsParticipant: a.isParticipant(ctx, c.ID, p.User.ID), CanShare: a.isParticipant(ctx, c.ID, p.User.ID)}
}

func (a *Server) latestEvent(ctx context.Context, id string) *Event {
	row, err := a.s.Q.LatestTranscript(ctx, id)
	if err != nil {
		return nil
	}
	ev, err := decodeEvent(row.Seq, row.Ts, row.Recv, row.Kind, row.Participant, row.Data)
	if err != nil {
		return nil
	}
	return &ev
}

func (a *Server) calls(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	rows, e := a.s.Q.ListCalls(r.Context(), pgtype.Text{String: p.User.ID, Valid: true})
	if e != nil {
		jsonError(w, 500, "database error")
		return
	}
	out := make([]Call, 0, len(rows))
	for _, row := range rows {
		var end *time.Time
		if row.Ended.Valid {
			v := row.Ended.Time
			end = &v
		}
		people, _ := a.s.Q.ListParticipants(r.Context(), row.ID)
		ps := make([]Person, 0, len(people))
		for _, q := range people {
			ps = append(ps, Person{ID: q.ParticipantID, Name: q.Name, Email: q.Email.String})
		}
		out = append(out, Call{ID: row.ID, Topic: row.Topic, Status: row.Status, Started: row.Started.Time, Ended: end, Host: Person{ID: row.HostUserID, Name: row.HostName, Email: row.HostEmail}, Participants: ps, LastSeq: row.LastSeq, LatestEvent: a.latestEvent(r.Context(), row.ID), SharedBy: row.SharedBy, IsParticipant: row.IsParticipant, CanShare: row.IsParticipant})
	}
	writeJSON(w, 200, out)
}
func (a *Server) call(w http.ResponseWriter, r *http.Request) {
	c, ok := a.authorizedCall(w, r)
	if ok {
		writeJSON(w, 200, a.toCall(r.Context(), c, principal(r)))
	}
}

func (a *Server) events(w http.ResponseWriter, r *http.Request) {
	c, ok := a.authorizedCall(w, r)
	if !ok {
		return
	}
	_ = c
	from, _ := parseFrom(r)
	events, err := a.h.snapshot(r.Context(), r.PathValue("id"), from)
	if err != nil {
		jsonError(w, 500, "database error")
		return
	}
	writeJSON(w, 200, events)
}
func parseFrom(r *http.Request) (int64, error) {
	v := r.URL.Query().Get("from")
	if v == "" {
		v = r.Header.Get("Last-Event-ID")
	}
	var n int64
	_, e := fmt.Sscan(v, &n)
	if e != nil || n < 0 {
		return 0, nil
	}
	return n, nil
}
func (a *Server) stream(w http.ResponseWriter, r *http.Request) {
	c, ok := a.authorizedCall(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	from, _ := parseFrom(r)
	replay, sub, unsub, err := a.h.replaySubscribe(r.Context(), id, from)
	if err != nil {
		jsonError(w, 500, "database error")
		return
	}
	defer unsub()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	fl, ok := w.(http.Flusher)
	if !ok {
		return
	}
	_, _ = fmt.Fprint(w, ": connected\n\n")
	fl.Flush()
	send := func(e Event) bool {
		b, _ := json.Marshal(e)
		_, er := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.Seq, e.Kind, b)
		fl.Flush()
		return er == nil
	}
	for _, e := range replay {
		if !send(e) {
			return
		}
	}
	if c.Status != "live" {
		_, _ = fmt.Fprint(w, "event: end\ndata: {}\n\n")
		fl.Flush()
		return
	}
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e, open := <-sub:
			if !open {
				return
			}
			if !send(e) {
				return
			}
			if e.Kind == "stream.stopped" || e.Kind == "stream.interrupted" {
				_, _ = fmt.Fprint(w, "event: end\ndata: {}\n\n")
				fl.Flush()
				return
			}
		case <-tick.C:
			if !a.stillValid(r.Context(), r, principal(r)) || !a.canAccess(r.Context(), id, principal(r).User.ID) {
				return
			}
			current, e := a.s.Q.GetCall(r.Context(), id)
			if e == nil && current.Status != "live" {
				_, _ = fmt.Fprint(w, "event: end\ndata: {}\n\n")
				fl.Flush()
				return
			}
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			fl.Flush()
		}
	}
}

func (a *Server) transcript(w http.ResponseWriter, r *http.Request) {
	c, ok := a.authorizedCall(w, r)
	if !ok {
		return
	}
	events, err := a.h.snapshot(r.Context(), r.PathValue("id"), 0)
	if err != nil {
		jsonError(w, 500, "database error")
		return
	}
	baseTS := c.Started.Time.UnixMilli()
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "txt"
	}
	var b strings.Builder
	switch format {
	case "json":
		filtered := make([]Event, 0)
		for _, e := range events {
			if e.Kind == "transcript" {
				filtered = append(filtered, e)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", "attachment; filename=transcript.json")
		_ = json.NewEncoder(w).Encode(filtered)
		return
	case "vtt":
		b.WriteString("WEBVTT\n\n")
		var seq []Event
		for _, e := range events {
			if e.Kind == "transcript" {
				seq = append(seq, e)
			}
		}
		for i, e := range seq {
			start := time.Duration(e.TS-baseTS) * time.Millisecond
			end := start + 5*time.Second
			if i+1 < len(seq) {
				end = time.Duration(seq[i+1].TS-baseTS) * time.Millisecond
			}
			name := "Unknown"
			if e.Participant != nil {
				name = e.Participant.Name
			}
			fmt.Fprintf(&b, "%s --> %s\n%s: %s\n\n", vttTime(start), vttTime(end), name, e.Data["text"])
		}
		w.Header().Set("Content-Type", "text/vtt")
		w.Header().Set("Content-Disposition", "attachment; filename=transcript.vtt")
	case "txt":
		for _, e := range events {
			if e.Kind != "transcript" {
				continue
			}
			name := "Unknown"
			if e.Participant != nil {
				name = e.Participant.Name
			}
			offset := time.Duration(e.TS-baseTS) * time.Millisecond
			fmt.Fprintf(&b, "%02d:%02d:%02d %s: %v\n", int(offset.Hours()), int(offset.Minutes())%60, int(offset.Seconds())%60, name, e.Data["text"])
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=transcript.txt")
	default:
		jsonError(w, 400, "format must be txt, vtt, or json")
		return
	}
	_, _ = w.Write([]byte(b.String()))
}
func vttTime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	ms := d.Milliseconds()
	return fmt.Sprintf("%02d:%02d:%02d.%03d", ms/3600000, (ms/60000)%60, (ms/1000)%60, ms%1000)
}

func (a *Server) people(w http.ResponseWriter, r *http.Request) {
	rows, e := a.s.Q.SearchPeople(r.Context(), strings.TrimSpace(r.URL.Query().Get("q")))
	if e != nil {
		jsonError(w, 500, "database error")
		return
	}
	p := principal(r)
	out := make([]User, 0)
	for _, u := range rows {
		if u.ID != p.User.ID {
			out = append(out, user(u))
		}
	}
	writeJSON(w, 200, out)
}
func (a *Server) keys(w http.ResponseWriter, r *http.Request) {
	ks, e := a.s.Q.ListKeys(r.Context(), principal(r).User.ID)
	if e != nil {
		jsonError(w, 500, "database error")
		return
	}
	out := make([]map[string]any, 0, len(ks))
	for _, k := range ks {
		x := map[string]any{"id": k.ID, "name": k.Name, "prefix": k.Prefix, "created_at": k.CreatedAt.Time}
		if k.LastUsedAt.Valid {
			x["last_used_at"] = k.LastUsedAt.Time
		}
		if k.RevokedAt.Valid {
			x["revoked_at"] = k.RevokedAt.Time
		}
		out = append(out, x)
	}
	writeJSON(w, 200, out)
}
func (a *Server) createKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if decode(r, &in) != nil || strings.TrimSpace(in.Name) == "" {
		jsonError(w, 400, "name required")
		return
	}
	secret := "zc_" + randomSecret(32)
	sum := sha256.Sum256([]byte(secret))
	k, e := a.s.Q.CreateKey(r.Context(), db.CreateKeyParams{ID: newID(), UserID: principal(r).User.ID, Name: strings.TrimSpace(in.Name), KeyHash: hex.EncodeToString(sum[:]), Prefix: secret[:8]})
	if e != nil {
		jsonError(w, 500, "database error")
		return
	}
	writeJSON(w, 201, map[string]any{"key": map[string]any{"id": k.ID, "name": k.Name, "prefix": k.Prefix, "created_at": k.CreatedAt.Time}, "secret": secret})
}
func (a *Server) revokeKey(w http.ResponseWriter, r *http.Request) {
	_ = a.s.Q.RevokeKey(r.Context(), db.RevokeKeyParams{ID: r.PathValue("id"), UserID: principal(r).User.ID})
	w.WriteHeader(204)
}

func (a *Server) currentKeyStillActive(ctx context.Context, id string) bool {
	_, e := a.s.Pool.Exec(ctx, `SELECT id FROM api_keys WHERE id=$1 AND revoked_at IS NULL`, id)
	return e == nil
}
func digest(s string) string  { x := sha256.Sum256([]byte(s)); return hex.EncodeToString(x[:]) }
func logf(f string, v ...any) { log.Printf(f, v...) }
func _ignore(err error)       { _ = err }

var _ = digest
var _ = logf
var _ = _ignore
var _ = errors.Is
