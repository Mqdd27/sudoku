package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"log"
	"math"
	mrand "math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/Mqdd27/sudoku/server/sudoku"
)

// PvP: every match lives in memory on this one instance; only finished matches go to SQLite.
// One mutex guards all hub state. Message handling is tiny, so contention is not a concern at this scale.
// Protocol: docs/protocol.md.

type phase int

const (
	lobby phase = iota
	countdown
	playing
	done
)

type qkey struct {
	size  int
	level string
}

type Hub struct {
	a       *App
	mu      sync.Mutex
	clients map[int64]*Client
	queues  map[qkey][]*Client
	rooms   map[string]*Room
	byUser  map[int64]*Room
	origins []string
	closed  bool

	grace, queueWait, countdown, ping time.Duration // overridable in tests
}

type Client struct {
	hub      *Hub
	user     User
	conn     *websocket.Conn
	send     chan []byte
	done     chan struct{}
	once     sync.Once
	queued   *qkey
	qtimer   *time.Timer
	tokens   float64
	lastTick time.Time
	drops    int
}

type Player struct {
	id       int64
	name     string
	client   *Client // nil while disconnected
	ready    bool
	rematch  bool
	rating   int
	values   []int
	mistakes int
	penalty  time.Duration
	finished time.Time
	timer    *time.Timer // disconnect grace
}

type Room struct {
	code     string
	size     int
	level    string
	ranked   bool
	shape    sudoku.Shape
	players  []*Player
	phase    phase
	round    int
	puzzle   []int
	solution []int
	startAt  time.Time
	timer    *time.Timer
}

func newHub(a *App) *Hub {
	h := &Hub{
		a: a, clients: map[int64]*Client{}, queues: map[qkey][]*Client{}, rooms: map[string]*Room{}, byUser: map[int64]*Room{},
		grace: 60 * time.Second, queueWait: 60 * time.Second, countdown: 3 * time.Second, ping: 25 * time.Second,
	}
	for _, o := range a.cfg.AllowedOrigins {
		if u, err := url.Parse(o); err == nil {
			h.origins = append(h.origins, u.Host)
		}
	}
	return h
}

// ---------- connection plumbing ----------

type inMsg struct {
	T      string `json:"t"`
	Size   int    `json:"size"`
	Level  string `json:"level"`
	Code   string `json:"code"`
	Cell   int    `json:"cell"`
	Value  int    `json:"value"`
	Values []int  `json:"values"`
}

type M map[string]any

func (h *Hub) serveWS(w http.ResponseWriter, r *http.Request) {
	u, _ := h.a.auth(w, r)
	if u == nil {
		httpError(w, http.StatusUnauthorized, "Please sign in.")
		return
	}
	// the http.Server timeouts are for short API calls; this connection lives as long as the match
	rc := http.NewResponseController(w)
	rc.SetReadDeadline(time.Time{})
	rc.SetWriteDeadline(time.Time{})
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: h.origins})
	if err != nil {
		return
	}
	conn.SetReadLimit(4096)
	c := &Client{hub: h, user: *u, conn: conn, send: make(chan []byte, 64), done: make(chan struct{}), tokens: burst, lastTick: time.Now()}
	if !h.attach(c) {
		conn.Close(websocket.StatusGoingAway, "server shutting down")
		return
	}
	go c.writeLoop()
	go c.pingLoop(h.ping)
	ctx := context.Background()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		if !c.allow() {
			continue
		}
		var m inMsg
		if json.Unmarshal(data, &m) != nil {
			c.emit(M{"t": "error", "msg": "bad message"})
			continue
		}
		h.handle(c, m)
	}
	h.detach(c)
	c.close(websocket.StatusNormalClosure, "")
}

// Message rate limit per connection: token bucket, 10/s sustained, bursts of 20.
const (
	rate  = 10.0
	burst = 20.0
)

func (c *Client) allow() bool {
	now := time.Now()
	c.tokens = min(burst, c.tokens+now.Sub(c.lastTick).Seconds()*rate)
	c.lastTick = now
	if c.tokens >= 1 {
		c.tokens--
		return true
	}
	if c.drops++; c.drops > 50 {
		c.close(websocket.StatusPolicyViolation, "too many messages")
	} else {
		c.emit(M{"t": "error", "msg": "slow down"})
	}
	return false
}

