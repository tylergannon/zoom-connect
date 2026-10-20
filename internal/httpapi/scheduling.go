package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tylergannon/zoom-connect/internal/store/db"
)

var invitationURLs = regexp.MustCompile(`https?://[^\s<>"\x{201c}\x{201d}]+`)
var meetingPath = regexp.MustCompile(`^/(?:j|wc/join)/([0-9]{9,11})/?$`)
var meetingNumber = regexp.MustCompile(`^[0-9]{9,11}$`)

// Extract only a meeting number. Invitations and embedded passcodes are never stored.
func invitationMeetingID(invitation string) (string, error) {
	id := ""
	for _, raw := range invitationURLs.FindAllString(invitation, -1) {
		u, err := url.Parse(strings.TrimRight(raw, ".,);"))
		if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
			continue
		}
		host := strings.ToLower(u.Hostname())
		allowed := false
		for _, domain := range []string{"zoom.us", "zoom.com", "zoomgov.com"} {
			if host == domain || strings.HasSuffix(host, "."+domain) {
				allowed = true
			}
		}
		match := meetingPath.FindStringSubmatch(u.Path)
		if !allowed || len(match) != 2 {
			continue
		}
		if id != "" && id != match[1] {
			return "", errors.New("paste an invitation for just one Zoom meeting")
		}
		id = match[1]
	}
	if id == "" {
		return "", errors.New("paste a Zoom invitation containing an https://…zoom.us/j/… meeting link")
	}
	return id, nil
}

// changeCall commits identity, lifecycle and its event together under the same
// lock used for replay/subscription, then publishes only committed events.
func (h *hub) changeCall(ctx context.Context, change func(pgx.Tx) (string, string, map[string]any, error)) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	tx, err := h.s.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(context.Background())
	id, kind, data, err := change(tx)
	if err != nil {
		return "", err
	}
	var ev Event
	if kind != "" {
		q := db.New(tx)
		seq, err := q.ReserveEventSeq(ctx, id)
		if err != nil {
			return "", err
		}
		now := time.Now().UTC().UnixMilli()
		ev = Event{Seq: seq, TS: now, Recv: now, Kind: kind, Data: data}
		params, err := eventParams(id, ev)
		if err != nil {
			return "", err
		}
		if err = q.InsertCallEvent(ctx, params); err != nil {
			return "", err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	if kind != "" {
		for ch := range h.subs[id] {
			select {
			case ch <- ev:
			default:
				delete(h.subs[id], ch)
				close(ch)
			}
		}
	}
	return id, nil
}

func (a *Server) scheduleCall(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Invitation  string     `json:"invitation"`
		Topic       string     `json:"topic"`
		ScheduledAt *time.Time `json:"scheduled_at"`
	}
	if decode(r, &in) != nil || len(in.Invitation) > 32000 || len(in.Topic) > 200 {
		jsonError(w, 400, "enter a Zoom invitation and a title of at most 200 characters")
		return
	}
	meetingID, err := invitationMeetingID(in.Invitation)
	if err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	topic := strings.TrimSpace(in.Topic)
	if topic == "" {
		topic = "Zoom meeting · " + meetingID
	}
	p := principal(r)
	id := newID()
	_, err = a.h.changeCall(r.Context(), func(tx pgx.Tx) (string, string, map[string]any, error) {
		_, err := tx.Exec(r.Context(), `INSERT INTO calls(id,topic,status,started,host_user_id,last_seq,event_path,scheduled_at,zoom_meeting_id) VALUES($1,$2,'scheduled',NULL,$3,0,'',$4,$5)`, id, topic, p.User.ID, in.ScheduledAt, meetingID)
		return id, "call.scheduled", map[string]any{"scheduled_at": in.ScheduledAt}, err
	})
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			jsonError(w, 409, "this Zoom link already has a pending or live call; use its existing call page or ask its owner to share it")
			return
		}
		jsonError(w, 500, "could not save scheduled call")
		return
	}
	c, err := a.s.Q.GetCall(r.Context(), id)
	if err != nil {
		jsonError(w, 500, "database error")
		return
	}
	writeJSON(w, 201, a.toCall(r.Context(), c, p))
}

var errCallConflict = errors.New("call is no longer scheduled")

func (a *Server) cancelScheduledCall(w http.ResponseWriter, r *http.Request) {
	c, ok := a.authorizedCall(w, r)
	if !ok {
		return
	}
	if c.HostUserID != principal(r).User.ID {
		jsonError(w, 403, "only the person who added this call can cancel it")
		return
	}
	_, err := a.h.changeCall(r.Context(), func(tx pgx.Tx) (string, string, map[string]any, error) {
		tag, err := tx.Exec(r.Context(), `UPDATE calls SET status='cancelled',ended=now() WHERE id=$1 AND status='scheduled'`, c.ID)
		if err == nil && tag.RowsAffected() != 1 {
			err = errCallConflict
		}
		return c.ID, "call.cancelled", map[string]any{}, err
	})
	if errors.Is(err, errCallConflict) {
		jsonError(w, 409, err.Error())
		return
	}
	if err != nil {
		jsonError(w, 500, "database error")
		return
	}
	writeJSON(w, 200, map[string]string{"status": "cancelled"})
}

