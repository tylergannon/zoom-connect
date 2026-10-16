package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const screenURLLifetime = 10 * time.Minute
const maxScreenBytes = 4 << 20

type screenStorage interface {
	put(context.Context, string, []byte) error
	sign(context.Context, string) (string, error)
}

type r2Screens struct {
	base, token string
	client      *http.Client
}

func (a *Server) configureScreens() error {
	base, token := os.Getenv("R2_MEDIA_URL"), os.Getenv("R2_MEDIA_TOKEN")
	if base == "" && token == "" {
		return nil
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.Host == "" || token == "" {
		return fmt.Errorf("R2_MEDIA_URL must be HTTPS and R2_MEDIA_TOKEN is required")
	}
	a.screens = &r2Screens{base: strings.TrimRight(base, "/"), token: token, client: &http.Client{Timeout: 10 * time.Second}}
	return nil
}
func (s *r2Screens) put(ctx context.Context, key string, data []byte) error {
	req, err := http.NewRequestWithContext(ctx, "PUT", s.base+"/"+key, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "image/jpeg")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("R2 upload unavailable")
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != 201 {
		return fmt.Errorf("R2 upload returned %d", resp.StatusCode)
	}
	return nil
}
func (s *r2Screens) sign(ctx context.Context, key string) (string, error) {
	expires := strconv.FormatInt(time.Now().Add(screenURLLifetime).Unix(), 10)
	mac := hmac.New(sha256.New, []byte(s.token))
	fmt.Fprintf(mac, "%s\n%s", key, expires)
	return s.base + "/" + key + "?expires=" + expires + "&signature=" + hex.EncodeToString(mac.Sum(nil)), nil
}

// Signed URLs belong to delivery, not durable history. Replays mint fresh links.
func (a *Server) screenEvent(ctx context.Context, ev Event) (Event, error) {
	if ev.Kind != "screen.frame" {
		return ev, nil
	}
	if a.screens == nil {
		return ev, fmt.Errorf("screen storage is not configured")
	}
	key, ok := ev.Data["object_key"].(string)
	if !ok || key == "" {
		return ev, fmt.Errorf("screen frame has no object key")
	}
	url, err := a.screens.sign(ctx, key)
	if err != nil {
		return ev, err
	}
	data := make(map[string]any, len(ev.Data)+2)
	for k, v := range ev.Data {
		if k != "object_key" {
			data[k] = v
		}
	}
	data["url"], data["expires_at"] = url, time.Now().UTC().Add(screenURLLifetime)
	ev.Data = data
	return ev, nil
}

func (a *Server) addScreenFrame(w http.ResponseWriter, r *http.Request) {
	c, ok := a.authorizedCall(w, r)
	if !ok {
		return
	}
	if !principal(r).User.IsAdmin || principal(r).User.ID != c.HostUserID {
		jsonError(w, 403, "call host only")
		return
	}
	if c.Status != "live" {
		jsonError(w, 409, "call has ended")
		return
	}
	if a.screens == nil {
		jsonError(w, 503, "screen storage is not configured")
		return
	}
	stamp, err := strconv.ParseInt(r.Header.Get("X-Zoom-Timestamp"), 10, 64)
	if err != nil || stamp <= 0 {
		jsonError(w, 400, "Zoom timestamp required")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxScreenBytes))
	if err != nil {
		jsonError(w, 413, "image exceeds 4 MiB")
		return
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width > 4096 || cfg.Height > 4096 {
		jsonError(w, 400, "valid JPEG up to 4096 pixels required")
		return
	}
	key := "screens/" + c.ID + "/" + newID() + ".jpg"
	if err := a.screens.put(r.Context(), key, data); err != nil {
		jsonError(w, 502, "image upload failed")
		return
	}
	p := &Person{ID: r.Header.Get("X-Zoom-User-ID"), Name: r.Header.Get("X-Zoom-User-Name")}
	now := time.Now().UTC().UnixMilli()
	ev, err := a.h.persist(r.Context(), c.ID, Event{TS: stamp, Recv: now, Kind: "screen.frame", Participant: p, Data: map[string]any{"object_key": key, "width": cfg.Width, "height": cfg.Height, "bytes": len(data), "source": "zoom"}}, true)
	if err != nil {
		jsonError(w, 500, "frame metadata could not be saved")
		return
	}
	ev, err = a.screenEvent(r.Context(), ev)
	if err != nil {
		jsonError(w, 500, "image link could not be signed")
		return
	}
	writeJSON(w, 201, ev)
}

func (a *Server) screenFrameLink(w http.ResponseWriter, r *http.Request) {
	c, ok := a.authorizedCall(w, r)
	if !ok {
		return
	}
	seq, err := strconv.ParseInt(r.PathValue("seq"), 10, 64)
	if err != nil {
		jsonError(w, 404, "frame not found")
		return
	}
	var data []byte
	err = a.s.Pool.QueryRow(r.Context(), `SELECT data FROM call_events WHERE call_id=$1 AND seq=$2 AND kind='screen.frame'`, c.ID, seq).Scan(&data)
	if err != nil {
		jsonError(w, 404, "frame not found")
		return
	}
	ev := Event{Seq: seq, Kind: "screen.frame"}
	if json.Unmarshal(data, &ev.Data) != nil {
		jsonError(w, 500, "invalid frame metadata")
		return
	}
	ev, err = a.screenEvent(r.Context(), ev)
	if err != nil {
		jsonError(w, 503, "image signing unavailable")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, ev.Data)
}
