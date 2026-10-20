package server

import (
	"net/http"
	"strconv"

	"github.com/damiankw/hvapi/internal/hyperv"
	"github.com/damiankw/hvapi/internal/openapi"
	"github.com/damiankw/hvapi/internal/version"
)

// AnalysisOptions are the configured diagnostics thresholds.
type AnalysisOptions struct {
	CheckpointDays   int
	FreeSpacePercent int
}

func (s *state) diagOptions(r *http.Request) hyperv.DiagOptions {
	o := hyperv.DiagOptions{CheckpointDays: s.deps.Diagnostics.CheckpointDays, FreeSpacePercent: s.deps.Diagnostics.FreeSpacePercent}
	q := r.URL.Query()
	if v, err := strconv.Atoi(q.Get("checkpoint_days")); err == nil && v > 0 {
		o.CheckpointDays = v
	}
	if v, err := strconv.Atoi(q.Get("free_space_percent")); err == nil && v > 0 {
		o.FreeSpacePercent = v
	}
	return o
}

// analysisRoutes are the phase 7 endpoints: diagnostics, metrics, raw WMI.
func (s *state) analysisRoutes() []route {
	thresholds := []openapi.Param{
		{Name: "checkpoint_days", Description: "Report checkpoints older than this many days (default from config, 7).", Type: "integer"},
		{Name: "free_space_percent", Description: "Report VM volumes with less free space than this percentage (default from config, 10; critical below half).", Type: "integer"},
	}
	return []route{
		{
			Operation: openapi.Operation{
				Method: "GET", Path: "/diagnostics", Tag: "analysis", Auth: true,
				Summary:     "What is wrong",
				Description: "Runs every rule over the cached data and returns findings, most severe first: stopped Hyper-V services, unhealthy VMs, running VMs without a heartbeat, disabled integration services, replication warnings, old checkpoints, missing disk files, low free space on VM volumes, failed or stuck jobs, host memory pressure, cluster nodes down, and error events. status is the worst severity.",
				Response:    hyperv.Diagnostics{}, Params: thresholds,
				Errors: []int{http.StatusServiceUnavailable},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				v, err := s.data().Diagnose(s.diagOptions(r))
				if err != nil {
					writeDataErr(w, err)
					return
				}
				writeJSON(w, http.StatusOK, v)
			},
		},
		{
			Operation: openapi.Operation{
				Method: "GET", Path: "/metrics", Tag: "analysis", Auth: true,
				Summary:     "Prometheus metrics",
				Description: "The cache in Prometheus text format: VM state, CPU, memory, heartbeat, checkpoints and replication; host CPU, memory, services, volumes, jobs, events, cluster nodes; and the diagnostics status. Send the API key as X-API-Key or as a bearer token (Authorization: Bearer <key>).",
				Response:    "", ContentType: "text/plain; version=0.0.4", Params: thresholds,
				Errors: []int{http.StatusServiceUnavailable},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				body := "# HELP hvapi_info Build information.\n# TYPE hvapi_info gauge\nhvapi_info{version=\"" + version.Version + "\"} 1\n" + s.data().Metrics(s.diagOptions(r))
				w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
				w.Header().Set("Cache-Control", "no-store")
				_, _ = w.Write([]byte(body))
			},
		},
		{
			Operation: openapi.Operation{
				Method: "GET", Path: "/wmi/classes", Tag: "analysis", Auth: true,
				Summary:     "Classes available to /wmi/{class}",
				Description: "The allowlist of WMI classes that may be read raw, with their namespaces and which poller source uses each.",
				Response:    hyperv.RawClassList{},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, hyperv.RawClasses())
			},
		},
		{
			Operation: openapi.Operation{
				Method: "GET", Path: "/wmi/{class}", Tag: "analysis", Auth: true,
				Summary:     "Raw WMI class (on demand)",
				Description: "Runs SELECT * FROM {class} against the host and returns the instances as WMI reports them. Only classes in /wmi/classes are accepted, and only ExecQuery is ever used. For looking at something the shaped endpoints do not expose.",
				Response:    hyperv.RawResult{},
				Params:      []openapi.Param{{Name: "limit", Description: "Most rows to return, default 1000.", Type: "integer"}},
				Errors:      []int{http.StatusNotFound, http.StatusServiceUnavailable, http.StatusGatewayTimeout},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
				v, err := s.deps.OnDemand.RawQuery(r.Context(), r.PathValue("class"), limit)
				if err != nil {
					writeDataErr(w, err)
					return
				}
				writeJSON(w, http.StatusOK, v)
			},
		},
	}
}
