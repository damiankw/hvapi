// Package server wires up the HTTP routes and middleware.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	"github.com/damiankw/hvapi/internal/auth"
	"github.com/damiankw/hvapi/internal/config"
	"github.com/damiankw/hvapi/internal/version"
)

// New returns a configured http.Server. It does not start listening.
func New(cfg *config.Config, log *slog.Logger) *http.Server {
	started := time.Now()
	guard := auth.New(cfg.Prefixes, cfg.KeyIDs, cfg.KeyHash, cfg.RateLimit.PerSecond, cfg.RateLimit.Burst)

	// Routes that need an API key.
	keyed := http.NewServeMux()
	keyed.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"version":    version.Version,
			"commit":     version.Commit,
			"build_date": version.Date,
			"go":         runtime.Version(),
		})
	})
	// Phase 2 onwards: /vms, /host, /switches and the rest register here.
	keyed.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	})

	// Top-level mux: /health needs no key; everything else goes through RequireKey.
	root := http.NewServeMux()
	root.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":         "ok",
			"started_at":     started.UTC().Format(time.RFC3339),
			"uptime_seconds": int64(time.Since(started).Seconds()),
		})
	})
	root.Handle("/", guard.RequireKey(keyed))

	// Every request: IP allowlist first, then logging around everything.
	handler := logRequests(log, guard.AllowIP(readOnly(root)))

	return &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
}

// readOnly rejects any method other than GET and HEAD before routing.
func readOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "read-only API: only GET is supported"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// logRequests logs every request. It never logs the API key, only its ID.
func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		r, keyID := auth.TrackKey(r)
		next.ServeHTTP(rec, r)
		log.Info("request",
			"remote", r.RemoteAddr,
			"method", r.Method,
			"path", r.URL.Path,
			"key_id", *keyID,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
