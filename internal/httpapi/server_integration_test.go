package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tylergannon/zoom-connect/internal/store"
)

func TestLoginSharePermissionsAndRevokedStreamKey(t *testing.T) {
	dsn := testDatabase(t)
	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("ARCHIVE_DIR", t.TempDir())
	h, err := New(http.NotFoundHandler())
	if err != nil {
		t.Skipf("local PostgreSQL is unavailable: %v", err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()
	login := func(email string) *http.Cookie {
		t.Helper()
		resp := request(t, ts.Client(), "POST", ts.URL+"/api/demo/login", nil, strings.NewReader(`{"email":"`+email+`"}`))
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("login %s: %s", email, resp.Status)
		}
		for _, c := range resp.Cookies() {
			if c.Name == "zc_session" {
				return c
			}
		}
		t.Fatal("login did not set session cookie")
		return nil
	}
	dana := login("dana@acme.co")
	resp := request(t, ts.Client(), "GET", ts.URL+"/api/calls", dana, nil)
	var seeded []Call
	if err := json.NewDecoder(resp.Body).Decode(&seeded); err != nil {
		resp.Body.Close()
		t.Fatalf("decode seeded calls: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || len(seeded) == 0 {
		t.Fatalf("list seeded calls: got %s with %d calls", resp.Status, len(seeded))
	}
	announceCtx, announceCancel := context.WithTimeout(context.Background(), time.Second)
	defer announceCancel()
	announceReq, _ := http.NewRequestWithContext(announceCtx, "GET", ts.URL+"/api/stream", nil)
	announceReq.AddCookie(dana)
	announceResp, err := ts.Client().Do(announceReq)
	if err != nil {
		t.Fatalf("open announcements stream: %v", err)
	}
	announceLine, err := bufio.NewReader(announceResp.Body).ReadString('\n')
	announceResp.Body.Close()
	if err != nil || announceLine != ": connected\n" {
		t.Fatalf("announcements did not flush connection comment: %q %v", announceLine, err)
	}
	resp = request(t, ts.Client(), "POST", ts.URL+"/zoom/webhook", nil, strings.NewReader(`{"event":"endpoint.url_validation","payload":{"plainToken":"integration-token"}}`))
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("Zoom webhook validation: %s", resp.Status)
	}
	resp = request(t, ts.Client(), "GET", ts.URL+"/api/admin/zoom", dana, nil)
	var zoomSettings map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&zoomSettings); err != nil {
		resp.Body.Close()
		t.Fatalf("decode Zoom settings: %v", err)
	}
	resp.Body.Close()
	lastWebhook, ok := zoomSettings["last_webhook"].(string)
	if resp.StatusCode != 200 || !ok {
		t.Fatalf("last_webhook must be an RFC3339 timestamp string, got %#v", zoomSettings["last_webhook"])
	}
	if _, err := time.Parse(time.RFC3339Nano, lastWebhook); err != nil {
		t.Fatalf("last_webhook is not RFC3339: %q", lastWebhook)
	}
	resp = request(t, ts.Client(), "POST", ts.URL+"/api/demo/calls", dana, strings.NewReader(`{"topic":"permission integration"}`))
	var call Call
	_ = json.NewDecoder(resp.Body).Decode(&call)
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("create call: %s", resp.Status)
	}
	jordan := login("jordan@acme.co")
	resp = request(t, ts.Client(), "GET", ts.URL+"/api/calls/"+call.ID, jordan, nil)
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("unshared call: got %s, want 403", resp.Status)
	}
	resp = request(t, ts.Client(), "POST", ts.URL+"/api/calls/"+call.ID+"/shares", dana, strings.NewReader(`{"email":"jordan@acme.co"}`))
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("share call: %s", resp.Status)
	}
	resp = request(t, ts.Client(), "GET", ts.URL+"/api/calls/"+call.ID, jordan, nil)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("shared call: got %s, want 200", resp.Status)
	}
	resp = request(t, ts.Client(), "GET", ts.URL+"/api/calls/"+call.ID+"/shares", jordan, nil)
	var access struct {
		Shares []struct {
			ID string `json:"id"`
		} `json:"shares"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&access)
	resp.Body.Close()
	if len(access.Shares) != 1 {
		t.Fatalf("share list has %d entries", len(access.Shares))
	}
	shareCtx, shareCancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer shareCancel()
	shareReq, _ := http.NewRequestWithContext(shareCtx, "GET", ts.URL+"/api/calls/"+call.ID+"/stream", nil)
	shareReq.AddCookie(jordan)
	shareResp, err := ts.Client().Do(shareReq)
	if err != nil {
		t.Fatal(err)
	}
	shareReader := bufio.NewReader(shareResp.Body)
	var shareLine string
	var shareReadErr error
	for {
		shareLine, shareReadErr = shareReader.ReadString('\n')
		if shareReadErr != nil || (shareLine != "\n" && !strings.HasPrefix(shareLine, ":")) {
			break
		}
	}
	if shareReadErr != nil || !strings.HasPrefix(shareLine, "id: ") {
		shareResp.Body.Close()
		t.Fatalf("shared SSE replay: %q %v", shareLine, shareReadErr)
	}
	resp = request(t, ts.Client(), "DELETE", ts.URL+"/api/calls/"+call.ID+"/shares/"+access.Shares[0].ID, dana, nil)
	resp.Body.Close()
	if resp.StatusCode != 204 {
		shareResp.Body.Close()
		t.Fatalf("revoke share: %s", resp.Status)
	}
	shareDone := make(chan error, 1)
	go func() { _, e := io.Copy(io.Discard, shareResp.Body); shareDone <- e }()
	select {
	case <-shareDone:
	case <-shareCtx.Done():
		shareResp.Body.Close()
		t.Fatal("open stream stayed authorized after share revocation")
	}
	shareResp.Body.Close()
	resp = request(t, ts.Client(), "GET", ts.URL+"/api/calls/"+call.ID, jordan, nil)
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("revoked share still grants access: %s", resp.Status)
	}
	resp = request(t, ts.Client(), "POST", ts.URL+"/api/keys", dana, strings.NewReader(`{"name":"revocation-test"}`))
	var made struct {
		Key struct {
			ID string `json:"id"`
		} `json:"key"`
		Secret string `json:"secret"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&made)
	resp.Body.Close()
	if resp.StatusCode != 201 || made.Secret == "" {
		t.Fatalf("create API key: %s", resp.Status)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/calls/"+call.ID+"/stream", nil)
	req.Header.Set("Authorization", "Bearer "+made.Secret)
	streamResp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(streamResp.Body)
	var line string
	var streamReadErr error
	for {
		line, streamReadErr = reader.ReadString('\n')
		if streamReadErr != nil || (line != "\n" && !strings.HasPrefix(line, ":")) {
			break
		}
	}
	if streamReadErr != nil || !strings.HasPrefix(line, "id: ") {
		streamResp.Body.Close()
		t.Fatalf("SSE replay did not include event id: %q %v", line, streamReadErr)
	}
	resp = request(t, ts.Client(), "DELETE", ts.URL+"/api/keys/"+made.Key.ID, dana, nil)
	resp.Body.Close()
	if resp.StatusCode != 204 {
		streamResp.Body.Close()
		t.Fatalf("revoke key: %s", resp.Status)
	}
	done := make(chan error, 1)
	go func() { _, e := io.Copy(io.Discard, streamResp.Body); done <- e }()
	select {
	case <-done:
	case <-ctx.Done():
		streamResp.Body.Close()
		t.Fatal("open stream stayed authorized after API key revocation")
	}
	streamResp.Body.Close()
}

