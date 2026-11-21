package main

import (
	"time"

	"github.com/Mqdd27/sudoku/server/sudoku"
)

// Time Attack: both players get the same puzzle and a fixed time. A correct digit locks its cell; a wrong
// one counts a mistake and freezes that cell for a cooldown. At the end the most completed boxes wins.
// Everything here is pure, so the rules are tested without sockets or timers.

type taBoard struct {
	shape    sudoku.Shape
	puzzle   []int
	solution []int
	values   []int       // givens + locked correct digits only; wrong digits are never stored
	cooldown []time.Time // cell refuses input until this time
	boxDone  []bool
	cells    int // empty cells filled correctly
	boxes    int // boxes completed
	mistakes int
}

func newTABoard(s sudoku.Shape, puzzle, solution []int) *taBoard {
	return &taBoard{shape: s, puzzle: puzzle, solution: solution, values: append([]int(nil), puzzle...),
		cooldown: make([]time.Time, len(puzzle)), boxDone: make([]bool, s.Size)}
}

type taVerdict struct {
	Correct  bool
	Rejected string // "", "locked", "cooldown", "invalid"
	Box      int    // box just completed, or -1
	Full     bool   // whole board done
	Until    time.Time
}

// place judges one digit at time now. Locked and cooling cells are refused without any effect,
// so re-sending a correct digit can't be farmed and guessing 1–9 fast is throttled.
func (b *taBoard) place(cell, value int, now time.Time, cooldown time.Duration) taVerdict {
	v := taVerdict{Box: -1}
	switch {
	case cell < 0 || cell >= len(b.puzzle) || value < 1 || value > b.shape.Size || b.puzzle[cell] != 0:
		v.Rejected = "invalid"
	case b.values[cell] != 0:
		v.Rejected = "locked"
	case now.Before(b.cooldown[cell]):
		v.Rejected, v.Until = "cooldown", b.cooldown[cell]
	case b.solution[cell] != value:
		b.mistakes++
		b.cooldown[cell] = now.Add(cooldown)
		v.Until = b.cooldown[cell]
	default:
		v.Correct = true
		b.values[cell] = value
		b.cells++
		if x := b.shape.Box(cell); b.boxComplete(x) {
			b.boxDone[x] = true
			b.boxes++
			v.Box = x
		}
		v.Full = b.boxes == b.shape.Size
	}
	return v
}

func (b *taBoard) boxComplete(x int) bool {
	for i, v := range b.values {
		if b.shape.Box(i) == x && v != b.solution[i] {
			return false
		}
	}
	return true
}

func (b *taBoard) score() taScore { return taScore{b.boxes, b.cells, b.mistakes} }

type taScore struct{ Boxes, Cells, Mistakes int }

// compareTA: 1 if a wins, -1 if b wins, 0 for a draw, plus what decided it.
// Order: more boxes, then more correct cells, then fewer mistakes.
func compareTA(a, b taScore) (int, string) {
	switch {
	case a.Boxes != b.Boxes:
		return sign(a.Boxes - b.Boxes), "boxes"
	case a.Cells != b.Cells:
		return sign(a.Cells - b.Cells), "cells"
	case a.Mistakes != b.Mistakes:
		return sign(b.Mistakes - a.Mistakes), "mistakes"
	}
	return 0, "draw"
}

func sign(n int) int {
	if n > 0 {
		return 1
	}
	return -1
}

// inTime: moves are accepted until endsAt plus a small allowance for network latency.
func inTime(now, endsAt time.Time, late time.Duration) bool { return !now.After(endsAt.Add(late)) }