// emit queues a message; a client that can't keep up is disconnected rather than buffered forever.
func (c *Client) emit(v any) {
	b, _ := json.Marshal(v)
	select {
	case c.send <- b:
	case <-c.done:
	default:
		c.close(websocket.StatusPolicyViolation, "too slow")
	}
}

func (c *Client) close(code websocket.StatusCode, reason string) {
	c.once.Do(func() {
		close(c.done)
		go c.conn.Close(code, reason)
	})
}

func (c *Client) writeLoop() {
	for {
		select {
		case b := <-c.send:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := c.conn.Write(ctx, websocket.MessageText, b)
			cancel()
			if err != nil {
				c.close(websocket.StatusGoingAway, "write failed")
				return
			}
		case <-c.done:
			return
		}
	}
}

// pingLoop is the heartbeat: a protocol-level ping every interval; no pong within 10s drops the connection.
func (c *Client) pingLoop(every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := c.conn.Ping(ctx)
			cancel()
			if err != nil {
				c.close(websocket.StatusGoingAway, "no pong")
				return
			}
		case <-c.done:
			return
		}
	}
}

// attach registers the connection, replacing an older one for the same user, and resumes any match.
func (h *Hub) attach(c *Client) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	if old := h.clients[c.user.ID]; old != nil {
		old.emit(M{"t": "replaced"})
		h.dequeue(old)
		old.close(websocket.StatusNormalClosure, "opened elsewhere")
	}
	h.clients[c.user.ID] = c
	room := h.byUser[c.user.ID]
	if room == nil {
		c.emit(M{"t": "hello", "user": c.user.Name})
		return true
	}
	p := room.player(c.user.ID)
	p.client = c
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	c.emit(room.stateFor(p, h.a.now()))
	if o := room.other(p); o != nil && room.phase != done {
		o.send(M{"t": "opponent_back"})
	}
	return true
}

// detach runs when a connection ends: lobby → leave the room, match in progress → start the grace timer.
func (h *Hub) detach(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[c.user.ID] != c {
		return // already replaced by a newer connection
	}
	delete(h.clients, c.user.ID)
	h.dequeue(c)
	room := h.byUser[c.user.ID]
	if room == nil {
		return
	}
	p := room.player(c.user.ID)
	p.client = nil
	switch room.phase {
	case lobby, done:
		h.leave(room, p)
	case countdown, playing:
		if o := room.other(p); o != nil {
			o.send(M{"t": "opponent_away", "graceSec": int(h.grace.Seconds())})
		}
		round := room.round
		p.timer = time.AfterFunc(h.grace, func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if p.client == nil && room.round == round && (room.phase == countdown || room.phase == playing) {
				h.finish(room, room.other(p), "disconnect")
				h.leave(room, p)
			}
		})
	}
}

// forget ends everything for a user who is deleting their account (called before the row is deleted).
func (h *Hub) forget(userID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if room := h.byUser[userID]; room != nil {
		p := room.player(userID)
		if room.phase == countdown || room.phase == playing {
			h.finish(room, room.other(p), "resign")
		}
		h.leave(room, p)
	}
	if c := h.clients[userID]; c != nil {
		h.dequeue(c)
		delete(h.clients, userID)
		c.close(websocket.StatusNormalClosure, "account deleted")
	}
}

func (h *Hub) shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for _, c := range h.clients {
		c.close(websocket.StatusGoingAway, "server restarting")
	}
}

// ---------- rooms ----------

func (r *Room) player(id int64) *Player {
	for _, p := range r.players {
		if p.id == id {
			return p
		}
	}
	return nil
}

func (r *Room) other(p *Player) *Player {
	for _, o := range r.players {
		if o != p {
			return o
		}
	}
	return nil
}

func (p *Player) send(v any) {
	if p.client != nil {
		p.client.emit(v)
	}
}

func (r *Room) broadcast(v any) {
	for _, p := range r.players {
		p.send(v)
	}
}

func (r *Room) lobbyMsg() M {
	ps := make([]M, len(r.players))
	for i, p := range r.players {
		ps[i] = M{"name": p.name, "ready": p.ready, "rating": p.rating}
	}
	return M{"t": "lobby", "code": r.code, "size": r.size, "level": r.level, "ranked": r.ranked, "players": ps}
}

