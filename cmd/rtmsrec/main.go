// rtmsrec is a spike tool: it answers Zoom's RTMS webhooks, connects to the
// signaling and media sockets, and records every message verbatim so the real
// protocol shapes can be checked against nlspec.md (deferred decision 1).
//
// It is deliberately a single file with no abstractions. Nothing here is meant
// to survive into the gateway.
package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

// Message types, from zoom/rtms src/rtms.h.
const (
	msgSignalingHandshakeReq  = 1
	msgSignalingHandshakeResp = 2
	msgDataHandshakeReq       = 3
	msgDataHandshakeResp      = 4
	msgEventSubscription      = 5
	msgEventUpdate            = 6
	msgClientReadyAck         = 7
	msgStreamStateUpdate      = 8
	msgSessionStateUpdate     = 9
	msgKeepAliveReq           = 12
	msgKeepAliveResp          = 13
	msgMediaDataAudio         = 14
	msgMediaDataTranscript    = 17
	msgMediaDataChat          = 18

	streamStateTerminated = 4
)

var mediaTypes = map[string]int{
	"audio":      1,
	"video":      2,
	"deskshare":  4,
	"transcript": 8,
	"chat":       16,
}

type config struct {
	clientID      string
	clientSecret  string
	webhookSecret string
	listenAddr    string
	recDir        string
	media         []string
	audioCodec    string // l16 | opus
	audioDataOpt  string // mixed | separate
	bufferData    bool
	bridge        *liveBridge
}

