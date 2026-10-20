package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
)

// At 1 fps a synchronous upload bounds memory and never blocks transcript sockets.
func (b *liveBridge) screen(streamID string, content map[string]any) {
	if b == nil {
		return
	}
	encoded, _ := content["data"].(string)
	if len(encoded) > 6<<20 {
		log.Print("screen frame exceeds capture limit")
		return
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		log.Print("screen frame: invalid base64")
		return
	}
	s := b.state(streamID)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	id, err := b.call(streamID, s)
	s.mu.Unlock()
	if err != nil {
		log.Printf("screen bridge call: %v", err)
		return
	}
	req, err := http.NewRequest("POST", b.base+"/api/demo/calls/"+id+"/screenshots", bytes.NewReader(data))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+b.key)
	req.Header.Set("Content-Type", "image/jpeg")
	stamp := fmt.Sprint(content["timestamp"])
	if value, ok := content["timestamp"].(float64); ok {
		stamp = strconv.FormatInt(int64(value), 10)
	}
	req.Header.Set("X-Zoom-Timestamp", stamp)
	req.Header.Set("X-Zoom-User-ID", fmt.Sprint(content["user_id"]))
	if name, ok := content["user_name"].(string); ok {
		req.Header.Set("X-Zoom-User-Name", name)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		log.Print("screen upload failed")
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusCreated {
		log.Printf("screen upload returned %s", strconv.Itoa(resp.StatusCode))
	}
}
