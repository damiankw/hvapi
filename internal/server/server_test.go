package server

import (
	"crypto/sha256"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/damiankw/hvapi/internal/config"
)

func testServer(t *testing.T) *http.Server {
	t.Helper()
	h := sha256.Sum256([]byte("secret"))
	cfg := &config.Config{Prefixes: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, KeyIDs: []string{"mon"}, KeyHash: [][32]byte{h}}
	cfg.RateLimit.PerSecond, cfg.RateLimit.Burst = 1, 2
	var logs strings.Builder
	return New(cfg, slog.New(slog.NewJSONHandler(&logs, nil)))
}

func TestAuthAndRouting(t *testing.T) {
	srv := testServer(t)
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
		t.Helper()
		if got != want {
			t.Errorf("got %d want %d", got, want)
		}
	}
	check(do("GET", "/health", "192.0.2.5", ""), 200)
	check(do("GET", "/health", "10.9.9.9", ""), 403)
	check(do("GET", "/docs", "192.0.2.5", ""), 200)
	check(do("GET", "/openapi.json", "192.0.2.5", ""), 200)
	check(do("GET", "/openapi.json", "10.9.9.9", ""), 403)
	check(do("GET", "/version", "192.0.2.5", ""), 401)
	check(do("GET", "/version", "192.0.2.5", "wrong"), 401)
	check(do("GET", "/version", "192.0.2.5", "secret"), 200)
	check(do("POST", "/version", "192.0.2.5", "secret"), 405)
	check(do("GET", "/nope", "192.0.2.5", "secret"), 404)
	check(do("GET", "/version", "192.0.2.5", "secret"), 429)
}

// Every route the server registers must be in the OpenAPI document with the
// right security, and vice versa. Spec() is what the CLI and CI publish.
func TestSpecMatchesRoutes(t *testing.T) {
	var doc struct {
		OpenAPI string `json:"openapi"`
		Paths   map[string]map[string]struct {
			Security []map[string][]string `json:"security"`
			Summary  string                `json:"summary"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(Spec(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.OpenAPI != "3.0.3" {
		t.Errorf("openapi version %q", doc.OpenAPI)
	}
	routes := (&state{}).routes()
	if len(doc.Paths) != len(routes) {
		t.Errorf("%d routes but %d documented paths", len(routes), len(doc.Paths))
	}
	for _, r := range routes {
		op, ok := doc.Paths[r.Path][strings.ToLower(r.Method)]
		if !ok {
			t.Errorf("%s %s is not documented", r.Method, r.Path)
			continue
		}
		if op.Summary == "" {
			t.Errorf("%s %s has no summary", r.Method, r.Path)
		}
		if (len(op.Security) > 0) != r.Auth {
			t.Errorf("%s %s: documented security %v, Auth=%v", r.Method, r.Path, op.Security, r.Auth)
		}
	}
}

func TestServedSpecIsTheSame(t *testing.T) {
	srv := testServer(t)
	r := httptest.NewRequest("GET", "/openapi.json", nil)
	r.RemoteAddr = "192.0.2.5:1"
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, r)
	if w.Body.String() != string(Spec()) {
		t.Error("served document differs from Spec()")
	}
}
