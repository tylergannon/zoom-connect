package main

// Optional demo bridge: forward actual RTMS text into the existing web app.
// Calls are attributed to the owner of the configured web application API key.
import (
	"bytes"
	"encoding/json"
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
	mu        sync.Mutex
	base, key string
	client    *http.Client
	calls     map[string]string
	closed    map[string]bool
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
	return &liveBridge{base: strings.TrimRight(base, "/"), key: key, client: &http.Client{Timeout: 5 * time.Second}, calls: map[string]string{}, closed: map[string]bool{}}
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
		return fmt.Errorf("web app returned %s", resp.Status)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

func (b *liveBridge) call(streamID string) (string, error) {
	if id := b.calls[streamID]; id != "" {
		return id, nil
	}
	var result struct {
		ID string `json:"id"`
	}
	err := b.post("/api/demo/calls", map[string]any{"topic": "Live Zoom meeting · " + time.Now().Format("3:04 PM"), "source": "zoom"}, &result)
	if err != nil {
		return "", err
	}
	if result.ID == "" {
		return "", fmt.Errorf("web app returned no call")
	}
	b.calls[streamID] = result.ID
	log.Printf("live Zoom call: %s/calls/%s", b.base, result.ID)
	return result.ID, nil
}

func (b *liveBridge) start(streamID string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, err := b.call(streamID); err != nil {
		log.Printf("live bridge start: %v", err)
	}
}

func (b *liveBridge) text(streamID, kind string, content map[string]any) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed[streamID] {
		return
	}
	id, err := b.call(streamID)
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

func (b *liveBridge) stop(streamID string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed[streamID] {
		return
	}
	if id := b.calls[streamID]; id != "" {
		if err := b.post("/api/demo/calls/"+id+"/stop", map[string]string{}, nil); err != nil {
			log.Printf("live bridge stop: %v", err)
			return
		}
	}
	b.closed[streamID] = true
}
