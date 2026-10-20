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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/tylergannon/zoom-connect/internal/store/db"
)

var invitationURLs = regexp.MustCompile(`https?://[^\s<>"\x{201c}\x{201d}]+`)
var meetingPath = regexp.MustCompile(`^/(?:j|s|wc/join)/([0-9]{9,11})/?$|^/wc/([0-9]{9,11})/join/?$`)
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
		if !allowed || len(match) == 0 {
			continue
		}
		number := match[1]
		if number == "" {
			number = match[2]
		}
		if id != "" && id != number {
			return "", errors.New("paste an invitation for just one Zoom meeting")
		}
		id = number
	}
	if id == "" {
		return "", errors.New("paste a Zoom invitation containing an https://…zoom.us/j/… meeting link")
	}
	return id, nil
}

type emitCallEvent func(string, string, map[string]any) error

// Lifecycle updates and every affected call's events commit before publication.
func (h *hub) changeCall(ctx context.Context, change func(pgx.Tx, emitCallEvent) (string, string, map[string]any, error)) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	tx, err := h.s.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(context.Background())
	type publication struct {
		id    string
		event Event
	}
	var publications []publication
	emit := func(id, kind string, data map[string]any) error {
		q := db.New(tx)
		seq, err := q.ReserveEventSeq(ctx, id)
		if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		ev := Event{Seq: seq, TS: now, Recv: now, Kind: kind, Data: data}
		params, err := eventParams(id, ev)
		if err != nil {
			return err
		}
		if err = q.InsertCallEvent(ctx, params); err != nil {
			return err
		}
		publications = append(publications, publication{id, ev})
		return nil
	}
	id, kind, data, err := change(tx, emit)
	if err != nil {
		return "", err
	}
	if kind != "" {
		if err = emit(id, kind, data); err != nil {
			return "", err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	for _, p := range publications {
		for ch := range h.subs[p.id] {
			select {
			case ch <- p.event:
			default:
				delete(h.subs[p.id], ch)
				close(ch)
			}
		}
	}
	return id, nil
}

// Registration is a trusted capture operation. Read access still comes from a
// participant identity or a share, never from the host/creator column alone.
func grantCaptureOwner(ctx context.Context, tx pgx.Tx, id string, p Principal) error {
	_, err := db.New(tx).CreateShare(ctx, db.CreateShareParams{ID: newID(), CallID: id, Lower: p.User.Email, GrantedBy: p.User.ID})
	return err
}

func (a *Server) scheduleCall(w http.ResponseWriter, r *http.Request) {
	if !principal(r).User.IsAdmin {
		jsonError(w, 403, "only admins can register Zoom calls")
		return
	}
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
	_, err = a.h.changeCall(r.Context(), func(tx pgx.Tx, emit emitCallEvent) (string, string, map[string]any, error) {
		_, err := tx.Exec(r.Context(), `INSERT INTO calls(id,topic,status,started,host_user_id,last_seq,event_path,scheduled_at,zoom_meeting_id) VALUES($1,$2,'scheduled',NULL,$3,0,'',$4,$5)`, id, topic, p.User.ID, in.ScheduledAt, meetingID)
		if err == nil {
			err = grantCaptureOwner(r.Context(), tx, id, p)
		}
		return id, "call.scheduled", map[string]any{"scheduled_at": in.ScheduledAt}, err
	})
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			jsonError(w, 409, "this Zoom link already has a pending call; use its existing call page or ask its owner to share it")
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
	_, err := a.h.changeCall(r.Context(), func(tx pgx.Tx, emit emitCallEvent) (string, string, map[string]any, error) {
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

// Runtime UUIDs bind sessions; meeting numbers only select pending registrations.
func (a *Server) attachRTMSStream(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !p.User.IsAdmin {
		jsonError(w, 403, "admin only")
		return
	}
	var in zoomStreamIdentity
	if decode(r, &in) != nil || (in.MeetingID != "" && !meetingNumber.MatchString(in.MeetingID)) || in.MeetingUUID == "" || in.StreamID == "" || len(in.MeetingUUID) > 256 || len(in.StreamID) > 256 {
		jsonError(w, 400, "runtime UUID and stream ID are required; meeting number is optional")
		return
	}
	id, err := a.h.changeCall(r.Context(), func(tx pgx.Tx, emit emitCallEvent) (string, string, map[string]any, error) {
		var existing pgtype.Text
		var streamStatus string
		err := tx.QueryRow(r.Context(), `SELECT call_id,status FROM rtms_streams WHERE stream_id=$1 FOR UPDATE`, in.StreamID).Scan(&existing, &streamStatus)
		if err == nil {
			if !existing.Valid {
				return "", "", nil, nil
			} // Durable stop-before-start tombstone.
			var uuid string
			var superseded pgtype.Text
			err = tx.QueryRow(r.Context(), `SELECT zoom_meeting_uuid,superseded_by_uuid FROM calls WHERE id=$1`, existing.String).Scan(&uuid, &superseded)
			if err == nil && uuid != in.MeetingUUID {
				err = errStreamIdentity
			}
			if err == nil && streamStatus == "interrupted" && !superseded.Valid {
				_, err = tx.Exec(r.Context(), `UPDATE rtms_streams SET status='live',interrupted_at=NULL WHERE stream_id=$1`, in.StreamID)
				if err == nil {
					_, err = tx.Exec(r.Context(), `UPDATE calls SET status='live',ended=NULL WHERE id=$1`, existing.String)
				}
				return existing.String, "stream.started", map[string]any{"source": "zoom", "meeting_uuid": in.MeetingUUID}, err
			}
			return existing.String, "", nil, err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", "", nil, err
		}
		var id, status string
		var number, superseded pgtype.Text
		err = tx.QueryRow(r.Context(), `SELECT id,status,zoom_meeting_id,superseded_by_uuid FROM calls WHERE zoom_meeting_uuid=$1 FOR UPDATE`, in.MeetingUUID).Scan(&id, &status, &number, &superseded)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", "", nil, err
		}
		knownRuntime := err == nil
		if knownRuntime && in.MeetingID != "" && number.Valid && number.String != in.MeetingID {
			return "", "", nil, errStreamIdentity
		}
		if knownRuntime && superseded.Valid {
			_, err = tx.Exec(r.Context(), `INSERT INTO rtms_streams(stream_id,call_id,status) VALUES($1,$2,'interrupted')`, in.StreamID, id)
			return id, "", nil, err
		}
		if in.MeetingID == "" && number.Valid {
			in.MeetingID = number.String
		}
		// A new runtime UUID supersedes a stale live instance of the same room.
		// A pending registration is independent of the old instance's stop delivery.
		if in.MeetingID != "" {
			rows, err := tx.Query(r.Context(), `SELECT id FROM calls WHERE zoom_meeting_id=$1 AND zoom_meeting_uuid IS DISTINCT FROM $2 AND status='live' FOR UPDATE`, in.MeetingID, in.MeetingUUID)
			if err != nil {
				return "", "", nil, err
			}
			var superseded []string
			for rows.Next() {
				var id string
				if err = rows.Scan(&id); err != nil {
					rows.Close()
					return "", "", nil, err
				}
				superseded = append(superseded, id)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return "", "", nil, err
			}
			for _, id := range superseded {
				if _, err = tx.Exec(r.Context(), `UPDATE calls SET status='interrupted',ended=now() WHERE id=$1`, id); err != nil {
					return "", "", nil, err
				}
				if _, err = tx.Exec(r.Context(), `UPDATE rtms_streams SET status='interrupted' WHERE call_id=$1 AND status='live'`, id); err != nil {
					return "", "", nil, err
				}
				if err = emit(id, "stream.interrupted", map[string]any{"reason": "a new Zoom session started on this meeting link"}); err != nil {
					return "", "", nil, err
				}
			}
		}
		if in.MeetingID != "" {
			if _, err := tx.Exec(r.Context(), `UPDATE calls SET superseded_by_uuid=$2 WHERE zoom_meeting_id=$1 AND zoom_meeting_uuid IS NOT NULL AND zoom_meeting_uuid<>$2 AND superseded_by_uuid IS NULL`, in.MeetingID, in.MeetingUUID); err != nil {
				return "", "", nil, err
			}
		}
		if knownRuntime {
			err = nil
		} else {
			err = pgx.ErrNoRows
		}
		if errors.Is(err, pgx.ErrNoRows) && in.MeetingID != "" {
			err = tx.QueryRow(r.Context(), `SELECT id,status,zoom_meeting_id FROM calls WHERE zoom_meeting_id=$1 AND status='scheduled' FOR UPDATE`, in.MeetingID).Scan(&id, &status, &number)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			id = newID()
			status = "scheduled"
			_, err = tx.Exec(r.Context(), `INSERT INTO calls(id,topic,status,host_user_id,last_seq,event_path,zoom_meeting_id) VALUES($1,$2,'scheduled',$3,0,'',NULLIF($4,''))`, id, "Live Zoom meeting", p.User.ID, in.MeetingID)
			if err == nil {
				err = grantCaptureOwner(r.Context(), tx, id, p)
			}
		}
		if err != nil {
			return "", "", nil, err
		}
		_, err = tx.Exec(r.Context(), `INSERT INTO rtms_streams(stream_id,call_id) VALUES($1,$2)`, in.StreamID, id)
		if err != nil {
			return "", "", nil, err
		}
		if status == "live" {
			return id, "", nil, nil
		}
		// A fresh stream may resume a runtime. Final stopped stream IDs stay closed.
		_, err = tx.Exec(r.Context(), `UPDATE calls SET status='live',started=COALESCE(started,now()),ended=NULL,zoom_meeting_uuid=$2,zoom_meeting_id=COALESCE(zoom_meeting_id,NULLIF($3,'')) WHERE id=$1`, id, in.MeetingUUID, in.MeetingID)
		return id, "stream.started", map[string]any{"source": "zoom", "meeting_uuid": in.MeetingUUID}, err
	})
	if err != nil {
		var pe *pgconn.PgError
		if errors.Is(err, errStreamIdentity) || (errors.As(err, &pe) && pe.Code == "23505") {
			jsonError(w, 409, "stream identity conflict")
		} else {
			jsonError(w, 500, "could not attach Zoom stream")
		}
		return
	}
	var streamStatus string
	if err = a.s.Pool.QueryRow(r.Context(), `SELECT status FROM rtms_streams WHERE stream_id=$1`, in.StreamID).Scan(&streamStatus); err != nil {
		jsonError(w, 500, "database error")
		return
	}
	if id == "" {
		writeJSON(w, 200, map[string]string{"status": streamStatus, "stream_status": streamStatus})
		return
	}
	c, err := a.s.Q.GetCall(r.Context(), id)
	if err != nil {
		jsonError(w, 500, "database error")
		return
	}
	writeJSON(w, 200, struct {
		Call
		StreamStatus string `json:"stream_status"`
	}{a.toCall(r.Context(), c, p), streamStatus})
}

type zoomStreamIdentity struct {
	MeetingID   string `json:"meeting_id"`
	MeetingUUID string `json:"meeting_uuid"`
	StreamID    string `json:"rtms_stream_id"`
}

var errStreamIdentity = errors.New("stream identity conflict")

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
	_, err := a.h.changeCall(r.Context(), func(tx pgx.Tx, emit emitCallEvent) (string, string, map[string]any, error) {
		var id string
		if _, err := tx.Exec(r.Context(), `INSERT INTO rtms_streams(stream_id,status) VALUES($1,$2) ON CONFLICT DO NOTHING`, r.PathValue("streamId"), in.Status); err != nil {
			return "", "", nil, err
		}
		err := tx.QueryRow(r.Context(), `UPDATE rtms_streams SET status=$2,interrupted_at=CASE WHEN $2='interrupted' THEN now() ELSE NULL END WHERE stream_id=$1 AND (status='live' OR (status='interrupted' AND $2='stopped')) RETURNING call_id`, r.PathValue("streamId"), in.Status).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", nil, nil
		}
		if err != nil {
			return "", "", nil, err
		}
		if in.Status == "interrupted" {
			return id, "stream.paused", map[string]any{"reason": "Zoom connection interrupted; waiting up to two minutes for recovery"}, nil
		}
		var active bool
		err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM rtms_streams WHERE call_id=$1 AND status IN ('live','interrupted'))`, id).Scan(&active)
		if err != nil || active {
			return id, "", nil, err
		}
		tag, err := tx.Exec(r.Context(), `UPDATE calls SET status=$2,ended=now() WHERE id=$1 AND status='live'`, id, in.Status)
		if err == nil && tag.RowsAffected() == 0 {
			return id, "", nil, nil
		}
		return id, "stream." + in.Status, map[string]any{"reason": fmt.Sprintf("Zoom stream %s", in.Status)}, err
	})
	if err != nil {
		jsonError(w, 500, "database error")
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

// Deadlines are persisted, so a process restart cannot reset the recovery window.
// Requests and open SSE connections sweep expired interruptions; no transcript
// inactivity timeout is applied to healthy streams.
func (a *Server) expireRTMSInterruptions(ctx context.Context) error {
	var due bool
	if err := a.s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM rtms_streams WHERE status='interrupted' AND interrupted_at < now()-interval '2 minutes')`).Scan(&due); err != nil || !due {
		return err
	}
	_, err := a.h.changeCall(ctx, func(tx pgx.Tx, emit emitCallEvent) (string, string, map[string]any, error) {
		rows, err := tx.Query(ctx, `UPDATE rtms_streams SET status='stopped',interrupted_at=NULL WHERE status='interrupted' AND interrupted_at < now()-interval '2 minutes' RETURNING call_id`)
		if err != nil {
			return "", "", nil, err
		}
		ids := map[string]bool{}
		for rows.Next() {
			var id pgtype.Text
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return "", "", nil, err
			}
			if id.Valid {
				ids[id.String] = true
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return "", "", nil, err
		}
		for id := range ids {
			tag, err := tx.Exec(ctx, `UPDATE calls SET status='interrupted',ended=now() WHERE id=$1 AND status='live' AND NOT EXISTS(SELECT 1 FROM rtms_streams WHERE call_id=$1 AND status IN ('live','interrupted'))`, id)
			if err != nil {
				return "", "", nil, err
			}
			if tag.RowsAffected() > 0 {
				if err = emit(id, "stream.interrupted", map[string]any{"reason": "Zoom recovery window expired"}); err != nil {
					return "", "", nil, err
				}
			}
		}
		return "", "", nil, nil
	})
	return err
}

func callOpen(status string) bool { return status == "scheduled" || status == "live" }
func (a *Server) canShareCall(ctx context.Context, id, userID string) bool {
	c, err := a.s.Q.GetCall(ctx, id)
	return err == nil && ((c.HostUserID == userID && a.canAccess(ctx, id, userID)) || a.isParticipant(ctx, id, userID))
}
