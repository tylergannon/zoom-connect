package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type hub struct {
	mu     sync.Mutex
	events map[string][]Event
	subs   map[string]map[chan Event]bool
	root   string
}

func newHub(root string) *hub {
	return &hub{events: map[string][]Event{}, subs: map[string]map[chan Event]bool{}, root: root}
}
func (h *hub) load(id, path string) {
	f, e := os.Open(path)
	if e != nil {
		return
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	var es []Event
	for {
		var ev Event
		if dec.Decode(&ev) != nil {
			break
		}
		es = append(es, ev)
	}
	h.mu.Lock()
	h.events[id] = es
	h.mu.Unlock()
}
func (h *hub) snapshot(id string, from int64) []Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Event, 0)
	for _, e := range h.events[id] {
		if e.Seq > from {
			out = append(out, e)
		}
	}
	return out
}
func (h *hub) replaySubscribe(id string, from int64) ([]Event, chan Event, func()) {
	h.mu.Lock()
	out := make([]Event, 0)
	for _, e := range h.events[id] {
		if e.Seq > from {
			out = append(out, e)
		}
	}
	c := make(chan Event, 256)
	if h.subs[id] == nil {
		h.subs[id] = map[chan Event]bool{}
	}
	h.subs[id][c] = true
	h.mu.Unlock()
	return out, c, func() {
		h.mu.Lock()
		if h.subs[id][c] {
			delete(h.subs[id], c)
			close(c)
		}
		h.mu.Unlock()
	}
}
func (h *hub) subscribe(id string) (chan Event, func()) {
	_, c, done := h.replaySubscribe(id, 1<<62)
	return c, done
}
func (h *hub) append(ctx context.Context, id string, ev Event) {
	b, _ := json.Marshal(ev)
	_ = os.MkdirAll(filepath.Join(h.root, "calls"), 0700)
	f, e := os.OpenFile(filepath.Join(h.root, "calls", id+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e == nil {
		_, _ = f.Write(append(b, '\n'))
		_ = f.Close()
	}
	h.mu.Lock()
	h.events[id] = append(h.events[id], ev)
	for c := range h.subs[id] {
		select {
		case c <- ev:
		default:
			delete(h.subs[id], c)
			close(c)
		}
	}
	h.mu.Unlock()
}
func (h *hub) appendNext(id, kind string, p *Person, data map[string]any) Event {
	now := time.Now().UTC()
	h.mu.Lock()
	ev := Event{Seq: int64(len(h.events[id]) + 1), TS: now.UnixMilli(), Recv: now.UnixMilli(), Kind: kind, Participant: p, Data: data}
	b, _ := json.Marshal(ev)
	_ = os.MkdirAll(filepath.Join(h.root, "calls"), 0700)
	f, err := os.OpenFile(filepath.Join(h.root, "calls", id+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err == nil {
		_, _ = f.Write(append(b, '\n'))
		_ = f.Close()
	}
	h.events[id] = append(h.events[id], ev)
	for c := range h.subs[id] {
		select {
		case c <- ev:
		default:
			delete(h.subs[id], c)
			close(c)
		}
	}
	h.mu.Unlock()
	return ev
}
func ts(t time.Time) int64 { return t.UnixMilli() }
