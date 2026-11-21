package main

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/Mqdd27/sudoku/server/sudoku"
)

var shapes = []sudoku.Shape{{Size: 4, BoxRows: 2, BoxCols: 2}, {Size: 6, BoxRows: 2, BoxCols: 3}, {Size: 9, BoxRows: 3, BoxCols: 3}}

func taFixture(t *testing.T, s sudoku.Shape, seed uint64) *taBoard {
	t.Helper()
	g := map[int][2]int{4: {8, 10}, 6: {14, 16}, 9: {32, 36}}[s.Size]
	p, sol := sudoku.GenerateOpen(s, g[0], g[1], rand.New(rand.NewPCG(seed, 7)), 2*time.Second)
	return newTABoard(s, p, sol)
}

func TestTABoxesCompleteForEverySize(t *testing.T) {
	now := time.Unix(1000, 0)
	for _, s := range shapes {
		b := taFixture(t, s, 1)
		var done []int
		for i, v := range b.puzzle {
			if v == 0 {
				if r := b.place(i, b.solution[i], now, time.Second); r.Box >= 0 {
					done = append(done, r.Box)
				}
			}
		}
		if b.boxes != s.Size || len(done) != s.Size || b.mistakes != 0 {
			t.Fatalf("%dx%d: boxes=%d events=%v", s.Size, s.Size, b.boxes, done)
		}
		// a box completes exactly when its last empty cell gets the right digit
		seen := map[int]bool{}
		for _, x := range done {
			if seen[x] || x < 0 || x >= s.Size {
				t.Fatalf("%dx%d: bad box events %v", s.Size, s.Size, done)
			}
			seen[x] = true
		}
	}
}

func TestTABoxOnlyWhenAllCellsRight(t *testing.T) {
	s := shapes[0]
	b := taFixture(t, s, 3)
	now := time.Unix(1000, 0)
	var box0 []int
	for i, v := range b.puzzle {
		if v == 0 && s.Box(i) == 0 {
			box0 = append(box0, i)
		}
	}
	last := box0[len(box0)-1]
	for _, i := range box0[:len(box0)-1] {
		if r := b.place(i, b.solution[i], now, time.Second); r.Box != -1 {
			t.Fatal("box finished before its last cell")
		}
	}
	if r := b.place(last, b.solution[last]%4+1, now, time.Second); r.Correct || r.Box != -1 || b.boxes != 0 {
		t.Fatal("wrong digit must not finish a box")
	}
	if r := b.place(last, b.solution[last], now.Add(3*time.Second), time.Second); r.Box != 0 || b.boxes != 1 {
		t.Fatalf("box 0 should be done: %+v", r)
	}
}

func TestTALockAndCooldown(t *testing.T) {
	b := taFixture(t, shapes[2], 5)
	now := time.Unix(1000, 0)
	cell := -1
	for i, v := range b.puzzle {
		if v == 0 {
			cell = i
			break
		}
	}
	good, bad := b.solution[cell], b.solution[cell]%9+1

	r := b.place(cell, bad, now, 2*time.Second)
	if r.Correct || b.mistakes != 1 || b.cells != 0 || !r.Until.Equal(now.Add(2*time.Second)) {
		t.Fatalf("wrong: %+v mistakes=%d", r, b.mistakes)
	}
	// during the cooldown even the right digit is refused, and nothing is counted
	if r := b.place(cell, good, now.Add(time.Second), 2*time.Second); r.Rejected != "cooldown" || b.mistakes != 1 || b.cells != 0 {
		t.Fatalf("cooldown: %+v", r)
	}
	if r := b.place(cell, good, now.Add(2*time.Second), 2*time.Second); !r.Correct || b.cells != 1 {
		t.Fatalf("after cooldown: %+v", r)
	}
	// locked: re-sending (or changing) a correct cell does nothing
	for range 5 {
		if r := b.place(cell, good, now.Add(3*time.Second), 2*time.Second); r.Rejected != "locked" {
			t.Fatalf("farm: %+v", r)
		}
	}
	if r := b.place(cell, bad, now.Add(3*time.Second), 2*time.Second); r.Rejected != "locked" || b.mistakes != 1 || b.cells != 1 {
		t.Fatalf("locked cell changed: %+v", r)
	}
	// givens and out-of-range input
	for i, v := range b.puzzle {
		if v != 0 {
			if r := b.place(i, v, now, time.Second); r.Rejected != "invalid" {
				t.Fatal("givens are not playable")
			}
			break
		}
	}
	if b.place(-1, 1, now, time.Second).Rejected != "invalid" || b.place(cell+1, 10, now, time.Second).Rejected != "invalid" {
		t.Fatal("out of range input accepted")
	}
}

