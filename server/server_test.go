package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestApp(t *testing.T) (*App, *httptest.Server) {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}}
	a := newApp(cfg, db)
	srv := httptest.NewServer(a.routes())
	t.Cleanup(func() { a.hub.shutdown(); srv.Close(); db.Close() })
	return a, srv
}

type client struct {
	t   *testing.T
	srv *httptest.Server
	hc  *http.Client
}

func newClient(t *testing.T, srv *httptest.Server) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t, srv, &http.Client{Jar: jar}}
}

// do sends a same-origin request and decodes a JSON reply into out (if non-nil). Returns the status.
func (c *client) do(method, path string, body any, out any) int {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.srv.URL+path, rd)
	req.Header.Set("Origin", c.srv.URL)
	res, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		json.NewDecoder(res.Body).Decode(out)
	}
	return res.StatusCode
}

func register(t *testing.T, srv *httptest.Server, name string) *client {
	c := newClient(t, srv)
	if code := c.do("POST", "/api/register", credentials{Username: name, Password: "password123"}, nil); code != 201 {
		t.Fatalf("register %s: %d", name, code)
	}
	return c
}

func TestPasswordHash(t *testing.T) {
	h := hashPassword("correct horse")
	if !strings.HasPrefix(h, "$argon2id$") || h == hashPassword("correct horse") {
		t.Fatal("hash must be argon2id and salted")
	}
	if !verifyPassword(h, "correct horse") || verifyPassword(h, "wrong horse") || verifyPassword("garbage", "x") {
		t.Fatal("verify mismatch")
	}
}

func TestLimiter(t *testing.T) {
	l := NewLimiter()
	now := time.Unix(0, 0)
	for i := range 3 {
		if ok, _ := l.Allow("k", 3, time.Minute, now); !ok {
			t.Fatalf("hit %d refused", i)
		}
	}
	if ok, wait := l.Allow("k", 3, time.Minute, now); ok || wait != time.Minute {
		t.Fatal("4th hit should be refused")
	}
	if ok, _ := l.Allow("other", 3, time.Minute, now); !ok {
		t.Fatal("keys must be independent")
	}
	if ok, _ := l.Allow("k", 3, time.Minute, now.Add(time.Minute+1)); !ok {
		t.Fatal("window should reset")
	}
}

func TestClientIP(t *testing.T) {
	a := &App{cfg: Config{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}}}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("CF-Connecting-IP", "203.0.113.7")
	r.RemoteAddr = "127.0.0.1:5000"
	if ip := a.clientIP(r); ip != "203.0.113.7" {
		t.Fatalf("behind trusted proxy: %s", ip)
	}
	r.RemoteAddr = "198.51.100.1:5000"
	if ip := a.clientIP(r); ip != "198.51.100.1" {
		t.Fatalf("untrusted peer must not be able to spoof: %s", ip)
	}
}

func TestStarsAndElo(t *testing.T) {
	cases := []struct {
		d          string
		el         time.Duration
		mist, hint int
		want       int
	}{
		{"easy", 5 * time.Minute, 0, 0, 3},
		{"easy", 7 * time.Minute, 0, 0, 2},
		{"easy", 5 * time.Minute, 3, 1, 1},
		{"expert", time.Hour, 9, 9, 1},
		{"hard", 14 * time.Minute, 2, 0, 3},
	}
	for _, c := range cases {
		if got := stars(c.d, c.el, c.mist, c.hint); got != c.want {
			t.Errorf("stars%v = %d", c, got)
		}
	}
	if a, b := elo(1000, 1000, 1); a != 1016 || b != 984 {
		t.Fatalf("elo equal: %d %d", a, b)
	}
	if a, b := elo(1400, 1000, 1); a-1400 >= 16 || a+b != 2400 {
		t.Fatalf("favourite should gain less, zero-sum: %d %d", a, b)
	}
}