// progress is the share of the player's open cells that are correct, 0–100. Digits are never shared.
func (r *Room) progress(p *Player) int {
	open, right := 0, 0
	for i, v := range r.puzzle {
		if v == 0 {
			open++
			if p.values[i] == r.solution[i] {
				right++
			}
		}
	}
	if open == 0 {
		return 100
	}
	return right * 100 / open
}

func (r *Room) startMsg(now time.Time) M {
	return M{"t": "start", "size": r.size, "boxRows": r.shape.BoxRows, "boxCols": r.shape.BoxCols, "level": r.level,
		"puzzle": r.puzzle, "startAt": r.startAt.UnixMilli(), "now": now.UnixMilli()}
}

// stateFor is everything a reconnecting player needs to pick up where they were.
func (r *Room) stateFor(p *Player, now time.Time) M {
	m := r.lobbyMsg()
	m["t"] = "state"
	m["phase"] = [...]string{"lobby", "countdown", "playing", "done"}[r.phase]
	m["now"] = now.UnixMilli()
	if r.phase == countdown {
		m["startAt"] = r.startAt.UnixMilli()
	}
	if r.phase == playing {
		for k, v := range r.startMsg(now) {
			if k != "t" {
				m[k] = v
			}
		}
		m["values"], m["mistakes"] = p.values, p.mistakes
		if o := r.other(p); o != nil {
			m["opponent"] = M{"progress": r.progress(o), "mistakes": o.mistakes, "connected": o.client != nil}
		}
	}
	return m
}

const codeChars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O/1/I

func (h *Hub) newRoom(size int, level string, ranked bool) *Room {
	var code string
	for code == "" || h.rooms[code] != nil {
		b := make([]byte, 6)
		rand.Read(b)
		for i := range b {
			b[i] = codeChars[int(b[i])%len(codeChars)]
		}
		code = string(b)
	}
	sc := h.a.shared.Sizes[strconv.Itoa(size)]
	r := &Room{code: code, size: size, level: level, ranked: ranked, shape: sudoku.Shape{Size: size, BoxRows: sc.BoxRows, BoxCols: sc.BoxCols}}
	h.rooms[code] = r
	return r
}

func (h *Hub) addPlayer(r *Room, c *Client) {
	p := &Player{id: c.user.ID, name: c.user.Name, client: c, rating: h.a.rating(c.user.ID, r.size)}
	r.players = append(r.players, p)
	h.byUser[c.user.ID] = r
}

// leave takes p out of the room; an emptied room is deleted.
func (h *Hub) leave(r *Room, p *Player) {
	if p.timer != nil {
		p.timer.Stop()
	}
	delete(h.byUser, p.id)
	for i, q := range r.players {
		if q == p {
			r.players = append(r.players[:i], r.players[i+1:]...)
			break
		}
	}
	if len(r.players) == 0 {
		if r.timer != nil {
			r.timer.Stop()
		}
		delete(h.rooms, r.code)
		return
	}
	for _, o := range r.players {
		o.ready, o.rematch = false, false
		if r.phase == done || r.ranked {
			// a quick-match partner (or post-game opponent) left: the room has no purpose any more
			o.send(M{"t": "opponent_left"})
			delete(h.byUser, o.id)
		} else {
			o.send(r.lobbyMsg())
		}
	}
	if r.phase == done || r.ranked {
		delete(h.rooms, r.code)
	}
}

func (h *Hub) dequeue(c *Client) {
	if c.queued == nil {
		return
	}
	q := h.queues[*c.queued]
	for i, x := range q {
		if x == c {
			h.queues[*c.queued] = append(q[:i], q[i+1:]...)
			break
		}
	}
	if c.qtimer != nil {
		c.qtimer.Stop()
	}
	c.queued = nil
}

func (h *Hub) startCountdown(r *Room) {
	g := h.a.shared.Givens[strconv.Itoa(r.size)][r.level]
	r.puzzle, r.solution = sudoku.Generate(r.shape, g[0], g[1], mrand.New(mrand.NewPCG(mrand.Uint64(), mrand.Uint64())), 2*time.Second)
	r.phase, r.round = countdown, r.round+1
	r.startAt = h.a.now().Add(h.countdown)
	for _, p := range r.players {
		p.values, p.mistakes, p.penalty, p.finished, p.ready, p.rematch = append([]int(nil), r.puzzle...), 0, 0, time.Time{}, false, false
	}
	r.broadcast(M{"t": "countdown", "startAt": r.startAt.UnixMilli(), "now": h.a.now().UnixMilli()})
	round := r.round
	r.timer = time.AfterFunc(h.countdown, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if r.round == round && r.phase == countdown {
			r.phase = playing
			r.broadcast(r.startMsg(h.a.now()))
		}
	})
}

