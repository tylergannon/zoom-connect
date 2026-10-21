package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/tylergannon/zoom-connect/internal/store/db"
)

// Prototype bounds; the HTML and title limits are repeated as table checks.
// Every version is kept as call history, so versions per page are not capped.
const (
	maxArtifactHTML     = 256 << 10
	maxArtifactBody     = 1 << 20
	maxArtifactTitle    = 120
	maxArtifactsPerCall = 10
	artifactHistoryPage = 50
	maxArtifactHistory  = 100
)

var errArtifactNotFound = errors.New("artifact not found")

type Artifact struct {
	ID         string    `json:"id"`
	CallID     string    `json:"call_id"`
	Title      string    `json:"title"`
	Version    int64     `json:"version"`
	Bytes      int64     `json:"bytes"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	CreatedBy  Person    `json:"created_by"`
	UpdatedBy  Person    `json:"updated_by"`
	UpdatedVia string    `json:"updated_via,omitempty"`
	ViewerURL  string    `json:"viewer_url"`
}

// ArtifactSnapshot is one complete version: the unit of reads and stream events.
type ArtifactSnapshot struct {
	Artifact
	HTML string `json:"html"`
}

// artifactHub wakes this instance's viewers after a committed update. It keeps
// only the newest snapshot of artifacts that currently have viewers; Postgres
// stays authoritative and every stream also polls the stored version.
type artifactHub struct {
	mu     sync.Mutex
	subs   map[string]map[chan struct{}]bool
	latest map[string]ArtifactSnapshot
}

func (h *artifactHub) subscribe(id string) (<-chan struct{}, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs == nil {
		h.subs, h.latest = map[string]map[chan struct{}]bool{}, map[string]ArtifactSnapshot{}
	}
	if h.subs[id] == nil {
		h.subs[id] = map[chan struct{}]bool{}
	}
	c := make(chan struct{}, 1)
	h.subs[id][c] = true
	return c, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.subs[id], c)
		if len(h.subs[id]) == 0 {
			delete(h.subs, id)
			delete(h.latest, id)
		}
	}
}
func (h *artifactHub) publish(s ArtifactSnapshot) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.subs[s.ID]) == 0 || h.latest[s.ID].Version >= s.Version {
		return
	}
	h.latest[s.ID] = s
	for c := range h.subs[s.ID] {
		select {
		case c <- struct{}{}:
		default: // a wake-up is already pending; the viewer reads the newest snapshot
		}
	}
}
func (h *artifactHub) newest(id string) ArtifactSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.latest[id]
}

func artifactPerson(id, name string) Person { return Person{ID: id, Name: name} }
func viewerURL(callID, id string) string     { return "/calls/" + callID + "/artifacts/" + id }

func (a *Server) loadArtifact(ctx context.Context, q *db.Queries, callID, id string) (ArtifactSnapshot, error) {
	row, err := q.GetCallArtifact(ctx, db.GetCallArtifactParams{ID: id, CallID: callID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ArtifactSnapshot{}, errArtifactNotFound
	}
	if err != nil {
		return ArtifactSnapshot{}, err
	}
	return ArtifactSnapshot{Artifact: Artifact{ID: row.ID, CallID: row.CallID, Title: row.Title, Version: row.Version, Bytes: int64(len(row.Html)), CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time, CreatedBy: artifactPerson(row.CreatedByID, row.CreatedByName), UpdatedBy: artifactPerson(row.UpdatedByID, row.UpdatedByName), UpdatedVia: row.KeyName.String, ViewerURL: viewerURL(row.CallID, row.ID)}, HTML: row.Html}, nil
}

type artifactInput struct {
	Title       *string `json:"title"`
	HTML        *string `json:"html"`
	BaseVersion *int64  `json:"base_version"`
}

// readArtifactInput applies the request bounds and writes the error response.
func readArtifactInput(w http.ResponseWriter, r *http.Request, create bool) (artifactInput, bool) {
	var in artifactInput
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxArtifactBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			jsonError(w, 413, "request body exceeds 1 MiB")
		} else {
			jsonError(w, 400, "invalid JSON body: "+err.Error())
		}
		return in, false
	}
	if in.HTML == nil {
		jsonError(w, 400, "html is required")
		return in, false
	}
	if len(*in.HTML) > maxArtifactHTML {
		jsonError(w, 413, "html exceeds 256 KiB")
		return in, false
	}
	if strings.ContainsRune(*in.HTML, 0) {
		jsonError(w, 400, "html must not contain NUL characters")
		return in, false
	}
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		in.Title = &t
		if t != "" && (utf8.RuneCountInString(t) > maxArtifactTitle || strings.IndexFunc(t, unicode.IsControl) >= 0) {
			jsonError(w, 400, "title must be plain text up to 120 characters")
			return in, false
		}
	}
	if create && (in.Title == nil || *in.Title == "") {
		jsonError(w, 400, "title is required")
		return in, false
	}
	if create && in.BaseVersion != nil {
		jsonError(w, 400, "base_version is only used for updates")
		return in, false
	}
	if !create && (in.BaseVersion == nil || *in.BaseVersion < 1) {
		jsonError(w, 400, "base_version is required")
		return in, false
	}
	return in, true
}

// Publishing uses the same tier as sharing: the call owner or a participant.
// People who only received a share, and their API keys, are readers.
func (a *Server) artifactWriter(w http.ResponseWriter, r *http.Request) (db.GetCallRow, bool) {
	c, ok := a.authorizedCall(w, r)
	if !ok {
		return c, false
	}
	if !a.canShareCall(r.Context(), c.ID, principal(r).User.ID) {
		jsonError(w, 403, "only the call owner or participants can publish artifacts")
		return c, false
	}
	return c, true
}

func (a *Server) listArtifacts(w http.ResponseWriter, r *http.Request) {
	c, ok := a.authorizedCall(w, r)
	if !ok {
		return
	}
	rows, err := a.s.Q.ListCallArtifacts(r.Context(), c.ID)
	if err != nil {
		jsonError(w, 500, "database error")
		return
	}
	out := make([]Artifact, 0, len(rows))
	for _, row := range rows {
		out = append(out, Artifact{ID: row.ID, CallID: row.CallID, Title: row.Title, Version: row.Version, Bytes: row.Bytes, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time, CreatedBy: artifactPerson(row.CreatedByID, row.CreatedByName), UpdatedBy: artifactPerson(row.UpdatedByID, row.UpdatedByName), UpdatedVia: row.KeyName.String, ViewerURL: viewerURL(row.CallID, row.ID)})
	}
	writeArtifactJSON(w, 200, out)
}

func (a *Server) getArtifact(w http.ResponseWriter, r *http.Request) {
	c, ok := a.authorizedCall(w, r)
	if !ok {
		return
	}
	s, err := a.loadArtifact(r.Context(), a.s.Q, c.ID, r.PathValue("artifactId"))
	if err != nil {
		artifactLoadError(w, err)
		return
	}
	writeArtifactJSON(w, 200, s)
}

func (a *Server) getArtifactVersion(w http.ResponseWriter, r *http.Request) {
	c, ok := a.authorizedCall(w, r)
	if !ok {
		return
	}
	n, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
	if err != nil || n < 1 {
		jsonError(w, 400, "version must be a positive integer")
		return
	}
	row, err := a.s.Q.GetArtifactVersion(r.Context(), db.GetArtifactVersionParams{ArtifactID: r.PathValue("artifactId"), CallID: c.ID, Version: n})
	if errors.Is(err, pgx.ErrNoRows) {
		jsonError(w, 404, "artifact version not found or no longer retained")
		return
	}
	if err != nil {
		jsonError(w, 500, "database error")
		return
	}
	writeArtifactJSON(w, 200, map[string]any{"artifact_id": r.PathValue("artifactId"), "call_id": c.ID, "version": row.Version, "title": row.Title, "html": row.Html, "created_at": row.CreatedAt.Time, "author": artifactPerson(row.AuthorID, row.AuthorName), "via": row.KeyName.String})
}

// ArtifactVersionInfo describes one stored version without its HTML.
type ArtifactVersionInfo struct {
	Version   int64     `json:"version"`
	Title     string    `json:"title"`
	Bytes     int64     `json:"bytes"`
	CreatedAt time.Time `json:"created_at"`
	Author    Person    `json:"author"`
	Via       string    `json:"via,omitempty"`
}

// listArtifactVersions pages through stored versions, newest first.
// ?before=N starts below version N; next_before continues the listing.
// Versions removed by the prototype's earlier 25-version limit leave gaps.
func (a *Server) listArtifactVersions(w http.ResponseWriter, r *http.Request) {
	c, ok := a.authorizedCall(w, r)
	if !ok {
		return
	}
	id := r.PathValue("artifactId")
	current, err := a.s.Q.CallArtifactVersion(r.Context(), db.CallArtifactVersionParams{ID: id, CallID: c.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		jsonError(w, 404, "artifact not found")
		return
	}
	if err != nil {
		jsonError(w, 500, "database error")
		return
	}
	before, limit := current+1, int64(artifactHistoryPage)
	if v := r.URL.Query().Get("before"); v != "" {
		if before, err = strconv.ParseInt(v, 10, 64); err != nil || before < 1 {
			jsonError(w, 400, "before must be a positive integer")
			return
		}
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if limit, err = strconv.ParseInt(v, 10, 64); err != nil || limit < 1 || limit > maxArtifactHistory {
			jsonError(w, 400, fmt.Sprintf("limit must be between 1 and %d", maxArtifactHistory))
			return
		}
	}
	rows, err := a.s.Q.ListArtifactVersions(r.Context(), db.ListArtifactVersionsParams{ArtifactID: id, CallID: c.ID, Version: before, Limit: int32(limit + 1)})
	if err != nil {
		jsonError(w, 500, "database error")
		return
	}
	var next *int64
	if int64(len(rows)) > limit {
		rows = rows[:limit]
		next = &rows[limit-1].Version
	}
	out := make([]ArtifactVersionInfo, 0, len(rows))
	for _, row := range rows {
		out = append(out, ArtifactVersionInfo{Version: row.Version, Title: row.Title, Bytes: row.Bytes, CreatedAt: row.CreatedAt.Time, Author: artifactPerson(row.AuthorID, row.AuthorName), Via: row.KeyName.String})
	}
	writeArtifactJSON(w, 200, map[string]any{"artifact_id": id, "call_id": c.ID, "current_version": current, "versions": out, "next_before": next})
}

func (a *Server) createArtifact(w http.ResponseWriter, r *http.Request) {
	c, ok := a.artifactWriter(w, r)
	if !ok {
		return
	}
	in, ok := readArtifactInput(w, r, true)
	if !ok {
		return
	}
	id := newID()
	s, status, msg, err := a.writeArtifact(r.Context(), c.ID, func(q *db.Queries) (int64, int, string, error) {
		n, err := q.CountCallArtifacts(r.Context(), c.ID)
		if err != nil {
			return 0, 0, "", err
		}
		if n >= maxArtifactsPerCall {
			return 0, 409, fmt.Sprintf("artifact limit reached (%d per call)", maxArtifactsPerCall), nil
		}
		return 1, 0, "", q.CreateCallArtifact(r.Context(), db.CreateCallArtifactParams{ID: id, CallID: c.ID, CreatedBy: principal(r).User.ID})
	}, id, *in.Title, *in.HTML, principal(r))
	if status != 0 {
		jsonError(w, status, msg)
		return
	}
	if err != nil {
		jsonError(w, 500, "database error")
		return
	}
	w.Header().Set("Location", "/api/calls/"+c.ID+"/artifacts/"+id)
	writeArtifactJSON(w, 201, s)
}

func (a *Server) updateArtifact(w http.ResponseWriter, r *http.Request) {
	c, ok := a.artifactWriter(w, r)
	if !ok {
		return
	}
	in, ok := readArtifactInput(w, r, false)
	if !ok {
		return
	}
	id := r.PathValue("artifactId")
	title := "" // an omitted title keeps the previous version's title
	if in.Title != nil {
		title = *in.Title
	}
	var current int64
	s, status, msg, err := a.writeArtifact(r.Context(), c.ID, func(q *db.Queries) (int64, int, string, error) {
		next, err := q.AdvanceCallArtifact(r.Context(), db.AdvanceCallArtifactParams{ID: id, CallID: c.ID, Version: *in.BaseVersion})
		if err == nil {
			return next, 0, "", nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return 0, 0, "", err
		}
		current, err = q.CallArtifactVersion(r.Context(), db.CallArtifactVersionParams{ID: id, CallID: c.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, 404, "artifact not found", nil
		}
		if err != nil {
			return 0, 0, "", err
		}
		return 0, 409, "version conflict", nil
	}, id, title, *in.HTML, principal(r))
	if status == 409 && msg == "version conflict" {
		writeArtifactJSON(w, 409, map[string]any{"error": msg, "current_version": current, "base_version": *in.BaseVersion})
		return
	}
	if status != 0 {
		jsonError(w, status, msg)
		return
	}
	if err != nil {
		jsonError(w, 500, "database error")
		return
	}
	writeArtifactJSON(w, 200, s)
}

// writeArtifact locks the call row so writes serialize with call lifecycle
// changes, runs step to choose the next version, and stores that snapshot.
// A nonzero status is a client-visible refusal with nothing committed.
func (a *Server) writeArtifact(ctx context.Context, callID string, step func(*db.Queries) (int64, int, string, error), id, title, html string, p Principal) (ArtifactSnapshot, int, string, error) {
	tx, err := a.s.Pool.Begin(ctx)
	if err != nil {
		return ArtifactSnapshot{}, 0, "", err
	}
	defer tx.Rollback(context.Background())
	q := a.s.Q.WithTx(tx)
	callStatus, err := q.LockCallStatus(ctx, callID)
	if err != nil {
		return ArtifactSnapshot{}, 0, "", err
	}
	if !callOpen(callStatus) {
		return ArtifactSnapshot{}, 409, "call has ended; artifacts are read-only", nil
	}
	version, status, msg, err := step(q)
	if err != nil || status != 0 {
		return ArtifactSnapshot{}, status, msg, err
	}
	if title == "" {
		prev, err := q.GetArtifactVersion(ctx, db.GetArtifactVersionParams{ArtifactID: id, CallID: callID, Version: version - 1})
		if err != nil {
			return ArtifactSnapshot{}, 0, "", err
		}
		title = prev.Title
	}
	key := pgtype.Text{String: p.KeyName, Valid: p.IsKey}
	if err = q.InsertArtifactVersion(ctx, db.InsertArtifactVersionParams{ArtifactID: id, Version: version, Title: title, Html: html, AuthorID: p.User.ID, KeyName: key}); err != nil {
		return ArtifactSnapshot{}, 0, "", err
	}
	s, err := a.loadArtifact(ctx, q, callID, id)
	if err != nil {
		return ArtifactSnapshot{}, 0, "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return ArtifactSnapshot{}, 0, "", err
	}
	a.artifacts.publish(s)
	return s, 0, "", nil
}

func artifactLoadError(w http.ResponseWriter, err error) {
	if errors.Is(err, errArtifactNotFound) {
		jsonError(w, 404, "artifact not found")
	} else {
		jsonError(w, 500, "database error")
	}
}

// Artifact HTML travels only as JSON data and is never served as a document.
func writeArtifactJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	writeJSON(w, status, v)
}

func (a *Server) artifactTickInterval() time.Duration {
	if a.artifactTick > 0 {
		return a.artifactTick
	}
	return 3 * time.Second
}

// artifactStream sends the latest snapshot on connect (unless Last-Event-ID or
// ?from already names it), then each newer version. Every tick rechecks the
// credential and call access and reads the stored version, so a missed wake-up
// or a write on another instance is delivered within one tick.
func (a *Server) artifactStream(w http.ResponseWriter, r *http.Request) {
	c, ok := a.authorizedCall(w, r)
	if !ok {
		return
	}
	id := r.PathValue("artifactId")
	wake, unsub := a.artifacts.subscribe(id)
	defer unsub()
	latest, err := a.loadArtifact(r.Context(), a.s.Q, c.ID, id)
	if err != nil {
		artifactLoadError(w, err)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		jsonError(w, 500, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = fmt.Fprint(w, ": connected\n\n")
	fl.Flush()
	var sent int64
	if from, _ := parseFrom(r); from == latest.Version {
		sent = from
	}
	send := func(s ArtifactSnapshot) bool {
		if s.Version <= sent {
			return true
		}
		b, _ := json.Marshal(s)
		_, err := fmt.Fprintf(w, "id: %d\nevent: artifact\ndata: %s\n\n", s.Version, b)
		fl.Flush()
		sent = s.Version
		return err == nil
	}
	end := func() {
		_, _ = fmt.Fprint(w, "event: end\ndata: {}\n\n")
		fl.Flush()
	}
	if !send(latest) {
		return
	}
	if !callOpen(c.Status) {
		end()
		return
	}
	tick := time.NewTicker(a.artifactTickInterval())
	defer tick.Stop()
	p := principal(r)
	// Access is checked before every later snapshot, and on each tick so an
	// idle stream also closes once its key, session or share is gone.
	allowed := func() bool {
		return a.stillValid(r.Context(), r, p) && a.canAccess(r.Context(), c.ID, p.User.ID)
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-wake:
			if !allowed() || !send(a.artifacts.newest(id)) {
				return
			}
		case <-tick.C:
			if !allowed() {
				return
			}
			if v, err := a.s.Q.CallArtifactVersion(r.Context(), db.CallArtifactVersionParams{ID: id, CallID: c.ID}); err == nil && v > sent {
				s, err := a.loadArtifact(r.Context(), a.s.Q, c.ID, id)
				if err != nil || !send(s) {
					return
				}
			}
			if current, err := a.s.Q.GetCall(r.Context(), c.ID); err == nil && !callOpen(current.Status) {
				end()
				return
			}
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			fl.Flush()
		}
	}
}