func TestAccountFlow(t *testing.T) {
	_, srv := newTestApp(t)
	c := register(t, srv, "Alice")

	if code := newClient(t, srv).do("POST", "/api/register", credentials{Username: "alice", Password: "password123"}, nil); code != 409 {
		t.Fatalf("usernames must be unique case-insensitively, got %d", code)
	}
	for _, bad := range []credentials{{Username: "ab", Password: "password123"}, {Username: "bob", Password: "short"}, {Username: "bob!", Password: "password123"}} {
		if code := newClient(t, srv).do("POST", "/api/register", bad, nil); code != 400 {
			t.Fatalf("%+v: %d", bad, code)
		}
	}

	var me User
	if c.do("GET", "/api/me", nil, &me) != 200 || me.Name != "Alice" {
		t.Fatalf("me: %+v", me)
	}

	// generic error for both unknown user and wrong password
	var e1, e2 map[string]string
	newClient(t, srv).do("POST", "/api/login", credentials{Username: "alice", Password: "nope-nope"}, &e1)
	newClient(t, srv).do("POST", "/api/login", credentials{Username: "nobody", Password: "nope-nope"}, &e2)
	if e1["error"] == "" || e1["error"] != e2["error"] {
		t.Fatalf("login errors must be identical: %v %v", e1, e2)
	}

	// second device; list and revoke it from the first
	c2 := newClient(t, srv)
	if c2.do("POST", "/api/login", credentials{Username: "ALICE", Password: "password123"}, nil) != 200 {
		t.Fatal("login is case-insensitive")
	}
	var sessions []sessionInfo
	c.do("GET", "/api/sessions", nil, &sessions)
	if len(sessions) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(sessions))
	}
	for _, s := range sessions {
		if !s.Current {
			c.do("DELETE", "/api/sessions/"+s.ID, nil, nil)
		}
	}
	if c2.do("GET", "/api/me", nil, nil) != 401 || c.do("GET", "/api/me", nil, nil) != 200 {
		t.Fatal("revoked device must be signed out, current one kept")
	}

	// CSRF: a cross-site origin is refused
	req, _ := http.NewRequest("POST", srv.URL+"/api/logout", nil)
	req.Header.Set("Origin", "https://evil.example")
	if res, _ := c.hc.Do(req); res.StatusCode != 403 {
		t.Fatalf("cross-origin POST: %d", res.StatusCode)
	}

	// import once
	stats := map[string]any{"stats": map[string]any{"9": map[string]any{"easy": map[string]int{"solved": 3, "best": 200}, "bogus": map[string]int{"solved": 1, "best": 1}}}}
	var imp map[string]int
	if c.do("POST", "/api/classic/import", stats, &imp) != 200 || imp["imported"] != 1 {
		t.Fatalf("import: %v", imp)
	}
	if c.do("POST", "/api/classic/import", stats, nil) != 409 {
		t.Fatal("import must run once")
	}
	c.do("POST", "/api/classic/result", map[string]any{"size": 9, "level": "easy", "ms": 150000}, nil)
	var prof struct {
		Classic []classicStat `json:"classic"`
	}
	c.do("GET", "/api/profile/alice", nil, &prof)
	if len(prof.Classic) != 1 || prof.Classic[0].Solved != 4 || *prof.Classic[0].BestMs != 150000 {
		t.Fatalf("profile: %+v", prof)
	}

	// delete account needs the password
	if c.do("DELETE", "/api/account", map[string]string{"password": "wrong-pass"}, nil) != 403 {
		t.Fatal("delete with wrong password")
	}
	if c.do("DELETE", "/api/account", map[string]string{"password": "password123"}, nil) != 204 {
		t.Fatal("delete account")
	}
	if c.do("GET", "/api/profile/alice", nil, nil) != 404 {
		t.Fatal("profile should be gone")
	}
}

func TestLoginRateLimit(t *testing.T) {
	_, srv := newTestApp(t)
	register(t, srv, "carol")
	c := newClient(t, srv)
	code := 0
	for range 11 {
		code = c.do("POST", "/api/login", credentials{Username: "carol", Password: "wrongwrong"}, nil)
	}
	if code != 429 {
		t.Fatalf("11th attempt for one username: %d", code)
	}
}

func TestStory(t *testing.T) {
	a, srv := newTestApp(t)
	c := register(t, srv, "dave")
	if len(a.story) < 40 {
		t.Fatalf("need at least 40 story levels, have %d", len(a.story))
	}
	if c.do("POST", "/api/story/2/start", nil, nil) != 403 {
		t.Fatal("level 2 must be locked")
	}
	var st struct {
		Puzzle []int `json:"puzzle"`
	}
	if c.do("POST", "/api/story/1/start", nil, &st) != 200 {
		t.Fatal("start level 1")
	}
	raw, _ := json.Marshal(st)
	if bytes.Contains(raw, []byte("solution")) {
		t.Fatal("start must not include the solution")
	}
	l := a.story[0]
	empty := -1
	for i, v := range l.puzzle {
		if v == 0 {
			empty = i
			break
		}
	}
	wrong := l.solution[empty]%9 + 1
	var chk struct {
		Correct  bool `json:"correct"`
		Mistakes int  `json:"mistakes"`
	}
	c.do("POST", "/api/story/1/check", map[string]int{"cell": empty, "value": wrong}, &chk)
	c.do("POST", "/api/story/1/check", map[string]int{"cell": empty, "value": wrong}, &chk) // same wrong digit again: not counted twice
	if chk.Correct || chk.Mistakes != 1 {
		t.Fatalf("check: %+v", chk)
	}
	var sub map[string]any
	c.do("POST", "/api/story/1/submit", map[string]any{"values": l.puzzle, "elapsedMs": 60000}, &sub)
	if sub["solved"] != false {
		t.Fatal("incomplete board accepted")
	}
	var hint map[string]int
	c.do("POST", "/api/story/1/hint", map[string]any{"values": l.puzzle, "cell": empty}, &hint)
	if hint["cell"] != empty || hint["value"] != l.solution[empty] {
		t.Fatalf("hint: %v", hint)
	}
	c.do("POST", "/api/story/1/submit", map[string]any{"values": l.solution, "elapsedMs": 60000}, &sub)
	if sub["solved"] != true || sub["stars"] != float64(2) { // hint used → 2 stars
		t.Fatalf("submit: %v", sub)
	}
	var m struct {
		Levels []struct {
			Unlocked bool `json:"unlocked"`
			Stars    int  `json:"stars"`
		} `json:"levels"`
		Continue int `json:"continue"`
	}
	c.do("GET", "/api/story", nil, &m)
	if m.Levels[0].Stars != 2 || !m.Levels[1].Unlocked || m.Levels[2].Unlocked || m.Continue != 2 {
		t.Fatalf("map after level 1: %+v", m)
	}
	var lb []boardRow
	c.do("GET", "/api/leaderboard?kind=story&level=1", nil, &lb)
	if len(lb) != 1 || lb[0].Username != "dave" {
		t.Fatalf("story leaderboard: %+v", lb)
	}
}
