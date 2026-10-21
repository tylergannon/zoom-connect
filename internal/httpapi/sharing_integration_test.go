package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestShareWithAnyEmailDomain(t *testing.T) {
	a, s := eventTestServer(t, testDatabase(t))
	owner := eventLogin(t, s)
	call := eventTestCall(t, s, owner)
	base := s.URL + "/api/calls/" + call.ID

	for _, email := range []string{"not-an-email", "person@", "one@example.com,two@example.com"} {
		body, _ := json.Marshal(map[string]string{"email": email})
		r := request(t, s.Client(), "POST", base+"/shares", owner, strings.NewReader(string(body)))
		r.Body.Close()
		if r.StatusCode != 400 {
			t.Fatalf("invalid email %q: %s", email, r.Status)
		}
	}
	// A recipient need not exist yet, and their email domain is unrestricted.
	r := request(t, s.Client(), "POST", base+"/shares", owner, strings.NewReader(`{"email":" Outside+Call@Example.NET "}`))
	var grant struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&grant); err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 201 || grant.Email != "outside+call@example.net" {
		t.Fatalf("external share: %s %+v", r.Status, grant)
	}
	_, err := a.s.Pool.Exec(context.Background(), `INSERT INTO users(id,email,name) VALUES('u-outside','outside+call@example.net','Outside Person')`)
	if err != nil {
		t.Fatal(err)
	}
	r = request(t, s.Client(), "POST", s.URL+"/api/demo/login", nil, strings.NewReader(`{"email":"outside+call@example.net"}`))
	var recipient *http.Cookie
	for _, cookie := range r.Cookies() {
		if cookie.Name == "zc_session" {
			recipient = cookie
		}
	}
	r.Body.Close()
	if recipient == nil {
		t.Fatal("recipient login failed")
	}
	for _, path := range []string{"", "/events", "/transcript?format=txt"} {
		r = request(t, s.Client(), "GET", base+path, recipient, nil)
		r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatalf("external recipient %s: %s", path, r.Status)
		}
	}
	r = request(t, s.Client(), "GET", base, nil, nil)
	r.Body.Close()
	if r.StatusCode != 401 {
		t.Fatalf("anonymous access: %s", r.Status)
	}
	r = request(t, s.Client(), "POST", base+"/shares", recipient, strings.NewReader(`{"email":"another@elsewhere.org"}`))
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatalf("nonparticipant sharing: %s", r.Status)
	}
}
