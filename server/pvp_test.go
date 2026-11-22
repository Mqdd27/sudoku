package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type wsc struct {
	t    *testing.T
	conn *websocket.Conn
	srv  *httptest.Server
}

func dialWS(t *testing.T, srv *httptest.Server, c *client) *wsc {
	t.Helper()
	h := http.Header{"Origin": {srv.URL}}
	for _, ck := range c.hc.Jar.Cookies(mustURL(srv.URL)) {
		h.Add("Cookie", ck.Name+"="+ck.Value)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	w := &wsc{t, conn, srv}
	w.expect("hello")
	return w
}

func (w *wsc) send(v any) {
	b, _ := json.Marshal(v)
	if err := w.conn.Write(context.Background(), websocket.MessageText, b); err != nil {
		w.t.Fatal(err)
	}
}

// expect reads messages until one of type typ arrives (others are skipped).
func (w *wsc) expect(typ string) map[string]any {
	w.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, b, err := w.conn.Read(ctx)
		if err != nil {
			w.t.Fatalf("waiting for %q: %v", typ, err)
		}
		var m map[string]any
		json.Unmarshal(b, &m)
		if m["t"] == typ {
			return m
		}
	}
}

// pvpSetup: app with fast timers and n registered players.
func pvpSetup(t *testing.T, names ...string) (*App, *httptest.Server, []*client) {
	a, srv := newTestApp(t)
	a.hub.countdown, a.hub.grace, a.hub.queueWait = 30*time.Millisecond, 300*time.Millisecond, 200*time.Millisecond
	var cs []*client
	for _, n := range names {
		cs = append(cs, register(t, srv, n))
	}
	return a, srv, cs
}

// startMatch queues two sockets for 4x4 easy, readies both and waits for "start".
func startMatch(t *testing.T, x, y *wsc) {
	x.send(M{"t": "queue", "size": 4, "level": "easy"})
	x.expect("queued")
	y.send(M{"t": "queue", "size": 4, "level": "easy"})
	x.expect("lobby")
	y.expect("lobby")
	x.send(M{"t": "ready"})
	y.send(M{"t": "ready"})
	x.expect("countdown")
	sx, sy := x.expect("start"), y.expect("start")
	if _, leaked := sx["solution"]; leaked {
		t.Fatal("start must not carry the solution")
	}
	if sx["size"] != float64(4) || len(sx["puzzle"].([]any)) != 16 || sy["startAt"] != sx["startAt"] {
		t.Fatalf("start: %v", sx)
	}
}

func roomOf(a *App, name string) *Room {
	a.hub.mu.Lock()
	defer a.hub.mu.Unlock()
	for id, r := range a.hub.byUser {
		if r.player(id).name == name {
			return r
		}
	}
	return nil
}

func TestPvPWinnerDecidedByServer(t *testing.T) {
	a, srv, cs := pvpSetup(t, "ann", "ben")
	x, y := dialWS(t, srv, cs[0]), dialWS(t, srv, cs[1])
	startMatch(t, x, y)
	r := roomOf(a, "ann")
	a.hub.mu.Lock()
	puzzle, sol := r.puzzle, r.solution
	a.hub.mu.Unlock()

	// cheat attempt: claim a finished board without playing it
	x.send(M{"t": "submit", "values": sol})
	x.expect("rejected")

	first := true
	for i, v := range puzzle {
		if v != 0 {
			continue
		}
		if first { // one wrong digit counts as a mistake and is visible to the opponent
			x.send(M{"t": "place", "cell": i, "value": sol[i]%4 + 1})
			if m := x.expect("checked"); m["correct"] != false || m["mistakes"] != float64(1) {
				t.Fatalf("wrong digit: %v", m)
			}
			if m := y.expect("opponent"); m["mistakes"] != float64(1) {
				t.Fatalf("opponent view: %v", m)
			}
			first = false
		}
		x.send(M{"t": "place", "cell": i, "value": sol[i]})
	}
	res := y.expect("result")
	if res["winner"] != "ann" || res["reason"] != "solved" || res["ranked"] != true {
		t.Fatalf("result: %v", res)
	}
	if a.rating(cs[0].id(t), "race", 4) != 1016 || a.rating(cs[1].id(t), "race", 4) != 984 {
		t.Fatal("elo not applied for the 4x4 board")
	}
	if a.rating(cs[0].id(t), "race", 9) != 1000 {
		t.Fatal("ratings must be separate per size")
	}

	// rematch starts a new round in the same room
	x.send(M{"t": "rematch"})
	y.send(M{"t": "rematch"})
	y.expect("countdown")
	y.expect("start")
}