func loadConfig() config {
	c := config{
		clientID:      os.Getenv("ZOOM_CLIENT_ID"),
		clientSecret:  os.Getenv("ZOOM_CLIENT_SECRET"),
		webhookSecret: os.Getenv("ZOOM_WEBHOOK_SECRET"),
		listenAddr:    envOr("LISTEN_ADDR", ":8080"),
		recDir:        envOr("REC_DIR", "recordings"),
		audioCodec:    envOr("RTMS_AUDIO_CODEC", "l16"),
		audioDataOpt:  envOr("RTMS_AUDIO_DATA_OPT", "separate"),
		bufferData:    envOr("RTMS_BUFFER_DATA", "true") == "true",
		bridge:        bridgeFromEnv(),
	}
	for _, m := range strings.Split(envOr("RTMS_MEDIA", "transcript,chat"), ",") {
		m = strings.TrimSpace(strings.ToLower(m))
		if _, ok := mediaTypes[m]; !ok {
			log.Fatalf("RTMS_MEDIA: unknown media type %q", m)
		}
		c.media = append(c.media, m)
	}
	for k, v := range map[string]string{"ZOOM_CLIENT_ID": c.clientID, "ZOOM_CLIENT_SECRET": c.clientSecret, "ZOOM_WEBHOOK_SECRET": c.webhookSecret} {
		if v == "" {
			log.Fatalf("%s is required", k)
		}
	}
	return c
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// ---------------------------------------------------------------------------
// Recording

// recorder appends JSONL lines to one file per socket, plus raw audio bytes
// per participant. Every line is {"t": <ms>, "dir": "in"|"out", "msg": <json>}.
type recorder struct {
	dir   string
	mu    sync.Mutex
	files map[string]*os.File
	seen  map[string]int
}

func newRecorder(dir string) (*recorder, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &recorder{dir: dir, files: map[string]*os.File{}, seen: map[string]int{}}, nil
}

func (r *recorder) file(name string) (*os.File, error) {
	if f, ok := r.files[name]; ok {
		return f, nil
	}
	f, err := os.OpenFile(filepath.Join(r.dir, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	r.files[name] = f
	return f, nil
}

func (r *recorder) line(socket, dir string, msg json.RawMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := r.file(socket + ".jsonl")
	if err != nil {
		log.Printf("record: %v", err)
		return
	}
	entry, _ := json.Marshal(map[string]any{"t": time.Now().UnixMilli(), "dir": dir, "msg": msg})
	f.Write(append(entry, '\n'))
}

func (r *recorder) bytes(name string, b []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := r.file(name)
	if err != nil {
		log.Printf("record: %v", err)
		return
	}
	f.Write(b)
}

// noteShape logs the first occurrence of each (socket, msg_type) so the console
// shows every distinct message shape once, in full.
func (r *recorder) noteShape(socket string, msgType int, raw []byte) {
	r.mu.Lock()
	key := fmt.Sprintf("%s/%d", socket, msgType)
	r.seen[key]++
	n := r.seen[key]
	r.mu.Unlock()
	if n == 1 {
		log.Printf("FIRST %s msg_type=%d: %s", socket, msgType, truncate(raw, 1500))
	}
}

func (r *recorder) summary() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var b strings.Builder
	for k, n := range r.seen {
		fmt.Fprintf(&b, "  %-24s %d\n", k, n)
	}
	return b.String()
}

func (r *recorder) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, f := range r.files {
		f.Close()
	}
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + fmt.Sprintf("...(%d bytes)", len(b))
}

// ---------------------------------------------------------------------------
// One RTMS stream

type stream struct {
	cfg         config
	meetingUUID string
	streamID    string
	rec         *recorder
	ctx         context.Context
	cancel      context.CancelFunc

	sigMu sync.Mutex
	sig   *websocket.Conn

	mediaMu sync.Mutex
	media   map[string]*websocket.Conn
}

func (s *stream) signature() string {
	mac := hmac.New(sha256.New, []byte(s.cfg.clientSecret))
	mac.Write([]byte(s.cfg.clientID + "," + s.meetingUUID + "," + s.streamID))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *stream) sendSignaling(msg any) {
	raw, _ := json.Marshal(msg)
	s.rec.line("signaling", "out", raw)
	s.sigMu.Lock()
	defer s.sigMu.Unlock()
	if s.sig != nil {
		if err := s.sig.WriteMessage(websocket.TextMessage, raw); err != nil {
			log.Printf("[%s] signaling write: %v", s.streamID, err)
		}
	}
}

func (s *stream) runSignaling(serverURL string) {
	defer s.closeAll()
	log.Printf("[%s] signaling: dialing %s", s.streamID, serverURL)
	conn, resp, err := websocket.DefaultDialer.DialContext(s.ctx, serverURL, nil)
	if err != nil {
		log.Printf("[%s] signaling dial failed: %v (resp=%v)", s.streamID, err, resp)
		return
	}
	s.sigMu.Lock()
	s.sig = conn
	s.sigMu.Unlock()
	defer conn.Close()

	s.sendSignaling(map[string]any{
		"msg_type":         msgSignalingHandshakeReq,
		"protocol_version": 1,
		"meeting_uuid":     s.meetingUUID,
		"rtms_stream_id":   s.streamID,
		"sequence":         rand.Intn(1e9),
		"signature":        s.signature(),
		"buffer_data":      s.cfg.bufferData,
	})

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			log.Printf("[%s] signaling closed: %v", s.streamID, err)
			return
		}
		s.rec.line("signaling", "in", raw)
		var msg struct {
			MsgType     int `json:"msg_type"`
			StatusCode  int `json:"status_code"`
			Reason      any `json:"reason"`
			Timestamp   any `json:"timestamp"`
			State       int `json:"state"`
			MediaServer struct {
				ServerURLs map[string]string `json:"server_urls"`
			} `json:"media_server"`
			Event struct {
				EventType int `json:"event_type"`
			} `json:"event"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			log.Printf("[%s] signaling: unparseable message: %s", s.streamID, truncate(raw, 300))
			continue
		}
		s.rec.noteShape("signaling", msg.MsgType, raw)

		switch msg.MsgType {
		case msgSignalingHandshakeResp:
			if msg.StatusCode != 0 {
				log.Printf("[%s] signaling handshake FAILED status=%d reason=%v", s.streamID, msg.StatusCode, msg.Reason)
				return
			}
			log.Printf("[%s] signaling handshake ok; media urls: %v", s.streamID, msg.MediaServer.ServerURLs)
			// Subscribe to every documented event type so we learn their shapes.
			var events []map[string]any
			for t := 1; t <= 9; t++ {
				events = append(events, map[string]any{"event_type": t, "subscribe": true})
			}
			s.sendSignaling(map[string]any{"msg_type": msgEventSubscription, "events": events})
			for _, name := range s.cfg.media {
				u, ok := msg.MediaServer.ServerURLs[name]
				if !ok || u == "" {
					log.Printf("[%s] no media url for %q (scopes/params?)", s.streamID, name)
					continue
				}
				go s.runMedia(name, u)
			}
		case msgKeepAliveReq:
			s.sendSignaling(map[string]any{"msg_type": msgKeepAliveResp, "timestamp": msg.Timestamp})
		case msgEventUpdate:
			log.Printf("[%s] event_type=%d: %s", s.streamID, msg.Event.EventType, truncate(raw, 400))
		case msgStreamStateUpdate:
			log.Printf("[%s] stream state=%d reason=%v", s.streamID, msg.State, msg.Reason)
			if msg.State == streamStateTerminated {
				return
			}
		case msgSessionStateUpdate:
			log.Printf("[%s] session state=%d: %s", s.streamID, msg.State, truncate(raw, 300))
		}
	}
}

func (s *stream) mediaParams(name string) map[string]any {
	codec := 1 // L16
	if s.cfg.audioCodec == "opus" {
		codec = 4
	}
	dataOpt := 2 // separate
	if s.cfg.audioDataOpt == "mixed" {
		dataOpt = 1
	}
	params := map[string]map[string]any{
		"audio":      {"content_type": 2, "sample_rate": 1, "channel": 1, "codec": codec, "data_opt": dataOpt, "send_rate": 20},
		"video":      {"content_type": 3, "codec": 7, "data_opt": 3, "resolution": 2, "fps": 25},
		"deskshare":  {"content_type": 3, "codec": 5, "resolution": 2, "fps": 1},
		"transcript": {"content_type": 5, "src_language": 9, "enable_lid": true},
		"chat":       {"content_type": 5},
	}
	return map[string]any{name: params[name]}
}

func (s *stream) runMedia(name, u string) {
	socket := "media-" + name
	log.Printf("[%s] %s: dialing %s", s.streamID, socket, u)
	conn, _, err := websocket.DefaultDialer.DialContext(s.ctx, u, nil)
	if err != nil {
		log.Printf("[%s] %s dial failed: %v", s.streamID, socket, err)
		return
	}
	s.mediaMu.Lock()
	s.media[name] = conn
	s.mediaMu.Unlock()
	defer conn.Close()

	var writeMu sync.Mutex
	send := func(msg any) {
		raw, _ := json.Marshal(msg)
		s.rec.line(socket, "out", raw)
		writeMu.Lock()
		defer writeMu.Unlock()
		conn.WriteMessage(websocket.TextMessage, raw)
	}

	send(map[string]any{
		"msg_type":           msgDataHandshakeReq,
		"protocol_version":   1,
		"meeting_uuid":       s.meetingUUID,
		"rtms_stream_id":     s.streamID,
		"signature":          s.signature(),
		"media_type":         mediaTypes[name],
		"payload_encryption": false,
		"media_params":       s.mediaParams(name),
	})

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			log.Printf("[%s] %s closed: %v", s.streamID, socket, err)
			return
		}
		var msg map[string]any
		if err := json.Unmarshal(raw, &msg); err != nil {
			log.Printf("[%s] %s: unparseable: %s", s.streamID, socket, truncate(raw, 300))
			continue
		}
		msgType, _ := msg["msg_type"].(float64)
		mt := int(msgType)

		// Audio payloads are large; keep the JSON line readable by moving the
		// bytes into a per-participant file and leaving only the byte count.
		if mt == msgMediaDataAudio {
			if content, ok := msg["content"].(map[string]any); ok {
				if data, ok := content["data"].(string); ok {
					b, _ := base64.StdEncoding.DecodeString(data)
					content["data"] = nil
					content["data_len"] = len(b)
					s.rec.bytes(fmt.Sprintf("audio-%v.%s", content["user_id"], s.cfg.audioCodec), b)
					raw, _ = json.Marshal(msg)
				}
			}
		}
		s.rec.line(socket, "in", raw)
		s.rec.noteShape(socket, mt, raw)

		switch mt {
		case msgDataHandshakeResp:
			status, _ := msg["status_code"].(float64)
			if status != 0 {
				log.Printf("[%s] %s handshake FAILED: %s", s.streamID, socket, truncate(raw, 400))
				return
			}
			log.Printf("[%s] %s handshake ok; sending CLIENT_READY_ACK", s.streamID, socket)
			s.sendSignaling(map[string]any{"msg_type": msgClientReadyAck, "rtms_stream_id": s.streamID})
		case msgKeepAliveReq:
			send(map[string]any{"msg_type": msgKeepAliveResp, "timestamp": msg["timestamp"]})
		case msgMediaDataTranscript:
			if c, ok := msg["content"].(map[string]any); ok {
				s.cfg.bridge.text(s.streamID, "transcript", c)
				log.Printf("[%s] %v: %v", s.streamID, c["user_name"], c["data"])
			}
		case msgMediaDataChat:
			if c, ok := msg["content"].(map[string]any); ok {
				s.cfg.bridge.text(s.streamID, "chat", c)
				log.Printf("[%s] chat %v: %v", s.streamID, c["user_name"], truncate([]byte(fmt.Sprint(c["data"])), 300))
			}
		}
	}
}

func (s *stream) closeAll() {
	s.cancel()
	s.sigMu.Lock()
	if s.sig != nil {
		s.sig.Close()
	}
	s.sigMu.Unlock()
	s.mediaMu.Lock()
	for _, c := range s.media {
		c.Close()
	}
	s.mediaMu.Unlock()
	log.Printf("[%s] closed. message counts:\n%s", s.streamID, s.rec.summary())
	s.rec.close()
}

// ---------------------------------------------------------------------------
// Webhook

type server struct {
	cfg     config
	mu      sync.Mutex
	streams map[string]*stream
	hooks   *recorder
}

func (sv *server) webhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read", http.StatusBadRequest)
		return
	}
	var hook struct {
		Event   string         `json:"event"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(body, &hook); err != nil {
		http.Error(w, "json", http.StatusBadRequest)
		return
	}

	// Record every delivery, verified or not, with the headers we care about.
	entry, _ := json.Marshal(map[string]any{
		"t":         time.Now().UnixMilli(),
		"signature": r.Header.Get("x-zm-signature"),
		"timestamp": r.Header.Get("x-zm-request-timestamp"),
		"body":      json.RawMessage(body),
	})
	sv.hooks.bytes("webhooks.jsonl", append(entry, '\n'))

	if hook.Event == "endpoint.url_validation" {
		plain, _ := hook.Payload["plainToken"].(string)
		mac := hmac.New(sha256.New, []byte(sv.cfg.webhookSecret))
		mac.Write([]byte(plain))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"plainToken": plain, "encryptedToken": hex.EncodeToString(mac.Sum(nil))})
		log.Printf("webhook: answered url_validation")
		return
	}
	if err := sv.verify(r, body); err != nil {
		log.Printf("webhook: REJECTED %s: %v", hook.Event, err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.WriteHeader(http.StatusOK)
	log.Printf("webhook: %s %s", hook.Event, truncate(body, 1200))

	meetingUUID, _ := hook.Payload["meeting_uuid"].(string)
	streamID, _ := hook.Payload["rtms_stream_id"].(string)

	switch hook.Event {
	case "meeting.rtms_started":
		// Docs show server_urls as a string; Zoom's own test fixtures show an
		// object keyed by socket. Accept both and record which one arrived.
		var serverURL string
		switch v := hook.Payload["server_urls"].(type) {
		case string:
			serverURL = v
		case map[string]any:
			serverURL, _ = v["signaling"].(string)
			log.Printf("webhook: server_urls is an OBJECT: %v", v)
		}
		if serverURL == "" || meetingUUID == "" || streamID == "" {
			log.Printf("webhook: rtms_started missing fields; not connecting")
			return
		}
		sv.start(meetingUUID, streamID, serverURL)
	case "meeting.rtms_stopped", "meeting.rtms_interrupted":
		sv.cfg.bridge.stop(streamID)
		sv.mu.Lock()
		s := sv.streams[streamID]
		delete(sv.streams, streamID)
		sv.mu.Unlock()
		if s != nil && hook.Event == "meeting.rtms_stopped" {
			s.closeAll()
		}
	}
}

func (sv *server) verify(r *http.Request, body []byte) error {
	sig := r.Header.Get("x-zm-signature")
	ts := r.Header.Get("x-zm-request-timestamp")
	if sig == "" || ts == "" {
		return errors.New("missing signature headers")
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return errors.New("bad timestamp")
	}
	if d := time.Since(time.Unix(sec, 0)); d > 5*time.Minute || d < -5*time.Minute {
		return fmt.Errorf("timestamp off by %s", d)
	}
	mac := hmac.New(sha256.New, []byte(sv.cfg.webhookSecret))
	mac.Write([]byte("v0:" + ts + ":"))
	mac.Write(body)
	if !hmac.Equal([]byte(sig), []byte("v0="+hex.EncodeToString(mac.Sum(nil)))) {
		return errors.New("signature mismatch")
	}
	return nil
}

func (sv *server) start(meetingUUID, streamID, serverURL string) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	if _, ok := sv.streams[streamID]; ok {
		log.Printf("[%s] already connected; ignoring duplicate rtms_started", streamID)
		return
	}
	rec, err := newRecorder(filepath.Join(sv.cfg.recDir, time.Now().UTC().Format("20060102-150405")+"-"+safe(streamID)))
	if err != nil {
		log.Printf("recorder: %v", err)
		return
	}
	meta, _ := json.Marshal(map[string]any{"meeting_uuid": meetingUUID, "rtms_stream_id": streamID, "server_url": serverURL, "media": sv.cfg.media, "audio_codec": sv.cfg.audioCodec, "audio_data_opt": sv.cfg.audioDataOpt})
	rec.bytes("meta.json", meta)
	ctx, cancel := context.WithCancel(context.Background())
	s := &stream{cfg: sv.cfg, meetingUUID: meetingUUID, streamID: streamID, rec: rec, ctx: ctx, cancel: cancel, media: map[string]*websocket.Conn{}}
	sv.streams[streamID] = s
	sv.cfg.bridge.start(streamID)
	go s.runSignaling(serverURL)
}

func safe(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, s)
}

func main() {
	cfg := loadConfig()
	hooks, err := newRecorder(cfg.recDir)
	if err != nil {
		log.Fatal(err)
	}
	sv := &server{cfg: cfg, streams: map[string]*stream{}, hooks: hooks}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /zoom/webhook", sv.webhook)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok\n")) })
	srv := &http.Server{Addr: cfg.listenAddr, Handler: mux}

	go func() {
		log.Printf("rtmsrec listening on %s (media=%v audio=%s/%s rec=%s)", cfg.listenAddr, cfg.media, cfg.audioCodec, cfg.audioDataOpt, cfg.recDir)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	log.Printf("shutting down")
	sv.mu.Lock()
	for _, s := range sv.streams {
		s.closeAll()
	}
	sv.mu.Unlock()
	hooks.close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}
