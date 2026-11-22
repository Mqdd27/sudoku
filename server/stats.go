package server

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
	Mode   string `json:"mode"`
	Size   int    `json:"size"`
	Rating int    `json:"rating"`
	Wins   int    `json:"wins"`
	Losses int    `json:"losses"`
	Draws  int    `json:"draws"`
}

type taStat struct {
	Wins      int     `json:"wins"`
	Losses    int     `json:"losses"`
	Draws     int     `json:"draws"`
	BestBoxes int     `json:"bestBoxes"`
	AvgBoxes  float64 `json:"avgBoxes"`
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
		PvP        []pvpStat `json:"pvp"`
		TimeAttack taStat    `json:"timeAttack"`
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
	rows, err = a.db.Query(`SELECT mode, size, rating, wins, losses, draws FROM pvp_ratings WHERE user_id = ? ORDER BY mode, size`, id)
	if err != nil {
		a.fail(w, err)
		return
	}
	for rows.Next() {
		var s pvpStat
		rows.Scan(&s.Mode, &s.Size, &s.Rating, &s.Wins, &s.Losses, &s.Draws)
		out.PvP = append(out.PvP, s)
		if s.Mode == "ta" {
			out.TimeAttack.Wins += s.Wins
			out.TimeAttack.Losses += s.Losses
			out.TimeAttack.Draws += s.Draws
		}
	}
	rows.Close()
	a.db.QueryRow(`SELECT COALESCE(MAX(b), 0), COALESCE(AVG(b), 0) FROM (
		SELECT p1_boxes AS b FROM matches WHERE mode = 'ta' AND p1 = ?1
		UNION ALL SELECT p2_boxes FROM matches WHERE mode = 'ta' AND p2 = ?1)`, id).
		Scan(&out.TimeAttack.BestBoxes, &out.TimeAttack.AvgBoxes)
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
	Value    int64  `json:"value"` // rating, milliseconds, or boxes for Time Attack records
	Wins     *int   `json:"wins,omitempty"`
	Losses   *int   `json:"losses,omitempty"`
	Cells    *int   `json:"cells,omitempty"` // Time Attack records
}

// leaderboard: ?kind=pvp&mode=race|ta&size=9 | ?kind=ta&size=9&level=easy&minutes=3 (record boxes + cells)
// | ?kind=classic&size=9&level=easy | ?kind=story&level=3
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
		mode := q.Get("mode")
		if mode == "" {
			mode = "race"
		}
		sqlText = `SELECT u.username, r.rating, r.wins, r.losses, NULL FROM pvp_ratings r JOIN users u ON u.id = r.user_id
			WHERE r.mode = ? AND r.size = ? AND r.wins + r.losses + r.draws > 0 ORDER BY r.rating DESC, r.wins DESC LIMIT 20`
		args = []any{mode, size}
	case "ta":
		minutes, _ := strconv.Atoi(q.Get("minutes"))
		if !a.validSizeLevel(size, level) {
			httpError(w, 400, "bad size or level")
			return
		}
		// each player's best single match: most boxes, then most correct cells
		sqlText = `SELECT username, boxes, NULL, NULL, cells FROM (
			SELECT u.username, s.boxes, s.cells, ROW_NUMBER() OVER (PARTITION BY s.uid ORDER BY s.boxes DESC, s.cells DESC) AS k
			FROM (SELECT p1 AS uid, p1_boxes AS boxes, p1_cells AS cells, size, level, duration_sec FROM matches WHERE mode = 'ta'
			      UNION ALL SELECT p2, p2_boxes, p2_cells, size, level, duration_sec FROM matches WHERE mode = 'ta') s
			JOIN users u ON u.id = s.uid WHERE s.size = ? AND s.level = ? AND s.duration_sec = ?)
			WHERE k = 1 ORDER BY boxes DESC, cells DESC LIMIT 20`
		args = []any{size, level, minutes * 60}
	case "classic":
		if !a.validSizeLevel(size, level) {
			httpError(w, 400, "bad size or level")
			return
		}
		sqlText = `SELECT u.username, c.best_ms, NULL, NULL, NULL FROM classic_stats c JOIN users u ON u.id = c.user_id
			WHERE c.size = ? AND c.level = ? AND c.best_ms IS NOT NULL ORDER BY c.best_ms LIMIT 20`
		args = []any{size, level}
	case "story":
		n, _ := strconv.Atoi(level)
		if n < 1 || n > len(a.story) {
			httpError(w, 400, "bad level")
			return
		}
		sqlText = `SELECT u.username, s.best_ms, NULL, NULL, NULL FROM story_progress s JOIN users u ON u.id = s.user_id
			WHERE s.level = ? ORDER BY s.best_ms LIMIT 20`
		args = []any{n}
	default:
		httpError(w, 400, "kind must be pvp, ta, classic or story")
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
		rows.Scan(&b.Username, &b.Value, &b.Wins, &b.Losses, &b.Cells)
		out = append(out, b)
	}
	writeJSON(w, http.StatusOK, out)
}