func (c *client) id(t *testing.T) int64 {
	var u User
	c.do("GET", "/api/me", nil, &u)
	return u.ID
}

func TestPvPQueuesBySizeAndLevel(t *testing.T) {
	_, srv, cs := pvpSetup(t, "cat", "dan", "eve")
	x, y, z := dialWS(t, srv, cs[0]), dialWS(t, srv, cs[1]), dialWS(t, srv, cs[2])
	x.send(M{"t": "queue", "size": 4, "level": "easy"})
	x.expect("queued")
	y.send(M{"t": "queue", "size": 6, "level": "easy"})
	y.expect("queued")
	z.send(M{"t": "queue", "size": 4, "level": "hard"})
	z.expect("queued")
	// nobody was paired; all three time out on their own queue
	x.expect("queue_timeout")
	y.expect("queue_timeout")
	z.expect("queue_timeout")
}

func TestPvPPrivateRoomReconnectAndForfeit(t *testing.T) {
	a, srv, cs := pvpSetup(t, "fay", "gus")
	x, y := dialWS(t, srv, cs[0]), dialWS(t, srv, cs[1])
	x.send(M{"t": "create", "size": 6, "level": "medium"})
	code := x.expect("lobby")["code"].(string)
	if len(code) != 6 {
		t.Fatalf("code %q", code)
	}
	y.send(M{"t": "join", "code": strings.ToLower(code)})
	y.expect("lobby")
	x.send(M{"t": "ready"})
	y.send(M{"t": "ready"})
	x.expect("start")
	y.expect("start")

	// drop and come back within the grace period: the match continues
	x.conn.Close(websocket.StatusNormalClosure, "")
	y.expect("opponent_away")
	x = dialWS2(t, srv, cs[0]) // reconnect gets the full state instead of "hello"
	st := x.expect("state")
	if st["phase"] != "playing" || len(st["values"].([]any)) != 36 {
		t.Fatalf("state: %v", st)
	}
	y.expect("opponent_back")

	// drop for longer than the grace period: forfeit, unranked (private room)
	x.conn.Close(websocket.StatusNormalClosure, "")
	res := y.expect("result")
	if res["winner"] != "gus" || res["reason"] != "disconnect" || res["ranked"] != false {
		t.Fatalf("result: %v", res)
	}
	if a.rating(cs[1].id(t), "race", 6) != 1000 {
		t.Fatal("private rooms are unranked")
	}
}

// dialWS2 dials without waiting for "hello" (used when a match is being resumed).
func dialWS2(t *testing.T, srv *httptest.Server, c *client) *wsc {
	h := http.Header{"Origin": {srv.URL}}
	for _, ck := range c.hc.Jar.Cookies(mustURL(srv.URL)) {
		h.Add("Cookie", ck.Name+"="+ck.Value)
	}
	conn, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return &wsc{t, conn, srv}
}

func TestPvPResignAndAuth(t *testing.T) {
	_, srv, cs := pvpSetup(t, "hal", "ivy")
	// no cookie: refused before upgrade
	if _, res, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", nil); err == nil || res.StatusCode != 401 {
		t.Fatal("anonymous websocket must be refused")
	}
	x, y := dialWS(t, srv, cs[0]), dialWS(t, srv, cs[1])
	startMatch(t, x, y)
	x.send(M{"t": "hint"})
	if m := x.expect("error"); !strings.Contains(m["msg"].(string), "Hints") {
		t.Fatalf("hint should be off: %v", m)
	}
	y.send(M{"t": "resign"})
	if res := x.expect("result"); res["winner"] != "hal" || res["reason"] != "resign" {
		t.Fatalf("resign: %v", res)
	}
}

func mustURL(s string) *url.URL { u, _ := url.Parse(s); return u }
