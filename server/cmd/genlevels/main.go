// Command genlevels writes story/levels.json: fixed Story Mode puzzles, the same for every player.
// Run from server/: go run ./cmd/genlevels. The seed is fixed, so re-running gives the same file.
// To add a chapter, append to `chapters` and run it again (existing levels stay identical).
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"strings"
	"time"

	"github.com/Mqdd27/sudoku/server/sudoku"
	"github.com/Mqdd27/sudoku/shared"
)

const levelsPerChapter = 10

var chapters = []struct{ title, difficulty string }{
	{"First Steps", "easy"},
	{"Finding Patterns", "medium"},
	{"Deep Thinking", "hard"},
	{"Master Class", "expert"},
}

type chapter struct {
	N          int    `json:"n"`
	Title      string `json:"title"`
	Difficulty string `json:"difficulty"`
}

type level struct {
	N          int    `json:"n"`
	Chapter    int    `json:"chapter"`
	Difficulty string `json:"difficulty"`
	Givens     int    `json:"givens"`
	Puzzle     string `json:"puzzle"`
	Solution   string `json:"solution"`
}

func str(g []int) string {
	var b strings.Builder
	for _, v := range g {
		fmt.Fprint(&b, v)
	}
	return b.String()
}

func main() {
	out := "story/levels.json"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	cfg := shared.Load()
	shape := sudoku.Shape{Size: 9, BoxRows: 3, BoxCols: 3}
	var f struct {
		Chapters []chapter `json:"chapters"`
		Levels   []level   `json:"levels"`
	}
	for ci, c := range chapters {
		f.Chapters = append(f.Chapters, chapter{ci + 1, c.title, c.difficulty})
		r := cfg.Givens["9"][c.difficulty]
		for li := range levelsPerChapter {
			n := ci*levelsPerChapter + li + 1
			// within a chapter, givens step down from the range's max to its min
			target := r[1] - (r[1]-r[0])*li/(levelsPerChapter-1)
			rng := rand.New(rand.NewPCG(20260101, uint64(n))) // one seed per level: adding chapters never changes earlier ones
			p, sol := sudoku.Generate(shape, target, target, rng, 10*time.Minute)
			if sudoku.CountSolutions(p, shape) != 1 || sudoku.Givens(p) != target {
				log.Fatalf("level %d: generator did not reach %d givens with a unique solution", n, target)
			}
			f.Levels = append(f.Levels, level{n, ci + 1, c.difficulty, target, str(p), str(sol)})
			fmt.Fprintf(os.Stderr, "level %2d  %-6s  %d givens\n", n, c.difficulty, target)
		}
	}
	b, _ := json.MarshalIndent(f, "", "  ")
	if err := os.WriteFile(out, append(b, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
}
