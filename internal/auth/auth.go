// Package auth provides a single-account login backed by environment variables,
// using a stateless signed cookie for the session (no server-side store, so it
// survives restarts and works with one or many machines).
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type ctxKey int

const adminKey ctxKey = 0

const cookieName = "ch_session"
const sessionTTL = 7 * 24 * time.Hour

// Authenticator checks credentials and issues/validates session cookies.
type Authenticator struct {
	username string
	password string
	secret   []byte
	secure   bool // Secure cookie flag (true behind HTTPS, e.g. on Fly.io)
}

// New builds an Authenticator. Set secure=true in production (HTTPS).
func New(username, password, secret string, secure bool) *Authenticator {
	return &Authenticator{
		username: username,
		password: password,
		secret:   []byte(secret),
		secure:   secure,
	}
}

// Check validates a username/password pair in constant time.
func (a *Authenticator) Check(username, password string) bool {
	u := subtle.ConstantTimeCompare([]byte(username), []byte(a.username)) == 1
	p := subtle.ConstantTimeCompare([]byte(password), []byte(a.password)) == 1
	return u && p
}

func (a *Authenticator) sign(value string) string {
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// IssueCookie sets a signed session cookie that expires after sessionTTL.
func (a *Authenticator) IssueCookie(w http.ResponseWriter) {
	exp := time.Now().Add(sessionTTL)
	val := strconv.FormatInt(exp.Unix(), 10)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    val + "." + a.sign(val),
		Path:     "/",
		Expires:  exp,
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearCookie logs the user out.
func (a *Authenticator) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *Authenticator) validCookie(value string) bool {
	i := strings.LastIndex(value, ".")
	if i < 0 {
		return false
	}
	payload, sig := value[:i], value[i+1:]
	if subtle.ConstantTimeCompare([]byte(sig), []byte(a.sign(payload))) != 1 {
		return false
	}
	exp, err := strconv.ParseInt(payload, 10, 64)
	if err != nil {
		return false
	}
	return time.Now().Unix() < exp
}

// Middleware injects the admin status into the request context for every request.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin := false
		if c, err := r.Cookie(cookieName); err == nil && a.validCookie(c.Value) {
			admin = true
		}
		ctx := context.WithValue(r.Context(), adminKey, admin)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// IsAdmin reports whether the current request is authenticated as admin.
// Used by both handlers and templ views (to show/hide edit controls).
func IsAdmin(ctx context.Context) bool {
	v, _ := ctx.Value(adminKey).(bool)
	return v
}

// RequireAdmin wraps a handler so non-admins get 403 Forbidden.
func RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !IsAdmin(r.Context()) {
			http.Error(w, "forbidden — admin login required", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}
