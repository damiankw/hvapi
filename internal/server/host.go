package server

import (
	"net/http"

	"github.com/damiankw/hvapi/internal/hyperv"
	"github.com/damiankw/hvapi/internal/openapi"
)

// hostRoutes are the phase 4 endpoints: the host itself and its switches.
func (s *state) hostRoutes() []route {
	get := func(path, tag, summary, desc string, resp any, build func(d *hyperv.Data) (any, error)) route {
		return route{
			Operation: openapi.Operation{
				Method: "GET", Path: path, Tag: tag, Auth: true, Summary: summary, Description: desc, Response: resp,
				Errors: []int{http.StatusServiceUnavailable},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				v, err := build(s.data())
				if err != nil {
					writeDataErr(w, err)
					return
				}
				writeJSON(w, http.StatusOK, v)
			},
		}
	}
	byID := func(path, summary, desc string, resp any, build func(d *hyperv.Data, id string) (any, error)) route {
		return route{
			Operation: openapi.Operation{
				Method: "GET", Path: path, Tag: "switches", Auth: true, Summary: summary,
				Description: "{id} is the switch's GUID or its name. " + desc, Response: resp,
				Errors: []int{http.StatusNotFound, http.StatusServiceUnavailable},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				v, err := build(s.data(), r.PathValue("id"))
				if err != nil {
					writeDataErr(w, err)
					return
				}
				writeJSON(w, http.StatusOK, v)
			},
		}
	}
	return []route{
		get("/host", "host", "Host overview", "Hardware, operating system, memory, processor and the Hyper-V defaults of the host, plus VM counts.", hyperv.HostInfo{},
			func(d *hyperv.Data) (any, error) { return d.Host() }),
		get("/host/services", "host", "Hyper-V services", "State of the Windows services Hyper-V depends on: vmms (management), vmcompute (host compute) and hvhost. all_running is the one to alert on.", hyperv.HostServices{},
			func(d *hyperv.Data) (any, error) { return d.HostServices() }),
		get("/host/storage", "host", "Volumes and free space", "Every volume on the host, including cluster shared volumes, with size, free space, health and which VMs keep files there.", hyperv.HostStorage{},
			func(d *hyperv.Data) (any, error) { return d.HostStorage() }),
		get("/host/network", "host", "Host network adapters", "Physical and host-side adapters with link state, speed and which virtual switch they are bound to.", hyperv.HostNetwork{},
			func(d *hyperv.Data) (any, error) { return d.HostNetwork() }),
		get("/host/perf", "host", "Host performance counters", "Hypervisor logical processor usage (the right measure of CPU on a Hyper-V host), partitions, memory and dynamic memory balancer pressure.", hyperv.HostPerf{},
			func(d *hyperv.Data) (any, error) { return d.HostPerf() }),
		get("/host/numa", "host", "NUMA topology", "NUMA nodes with their logical processors and the memory each can give to VMs.", hyperv.HostNUMA{},
			func(d *hyperv.Data) (any, error) { return d.HostNUMA() }),
		get("/host/hypervisor", "host", "Hypervisor details", "Whether the hypervisor is running, OS build, scheduler type (once the event log is collected), partition counts and virtualization-based security status.", hyperv.HostHypervisor{},
			func(d *hyperv.Data) (any, error) { return d.HostHypervisor() }),
		get("/host/migration", "host", "Live migration settings", "Whether live migration is enabled, limits, authentication, performance option and the migration networks.", hyperv.HostMigration{},
			func(d *hyperv.Data) (any, error) { return d.HostMigration() }),
		get("/host/cluster", "host", "Failover cluster", "Cluster name, nodes, roles and resources when the host is clustered; clustered is false otherwise.", hyperv.HostCluster{},
			func(d *hyperv.Data) (any, error) { return d.HostCluster() }),
		get("/switches", "switches", "List virtual switches", "Every virtual switch with its type (External, Internal, Private), bound adapters, teaming, SR-IOV, extensions and how many VM adapters are connected.", hyperv.SwitchList{},
			func(d *hyperv.Data) (any, error) { return d.Switches() }),
		byID("/switches/{id}", "One virtual switch", "The switch with all of its ports.", hyperv.SwitchDetail{},
			func(d *hyperv.Data, id string) (any, error) { return d.Switch(id) }),
		byID("/switches/{id}/ports", "Ports on a switch", "Host-side ports (external adapters, management OS) and every VM adapter connected to the switch, with VLAN settings.", hyperv.SwitchPorts{},
			func(d *hyperv.Data, id string) (any, error) { return d.SwitchPorts(id) }),
	}
}
