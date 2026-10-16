package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

type testScreens struct {
	data  map[string][]byte
	fail  bool
	nonce int
}

func (s *testScreens) put(ctx context.Context, k string, b []byte) error {
	if s.fail {
		return io.ErrUnexpectedEOF
	}
	s.data[k] = append([]byte{}, b...)
	return nil
}
func (s *testScreens) sign(ctx context.Context, k string) (string, error) {
	s.nonce++
	return "https://media.example/" + k + "?nonce=" + strconv.Itoa(s.nonce), nil
}

func TestScreenFramesPersistReplaySignAndPermissions(t *testing.T) {
	dsn := testDatabase(t)
	a, s := eventTestServer(t, dsn)
	media := &testScreens{data: map[string][]byte{}}
	a.screens = media
	cookie := eventLogin(t, s)
	call := eventTestCall(t, s, cookie)
	var jpg bytes.Buffer
	jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 20, 10)), nil)
	upload := func(data []byte) *http.Response {
		req, _ := http.NewRequest("POST", s.URL+"/api/demo/calls/"+call.ID+"/screenshots", bytes.NewReader(data))
		req.AddCookie(cookie)
		req.Header.Set("X-Zoom-Timestamp", "1792184000123")
		req.Header.Set("X-Zoom-User-Name", "Presenter")
		resp, err := s.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	resp := upload([]byte("not an image"))
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("invalid JPEG accepted: %s", resp.Status)
	}
	media.fail = true
	resp = upload(jpg.Bytes())
	resp.Body.Close()
	if resp.StatusCode != 502 {
		t.Fatalf("failed upload accepted: %s", resp.Status)
	}
	media.fail = false
	resp = upload(jpg.Bytes())
	var ev Event
	json.NewDecoder(resp.Body).Decode(&ev)
	resp.Body.Close()
	if resp.StatusCode != 201 || ev.Kind != "screen.frame" || ev.TS != 1792184000123 {
		t.Fatalf("capture: %s %+v", resp.Status, ev)
	}
	if ev.Data["object_key"] != nil || ev.Data["url"] == nil {
		t.Fatalf("delivery metadata: %+v", ev.Data)
	}
	rows, err := a.h.snapshot(context.Background(), call.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	stored := rows[len(rows)-1]
	key := stored.Data["object_key"].(string)
	if stored.Data["url"] != nil || !bytes.Equal(media.data[key], jpg.Bytes()) {
		t.Fatal("database must store only metadata; media store must retain exact bytes")
	}
	// Fresh handler/hub reconnects to the same durable metadata and signs a new URL.
	s.Close()
	restarted, server := eventTestServer(t, dsn)
	restarted.screens = media
	resp = request(t, server.Client(), "GET", server.URL+"/api/calls/"+call.ID+"/events", cookie, nil)
	var replay []Event
	json.NewDecoder(resp.Body).Decode(&replay)
	resp.Body.Close()
	if len(replay) != len(rows) || replay[len(replay)-1].Data["url"] == ev.Data["url"] {
		t.Fatal("replay did not renew image URL")
	}
	framePath := server.URL + "/api/calls/" + call.ID + "/screenshots/" + strconv.FormatInt(ev.Seq, 10)
	resp = request(t, server.Client(), "GET", framePath, nil, nil)
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal("anonymous image link allowed")
	}
	resp = request(t, server.Client(), "POST", server.URL+"/api/demo/login", nil, strings.NewReader(`{"email":"jordan@acme.co"}`))
	var outsider *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "zc_session" {
			outsider = c
		}
	}
	resp.Body.Close()
	resp = request(t, server.Client(), "GET", framePath, outsider, nil)
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("unshared image link allowed")
	}
	resp = request(t, server.Client(), "GET", framePath, cookie, nil)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal("host cannot renew image link")
	}
}
func TestMediaLinkTenMinuteSignature(t *testing.T) {
	s := &r2Screens{base: "https://media.example", token: "test-secret"}
	link, err := s.sign(context.Background(), "screens/ABC/DEF.jpg")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(link)
	expires, _ := strconv.ParseInt(u.Query().Get("expires"), 10, 64)
	if delta := expires - time.Now().Unix(); delta < 599 || delta > 600 {
		t.Fatalf("expiry %d seconds", delta)
	}
	if len(u.Query().Get("signature")) != 64 {
		t.Fatal("missing HMAC signature")
	}
}
