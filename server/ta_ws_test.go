package main

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// taSetup: one Time Attack "minute" lasts `unit`; the app clock can be pushed forward with skew.
func taSetup(t *testing.T, unit time.Duration, names ...string) (*App, *atomic.Int64, []*wsc, []*client) {
	a, srv, cs := pvpSetup(t, names...)
	a.hub.taUnit = unit
	skew := &atomic.Int64{}
	a.now = func() time.Time { return time.Now().Add(time.Duration(skew.Load())) }
	var ws []*wsc
	for _, c := range cs {
		ws = append(ws, dialWS(t, srv, c))
	}
	return a, skew, ws, cs
}

// startTA queues both for ranked 4x4 easy Time Attack (1 "minute") and returns both ta_start messages.
func startTA(t *testing.T, x, y *wsc) (map[string]any, map[string]any) {
	x.send(M{"t": "queue", "mode": "ta", "minutes": 1, "size": 4, "level": "easy"})
	x.expect("queued")
	y.send(M{"t": "queue", "mode": "ta", "minutes": 1, "size": 4, "level": "easy"})
	if l := x.expect("lobby"); l["mode"] != "ta" || l["minutes"] != float64(1) {
		t.Fatalf("lobby: %v", l)
	}
	y.expect("lobby")
	x.send(M{"t": "ready"})
	y.send(M{"t": "ready"})
	return x.expect("ta_start"), y.expect("ta_start")
}

func solutionOf(a *App, name string) (puzzle, sol []int) {
	r := roomOf(a, name)
	a.hub.mu.Lock()
	defer a.hub.mu.Unlock()
	return r.puzzle, r.solution
}

func empties(p []int) (out []int) {
	for i, v := range p {
		if v == 0 {
			out = append(out, i)
		}
	}
	return
}

func TestTAPlayUntilTimeUp(t *testing.T) {
	a, _, ws, cs := taSetup(t, 600*time.Millisecond, "ana", "bob")
	x, y := ws[0], ws[1]
	sx, sy := startTA(t, x, y)
	if _, leak := sx["solution"]; leak {
		t.Fatal("solution sent")
	}
	if fmtList(sx["puzzle"]) != fmtList(sy["puzzle"]) || sx["endsAt"] != sy["endsAt"] || sx["serverNow"] == nil {
		t.Fatal("both players must get the same puzzle and end time")
	}
	if sx["endsAt"].(float64)-sx["startsAt"].(float64) != 600 {
		t.Fatalf("duration: %v", sx)
	}
	p, sol := solutionOf(a, "ana")
	open := empties(p)

	// ana: one correct cell. bo: one wrong digit, then the same cell again inside the cooldown.
	x.send(M{"t": "place", "cell": open[0], "value": sol[open[0]]})
	if r := x.expect("ta_cell_result"); r["correct"] != true || r["cells"] != float64(1) {
		t.Fatalf("correct move: %v", r)
	}
	if s := y.expect("ta_score"); s["opponent"].(map[string]any)["cells"] != float64(1) {
		t.Fatalf("live score: %v", s)
	}
	y.send(M{"t": "place", "cell": open[1], "value": sol[open[1]]%4 + 1})
	if r := y.expect("ta_cell_result"); r["correct"] != false || r["mistakes"] != float64(1) || r["cooldownUntil"] == nil {
		t.Fatalf("wrong move: %v", r)
	}
	y.send(M{"t": "place", "cell": open[1], "value": sol[open[1]]})
	if r := y.expect("ta_cell_result"); r["rejected"] != "cooldown" || r["cells"] != float64(0) {
		t.Fatalf("cooldown: %v", r)
	}

	end := x.expect("ta_end")
	// one correct cell may also complete a box on a random puzzle, so the deciding rule is boxes or cells
	if end["winner"] != "ana" || (end["reason"] != "cells" && end["reason"] != "boxes") || end["early"] != false || end["ranked"] != true {
		t.Fatalf("end: %v", end)
	}
	y.expect("ta_end")
	if a.rating(cs[0].id(t), "ta", 4) != 1016 || a.rating(cs[0].id(t), "race", 4) != 1000 {
		t.Fatal("Time Attack rating must be separate from Race")
	}

	// rematch keeps mode, size and duration
	x.send(M{"t": "rematch"})
	y.send(M{"t": "rematch"})
	if s := y.expect("ta_start"); s["durationSec"] != float64(60) || s["size"] != float64(4) {
		t.Fatalf("rematch: %v", s)
	}
}

func fmtList(v any) string {
	out := ""
	for _, x := range v.([]any) {
		out += string(rune('0' + int(x.(float64))))
	}
	return out
}

