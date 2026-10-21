package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/tylergannon/zoom-connect/internal/store/db"
)

type passwordLogin struct {
	email  string
	digest [32]byte
}

func (a *Server) configureLogin(ctx context.Context) error {
	mode := env("AUTH_MODE", "demo")
	if mode == "demo" {
		if strings.HasPrefix(os.Getenv("PUBLIC_URL"), "https://") {
			return fmt.Errorf("public deployment requires AUTH_MODE=password")
		}
		return nil
	}
	if mode != "password" {
		return fmt.Errorf("unsupported AUTH_MODE")
	}
	email := strings.ToLower(strings.TrimSpace(os.Getenv("DEMO_LOGIN_EMAIL")))
	password := os.Getenv("DEMO_LOGIN_PASSWORD")
	if !strings.Contains(email, "@") || len(password) < 20 {
		return fmt.Errorf("password mode requires DEMO_LOGIN_EMAIL and a DEMO_LOGIN_PASSWORD of at least 20 characters")
	}
	a.login = &passwordLogin{email: email, digest: sha256.Sum256([]byte(password))}
	_, err := a.s.Pool.Exec(ctx, `INSERT INTO users(id,email,name,is_admin) VALUES($1,$2,$3,true) ON CONFLICT(email) DO UPDATE SET is_admin=true`, newID(), email, env("DEMO_LOGIN_NAME", "Demo account"))
	return err
}
func (a *Server) authOptions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]bool{"password": a.login != nil, "demo": a.login == nil})
}
func (a *Server) passwordSignIn(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if a.login == nil {
		jsonError(w, 404, "not found")
		return
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if decode(r, &in) != nil {
		jsonError(w, 400, "invalid body")
		return
	}
	// Fixed-size comparisons avoid password-dependent timing. The deployment uses
	// a generated high-entropy password from Doppler, never a user database hash.
	digest := sha256.Sum256([]byte(in.Password))
	emailDigest := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(in.Email))))
	wantEmail := sha256.Sum256([]byte(a.login.email))
	passwordOK := subtle.ConstantTimeCompare(digest[:], a.login.digest[:])
	emailOK := subtle.ConstantTimeCompare(emailDigest[:], wantEmail[:])
	if passwordOK&emailOK != 1 {
		jsonError(w, 401, "invalid email or password")
		return
	}
	u, err := a.s.Q.GetUserByEmail(r.Context(), a.login.email)
	if err != nil {
		jsonError(w, 500, "database error")
		return
	}
	a.createLoginSession(w, r, user(u))
}
func (a *Server) createLoginSession(w http.ResponseWriter, r *http.Request, u User) {
	id := randomSecret(32)
	expires := time.Now().Add(24 * time.Hour)
	if err := a.s.Q.CreateSession(r.Context(), db.CreateSessionParams{ID: id, UserID: u.ID, ExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true}}); err != nil {
		jsonError(w, 500, "database error")
		return
	}
	secure := r.TLS != nil || strings.HasPrefix(os.Getenv("PUBLIC_URL"), "https://")
	http.SetCookie(w, &http.Cookie{Name: "zc_session", Value: id, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secure, Expires: expires})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, u)
}
