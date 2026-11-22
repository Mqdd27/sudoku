package server

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxBody = 64 << 10

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// readJSON decodes a size-limited body into v. On failure it has already written a 400/413.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			httpError(w, http.StatusRequestEntityTooLarge, "request too large")
		} else {
			httpError(w, http.StatusBadRequest, "invalid request body")
		}
		return false
	}
	return true
}

// guard wraps every request: body size cap, basic headers, and an Origin check on anything that changes data.
func (a *App) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if !a.originOK(r) {
				httpError(w, http.StatusForbidden, "bad origin")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// originOK: the Origin header must match the Host we were reached on, or be listed in ALLOWED_ORIGINS.
// Together with SameSite=Lax cookies this blocks cross-site form/fetch posts.
func (a *App) originOK(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return false
	}
	u, err := url.Parse(o)
	if err != nil {
		return false
	}
	return u.Host == r.Host || slices.Contains(a.cfg.AllowedOrigins, o)
}

// clientIP trusts CF-Connecting-IP / X-Real-IP only when the TCP peer is a trusted proxy (nginx on loopback).
// A direct client can't spoof its address that way; behind nginx RemoteAddr alone would always be 127.0.0.1.
func (a *App) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	for _, p := range a.cfg.TrustedProxies {
		if p.Contains(peer.Unmap()) {
			for _, h := range a.cfg.IPHeaders {
				if ip, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get(h))); err == nil {
					return ip.String()
				}
			}
			break
		}
	}
	return peer.String()
}

// Limiter is a fixed-window counter per key. Enough for login/register throttling on one instance.
type Limiter struct {
	mu    sync.Mutex
	m     map[string]*window
	calls int
}

type window struct {
	n     int
	reset time.Time
}

func NewLimiter() *Limiter { return &Limiter{m: map[string]*window{}} }

// Allow counts one hit for key. When the key is over max within per, it returns false and the wait time.
func (l *Limiter) Allow(key string, max int, per time.Duration, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.calls++; l.calls%1024 == 0 { // drop expired windows now and then
		for k, w := range l.m {
			if now.After(w.reset) {
				delete(l.m, k)
			}
		}
	}
	w := l.m[key]
	if w == nil || now.After(w.reset) {
		w = &window{reset: now.Add(per)}
		l.m[key] = w
	}
	w.n++
	if w.n > max {
		return false, w.reset.Sub(now)
	}
	return true, 0
}

// limit applies several limits; on the first refusal it writes 429 and returns false.
func (a *App) limit(w http.ResponseWriter, rules ...limitRule) bool {
	now := a.now()
	for _, r := range rules {
		if ok, wait := a.limiter.Allow(r.key, r.max, r.per, now); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			httpError(w, http.StatusTooManyRequests, "Too many attempts. Try again later.")
			return false
		}
	}
	return true
}

type limitRule struct {
	key string
	max int
	per time.Duration
}
