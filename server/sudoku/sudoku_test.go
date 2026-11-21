package sudoku

import (
	"encoding/json"
	"math/rand/v2"
	"strconv"
	"testing"
	"time"

	"github.com/Mqdd27/sudoku/shared"
)

type fixtureFile struct {
	Size, BoxRows, BoxCols int
	Puzzles                []struct {
		Level            string
		Givens           int
		Puzzle, Solution string
	}
}

func parse(s string) []int {
	g := make([]int, len(s))
	for i, c := range s {
		g[i] = int(c - '0')
	}
	return g
}

func equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var cfg = shared.Load()

func shapeOf(size int) Shape {
	c := cfg.Sizes[strconv.Itoa(size)]
	return Shape{size, c.BoxRows, c.BoxCols}
}

// Same vectors as web/test/sudoku.test.ts: both solvers must agree.
func TestSharedFixtures(t *testing.T) {
	for _, size := range []int{4, 6, 9} {
		raw, err := shared.Fixtures.ReadFile("fixtures/sudoku-" + strconv.Itoa(size) + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var f fixtureFile
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatal(err)
		}
		s := Shape{f.Size, f.BoxRows, f.BoxCols}
		if s != shapeOf(size) || !s.Valid() {
			t.Fatalf("fixture shape %v does not match config", s)
		}
		for _, p := range f.Puzzles {
			grid := parse(p.Puzzle)
			if Givens(grid) != p.Givens {
				t.Errorf("%dx%d %s: givens %d, fixture says %d", size, size, p.Level, Givens(grid), p.Givens)
			}
			if n := CountSolutions(grid, s); n != 1 {
				t.Errorf("%dx%d %s: %d solutions", size, size, p.Level, n)
			}
			if !equal(Solve(grid, s), parse(p.Solution)) {
				t.Errorf("%dx%d %s: solution differs from fixture", size, size, p.Level)
			}
			if r, ok := cfg.Givens[strconv.Itoa(size)][p.Level]; ok && (p.Givens < r[0] || p.Givens > r[1]) {
				t.Errorf("%dx%d %s: givens %d outside %v", size, size, p.Level, p.Givens, r)
			}
		}
	}
}

func TestShapes(t *testing.T) {
	if (Shape{3, 1, 3}).Valid() || (Shape{6, 3, 3}).Valid() {
		t.Fatal("invalid shapes accepted")
	}
	s := shapeOf(6)
	g := make([]int, 36)
	g[0], g[8] = 1, 1 // (0,0) and (1,2): same 2x3 box
	if c := Conflicts(g, s); len(c) != 2 {
		t.Fatalf("box conflict not found: %v", c)
	}
	g[8], g[13] = 0, 1 // (2,1): next box, other row and column
	if c := Conflicts(g, s); len(c) != 0 {
		t.Fatalf("false conflict: %v", c)
	}
}

func TestSolverEdgeCases(t *testing.T) {
	for _, size := range []int{4, 6, 9} {
		if n := CountSolutions(make([]int, size*size), shapeOf(size)); n != 2 {
			t.Errorf("%dx%d empty: want 2 (many), got %d", size, size, n)
		}
	}
	g := make([]int, 16)
	g[0] = 5 // digit out of range for 4x4
	if CountSolutions(g, shapeOf(4)) != 0 {
		t.Error("out-of-range digit accepted")
	}
	g[0], g[1] = 1, 1
	if CountSolutions(g, shapeOf(4)) != 0 {
		t.Error("clashing givens accepted")
	}
	if CountSolutions(make([]int, 10), shapeOf(4)) != 0 {
		t.Error("wrong length accepted")
	}
}

func TestGenerate(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for _, size := range []int{4, 6, 9} {
		s := shapeOf(size)
		for level, r := range cfg.Givens[strconv.Itoa(size)] {
			for range 3 {
				p, sol := Generate(s, r[0], r[1], rng, 3*time.Second)
				if n := Givens(p); n < r[0] || n > r[1] {
					t.Errorf("%dx%d %s: %d givens, want %v", size, size, level, n, r)
				}
				if CountSolutions(p, s) != 1 || len(Conflicts(sol, s)) != 0 || Givens(sol) != size*size {
					t.Errorf("%dx%d %s: bad puzzle", size, size, level)
				}
				for i, v := range p {
					if v != 0 && v != sol[i] {
						t.Fatalf("%dx%d %s: puzzle disagrees with solution", size, size, level)
					}
				}
			}
		}
	}
}

func TestGenerateDeterministic(t *testing.T) {
	a, _ := Generate(shapeOf(9), 30, 34, rand.New(rand.NewPCG(7, 7)), time.Minute)
	b, _ := Generate(shapeOf(9), 30, 34, rand.New(rand.NewPCG(7, 7)), time.Minute)
	if !equal(a, b) {
		t.Fatal("same seed gave different puzzles")
	}
}