func TestTAFullBoardWinsEarly(t *testing.T) {
	b := taFixture(t, shapes[1], 9)
	now := time.Unix(1000, 0)
	var last taVerdict
	for i, v := range b.puzzle {
		if v == 0 {
			last = b.place(i, b.solution[i], now, time.Second)
		}
	}
	if !last.Full {
		t.Fatal("last correct cell must report a full board")
	}
}

func TestTATieBreakers(t *testing.T) {
	cases := []struct {
		a, b   taScore
		want   int
		reason string
	}{
		{taScore{3, 10, 9}, taScore{2, 30, 0}, 1, "boxes"},     // boxes beat everything
		{taScore{0, 5, 4}, taScore{0, 7, 0}, -1, "cells"},      // no boxes at all: correct cells decide
		{taScore{2, 12, 1}, taScore{2, 11, 0}, 1, "cells"},     // same boxes: cells
		{taScore{2, 12, 3}, taScore{2, 12, 1}, -1, "mistakes"}, // same boxes and cells: fewer mistakes
		{taScore{1, 6, 2}, taScore{1, 6, 2}, 0, "draw"},
		{taScore{0, 0, 0}, taScore{0, 0, 0}, 0, "draw"},
	}
	for _, c := range cases {
		if got, why := compareTA(c.a, c.b); got != c.want || why != c.reason {
			t.Errorf("compare(%v, %v) = %d %s, want %d %s", c.a, c.b, got, why, c.want, c.reason)
		}
		if got, _ := compareTA(c.b, c.a); got != -c.want {
			t.Errorf("compare must be symmetric for %v %v", c.a, c.b)
		}
	}
}

func TestTAGeneratorLeavesEveryBoxOpen(t *testing.T) {
	g := map[int][][2]int{4: {{8, 10}, {6, 7}, {5, 5}, {4, 4}}, 6: {{18, 20}, {14, 16}}, 9: {{40, 45}, {24, 27}}}
	for _, s := range shapes {
		for _, r := range g[s.Size] {
			for seed := range uint64(15) {
				p, sol := sudoku.GenerateOpen(s, r[0], r[1], rand.New(rand.NewPCG(seed, uint64(s.Size))), time.Second)
				if !sudoku.OpenBoxes(p, s) || sudoku.CountSolutions(p, s) != 1 || len(sol) != s.Size*s.Size {
					t.Fatalf("%dx%d givens %v seed %d: full box or not unique", s.Size, s.Size, r, seed)
				}
			}
		}
	}
}

func TestTALateInput(t *testing.T) {
	end := time.Unix(2000, 0)
	late := 300 * time.Millisecond
	if !inTime(end.Add(-time.Second), end, late) || !inTime(end.Add(299*time.Millisecond), end, late) {
		t.Fatal("moves before the end (and within latency allowance) must count")
	}
	if inTime(end.Add(301*time.Millisecond), end, late) {
		t.Fatal("moves after endsAt + allowance must be refused")
	}
}

func TestEloDraw(t *testing.T) {
	if a, b := elo(1000, 1000, 0.5); a != 1000 || b != 1000 {
		t.Fatalf("equal draw: %d %d", a, b)
	}
	if a, b := elo(1200, 1000, 0.5); a >= 1200 || b <= 1000 || a+b != 2200 {
		t.Fatalf("a draw costs the favourite: %d %d", a, b)
	}
}