// Only the authenticated recorder attaches live Zoom identities. A reusable
// meeting number selects its explicitly registered next session; the immutable
// runtime UUID and stream IDs then own correlation, including after restart.
func (a *Server) attachRTMSStream(w http.ResponseWriter, r *http.Request) {
	if !principal(r).User.IsAdmin {
		jsonError(w, 403, "admin only")
		return
	}
	var in struct {
		MeetingID   string `json:"meeting_id"`
		MeetingUUID string `json:"meeting_uuid"`
		StreamID    string `json:"rtms_stream_id"`
	}
	if decode(r, &in) != nil || !meetingNumber.MatchString(in.MeetingID) || in.MeetingUUID == "" || in.StreamID == "" || len(in.MeetingUUID) > 256 || len(in.StreamID) > 256 {
		jsonError(w, 400, "meeting number, runtime UUID and stream ID are required")
		return
	}
	id, err := a.h.changeCall(r.Context(), func(tx pgx.Tx) (string, string, map[string]any, error) {
		var id, status string
		err := tx.QueryRow(r.Context(), `SELECT c.id,c.status FROM rtms_streams s JOIN calls c ON c.id=s.call_id WHERE s.stream_id=$1 AND c.zoom_meeting_uuid=$2 AND c.zoom_meeting_id=$3`, in.StreamID, in.MeetingUUID, in.MeetingID).Scan(&id, &status)
		if err == nil {
			return id, "", nil, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", "", nil, err
		}
		err = tx.QueryRow(r.Context(), `SELECT id,status FROM calls WHERE zoom_meeting_uuid=$1 AND zoom_meeting_id=$2 FOR UPDATE`, in.MeetingUUID, in.MeetingID).Scan(&id, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(r.Context(), `SELECT id,status FROM calls WHERE zoom_meeting_id=$1 AND status='scheduled' FOR UPDATE`, in.MeetingID).Scan(&id, &status)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			id = newID()
			status = "scheduled"
			_, err = tx.Exec(r.Context(), `INSERT INTO calls(id,topic,status,host_user_id,last_seq,event_path,zoom_meeting_id) VALUES($1,$2,'scheduled',$3,0,'',$4)`, id, "Live Zoom meeting · "+in.MeetingID, principal(r).User.ID, in.MeetingID)
		}
		if err != nil {
			return "", "", nil, err
		}
		if status != "scheduled" && status != "live" {
			return id, "", nil, nil
		}
		_, err = tx.Exec(r.Context(), `INSERT INTO rtms_streams(stream_id,call_id) VALUES($1,$2)`, in.StreamID, id)
		if err != nil {
			return "", "", nil, err
		}
		if status == "live" {
			return id, "", nil, nil
		}
		_, err = tx.Exec(r.Context(), `UPDATE calls SET status='live',started=now(),zoom_meeting_uuid=$2 WHERE id=$1`, id, in.MeetingUUID)
		return id, "stream.started", map[string]any{"source": "zoom", "meeting_uuid": in.MeetingUUID}, err
	})
	if err != nil {
		jsonError(w, 409, "could not attach stream to this meeting")
		return
	}
	c, err := a.s.Q.GetCall(r.Context(), id)
	if err != nil {
		jsonError(w, 500, "database error")
		return
	}
	var streamStatus string
	err = a.s.Pool.QueryRow(r.Context(), `SELECT status FROM rtms_streams WHERE stream_id=$1`, in.StreamID).Scan(&streamStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		streamStatus = c.Status
	} else if err != nil {
		jsonError(w, 500, "database error")
		return
	}
	writeJSON(w, 200, struct {
		Call
		StreamStatus string `json:"stream_status"`
	}{Call: a.toCall(r.Context(), c, principal(r)), StreamStatus: streamStatus})
}

func (a *Server) stopRTMSStream(w http.ResponseWriter, r *http.Request) {
	if !principal(r).User.IsAdmin {
		jsonError(w, 403, "admin only")
		return
	}
	var in struct {
		Status string `json:"status"`
	}
	if decode(r, &in) != nil || (in.Status != "stopped" && in.Status != "interrupted") {
		jsonError(w, 400, "invalid stream status")
		return
	}
	_, err := a.h.changeCall(r.Context(), func(tx pgx.Tx) (string, string, map[string]any, error) {
		var id string
		if _, err := tx.Exec(r.Context(), `INSERT INTO rtms_streams(stream_id,status) VALUES($1,$2) ON CONFLICT DO NOTHING`, r.PathValue("streamId"), in.Status); err != nil {
			return "", "", nil, err
		}
		err := tx.QueryRow(r.Context(), `UPDATE rtms_streams SET status=$2 WHERE stream_id=$1 AND status='live' RETURNING call_id`, r.PathValue("streamId"), in.Status).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", nil, nil
		}
		if err != nil {
			return "", "", nil, err
		}
		var active bool
		err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM rtms_streams WHERE call_id=$1 AND status='live')`, id).Scan(&active)
		if err != nil || active {
			return id, "", nil, err
		}
		_, err = tx.Exec(r.Context(), `UPDATE calls SET status=$2,ended=now() WHERE id=$1 AND status='live'`, id, in.Status)
		return id, "stream." + in.Status, map[string]any{"reason": fmt.Sprintf("Zoom stream %s", in.Status)}, err
	})
	if err != nil {
		jsonError(w, 500, "database error")
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func callOpen(status string) bool { return status == "scheduled" || status == "live" }
func (a *Server) canShareCall(ctx context.Context, id, userID string) bool {
	c, err := a.s.Q.GetCall(ctx, id)
	return err == nil && (c.HostUserID == userID || a.isParticipant(ctx, id, userID))
}
