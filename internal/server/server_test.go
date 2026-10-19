package server

import (
	"crypto/sha256"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/damiankw/hvapi/internal/config"
)

func TestAuthAndRouting(t *testing.T) {
	h := sha256.Sum256([]byte("secret"))
	cfg := &config.Config{Prefixes: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, KeyIDs: []string{"mon"}, KeyHash: [][32]byte{h}}
	cfg.RateLimit.PerSecond, cfg.RateLimit.Burst = 1, 2
	var logs strings.Builder
	srv := New(cfg, slog.New(slog.NewJSONHandler(&logs, nil)))
	do := func(method, path, ip, key string) int {
		r := httptest.NewRequest(method, path, nil)
		r.RemoteAddr = ip + ":5555"
		if key != "" {
			r.Header.Set("X-API-Key", key)
		}
		w := httptest.NewRecorder()
		srv.Handler.ServeHTTP(w, r)
		b, _ := io.ReadAll(w.Body)
		t.Logf("%s %s %s -> %d %s", method, path, ip, w.Code, strings.TrimSpace(string(b)))
		return w.Code
	}
	check := func(got, want int) {
		if got != want {
			t.Errorf("got %d want %d", got, want)
		}
	}
	check(do("GET", "/health", "192.0.2.5", ""), 200)
	check(do("GET", "/health", "10.9.9.9", ""), 403)
	check(do("GET", "/version", "192.0.2.5", ""), 401)
	check(do("GET", "/version", "192.0.2.5", "wrong"), 401)
	check(do("GET", "/version", "192.0.2.5", "secret"), 200)
	check(do("POST", "/version", "192.0.2.5", "secret"), 405)
	check(do("GET", "/nope", "192.0.2.5", "secret"), 404)
	check(do("GET", "/version", "192.0.2.5", "secret"), 429)
}