func testDatabase(t *testing.T) string {
	t.Helper()
	dsn := "postgres://zoom:zoom@127.0.0.1:5432/zoom_connect_test?sslmode=disable"
	if override := os.Getenv("TEST_DATABASE_URL"); override != "" {
		dsn = override
	}
	parsed, err := url.Parse(dsn)
	if err != nil || !strings.Contains(parsed.Path, "_test") {
		t.Fatalf("refusing to use a non-test database for integration test: %q", dsn)
	}
	admin := *parsed
	admin.Path = "/postgres"
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	pool, e := pgxpool.New(ctx, admin.String())
	if e != nil {
		t.Skipf("local PostgreSQL is unavailable: %v", e)
	}
	defer pool.Close()
	dbName := strings.TrimPrefix(parsed.Path, "/")
	_, e = pool.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{dbName}.Sanitize())
	if e != nil && !strings.Contains(strings.ToLower(e.Error()), "already exists") {
		t.Skipf("cannot create isolated integration database: %v", e)
	}
	testStore, e := store.Open(ctx, dsn)
	if e != nil {
		t.Skipf("test PostgreSQL is unavailable: %v", e)
	}
	defer testStore.Close()
	_, e = testStore.Pool.Exec(ctx, `TRUNCATE settings,shares,call_participants,calls,api_keys,sessions,users CASCADE`)
	if e != nil {
		t.Fatalf("clear isolated test database: %v", e)
	}
	return dsn
}

func request(t *testing.T, c *http.Client, method, url string, cookie *http.Cookie, body io.Reader) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
