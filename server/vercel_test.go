package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// The Vercel entry point: rewritten paths are restored, PvP is off and flagged, deleting an account clears its rows.
func TestVercelHandler(t *testing.T) {
	t.Setenv("TURSO_DATABASE_URL", "")
	t.Setenv("DB_PATH", "")
	rec := httptest.NewRecorder()
	Handler(rec, httptest.NewRequest("GET", "/api/index?__path=me", nil))
	if rec.Code != 404 {
		t.Fatalf("no database: want 404 so the app shows Classic only, got %d", rec.Code)
	}

	dbPath := filepath.Join(t.TempDir(), "v.db")
	t.Setenv("DB_PATH", dbPath)
	t.Setenv("ENV_FILE", "/nonexistent")
	vercelH = nil
	t.Cleanup(func() { vercelH = nil })
	srv := httptest.NewServer(http.HandlerFunc(Handler))
	defer srv.Close()

	c := newClient(t, srv)
	if code := c.do("POST", "/api/index?__path=register", credentials{Username: "vera", Password: "password123"}, nil); code != 201 {
		t.Fatalf("register through rewrite: %d", code)
	}
	var lb []boardRow
	if code := c.do("GET", "/api/index?__path=leaderboard&kind=classic&size=9&level=easy", nil, &lb); code != 200 {
		t.Fatalf("leaderboard query kept: %d", code)
	}
	res, _ := c.hc.Get(srv.URL + "/api/index?__path=me")
	if res.StatusCode != 200 || res.Header.Get("X-PvP") != "0" {
		t.Fatalf("me: %d, X-PvP=%q", res.StatusCode, res.Header.Get("X-PvP"))
	}
	if res, _ := c.hc.Get(srv.URL + "/ws"); res.StatusCode == http.StatusSwitchingProtocols || res.StatusCode == 401 {
		t.Fatal("no PvP socket on Vercel")
	}
	c.do("POST", "/api/index?__path=classic/result", map[string]any{"size": 9, "level": "easy", "ms": 90000}, nil)
	if code := c.do("DELETE", "/api/index?__path=account", map[string]string{"password": "password123"}, nil); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	db, err := openDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	db.QueryRow(`SELECT (SELECT COUNT(*) FROM classic_stats) + (SELECT COUNT(*) FROM sessions) + (SELECT COUNT(*) FROM users)`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d rows left after deleting the account", n)
	}
}
