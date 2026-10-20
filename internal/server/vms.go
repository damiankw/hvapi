package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/damiankw/hvapi/internal/hyperv"
	"github.com/damiankw/hvapi/internal/openapi"
)

// Deps are what the handlers read from. Data returns the current cache;
// Status describes the poller. Both may be nil in tests, which makes every VM
// endpoint report "not collected yet".
type Deps struct {
	Data       func() *hyperv.Data
	Status     func() hyperv.Status
	CertExpiry func() (time.Time, bool) // when serving TLS: the certificate's NotAfter
	OnDemand   *hyperv.OnDemand         // disk state and thumbnails; nil disables them
}

func (s *state) data() *hyperv.Data {
	if s.deps.Data == nil {
		return &hyperv.Data{}
	}
	return s.deps.Data()
}

// vmRoutes are the phase 2 endpoints. Every one serves from the cache.
func (s *state) vmRoutes() []route {
	const tag = "vms"
	part := s.vmPart
	return []route{
		{
			Operation: openapi.Operation{
				Method: "GET", Path: "/status", Tag: "service", Auth: true,
				Summary:     "Collector status",
				Description: "What the background poller is doing: when each loop last ran, how long it took, and which WMI queries failed. Check this first when a VM endpoint returns notes.",
				Response:    hyperv.Status{},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				if s.deps.Status == nil {
					writeJSON(w, http.StatusOK, hyperv.Status{})
					return
				}
				writeJSON(w, http.StatusOK, s.deps.Status())
			},
		},
		{
			Operation: openapi.Operation{
				Method: "GET", Path: "/vms", Tag: tag, Auth: true,
				Summary:     "List virtual machines",
				Description: "Every VM on the host with its state, health and uptime, sorted by name. Refreshed by the state poll (every poll.state_seconds).",
				Response:    hyperv.VMList{},
				Errors:      []int{http.StatusServiceUnavailable},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				v, err := s.data().VMs()
				if err != nil {
					writeDataErr(w, err)
					return
				}
				writeJSON(w, http.StatusOK, v)
			},
		},
		{
			Operation: openapi.Operation{
				Method: "GET", Path: "/vms/summary", Tag: tag, Auth: true,
				Summary:     "Summary of every virtual machine",
				Description: "State, processor load, memory, guest heartbeat and OS, checkpoint count and replication health for each VM. The equivalent of the Hyper-V Manager list view.",
				Response:    hyperv.SummaryList{},
				Errors:      []int{http.StatusServiceUnavailable},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				v, err := s.data().Summaries()
				if err != nil {
					writeDataErr(w, err)
					return
				}
				writeJSON(w, http.StatusOK, v)
			},
		},
		part("/vms/{id}", "Everything about one virtual machine",
			"Returns the VM's list entry, its summary and, once the settings poll has run, its configuration, processor, memory, disks and network in one response.",
			hyperv.VMDetail{}, func(d *hyperv.Data, id string) (any, error) { return d.Detail(id) }),
		part("/vms/{id}/summary", "Summary of one virtual machine",
			"The same fields as one entry of /vms/summary.",
			hyperv.VMSummary{}, func(d *hyperv.Data, id string) (any, error) { return d.Summary(id) }),
		part("/vms/{id}/config", "Configuration",
			"The realized configuration: generation, version, file locations, automatic start/stop actions, firmware and boot settings.",
			hyperv.VMConfig{}, func(d *hyperv.Data, id string) (any, error) { return d.Config(id) }),
		part("/vms/{id}/processor", "Processor settings and load",
			"Virtual processor count, reserve, limit and weight, compatibility and nested virtualization settings, and the current load per virtual processor while running.",
			hyperv.VMProcessor{}, func(d *hyperv.Data, id string) (any, error) { return d.Processor(id) }),
		part("/vms/{id}/memory", "Memory settings and usage",
			"Startup, minimum and maximum memory, dynamic memory settings, and the memory currently assigned plus demand and pressure from the balancer while running.",
			hyperv.VMMemory{}, func(d *hyperv.Data, id string) (any, error) { return d.Memory(id) }),
		part("/vms/{id}/disks", "Storage controllers and drives",
			"IDE and SCSI controllers with their drives and the VHD, ISO or physical disk attached to each.",
			hyperv.VMDisks{}, func(d *hyperv.Data, id string) (any, error) { return d.Disks(id) }),
		part("/vms/{id}/network", "Network adapters",
			"Each adapter with its MAC address, switch connection, VLAN and the addresses the guest reports for it.",
			hyperv.VMNetwork{}, func(d *hyperv.Data, id string) (any, error) { return d.Network(id) }),
	}
}

const vmDesc = "{id} is the VM's GUID (as in /vms) or its exact display name; a name is also matched case-insensitively when unique. "

// vmPart is a route that resolves {id} and serves one cached part of a VM.
func (s *state) vmPart(path, summary, desc string, resp any, build func(d *hyperv.Data, id string) (any, error)) route {
	return route{
		Operation: openapi.Operation{
			Method: "GET", Path: path, Tag: "vms", Auth: true,
			Summary: summary, Description: vmDesc + desc, Response: resp,
			Errors: []int{http.StatusNotFound, http.StatusServiceUnavailable},
		},
		handler: func(w http.ResponseWriter, r *http.Request) {
			d := s.data()
			id, err := d.Resolve(r.PathValue("id"))
			if err != nil {
				writeDataErr(w, err)
				return
			}
			v, err := build(d, id)
			if err != nil {
				writeDataErr(w, err)
				return
			}
			writeJSON(w, http.StatusOK, v)
		},
	}
}

func writeDataErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, hyperv.ErrNotReady):
		w.Header().Set("Retry-After", "5")
		writeJSON(w, http.StatusServiceUnavailable, openapi.Error{Error: err.Error()})
	case errors.Is(err, hyperv.ErrNotFound):
		writeJSON(w, http.StatusNotFound, openapi.Error{Error: err.Error()})
	case errors.Is(err, hyperv.ErrNotRunning):
		writeJSON(w, http.StatusConflict, openapi.Error{Error: err.Error()})
	case errors.Is(err, hyperv.ErrOnDemandUnavailable):
		writeJSON(w, http.StatusServiceUnavailable, openapi.Error{Error: err.Error()})
	case errors.Is(err, context.DeadlineExceeded):
		writeJSON(w, http.StatusGatewayTimeout, openapi.Error{Error: "the host did not answer in time: " + err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, openapi.Error{Error: err.Error()})
	}
}
