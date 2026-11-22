package server

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"github.com/Mqdd27/sudoku/server/sudoku"
)

// story/levels.json is produced by `go run ./cmd/genlevels` and never changes at runtime.
//
//go:embed story/levels.json
var storyJSON []byte

var story9 = sudoku.Shape{Size: 9, BoxRows: 3, BoxCols: 3}

type StoryChapter struct {
	N          int    `json:"n"`
	Title      string `json:"title"`
	Difficulty string `json:"difficulty"`
}

type StoryLevel struct {
	N          int    `json:"n"`
	Chapter    int    `json:"chapter"`
	Difficulty string `json:"difficulty"`
	Puzzle     string `json:"puzzle"`
	Solution   string `json:"solution"`
	puzzle     []int
	solution   []int
}

type storyFile struct {
	Chapters []StoryChapter `json:"chapters"`
	Levels   []StoryLevel   `json:"levels"`
}

var storyChapters []StoryChapter

func digits(s string) []int {
	g := make([]int, len(s))
	for i, c := range s {
		g[i] = int(c - '0')
	}
	return g
}

func loadStory() []StoryLevel {
	var f storyFile
	if err := json.Unmarshal(storyJSON, &f); err != nil {
		panic("story/levels.json: " + err.Error())
	}
	for i := range f.Levels {
		f.Levels[i].puzzle, f.Levels[i].solution = digits(f.Levels[i].Puzzle), digits(f.Levels[i].Solution)
	}
	storyChapters = f.Chapters
	return f.Levels
}

// Par time per difficulty for the time star.
var parTime = map[string]time.Duration{"easy": 6 * time.Minute, "medium": 10 * time.Minute, "hard": 15 * time.Minute, "expert": 20 * time.Minute}

// stars: start at 3, lose one each for using hints, more than 2 mistakes, and going over par. Never below 1.
func stars(difficulty string, elapsed time.Duration, mistakes, hints int) int {
	s := 3
	if hints > 0 {
		s--
	}
	if mistakes > 2 {
		s--
	}
	if elapsed > parTime[difficulty] {
		s--
	}
	return max(s, 1)
}

// storyLevel resolves {n} and checks it is unlocked for u. On failure it has written the response.
func (a *App) storyLevel(w http.ResponseWriter, r *http.Request, u *User) *StoryLevel {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 1 || n > len(a.story) {
		httpError(w, http.StatusNotFound, "No such level.")
		return nil
	}
	if n > 1 {
		var x int
		if err := a.db.QueryRow(`SELECT 1 FROM story_progress WHERE user_id = ? AND level = ?`, u.ID, n-1).Scan(&x); err != nil {
			httpError(w, http.StatusForbidden, "Finish the previous level first.")
			return nil
		}
	}
	return &a.story[n-1]
}

type storyBoard struct {
	Values []int `json:"values"`
	Notes  []int `json:"notes"`
}

type storySave struct {
	storyBoard
	ElapsedMs int64 `json:"elapsedMs"`
	Mistakes  int   `json:"mistakes"`
	Hints     int   `json:"hints"`
	startedAt int64
}

func (a *App) loadSave(userID int64, n int) (*storySave, error) {
	var s storySave
	var board string
	err := a.db.QueryRow(`SELECT board, elapsed_ms, mistakes, hints, started_at FROM story_saves WHERE user_id = ? AND level = ?`, userID, n).
		Scan(&board, &s.ElapsedMs, &s.Mistakes, &s.Hints, &s.startedAt)
	if err != nil {
		return nil, err
	}
	return &s, json.Unmarshal([]byte(board), &s.storyBoard)
}

func (a *App) writeSave(userID int64, n int, s *storySave) error {
	board, _ := json.Marshal(s.storyBoard)
	_, err := a.db.Exec(`INSERT INTO story_saves(user_id, level, board, elapsed_ms, mistakes, hints, started_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?) ON CONFLICT(user_id, level) DO UPDATE SET board = excluded.board,
		elapsed_ms = excluded.elapsed_ms, mistakes = excluded.mistakes, hints = excluded.hints, updated_at = excluded.updated_at`,
		userID, n, string(board), s.ElapsedMs, s.Mistakes, s.Hints, s.startedAt, a.now().UnixMilli())
	return err
}

