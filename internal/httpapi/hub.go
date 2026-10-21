package httpapi

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/tylergannon/zoom-connect/internal/store"
	"github.com/tylergannon/zoom-connect/internal/store/db"
)

// Postgres owns history; the hub only owns this instance's live subscriptions.
// The mutex spans commit and fanout, and replay and registration, so subscribers
// cannot miss a commit between catching up and following live.
type hub struct {
	mu   sync.Mutex
	subs map[string]map[chan Event]bool
	s    *store.Store
}

func newHub(s *store.Store) *hub {
	return &hub{subs: map[string]map[chan Event]bool{}, s: s}
}
func decodeEvent(seq, ts, recv int64, kind string, participant, data []byte) (Event, error) {
	e := Event{Seq: seq, TS: ts, Recv: recv, Kind: kind}
	if len(participant) > 0 {
		if err := json.Unmarshal(participant, &e.Participant); err != nil {
			return e, err
		}
	}
	err := json.Unmarshal(data, &e.Data)
	return e, err
}
func (h *hub) snapshot(ctx context.Context, id string, from int64) ([]Event, error) {
	rows, err := h.s.Q.ListCallEvents(ctx, db.ListCallEventsParams{CallID: id, Seq: from})
	if err != nil {
		return nil, err
	}
	out := make([]Event, 0, len(rows))
	for _, r := range rows {
		e, err := decodeEvent(r.Seq, r.Ts, r.Recv, r.Kind, r.Participant, r.Data)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}
func (h *hub) replaySubscribe(ctx context.Context, id string, from int64) ([]Event, chan Event, func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	out, err := h.snapshot(ctx, id, from)
	if err != nil {
		return nil, nil, nil, err
	}
	c := make(chan Event, 256)
	if h.subs[id] == nil {
		h.subs[id] = map[chan Event]bool{}
	}
	h.subs[id][c] = true
	return out, c, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.subs[id][c] {
			delete(h.subs[id], c)
			close(c)
		}
		if len(h.subs[id]) == 0 {
			delete(h.subs, id)
		}
	}, nil
}
func eventParams(id string, ev Event) (db.InsertCallEventParams, error) {
	p, err := json.Marshal(ev.Participant)
	if err != nil {
		return db.InsertCallEventParams{}, err
	}
	d, err := json.Marshal(ev.Data)
	return db.InsertCallEventParams{CallID: id, Seq: ev.Seq, Ts: ev.TS, Recv: ev.Recv, Kind: ev.Kind, Participant: p, Data: d}, err
}

// append preserves sequence and timestamps when seeding historical events.
func (h *hub) append(ctx context.Context, id string, ev Event) error {
	_, err := h.persist(ctx, id, ev, false)
	return err
}
func (h *hub) appendNext(ctx context.Context, id, kind string, p *Person, data map[string]any) (Event, error) {
	now := time.Now().UTC().UnixMilli()
	return h.persist(ctx, id, Event{TS: now, Recv: now, Kind: kind, Participant: p, Data: data}, true)
}
func (h *hub) persist(ctx context.Context, id string, ev Event, next bool) (Event, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	tx, err := h.s.Pool.Begin(ctx)
	if err != nil {
		return Event{}, err
	}
	defer tx.Rollback(context.Background())
	q := h.s.Q.WithTx(tx)
	if next {
		ev.Seq, err = q.ReserveEventSeq(ctx, id)
	} else {
		err = q.AdvanceEventSeq(ctx, db.AdvanceEventSeqParams{ID: id, LastSeq: ev.Seq})
	}
	if err != nil {
		return Event{}, err
	}
	params, err := eventParams(id, ev)
	if err != nil {
		return Event{}, err
	}
	if err = q.InsertCallEvent(ctx, params); err != nil {
		return Event{}, err
	}
	if ev.Kind == "stream.stopped" || ev.Kind == "stream.interrupted" {
		status := "stopped"
		if ev.Kind == "stream.interrupted" {
			status = "interrupted"
		}
		if err = q.StopCall(ctx, db.StopCallParams{ID: id, Status: status}); err != nil {
			return Event{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Event{}, err
	}
	for c := range h.subs[id] {
		select {
		case c <- ev:
		default:
			delete(h.subs[id], c)
			close(c)
		}
	}
	return ev, nil
}
func ts(t time.Time) int64 { return t.UnixMilli() }
