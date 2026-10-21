package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestPasswordLoginDisablesPersonaBypass(t *testing.T) {
	dsn := testDatabase(t)
	a, s := eventTestServer(t, dsn)
	t.Setenv("AUTH_MODE", "password")
	t.Setenv("DEMO_LOGIN_EMAIL", "tyler@tylergannon.com")
	t.Setenv("DEMO_LOGIN_PASSWORD", "test-only-generated-long-secret-12345")
	t.Setenv("PUBLIC_URL", "https://example.test")
	if err := a.configureLogin(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/demo/users", "/api/demo/login"} {
		method := "GET"
		if strings.HasSuffix(path, "login") {
			method = "POST"
		}
		r := request(t, s.Client(), method, s.URL+path, nil, strings.NewReader(`{"email":"dana@acme.co"}`))
		r.Body.Close()
		if r.StatusCode != 404 {
			t.Fatalf("demo bypass exposed: %s %s", path, r.Status)
		}
	}
	for _, payload := range []string{`{"email":"tyler@tylergannon.com","password":"wrong"}`, `{"email":"other@example.com","password":"test-only-generated-long-secret-12345"}`} {
		r := request(t, s.Client(), "POST", s.URL+"/api/login", nil, strings.NewReader(payload))
		r.Body.Close()
		if r.StatusCode != 401 || len(r.Cookies()) != 0 {
			t.Fatalf("invalid credentials accepted: %s", r.Status)
		}
	}
	r := request(t, s.Client(), "POST", s.URL+"/api/login", nil, strings.NewReader(`{"email":"TYLER@TYLERGANNON.COM","password":"test-only-generated-long-secret-12345"}`))
	var u User
	if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 200 || u.Email != "tyler@tylergannon.com" || !u.IsAdmin {
		t.Fatalf("login failed: %s %+v", r.Status, u)
	}
	var session *http.Cookie
	for _, c := range r.Cookies() {
		if c.Name == "zc_session" {
			session = c
		}
	}
	if session == nil || !session.Secure || !session.HttpOnly || session.SameSite != http.SameSiteLaxMode {
		t.Fatalf("unsafe session cookie: %+v", session)
	}
	r = request(t, s.Client(), "GET", s.URL+"/api/me", session, nil)
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("session rejected: %s", r.Status)
	}
	r = request(t, s.Client(), "POST", s.URL+"/api/logout", session, strings.NewReader(`{}`))
	r.Body.Close()
	r = request(t, s.Client(), "GET", s.URL+"/api/me", session, nil)
	r.Body.Close()
	if r.StatusCode != 401 {
		t.Fatalf("logged-out session accepted: %s", r.Status)
	}
}

func TestPublicLoginFailsClosedWithoutCredentials(t *testing.T) {
	t.Setenv("AUTH_MODE", "password")
	t.Setenv("DEMO_LOGIN_EMAIL", "")
	t.Setenv("DEMO_LOGIN_PASSWORD", "")
	a := &Server{}
	if err := a.configureLogin(context.Background()); err == nil {
		t.Fatal("missing password config accepted")
	}
	t.Setenv("AUTH_MODE", "demo")
	t.Setenv("PUBLIC_URL", "https://example.test")
	if err := a.configureLogin(context.Background()); err == nil {
		t.Fatal("public simulated login accepted")
	}
}
