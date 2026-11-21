package main

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// validSizeLevel checks a (size, level) pair against shared/config.json.
func (a *App) validSizeLevel(size int, level string) bool {
	_, ok := a.shared.Givens[strconv.Itoa(size)][level]
	return ok
}

type classicStat struct {
	Size   int    `json:"size"`
	Level  string `json:"level"`
	Solved int    `json:"solved"`
	BestMs *int64 `json:"bestMs"`
}

type pvpStat struct {
	Size   int `json:"size"`
	Rating int `json:"rating"`
	Wins   int `json:"wins"`
	Losses int `json:"losses"`
}

func (a *App) profile(w http.ResponseWriter, r *http.Request) {
	var id, created int64
	var name string
	err := a.db.QueryRow(`SELECT id, username, created_at FROM users WHERE username_lc = ?`, strings.ToLower(r.PathValue("username"))).
		Scan(&id, &name, &created)
	if err != nil {
		httpError(w, http.StatusNotFound, "No such player.")
		return
	}
	out := struct {
		Username string        `json:"username"`
		Created  int64         `json:"createdAt"`
		Classic  []classicStat `json:"classic"`
		Story    struct {
			Completed int `json:"completed"`
			Stars     int `json:"stars"`
			Total     int `json:"total"`
		} `json:"story"`
		PvP []pvpStat `json:"pvp"`
	}{Username: name, Created: created, Classic: []classicStat{}, PvP: []pvpStat{}}

	rows, err := a.db.Query(`SELECT size, level, solved, best_ms FROM classic_stats WHERE user_id = ? ORDER BY size, level`, id)
	if err != nil {
		a.fail(w, err)
		return
	}
	for rows.Next() {
		var s classicStat
		rows.Scan(&s.Size, &s.Level, &s.Solved, &s.BestMs)
		out.Classic = append(out.Classic, s)
	}
	rows.Close()
	a.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(stars), 0) FROM story_progress WHERE user_id = ?`, id).
		Scan(&out.Story.Completed, &out.Story.Stars)
	out.Story.Total = len(a.story)
	rows, err = a.db.Query(`SELECT size, rating, wins, losses FROM ratings WHERE user_id = ? ORDER BY size`, id)
	if err != nil {
		a.fail(w, err)
		return
	}
	for rows.Next() {
		var s pvpStat
		rows.Scan(&s.Size, &s.Rating, &s.Wins, &s.Losses)
		out.PvP = append(out.PvP, s)
	}
	rows.Close()
	writeJSON(w, http.StatusOK, out)
}

// Classic games are generated and judged in the browser, so these times are self-reported.
// Note: the Classic leaderboard is therefore trust-based; move Classic to server puzzles if it gets gamed.
func (a *App) recordClassic(userID int64, size int, level string, solved int, bestMs int64) error {
	_, err := a.db.Exec(`INSERT INTO classic_stats(user_id, size, level, solved, best_ms) VALUES (?,?,?,?,?)
		ON CONFLICT(user_id, size, level) DO UPDATE SET solved = solved + excluded.solved,
		best_ms = MIN(COALESCE(best_ms, excluded.best_ms), excluded.best_ms)`, userID, size, level, solved, bestMs)
	return err
}

func (a *App) classicResult(w http.ResponseWriter, r *http.Request, u *User) {
	var b struct {
		Size  int    `json:"size"`
		Level string `json:"level"`
		Ms    int64  `json:"ms"`
	}
	if !readJSON(w, r, &b) {
		return
	}
	if !a.validSizeLevel(b.Size, b.Level) || b.Ms < 1000 || b.Ms > (24*time.Hour).Milliseconds() {
		httpError(w, http.StatusBadRequest, "invalid result")
		return
	}
	if err := a.recordClassic(u.ID, b.Size, b.Level, 1, b.Ms); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// classicImport merges the browser's local Classic stats into the account, once per account.
// Body: {"stats": {"9": {"easy": {"solved": 3, "best": 312}}}}, best in seconds (localStorage format).
func (a *App) classicImport(w http.ResponseWriter, r *http.Request, u *User) {
	var b struct {
		Stats map[string]map[string]struct {
			Solved int   `json:"solved"`
			Best   int64 `json:"best"`
		} `json:"stats"`
	}
	if !readJSON(w, r, &b) {
		return
	}
	res, err := a.db.Exec(`UPDATE users SET imported_at = ? WHERE id = ? AND imported_at IS NULL`, a.now().UnixMilli(), u.ID)
	if err != nil {
		a.fail(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpError(w, http.StatusConflict, "Stats were already imported.")
		return
	}
	imported := 0
	for sizeStr, levels := range b.Stats {
		size, _ := strconv.Atoi(sizeStr)
		for level, s := range levels {
			if !a.validSizeLevel(size, level) || s.Solved <= 0 || s.Best < 1 {
				continue
			}
			if err := a.recordClassic(u.ID, size, level, min(s.Solved, 10000), s.Best*1000); err != nil {
				a.fail(w, err)
				return
			}
			imported++
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"imported": imported})
}

type boardRow struct {
	Username string `json:"username"`
	Value    int64  `json:"value"` // rating or milliseconds
	Wins     *int   `json:"wins,omitempty"`
	Losses   *int   `json:"losses,omitempty"`
}

// leaderboard: ?kind=pvp&size=9 | ?kind=classic&size=9&level=easy | ?kind=story&level=3
func (a *App) leaderboard(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	size, _ := strconv.Atoi(q.Get("size"))
	level := q.Get("level")
	var sqlText string
	var args []any
	switch q.Get("kind") {
	case "pvp":
		if !a.validSizeLevel(size, "easy") {
			httpError(w, 400, "bad size")
			return
		}
		sqlText = `SELECT u.username, r.rating, r.wins, r.losses FROM ratings r JOIN users u ON u.id = r.user_id
			WHERE r.size = ? AND r.wins + r.losses > 0 ORDER BY r.rating DESC, r.wins DESC LIMIT 20`
		args = []any{size}
	case "classic":
		if !a.validSizeLevel(size, level) {
			httpError(w, 400, "bad size or level")
			return
		}
		sqlText = `SELECT u.username, c.best_ms, NULL, NULL FROM classic_stats c JOIN users u ON u.id = c.user_id
			WHERE c.size = ? AND c.level = ? AND c.best_ms IS NOT NULL ORDER BY c.best_ms LIMIT 20`
		args = []any{size, level}
	case "story":
		n, _ := strconv.Atoi(level)
		if n < 1 || n > len(a.story) {
			httpError(w, 400, "bad level")
			return
		}
		sqlText = `SELECT u.username, s.best_ms, NULL, NULL FROM story_progress s JOIN users u ON u.id = s.user_id
			WHERE s.level = ? ORDER BY s.best_ms LIMIT 20`
		args = []any{n}
	default:
		httpError(w, 400, "kind must be pvp, classic or story")
		return
	}
	rows, err := a.db.Query(sqlText, args...)
	if err != nil {
		a.fail(w, err)
		return
	}
	defer rows.Close()
	out := []boardRow{}
	for rows.Next() {
		var b boardRow
		rows.Scan(&b.Username, &b.Value, &b.Wins, &b.Losses)
		out = append(out, b)
	}
	writeJSON(w, http.StatusOK, out)
}
