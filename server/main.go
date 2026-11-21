// Command server is the Sudoku backend: accounts, Story Mode and PvP over one SQLite file.
package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Mqdd27/sudoku/shared"
)

type Config struct {
	Port           int
	DBPath         string
	CookieSecure   bool
	AllowedOrigins []string
	TrustedProxies []netip.Prefix
	BindAddr       string   // 127.0.0.1 behind nginx; 0.0.0.0 inside a container (Fly.io)
	IPHeaders      []string // headers a trusted proxy puts the client IP in
	ServeWeb       bool
}

type App struct {
	cfg       Config
	db        *sql.DB
	shared    shared.Config
	limiter   *Limiter
	hashSem   chan struct{}
	dummyHash string
	story     []StoryLevel
	hub       *Hub
	now       func() time.Time
}

func newApp(cfg Config, db *sql.DB) *App {
	a := &App{
		cfg: cfg, db: db, shared: shared.Load(), limiter: NewLimiter(),
		hashSem: make(chan struct{}, 2), dummyHash: dummyHash,
		story: loadStory(), now: time.Now,
	}
	a.hub = newHub(a)
	return a
}

// dummyHash is verified against for unknown usernames so the response time matches a real login.
// Precomputed (same argon2 parameters) so startup doesn't spend 19 MiB on it.
const dummyHash = "$argon2id$v=19$m=19456,t=2,p=1$+zAwv5pRwLBHpE9O54KWGQ$IenCKL3vK138ZDcbyBs8qyho0ugWzCKmlyn40nvZRPs"

func (a *App) routes() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]bool{"ok": true}) })
	m.HandleFunc("POST /api/register", a.register)
	m.HandleFunc("POST /api/login", a.login)
	m.HandleFunc("POST /api/logout", a.logout)
	m.HandleFunc("GET /api/me", a.me)
	m.HandleFunc("GET /api/sessions", a.listSessions)
	m.HandleFunc("DELETE /api/sessions/{id}", a.need(a.deleteSession))
	m.HandleFunc("DELETE /api/account", a.need(a.deleteAccount))
	m.HandleFunc("GET /api/profile/{username}", a.profile)
	m.HandleFunc("POST /api/classic/result", a.need(a.classicResult))
	m.HandleFunc("POST /api/classic/import", a.need(a.classicImport))
	m.HandleFunc("GET /api/leaderboard", a.leaderboard)
	m.HandleFunc("GET /api/story", a.need(a.storyMap))
	m.HandleFunc("POST /api/story/{n}/start", a.need(a.storyStart))
	m.HandleFunc("PUT /api/story/{n}/save", a.need(a.storySave))
	m.HandleFunc("POST /api/story/{n}/check", a.need(a.storyCheck))
	m.HandleFunc("POST /api/story/{n}/hint", a.need(a.storyHint))
	m.HandleFunc("POST /api/story/{n}/submit", a.need(a.storySubmit))
	m.HandleFunc("GET /ws", a.hub.serveWS)
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { httpError(w, 404, "not found") })
	if a.cfg.ServeWeb {
		m.Handle("/", webHandler())
	}
	return a.guard(m)
}

func (a *App) fail(w http.ResponseWriter, err error) {
	log.Printf("error: %v", err)
	httpError(w, http.StatusInternalServerError, "Something went wrong.")
}

// loadDotEnv sets KEY=VALUE pairs from a file without overriding the real environment.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.HasPrefix(line, "#") {
			continue
		}
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`)
		if _, set := os.LookupEnv(k); !set {
			os.Setenv(k, v)
		}
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func loadConfig() (Config, error) {
	loadDotEnv(env("ENV_FILE", ".env"))
	c := Config{DBPath: env("DB_PATH", "data/sudoku.db"), CookieSecure: env("COOKIE_SECURE", "true") != "false"}
	var err error
	if c.Port, err = strconv.Atoi(env("PORT", "8095")); err != nil || c.Port < 1 || c.Port > 65535 {
		return c, errors.New("PORT must be a port number")
	}
	for _, o := range strings.Split(env("ALLOWED_ORIGINS", ""), ",") {
		if o = strings.TrimSpace(o); o != "" {
			c.AllowedOrigins = append(c.AllowedOrigins, o)
		}
	}
	c.BindAddr = env("BIND_ADDR", "127.0.0.1")
	for _, h := range strings.Split(env("IP_HEADERS", "CF-Connecting-IP,X-Real-IP"), ",") {
		if h = strings.TrimSpace(h); h != "" {
			c.IPHeaders = append(c.IPHeaders, h)
		}
	}
	for _, p := range strings.Split(env("TRUSTED_PROXIES", "127.0.0.1/32,::1/128"), ",") {
		pfx, err := netip.ParsePrefix(strings.TrimSpace(p))
		if err != nil {
			return c, errors.New("TRUSTED_PROXIES: " + err.Error())
		}
		c.TrustedProxies = append(c.TrustedProxies, pfx)
	}
	return c, nil
}

func main() {
	serveWeb := flag.Bool("web", false, "also serve the embedded web app (build with -tags embedweb)")
	flag.Parse()
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	cfg.ServeWeb = *serveWeb
	if cfg.ServeWeb && webHandler() == nil {
		log.Fatal("-web needs a binary built with -tags embedweb (see deploy/deploy.sh)")
	}
	db, err := openDB(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	a := newApp(cfg, db)

	srv := &http.Server{
		Handler:           a.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second, // /ws clears these for its own long-lived connections
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(cfg.BindAddr, strconv.Itoa(cfg.Port))) // loopback by default: nginx fronts it
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("listening on %s (db %s)", ln.Addr(), cfg.DBPath)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	go a.sweepSessions(ctx)
	<-ctx.Done()

	log.Print("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.hub.shutdown()
	srv.Shutdown(shutdownCtx)
	db.Close()
}

// sweepSessions deletes expired sessions once an hour.
func (a *App) sweepSessions(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		a.db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, a.now().UnixMilli())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