// clampElapsed: the client's clock pauses, so it may report less than wall time, never more.
func (a *App) clampElapsed(s *storySave, claimed int64) int64 {
	return max(0, min(claimed, a.now().UnixMilli()-s.startedAt+2000))
}

// validBoard: right length, digits in range, givens untouched, notes are bitmasks.
func validBoard(l *StoryLevel, b storyBoard) bool {
	if len(b.Values) != len(l.puzzle) || len(b.Notes) != len(l.puzzle) {
		return false
	}
	for i, v := range b.Values {
		if v < 0 || v > 9 || (l.puzzle[i] != 0 && v != l.puzzle[i]) || b.Notes[i] < 0 || b.Notes[i] > 1023 {
			return false
		}
	}
	return true
}

func (a *App) storyMap(w http.ResponseWriter, r *http.Request, u *User) {
	type lvl struct {
		N          int    `json:"n"`
		Chapter    int    `json:"chapter"`
		Difficulty string `json:"difficulty"`
		Stars      int    `json:"stars"`
		BestMs     int64  `json:"bestMs,omitempty"`
		Unlocked   bool   `json:"unlocked"`
		InProgress bool   `json:"inProgress"`
	}
	levels := make([]lvl, len(a.story))
	for i, l := range a.story {
		levels[i] = lvl{N: l.N, Chapter: l.Chapter, Difficulty: l.Difficulty, Unlocked: i == 0}
	}
	rows, err := a.db.Query(`SELECT level, stars, best_ms FROM story_progress WHERE user_id = ?`, u.ID)
	if err != nil {
		a.fail(w, err)
		return
	}
	for rows.Next() {
		var n, s int
		var ms int64
		rows.Scan(&n, &s, &ms)
		if n >= 1 && n <= len(levels) {
			levels[n-1].Stars, levels[n-1].BestMs = s, ms
			if n < len(levels) {
				levels[n].Unlocked = true
			}
		}
	}
	rows.Close()
	cont, latest := 0, int64(0)
	rows, err = a.db.Query(`SELECT level, updated_at FROM story_saves WHERE user_id = ?`, u.ID)
	if err != nil {
		a.fail(w, err)
		return
	}
	for rows.Next() {
		var n int
		var at int64
		rows.Scan(&n, &at)
		if n >= 1 && n <= len(levels) {
			levels[n-1].InProgress = true
			if at > latest {
				cont, latest = n, at
			}
		}
	}
	rows.Close()
	if cont == 0 { // nothing in progress: the first unlocked level not finished yet, else the last one
		cont = len(levels)
		for _, l := range levels {
			if l.Unlocked && l.Stars == 0 {
				cont = l.N
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"chapters": storyChapters, "levels": levels, "continue": cont})
}

func (a *App) storyStart(w http.ResponseWriter, r *http.Request, u *User) {
	l := a.storyLevel(w, r, u)
	if l == nil {
		return
	}
	s, err := a.loadSave(u.ID, l.N)
	if errors.Is(err, sql.ErrNoRows) {
		s = &storySave{storyBoard: storyBoard{Values: append([]int(nil), l.puzzle...), Notes: make([]int, len(l.puzzle))}, startedAt: a.now().UnixMilli()}
		err = a.writeSave(u.ID, l.N, s)
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"n": l.N, "chapter": l.Chapter, "difficulty": l.Difficulty, "puzzle": l.puzzle,
		"values": s.Values, "notes": s.Notes, "elapsedMs": s.ElapsedMs, "mistakes": s.Mistakes, "hints": s.Hints,
	})
}

// session loads the in-progress save for the level in the URL.
func (a *App) storySession(w http.ResponseWriter, r *http.Request, u *User) (*StoryLevel, *storySave) {
	l := a.storyLevel(w, r, u)
	if l == nil {
		return nil, nil
	}
	s, err := a.loadSave(u.ID, l.N)
	if err != nil {
		httpError(w, http.StatusConflict, "Start the level first.")
		return nil, nil
	}
	return l, s
}

