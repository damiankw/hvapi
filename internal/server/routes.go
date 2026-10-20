package server

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/damiankw/hvapi/internal/openapi"
	"github.com/damiankw/hvapi/internal/version"
)

// Response types. The doc tag becomes the field's description in the API
// documentation, so keep them meaningful.

type Health struct {
	Status        string     `json:"status" doc:"Always \"ok\" when the service is up."`
	StartedAt     time.Time  `json:"started_at" doc:"When the service started, UTC."`
	UptimeSeconds int64      `json:"uptime_seconds" doc:"Seconds since the service started."`
	TLS           bool       `json:"tls" doc:"True when this listener serves HTTPS."`
	TLSExpires    *time.Time `json:"tls_expires,omitempty" doc:"When the served certificate expires, UTC. Renew before then; the new files are picked up without a restart."`
}

type Version struct {
	Version   string `json:"version" doc:"Release tag, or dev-<commit> for a build from a branch."`
	Commit    string `json:"commit" doc:"Short git commit the binary was built from."`
	BuildDate string `json:"build_date" doc:"Build time, UTC, RFC 3339."`
	Go        string `json:"go" doc:"Go toolchain the binary was built with."`
}

// route is one entry in the table: its documentation and its handler.
type route struct {
	openapi.Operation
	handler http.HandlerFunc
}

// state is what handlers close over.
type state struct {
	started  time.Time
	tls      bool
	deps     Deps
	specOnce sync.Once
	specJSON []byte
}

var tags = []openapi.Tag{
	{Name: "service", Description: "Health, version, documentation and collector status of hvapi itself."},
	{Name: "vms", Description: "Virtual machines: inventory, state, configuration and resources. Served from a cache the background poller keeps fresh."},
	// Phase 3 onwards: host, switches, jobs, events, diagnostics, wmi.
}

// routes is the single source of truth: every entry is both registered on
// the mux and written into the OpenAPI document.
func (s *state) routes() []route {
	return append(s.serviceRoutes(), s.vmRoutes()...)
}

func (s *state) serviceRoutes() []route {
	return []route{
		{
			Operation: openapi.Operation{
				Method: "GET", Path: "/health", Tag: "service",
				Summary:     "Liveness check",
				Description: "Reports that the service is up. Needs no API key, so a load balancer or monitor can probe it. The IP allowlist still applies.",
				Response:    Health{},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				h := Health{
					Status:        "ok",
					StartedAt:     s.started.UTC(),
					UptimeSeconds: int64(time.Since(s.started).Seconds()),
					TLS:           s.tls,
				}
				if s.deps.CertExpiry != nil {
					if t, ok := s.deps.CertExpiry(); ok {
						t = t.UTC()
						h.TLSExpires = &t
					}
				}
				writeJSON(w, http.StatusOK, h)
			},
		},
		{
			Operation: openapi.Operation{
				Method: "GET", Path: "/version", Tag: "service", Auth: true,
				Summary:     "Build information",
				Description: "Version, commit and build date of the running binary.",
				Response:    Version{},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, Version{
					Version:   version.Version,
					Commit:    version.Commit,
					BuildDate: version.Date,
					Go:        goVersion(),
				})
			},
		},
		{
			Operation: openapi.Operation{
				Method: "GET", Path: "/openapi.json", Tag: "service",
				Summary:     "OpenAPI document",
				Description: "The OpenAPI 3.0 description of this API, generated from the running build. Needs no API key.",
				Response:    map[string]any{},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Cache-Control", "no-store")
				_, _ = w.Write(s.spec())
			},
		},
		{
			Operation: openapi.Operation{
				Method: "GET", Path: "/docs", Tag: "service",
				Summary:     "Interactive API documentation",
				Description: "Swagger UI for this API (HTML, not JSON). Use its Authorize button to enter an API key and try requests. Needs no API key to open.",
				Response:    "",
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("Cache-Control", "no-store")
				_, _ = w.Write([]byte(docsHTML))
			},
		},
	}
}

// spec builds the OpenAPI document once and caches it.
func (s *state) spec() []byte {
	s.specOnce.Do(func() {
		var ops []openapi.Operation
		for _, r := range s.routes() {
			ops = append(ops, r.Operation)
		}
		doc := openapi.Build(openapi.Info{
			Title:   "hvapi",
			Version: version.Version,
			Description: "Read-only HTTP/JSON API exposing Hyper-V diagnostic data. " +
				"Every endpoint is GET; nothing can be changed through this API. " +
				"Send the API key in the " + openapi.HeaderName + " header. " +
				"Data that does not exist on this host (older OS, no cluster) comes back empty with a note rather than as an error.",
		}, tags, ops)
		raw, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			panic("openapi: " + err.Error()) // only a programming error in the route table can cause this
		}
		s.specJSON = append(raw, '\n')
	})
	return s.specJSON
}
