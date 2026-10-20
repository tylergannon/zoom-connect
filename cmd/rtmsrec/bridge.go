package main

// Optional demo bridge: forward actual RTMS text into the existing web app.
// Calls are attributed to the owner of the configured web application API key.
import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type liveBridge struct {
	mu                      sync.Mutex
	base, key               string
	client                  *http.Client
	streams                 map[string]*bridgeStream
	retryWindow, retryDelay time.Duration
}
type bridgeStream struct {
	mu         sync.Mutex
	callID     string
	closed     bool
	paused     bool
	identity   zoomIdentity
	identified bool
	standalone bool
}

func (b *liveBridge) state(id string) *bridgeStream {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.streams == nil {
		b.streams = map[string]*bridgeStream{}
	}
	if b.streams[id] == nil {
		b.streams[id] = &bridgeStream{}
	}
	return b.streams[id]
}

func bridgeFromEnv() *liveBridge {
	base, key := os.Getenv("ZOOM_CONNECT_URL"), os.Getenv("ZOOM_CONNECT_KEY")
	if base == "" || key == "" {
		return nil
	}
	u, err := url.Parse(base)
	if err != nil {
		log.Fatal("invalid local web app address")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "http" || (u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())) {
		log.Fatal("demo bridge must target a loopback HTTP address")
	}
	return &liveBridge{base: strings.TrimRight(base, "/"), key: key, client: &http.Client{Timeout: 5 * time.Second}, streams: map[string]*bridgeStream{}}
}

func (b *liveBridge) post(path string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", b.base+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+b.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		io.Copy(io.Discard, resp.Body)
		return bridgeHTTPError{resp.StatusCode}
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

type bridgeHTTPError struct{ Status int }

func (e bridgeHTTPError) Error() string { return fmt.Sprintf("web app returned HTTP %d", e.Status) }

type zoomIdentity struct {
	MeetingID   string `json:"meeting_id"`
	MeetingUUID string `json:"meeting_uuid"`
	StreamID    string `json:"rtms_stream_id"`
}

// Each stream serializes its own attachment and text, so a temporary app
// failure holds that stream's opening frames without stalling other meetings.
func (b *liveBridge) call(streamID string, s *bridgeStream) (string, error) {
	if s.callID != "" {
		return s.callID, nil
	}
	var result struct {
		ID           string `json:"id"`
		Status       string `json:"status"`
		StreamStatus string `json:"stream_status"`
	}
	if s.identified && !s.standalone {
		window, delay := b.retryWindow, b.retryDelay
		if window == 0 {
			window = 30 * time.Second
		}
		if delay == 0 {
			delay = time.Second
		}
		deadline := time.Now().Add(window)
		for {
			err := b.post("/api/rtms/streams", s.identity, &result)
			if err == nil {
				if result.Status != "live" || (result.StreamStatus != "" && result.StreamStatus != "live") {
					s.closed = true
					return "", fmt.Errorf("Zoom session already ended")
				}
				break
			}
			var status bridgeHTTPError
			permanent := errors.As(err, &status) && status.Status >= 400 && status.Status < 500
			if permanent || !time.Now().Before(deadline) {
				s.standalone = true
				// A timed-out response may have committed an attachment. Retire that
				// mapping as well, even though capture continues in the fallback call.
				if !permanent {
					if stopErr := b.post("/api/rtms/streams/"+url.PathEscape(streamID)+"/stop", map[string]string{"status": "stopped"}, nil); stopErr != nil {
						log.Printf("ERROR stream %s: could not retire scheduled attachment: %v", streamID, stopErr)
					}
				}
				log.Printf("ERROR stream %s: scheduled attachment unavailable (%v); capturing a standalone call", streamID, err)
				break
			}
			wait := min(delay, time.Until(deadline))
			time.Sleep(wait)
			delay = min(delay*2, 5*time.Second)
		}
	}
	if !s.identified || s.standalone {
		result.ID = ""
		if err := b.post("/api/demo/calls", map[string]any{"topic": "Live Zoom meeting · " + time.Now().Format("3:04 PM"), "source": "zoom"}, &result); err != nil {
			return "", err
		}
	}
	if result.ID == "" {
		return "", fmt.Errorf("web app returned no call")
	}
	s.callID = result.ID
	log.Printf("live Zoom call: %s/calls/%s", b.base, result.ID)
	return result.ID, nil
}

func (b *liveBridge) start(streamID string, identity ...string) {
	if b == nil {
		return
	}
	s := b.state(streamID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(identity) == 2 {
		s.identity = zoomIdentity{MeetingID: identity[0], MeetingUUID: identity[1], StreamID: streamID}
		s.identified = true
	}
	if s.closed {
		return
	}
	if s.paused {
		s.callID = ""
		s.paused = false
	}
	if _, err := b.call(streamID, s); err != nil {
		log.Printf("live bridge start: %v", err)
	}
}

func (b *liveBridge) text(streamID, kind string, content map[string]any) {
	if b == nil {
		return
	}
	s := b.state(streamID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	id, err := b.call(streamID, s)
	if err != nil {
		log.Printf("live bridge call: %v", err)
		return
	}
	text, _ := content["data"].(string)
	if text == "" {
		return
	}
	name, _ := content["user_name"].(string)
	participantID := fmt.Sprint(content["user_id"])
	data := map[string]any{"text": text, "final": true, "source": "zoom", "zoom_timestamp": content["timestamp"]}
	if lang, ok := content["language"]; ok {
		data["lang"] = lang
	}
	body := map[string]any{"kind": kind, "participant": map[string]string{"id": participantID, "name": name}, "data": data}
	if err = b.post("/api/demo/calls/"+id+"/events", body, nil); err != nil {
		log.Printf("live bridge text: %v", err)
	}
}

func (b *liveBridge) stop(streamID string, status ...string) {
	if b == nil {
		return
	}
	s := b.state(streamID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if len(status) > 0 && !s.standalone {
		if err := b.post("/api/rtms/streams/"+url.PathEscape(streamID)+"/stop", map[string]string{"status": status[0]}, nil); err != nil {
			log.Printf("live bridge stop: %v", err)
			return
		}
	} else if id := s.callID; id != "" {
		if err := b.post("/api/demo/calls/"+id+"/stop", map[string]string{}, nil); err != nil {
			log.Printf("live bridge stop: %v", err)
			return
		}
	}
	if len(status) > 0 && status[0] == "interrupted" && !s.standalone {
		s.paused = true
	} else {
		s.closed = true
	}
}
