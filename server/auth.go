package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	sessionCookie = "sid"
	sessionTTL    = 30 * 24 * time.Hour
	slideEvery    = time.Hour // refresh expiry at most this often to keep writes rare
)

// argon2id parameters: OWASP minimum (19 MiB, t=2, p=1).
const (
	argonTime    = 2
	argonMemory  = 19 * 1024
	argonThreads = 1
	argonKeyLen  = 32
)

var errBadLogin = errors.New("Wrong username or password.")

func hashPassword(pw string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding.EncodeToString
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads, b64(salt), b64(key))
}

func verifyPassword(encoded, pw string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// withHashSlot bounds concurrent argon2 runs (19 MiB each) so a login burst can't blow the RAM budget.
func (a *App) withHashSlot(ctx context.Context, f func()) error {
	select {
	case a.hashSem <- struct{}{}:
		f()
		<-a.hashSem
		if len(a.hashSem) == 0 {
			debug.FreeOSMemory() // hand the 19 MiB back right away; logins are rare, so the GC cost is fine
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

var usernameRE = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,20}$`)

type User struct {
	ID       int64  `json:"id"`
	Name     string `json:"username"`
	Created  int64  `json:"createdAt"`
	Imported bool   `json:"imported"`
}

func tokenHash(tok string) []byte { h := sha256.Sum256([]byte(tok)); return h[:] }

func (a *App) setCookie(w http.ResponseWriter, tok string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: tok, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: a.cfg.CookieSecure, SameSite: http.SameSiteLaxMode,
	})
}

func (a *App) newSession(w http.ResponseWriter, r *http.Request, userID int64) error {
	raw := make([]byte, 32)
	rand.Read(raw)
	tok := base64.RawURLEncoding.EncodeToString(raw)
	now := a.now().UnixMilli()
	ua := r.UserAgent()
	if len(ua) > 200 {
		ua = ua[:200]
	}
	_, err := a.db.Exec(`INSERT INTO sessions(token_hash, user_id, created_at, last_seen, expires_at, user_agent, ip) VALUES (?,?,?,?,?,?,?)`,
		tokenHash(tok), userID, now, now, now+sessionTTL.Milliseconds(), ua, a.clientIP(r))
	if err == nil {
		a.setCookie(w, tok, int(sessionTTL.Seconds()))
	}
	return err
}

// auth returns the signed-in user (or nil) and slides the session expiry forward.
func (a *App) auth(w http.ResponseWriter, r *http.Request) (*User, []byte) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil, nil
	}
	h := tokenHash(c.Value)
	now := a.now().UnixMilli()
	var u User
	var imported sql.NullInt64
	var lastSeen int64
	err = a.db.QueryRow(`SELECT u.id, u.username, u.created_at, u.imported_at, s.last_seen
		FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.token_hash = ? AND s.expires_at > ?`, h, now).
		Scan(&u.ID, &u.Name, &u.Created, &imported, &lastSeen)
	if err != nil {
		return nil, nil
	}
	u.Imported = imported.Valid
	if now-lastSeen > slideEvery.Milliseconds() {
		a.db.Exec(`UPDATE sessions SET last_seen = ?, expires_at = ? WHERE token_hash = ?`, now, now+sessionTTL.Milliseconds(), h)
		if w != nil {
			a.setCookie(w, c.Value, int(sessionTTL.Seconds()))
		}
	}
	return &u, h
}

// need wraps a handler that requires a signed-in user.
func (a *App) need(h func(http.ResponseWriter, *http.Request, *User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, _ := a.auth(w, r)
		if u == nil {
			httpError(w, http.StatusUnauthorized, "Please sign in.")
			return
		}
		h(w, r, u)
	}
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email,omitempty"`
}

func (a *App) register(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if !readJSON(w, r, &c) {
		return
	}
	if !a.limit(w, limitRule{"reg-ip:" + a.clientIP(r), 5, time.Hour}) {
		return
	}
	c.Email = strings.TrimSpace(c.Email)
	switch {
	case !usernameRE.MatchString(c.Username):
		httpError(w, http.StatusBadRequest, "Username must be 3–20 letters, digits, dots, dashes or underscores.")
		return
	case len(c.Password) < 8 || len(c.Password) > 256:
		httpError(w, http.StatusBadRequest, "Password must be at least 8 characters.")
		return
	case c.Email != "" && (len(c.Email) > 254 || !strings.Contains(c.Email[1:], "@")):
		httpError(w, http.StatusBadRequest, "That email address doesn't look right.")
		return
	}
	var hash string
	if err := a.withHashSlot(r.Context(), func() { hash = hashPassword(c.Password) }); err != nil {
		return
	}
	var email any
	if c.Email != "" {
		email = c.Email
	}
	res, err := a.db.Exec(`INSERT INTO users(username, username_lc, email, pw_hash, created_at) VALUES (?,?,?,?,?)`,
		c.Username, strings.ToLower(c.Username), email, hash, a.now().UnixMilli())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			httpError(w, http.StatusConflict, "That username is taken.")
			return
		}
		a.fail(w, err)
		return
	}
	id, _ := res.LastInsertId()
	if err := a.newSession(w, r, id); err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, User{ID: id, Name: c.Username, Created: a.now().UnixMilli()})
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if !readJSON(w, r, &c) {
		return
	}
	lc := strings.ToLower(c.Username)
	if !a.limit(w, limitRule{"login-ip:" + a.clientIP(r), 20, 15 * time.Minute}, limitRule{"login-user:" + lc, 10, 15 * time.Minute}) {
		return
	}
	var u User
	var hash string
	var imported sql.NullInt64
	err := a.db.QueryRow(`SELECT id, username, created_at, pw_hash, imported_at FROM users WHERE username_lc = ?`, lc).
		Scan(&u.ID, &u.Name, &u.Created, &hash, &imported)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		a.fail(w, err)
		return
	}
	if hash == "" {
		hash = a.dummyHash // same work for unknown users, so timing doesn't reveal which names exist
	}
	ok := false
	if err := a.withHashSlot(r.Context(), func() { ok = verifyPassword(hash, c.Password) }); err != nil {
		return
	}
	if !ok || u.ID == 0 {
		httpError(w, http.StatusUnauthorized, errBadLogin.Error())
		return
	}
	u.Imported = imported.Valid
	if err := a.newSession(w, r, u.ID); err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if _, h := a.auth(nil, r); h != nil {
		a.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, h)
	}
	a.setCookie(w, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) me(w http.ResponseWriter, r *http.Request) {
	u, _ := a.auth(w, r)
	if u == nil {
		httpError(w, http.StatusUnauthorized, "Please sign in.")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

type sessionInfo struct {
	ID        string `json:"id"`
	Current   bool   `json:"current"`
	Created   int64  `json:"createdAt"`
	LastSeen  int64  `json:"lastSeen"`
	UserAgent string `json:"userAgent"`
	IP        string `json:"ip"`
}

func (a *App) listSessions(w http.ResponseWriter, r *http.Request) {
	u, cur := a.auth(w, r)
	if u == nil {
		httpError(w, http.StatusUnauthorized, "Please sign in.")
		return
	}
	rows, err := a.db.Query(`SELECT token_hash, created_at, last_seen, user_agent, ip FROM sessions
		WHERE user_id = ? AND expires_at > ? ORDER BY last_seen DESC`, u.ID, a.now().UnixMilli())
	if err != nil {
		a.fail(w, err)
		return
	}
	defer rows.Close()
	out := []sessionInfo{}
	for rows.Next() {
		var s sessionInfo
		var h []byte
		rows.Scan(&h, &s.Created, &s.LastSeen, &s.UserAgent, &s.IP)
		s.ID, s.Current = hex.EncodeToString(h), subtle.ConstantTimeCompare(h, cur) == 1
		out = append(out, s)
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) deleteSession(w http.ResponseWriter, r *http.Request, u *User) {
	h, err := hex.DecodeString(r.PathValue("id"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad session id")
		return
	}
	a.db.Exec(`DELETE FROM sessions WHERE token_hash = ? AND user_id = ?`, h, u.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) deleteAccount(w http.ResponseWriter, r *http.Request, u *User) {
	var body struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &body) || !a.limit(w, limitRule{"login-user:" + strings.ToLower(u.Name), 10, 15 * time.Minute}) {
		return
	}
	var hash string
	a.db.QueryRow(`SELECT pw_hash FROM users WHERE id = ?`, u.ID).Scan(&hash)
	ok := false
	if err := a.withHashSlot(r.Context(), func() { ok = verifyPassword(hash, body.Password) }); err != nil {
		return
	}
	if !ok {
		httpError(w, http.StatusForbidden, "Wrong password.")
		return
	}
	a.hub.forget(u.ID) // ends any live match first, while the user row still exists for the match record
	// Note: explicit deletes instead of ON DELETE CASCADE, which Turso over HTTP doesn't enforce per request.
	tx, err := a.db.Begin()
	if err != nil {
		a.fail(w, err)
		return
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM sessions WHERE user_id = ?`, `DELETE FROM classic_stats WHERE user_id = ?`,
		`DELETE FROM story_progress WHERE user_id = ?`, `DELETE FROM story_saves WHERE user_id = ?`,
		`DELETE FROM pvp_ratings WHERE user_id = ?`,
		`UPDATE matches SET p1 = NULL WHERE p1 = ?`, `UPDATE matches SET p2 = NULL WHERE p2 = ?`,
		`UPDATE matches SET winner = NULL WHERE winner = ?`, `DELETE FROM users WHERE id = ?`,
	} {
		if _, err := tx.Exec(q, u.ID); err != nil {
			a.fail(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		a.fail(w, err)
		return
	}
	a.setCookie(w, "", -1)
	w.WriteHeader(http.StatusNoContent)
}
