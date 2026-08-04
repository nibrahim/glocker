package stats

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"glocker/internal/store"
)

// sessionCookie is the httpOnly cookie holding the opaque browser session token.
const sessionCookie = "glockpeek_session"

// secureCookies is set from Options; true marks session cookies Secure (for a
// hosted instance served over HTTPS, even when TLS is terminated by a proxy).
var secureCookies bool

type ctxKey int

const userKey ctxKey = 0

// userFrom returns the authenticated user attached by requireUser/requireToken.
func userFrom(r *http.Request) *store.User {
	u, _ := r.Context().Value(userKey).(*store.User)
	return u
}

// withUser stores the user on the request context.
func withUser(r *http.Request, u *store.User) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), userKey, u))
}

// requireUser gates a handler behind a valid browser session (cookie).
func requireUser(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		u, err := db.UserBySession(c.Value)
		if err != nil {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		h(w, withUser(r, u))
	}
}

// requireToken gates a handler behind a valid API bearer token (the syncer).
func requireToken(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := bearer(r)
		u, err := db.UserByAPIToken(tok)
		if err != nil {
			http.Error(w, "invalid or missing API token", http.StatusUnauthorized)
			return
		}
		h(w, withUser(r, u))
	}
}

// bearer extracts a token from "Authorization: Bearer <tok>".
func bearer(r *http.Request) string {
	const p = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) > len(p) && h[:len(p)] == p {
		return h[len(p):]
	}
	return ""
}

// handleLogin authenticates and starts a session (public route).
func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8*1024))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	u, err := db.Authenticate(in.Username, in.Password)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCredentials) {
			http.Error(w, "invalid username or password", http.StatusUnauthorized)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tok, err := db.CreateSession(u.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		Secure:   secureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(store.SessionTTL.Seconds()),
	})
	writeJSON(w, map[string]any{"user": map[string]any{"id": u.ID, "username": u.Username}})
}

// handleLogout ends the current session (requires a session).
func handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = db.DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", HttpOnly: true,
		Secure: secureCookies, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
	writeJSON(w, map[string]any{"ok": true})
}

// handleMe returns the current account (requires a session).
func handleMe(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	writeJSON(w, map[string]any{"id": u.ID, "username": u.Username})
}