func (a *App) storySave(w http.ResponseWriter, r *http.Request, u *User) {
	var b struct {
		storyBoard
		ElapsedMs int64 `json:"elapsedMs"`
	}
	if !readJSON(w, r, &b) {
		return
	}
	l, s := a.storySession(w, r, u)
	if l == nil {
		return
	}
	if !validBoard(l, b.storyBoard) {
		httpError(w, http.StatusBadRequest, "invalid board")
		return
	}
	s.storyBoard, s.ElapsedMs = b.storyBoard, a.clampElapsed(s, b.ElapsedMs)
	if err := a.writeSave(u.ID, l.N, s); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// storyCheck judges one placed digit. A wrong digit counts as a mistake unless it is the
// same wrong digit the server already saw in that cell (e.g. after an undo).
func (a *App) storyCheck(w http.ResponseWriter, r *http.Request, u *User) {
	var b struct {
		Cell  int `json:"cell"`
		Value int `json:"value"`
	}
	if !readJSON(w, r, &b) {
		return
	}
	l, s := a.storySession(w, r, u)
	if l == nil {
		return
	}
	if b.Cell < 0 || b.Cell >= len(l.puzzle) || b.Value < 1 || b.Value > 9 || l.puzzle[b.Cell] != 0 {
		httpError(w, http.StatusBadRequest, "invalid cell")
		return
	}
	correct := l.solution[b.Cell] == b.Value
	if !correct && s.Values[b.Cell] != b.Value {
		s.Mistakes++
	}
	s.Values[b.Cell] = b.Value
	if err := a.writeSave(u.ID, l.N, s); err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"correct": correct, "mistakes": s.Mistakes})
}

// storyHint reveals one cell: the requested one if it's still wrong/empty, otherwise a random open cell.
func (a *App) storyHint(w http.ResponseWriter, r *http.Request, u *User) {
	var b struct {
		Values []int `json:"values"`
		Cell   int   `json:"cell"`
	}
	if !readJSON(w, r, &b) {
		return
	}
	l, s := a.storySession(w, r, u)
	if l == nil {
		return
	}
	if !validBoard(l, storyBoard{b.Values, s.Notes}) {
		httpError(w, http.StatusBadRequest, "invalid board")
		return
	}
	open := func(i int) bool { return b.Values[i] != l.solution[i] }
	cell := b.Cell
	if cell < 0 || cell >= len(b.Values) || !open(cell) {
		var cands []int
		for i := range b.Values {
			if open(i) {
				cands = append(cands, i)
			}
		}
		if len(cands) == 0 {
			writeJSON(w, http.StatusOK, nil)
			return
		}
		cell = cands[rand.IntN(len(cands))]
	}
	s.Values = b.Values
	s.Values[cell] = l.solution[cell]
	s.Notes[cell] = 0
	s.Hints++
	if err := a.writeSave(u.ID, l.N, s); err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"cell": cell, "value": l.solution[cell]})
}

func (a *App) storySubmit(w http.ResponseWriter, r *http.Request, u *User) {
	var b struct {
		Values    []int `json:"values"`
		ElapsedMs int64 `json:"elapsedMs"`
	}
	if !readJSON(w, r, &b) {
		return
	}
	l, s := a.storySession(w, r, u)
	if l == nil {
		return
	}
	if len(b.Values) != len(l.solution) {
		httpError(w, http.StatusBadRequest, "invalid board")
		return
	}
	for i, v := range b.Values {
		if v != l.solution[i] {
			writeJSON(w, http.StatusOK, map[string]any{"solved": false})
			return
		}
	}
	ms := a.clampElapsed(s, b.ElapsedMs)
	st := stars(l.Difficulty, time.Duration(ms)*time.Millisecond, s.Mistakes, s.Hints)
	tx, err := a.db.Begin()
	if err != nil {
		a.fail(w, err)
		return
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO story_progress(user_id, level, stars, best_ms, completed_at) VALUES (?,?,?,?,?)
		ON CONFLICT(user_id, level) DO UPDATE SET stars = MAX(stars, excluded.stars), best_ms = MIN(best_ms, excluded.best_ms)`,
		u.ID, l.N, st, ms, a.now().UnixMilli())
	if err == nil {
		_, err = tx.Exec(`DELETE FROM story_saves WHERE user_id = ? AND level = ?`, u.ID, l.N)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	next := 0
	if l.N < len(a.story) {
		next = l.N + 1
	}
	writeJSON(w, http.StatusOK, map[string]any{"solved": true, "stars": st, "ms": ms, "mistakes": s.Mistakes, "hints": s.Hints, "next": next})
}
