// Package sudoku is the solver, generator and validator for box-shaped Sudoku up to 9x9.
// It mirrors web/src/logic/sudoku.ts; both are checked against shared/fixtures.
package sudoku

import (
	"math/bits"
	"math/rand/v2"
	"sync"
	"time"
)

// Shape describes a board: Size x Size cells, boxes of BoxRows x BoxCols.
type Shape struct{ Size, BoxRows, BoxCols int }

// Valid reports whether the shape is a real Sudoku this package supports (4x4, 6x6, 9x9 style).
func (s Shape) Valid() bool {
	return s.Size >= 4 && s.Size <= 9 && s.BoxRows*s.BoxCols == s.Size && s.Size%s.BoxCols == 0 && s.Size%s.BoxRows == 0
}

type geo struct {
	n, cells int
	box      []int
	peers    [][]int
	full     uint16
}

var geos sync.Map // Shape -> *geo

func (s Shape) geo() *geo {
	if g, ok := geos.Load(s); ok {
		return g.(*geo)
	}
	n, cells, perRow := s.Size, s.Size*s.Size, s.Size/s.BoxCols
	g := &geo{n: n, cells: cells, box: make([]int, cells), peers: make([][]int, cells), full: uint16(1<<(n+1)) - 2}
	for i := range cells {
		g.box[i] = (i/n)/s.BoxRows*perRow + (i%n)/s.BoxCols
	}
	for i := range cells {
		for j := range cells {
			if j != i && (j/n == i/n || j%n == i%n || g.box[j] == g.box[i]) {
				g.peers[i] = append(g.peers[i], j)
			}
		}
	}
	geos.Store(s, g)
	return g
}

// Peers returns the cells sharing a row, column or box with cell i.
func (s Shape) Peers(i int) []int { return s.geo().peers[i] }

// Search counts solutions up to limit and returns the first one found.
// rng != nil fills digits in random order (used to make random full grids).
func Search(grid []int, s Shape, limit int, rng *rand.Rand) (count int, solution []int) {
	g := s.geo()
	if len(grid) != g.cells {
		return 0, nil
	}
	b := append([]int(nil), grid...)
	rows, cols, boxes := make([]uint16, g.n), make([]uint16, g.n), make([]uint16, g.n)
	for i, v := range b {
		if v == 0 {
			continue
		}
		bit := uint16(1) << v
		r, c, x := i/g.n, i%g.n, g.box[i]
		if v < 0 || v > g.n || (rows[r]|cols[c]|boxes[x])&bit != 0 {
			return 0, nil
		}
		rows[r] |= bit
		cols[c] |= bit
		boxes[x] |= bit
	}
	digits := make([]int, g.n)
	for i := range digits {
		digits[i] = i + 1
	}

	var rec func() bool
	rec = func() bool {
		best, bestN := -1, g.n+1
		var bestMask uint16
		for i := range g.cells {
			if b[i] != 0 {
				continue
			}
			m := ^(rows[i/g.n] | cols[i%g.n] | boxes[g.box[i]]) & g.full
			if m == 0 {
				return false
			}
			if k := bits.OnesCount16(m); k < bestN {
				best, bestMask, bestN = i, m, k
				if k == 1 {
					break
				}
			}
		}
		if best < 0 {
			count++
			if count == 1 {
				solution = append([]int(nil), b...)
			}
			return count >= limit
		}
		r, c, x := best/g.n, best%g.n, g.box[best]
		order := digits
		if rng != nil {
			order = append([]int(nil), digits...)
			rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
		}
		for _, d := range order {
			bit := uint16(1) << d
			if bestMask&bit == 0 {
				continue
			}
			b[best] = d
			rows[r] |= bit
			cols[c] |= bit
			boxes[x] |= bit
			if rec() {
				return true
			}
			b[best] = 0
			rows[r] &^= bit
			cols[c] &^= bit
			boxes[x] &^= bit
		}
		return false
	}
	rec()
	return count, solution
}

// CountSolutions returns 0, 1 or 2 (2 meaning "more than one").
func CountSolutions(grid []int, s Shape) int { n, _ := Search(grid, s, 2, nil); return n }

// Solve returns the first solution or nil.
func Solve(grid []int, s Shape) []int { _, sol := Search(grid, s, 1, nil); return sol }

// Generate makes a puzzle with a unique solution and between min and max givens.
// If the budget runs out it returns the closest unique puzzle found (a few givens above max).
func Generate(s Shape, min, max int, rng *rand.Rand, budget time.Duration) (puzzle, solution []int) {
	cells := s.Size * s.Size
	deadline := time.Now().Add(budget)
	bestGivens := cells + 1
	for {
		_, sol := Search(make([]int, cells), s, 1, rng)
		p := append([]int(nil), sol...)
		target := min + rng.IntN(max-min+1)
		givens := cells
		for _, i := range rng.Perm(cells) {
			if givens <= target {
				break
			}
			v := p[i]
			p[i] = 0
			if CountSolutions(p, s) == 1 {
				givens--
			} else {
				p[i] = v
			}
		}
		if givens <= max {
			return p, sol
		}
		if givens < bestGivens {
			puzzle, solution, bestGivens = p, sol, givens
		}
		if time.Now().After(deadline) {
			return puzzle, solution
		}
	}
}

// Conflicts returns the filled cells that duplicate a peer's value.
func Conflicts(grid []int, s Shape) []int {
	g := s.geo()
	var out []int
	for i, v := range grid {
		if v == 0 {
			continue
		}
		for _, j := range g.peers[i] {
			if grid[j] == v {
				out = append(out, i)
				break
			}
		}
	}
	return out
}

// Givens counts non-empty cells.
func Givens(grid []int) int {
	n := 0
	for _, v := range grid {
		if v != 0 {
			n++
		}
	}
	return n
}
