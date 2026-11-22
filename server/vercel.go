package server

import (
	"log"
	"net/http"
	"os"
	"sync"
)

var (
	vercelMu sync.Mutex
	vercelH  http.Handler
)

// Handler serves the API as a Vercel Function: Turso for storage, no PvP (no long-lived connections there).
// vercel.json rewrites /api/<path> to /api/index?__path=<path>; the original path is restored here.
func Handler(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("TURSO_DATABASE_URL") == "" && os.Getenv("DB_PATH") == "" {
		httpError(w, http.StatusNotFound, "not found") // no database connected yet: the web app falls back to Classic only
		return
	}
	h, err := vercelHandler()
	if err != nil {
		log.Print(err)
		httpError(w, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	if q := r.URL.Query(); q.Has("__path") {
		r.URL.Path = "/api/" + q.Get("__path")
		q.Del("__path")
		r.URL.RawQuery = q.Encode()
	}
	h.ServeHTTP(w, r)
}

// vercelHandler builds the app once per warm instance; a failed start is retried on the next request.
func vercelHandler() (http.Handler, error) {
	vercelMu.Lock()
	defer vercelMu.Unlock()
	if vercelH != nil {
		return vercelH, nil
	}
	// Vercel's edge is the only way in and it sets X-Real-IP itself.
	for k, v := range map[string]string{"TRUSTED_PROXIES": "0.0.0.0/0,::/0", "IP_HEADERS": "X-Real-IP", "PVP": "off"} {
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	db, err := openDB(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	vercelH = newApp(cfg, db).routes()
	return vercelH, nil
}