func TestTAFakeScoresIgnoredAndEarlyWin(t *testing.T) {
	a, _, ws, _ := taSetup(t, time.Minute, "cyd", "dia")
	x, y := ws[0], ws[1]
	startTA(t, x, y)
	p, sol := solutionOf(a, "cyd")
	open := empties(p)

	// a cheating client: made-up score messages, a "finished" claim, and farming a locked cell
	x.send(M{"t": "ta_score", "you": M{"boxes": 4, "cells": 99}})
	x.expect("error")
	x.send(M{"t": "submit", "values": sol})
	x.expect("error")
	x.send(M{"t": "place", "cell": open[0], "value": sol[open[0]]})
	x.expect("ta_cell_result")
	for range 3 {
		x.send(M{"t": "place", "cell": open[0], "value": sol[open[0]]})
		if r := x.expect("ta_cell_result"); r["rejected"] != "locked" || r["cells"] != float64(1) {
			t.Fatalf("farming: %v", r)
		}
	}
	if s := y.expect("ta_score"); s["opponent"].(map[string]any)["cells"] != float64(1) {
		t.Fatalf("opponent sees the real score only: %v", s)
	}

	// finishing the whole board ends the match at once
	boxes := 0
	for _, i := range open[1:] {
		x.send(M{"t": "place", "cell": i, "value": sol[i]})
		for {
			if r := x.expect("ta_cell_result"); r["cell"] == float64(i) {
				break
			}
		}
	}
	end := y.expect("ta_end")
	for _, pl := range end["players"].([]any) {
		if pm := pl.(map[string]any); pm["name"] == "cyd" {
			boxes = int(pm["boxes"].(float64))
		}
	}
	if end["winner"] != "cyd" || end["reason"] != "board" || end["early"] != true || boxes != 4 {
		t.Fatalf("early win: %v", end)
	}
}

func TestTAReconnectAndLateInput(t *testing.T) {
	a, skew, ws, cs := taSetup(t, time.Minute, "eddy", "fin")
	x, y := ws[0], ws[1]
	startTA(t, x, y)
	p, sol := solutionOf(a, "eddy")
	open := empties(p)
	x.send(M{"t": "place", "cell": open[0], "value": sol[open[0]]})
	x.expect("ta_cell_result")
	x.send(M{"t": "place", "cell": open[1], "value": sol[open[1]]%4 + 1})
	x.expect("ta_cell_result")

	// drop and come back: the clock kept running, the server state is restored
	x.conn.Close(websocket.StatusNormalClosure, "")
	y.expect("opponent_away")
	x = dialWS2(t, ws[1].srv, cs[0])
	st := x.expect("state")
	you := st["you"].(map[string]any)
	if st["phase"] != "playing" || st["endsAt"] == nil || you["cells"] != float64(1) || you["mistakes"] != float64(1) {
		t.Fatalf("state: %v", st)
	}
	if st["values"].([]any)[open[0]] != float64(sol[open[0]]) || st["values"].([]any)[open[1]] != float64(0) {
		t.Fatal("restored board must hold locked cells only")
	}
	if len(st["cooldowns"].(map[string]any)) != 1 {
		t.Fatalf("cooldown lost on reconnect: %v", st["cooldowns"])
	}
	y.expect("opponent_back")

	// past endsAt + allowance (fake clock), moves are refused
	skew.Store(int64(61 * time.Second))
	x.send(M{"t": "place", "cell": open[2], "value": sol[open[2]]})
	if r := x.expect("ta_cell_result"); r["rejected"] != "time" {
		t.Fatalf("late move: %v", r)
	}
}

func TestTADisconnectFreezesScore(t *testing.T) {
	a, _, ws, _ := taSetup(t, 500*time.Millisecond, "gil", "hugo")
	x, y := ws[0], ws[1]
	startTA(t, x, y)
	p, sol := solutionOf(a, "gil")
	open := empties(p)
	x.send(M{"t": "place", "cell": open[0], "value": sol[open[0]]})
	x.expect("ta_cell_result")
	x.conn.Close(websocket.StatusNormalClosure, "")
	// no forfeit: gil keeps the frozen score and still wins when time runs out
	if end := y.expect("ta_end"); end["winner"] != "gil" || end["early"] != false { // decided by boxes or cells
		t.Fatalf("frozen score: %v", end)
	}
}

func TestTAQueueValidation(t *testing.T) {
	_, _, ws, _ := taSetup(t, time.Minute, "ian")
	x := ws[0]
	x.send(M{"t": "queue", "mode": "ta", "minutes": 10, "size": 4, "level": "easy"}) // 10 min on 4x4 is hidden in config
	x.expect("error")
	x.send(M{"t": "queue", "mode": "blitz", "size": 4, "level": "easy"})
	x.expect("error")
	x.send(M{"t": "queue", "mode": "ta", "minutes": 3, "size": 4, "level": "easy"})
	x.expect("queued")
}
