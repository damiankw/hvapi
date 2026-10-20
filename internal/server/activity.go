package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/damiankw/hvapi/internal/hyperv"
	"github.com/damiankw/hvapi/internal/openapi"
)

var eventParams = []openapi.Param{
	{Name: "level", Description: "Minimum severity: critical, error, warning, info or verbose. Default: everything."},
	{Name: "hours", Description: "Only events from the last N hours (within what is cached).", Type: "integer"},
	{Name: "limit", Description: "Most events to return, default 200.", Type: "integer"},
	{Name: "id", Description: "Only this event ID.", Type: "integer"},
	{Name: "vm", Description: "Only events that mention this VM (GUID or name)."},
}

func (s *state) eventFilter(r *http.Request, d *hyperv.Data) (hyperv.EventFilter, error) {
	q := r.URL.Query()
	f := hyperv.EventFilter{Log: r.PathValue("log"), Level: q.Get("level")}
	f.Hours, _ = strconv.Atoi(q.Get("hours"))
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	f.ID, _ = strconv.Atoi(q.Get("id"))
	if vm := q.Get("vm"); vm != "" {
		id, err := d.Resolve(vm)
		if err != nil {
			return f, err
		}
		f.VMID = id
	}
	return f, nil
}

// activityRoutes are the phase 6 endpoints: jobs, events and VM performance.
func (s *state) activityRoutes() []route {
	events := func(path, summary, desc string) route {
		return route{
			Operation: openapi.Operation{
				Method: "GET", Path: path, Tag: "activity", Auth: true, Summary: summary, Description: desc,
				Response: hyperv.EventList{}, Params: eventParams,
				Errors: []int{http.StatusBadRequest, http.StatusNotFound, http.StatusServiceUnavailable},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				d := s.data()
				f, err := s.eventFilter(r, d)
				if err != nil {
					writeDataErr(w, err)
					return
				}
				v, err := d.Events(f)
				if err != nil {
					writeDataErr(w, err)
					return
				}
				writeJSON(w, http.StatusOK, v)
			},
		}
	}
	return []route{
		{
			Operation: openapi.Operation{
				Method: "GET", Path: "/jobs", Tag: "activity", Auth: true,
				Summary:     "Hyper-V jobs",
				Description: "Running and recently finished jobs: VM operations, storage merges, exports, imports. failed and stuck counts are the ones to alert on.",
				Response:    hyperv.JobList{},
				Errors:      []int{http.StatusServiceUnavailable},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				v, err := s.data().Jobs()
				if err != nil {
					writeDataErr(w, err)
					return
				}
				writeJSON(w, http.StatusOK, v)
			},
		},
		events("/events", "Hyper-V event logs",
			"Events from the Hyper-V logs (VMMS, Worker, Hypervisor, Compute, VmSwitch, High Availability) cached from the last events.hours, newest first, with per-log counts by severity."),
		events("/events/{log}", "One Hyper-V event log",
			"{log} is a short name from the logs list in /events: vmms, worker, hypervisor, compute, vmswitch or high-availability."),
		s.vmPart("/vms/{id}/perf", "Performance counters",
			"Per virtual processor run time, virtual network adapter throughput and drops, and virtual disk throughput, queue length and errors, from the Hyper-V performance counters. Present only while the VM runs.",
			hyperv.VMPerf{}, func(d *hyperv.Data, id string) (any, error) { return d.VMPerf(id) }),
	}
}

var _ = errors.New