// ---------- messages ----------

func (h *Hub) handle(c *Client, m inMsg) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[c.user.ID] != c {
		return
	}
	room := h.byUser[c.user.ID]
	var p *Player
	if room != nil {
		p = room.player(c.user.ID)
	}
	fail := func(msg string) { c.emit(M{"t": "error", "msg": msg}) }
	busy := room != nil && room.phase != done

	switch m.T {
	case "ping":
		c.emit(M{"t": "pong"})

	case "queue", "create":
		if busy || c.queued != nil {
			fail("You are already in a match.")
			return
		}
		if !h.a.validSizeLevel(m.Size, m.Level) {
			fail("Unknown board size or level.")
			return
		}
		if room != nil {
			h.leave(room, p)
		}
		if m.T == "create" {
			r := h.newRoom(m.Size, m.Level, false)
			h.addPlayer(r, c)
			c.emit(r.lobbyMsg())
			return
		}
		k := qkey{m.Size, m.Level}
		if q := h.queues[k]; len(q) > 0 {
			other := q[0]
			h.dequeue(other)
			r := h.newRoom(m.Size, m.Level, true)
			h.addPlayer(r, other)
			h.addPlayer(r, c)
			r.broadcast(r.lobbyMsg())
			return
		}
		c.queued = &k
		h.queues[k] = append(h.queues[k], c)
		c.qtimer = time.AfterFunc(h.queueWait, func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if c.queued != nil && *c.queued == k {
				h.dequeue(c)
				c.emit(M{"t": "queue_timeout"})
			}
		})
		c.emit(M{"t": "queued", "size": m.Size, "level": m.Level, "timeoutSec": int(h.queueWait.Seconds())})

	case "cancel":
		h.dequeue(c)
		if room != nil && room.phase == lobby {
			h.leave(room, p)
		}
		c.emit(M{"t": "cancelled"})

	case "join":
		r := h.rooms[strings.ToUpper(strings.TrimSpace(m.Code))]
		switch {
		case busy || c.queued != nil:
			fail("You are already in a match.")
		case r == nil || r.ranked || r.phase != lobby:
			fail("No open room with that code.")
		case len(r.players) >= 2:
			fail("That room is full.")
		default:
			if room != nil {
				h.leave(room, p)
			}
			h.addPlayer(r, c)
			r.broadcast(r.lobbyMsg())
		}

	case "leave":
		if room == nil {
			return
		}
		if room.phase == countdown || room.phase == playing {
			h.finish(room, room.other(p), "resign")
		}
		h.leave(room, p)
		c.emit(M{"t": "left"})

	case "ready":
		if room == nil || room.phase != lobby {
			return
		}
		p.ready = true
		room.broadcast(room.lobbyMsg())
		if len(room.players) == 2 && room.players[0].ready && room.players[1].ready {
			h.startCountdown(room)
		}

	case "place":
		if room == nil || room.phase != playing || !p.finished.IsZero() {
			fail("Not playing.")
			return
		}
		if m.Cell < 0 || m.Cell >= len(room.puzzle) || room.puzzle[m.Cell] != 0 || m.Value < 0 || m.Value > room.size {
			fail("Invalid move.")
			return
		}
		correct := m.Value != 0 && room.solution[m.Cell] == m.Value
		if m.Value != 0 && !correct && p.values[m.Cell] != m.Value {
			p.mistakes++
		}
		p.values[m.Cell] = m.Value
		c.emit(M{"t": "checked", "cell": m.Cell, "value": m.Value, "correct": correct, "mistakes": p.mistakes})
		if o := room.other(p); o != nil {
			o.send(M{"t": "opponent", "progress": room.progress(p), "mistakes": p.mistakes})
		}
		if room.progress(p) == 100 {
			h.finish(room, p, "solved")
		}

	case "submit":
		// The server only trusts the board it built from "place" messages; whatever the client sends is ignored.
		if room == nil || room.phase != playing {
			fail("Not playing.")
		} else if room.progress(p) == 100 {
			h.finish(room, p, "solved")
		} else {
			c.emit(M{"t": "rejected", "msg": "The board is not solved."})
		}

	case "hint":
		pv := h.a.shared.PvP
		if room == nil || room.phase != playing || !pv.Hints {
			fail("Hints are off in PvP.")
			return
		}
		var open []int
		for i, v := range p.values {
			if v != room.solution[i] {
				open = append(open, i)
			}
		}
		if len(open) == 0 {
			return
		}
		i := open[mrand.IntN(len(open))]
		p.values[i] = room.solution[i]
		p.penalty += time.Duration(pv.HintPenaltySec) * time.Second
		c.emit(M{"t": "hinted", "cell": i, "value": room.solution[i], "penaltySec": pv.HintPenaltySec})
		if o := room.other(p); o != nil {
			o.send(M{"t": "opponent", "progress": room.progress(p), "mistakes": p.mistakes})
		}
		if room.progress(p) == 100 {
			h.finish(room, p, "solved")
		}

	case "resign":
		if room != nil && (room.phase == countdown || room.phase == playing) {
			h.finish(room, room.other(p), "resign")
		}

	case "rematch":
		if room == nil || room.phase != done || len(room.players) != 2 {
			fail("No rematch available.")
			return
		}
		p.rematch = true
		room.broadcast(M{"t": "rematch", "from": p.name})
		if o := room.other(p); o.rematch && o.client != nil {
			h.startCountdown(room)
		}

	default:
		fail("Unknown message type.")
	}
}

// finish ends the match: winner may be nil only if both are gone. Times come from the server clock.
func (h *Hub) finish(r *Room, winner *Player, reason string) {
	if r.phase == done {
		return
	}
	now := h.a.now()
	if winner != nil && reason == "solved" {
		winner.finished = now
	}
	r.phase = done
	if r.timer != nil {
		r.timer.Stop()
	}
	if len(r.players) < 2 {
		return
	}
	a, b := r.players[0], r.players[1]
	ms := func(p *Player) *int64 {
		if p.finished.IsZero() {
			return nil
		}
		v := (p.finished.Sub(r.startAt) + p.penalty).Milliseconds()
		return &v
	}
	newA, newB := a.rating, b.rating
	if r.ranked && winner != nil {
		newA, newB = elo(a.rating, b.rating, winner == a)
	}
	var winnerID any
	if winner != nil {
		winnerID = winner.id
	}
	_, err := h.a.db.Exec(`INSERT INTO matches(size, level, ranked, p1, p2, winner, p1_ms, p2_ms, p1_mistakes, p2_mistakes, reason, finished_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, r.size, r.level, r.ranked, a.id, b.id, winnerID, ms(a), ms(b), a.mistakes, b.mistakes, reason, now.UnixMilli())
	if err != nil {
		log.Printf("record match: %v", err)
	}
	for _, x := range []struct {
		p      *Player
		rating int
	}{{a, newA}, {b, newB}} {
		won := 0
		if x.p == winner {
			won = 1
		}
		_, err := h.a.db.Exec(`INSERT INTO ratings(user_id, size, rating, wins, losses) VALUES (?,?,?,?,?)
			ON CONFLICT(user_id, size) DO UPDATE SET rating = excluded.rating, wins = wins + excluded.wins, losses = losses + excluded.losses`,
			x.p.id, r.size, x.rating, won, 1-won)
		if err != nil {
			log.Printf("record rating: %v", err)
		}
	}
	res := func(p *Player, after int) M {
		return M{"name": p.name, "ms": ms(p), "mistakes": p.mistakes, "rating": after, "delta": after - p.rating}
	}
	var wname any
	if winner != nil {
		wname = winner.name
	}
	r.broadcast(M{"t": "result", "winner": wname, "reason": reason, "ranked": r.ranked, "players": []M{res(a, newA), res(b, newB)}})
	a.rating, b.rating = newA, newB
}

// elo returns new ratings after a decisive game (K=32), zero-sum.
func elo(ra, rb int, aWon bool) (int, int) {
	ea := 1 / (1 + math.Pow(10, float64(rb-ra)/400))
	sa := 0.0
	if aWon {
		sa = 1
	}
	d := int(math.Round(32 * (sa - ea)))
	return ra + d, rb - d
}

func (a *App) rating(userID int64, size int) int {
	r := 1000
	a.db.QueryRow(`SELECT rating FROM ratings WHERE user_id = ? AND size = ?`, userID, size).Scan(&r)
	return r
}
