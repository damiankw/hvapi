package server

import (
	"net/http"
	"strconv"

	"github.com/damiankw/hvapi/internal/hyperv"
	"github.com/damiankw/hvapi/internal/openapi"
)

// deepRoutes are the phase 5 endpoints: more about one VM, plus the two
// on-demand calls.
func (s *state) deepRoutes() []route {
	part := s.vmPart
	return []route{
		part("/vms/{id}/integration", "Integration services",
			"Each integration service with whether it is enabled in the VM's settings and its current status as reported by the guest.",
			hyperv.VMIntegration{}, func(d *hyperv.Data, id string) (any, error) { return d.Integration(id) }),
		part("/vms/{id}/kvp", "Key-value pair data exchange",
			"Everything the guest publishes through data exchange (intrinsic and guest-set items) and what the host has sent to the guest.",
			hyperv.VMKVP{}, func(d *hyperv.Data, id string) (any, error) { return d.KVP(id) }),
		part("/vms/{id}/checkpoints", "Checkpoints",
			"The VM's checkpoints as a tree with age, type, which one the VM currently runs from, and the differencing disks each one owns with their sizes.",
			hyperv.VMCheckpoints{}, func(d *hyperv.Data, id string) (any, error) { return d.Checkpoints(id) }),
		part("/vms/{id}/replication", "Hyper-V Replica",
			"Replication state, health, last replication times, the replica server and the replication settings. enabled is false when the VM is not replicated.",
			hyperv.VMReplication{}, func(d *hyperv.Data, id string) (any, error) { return d.Replication(id) }),
		part("/vms/{id}/devices", "Other devices",
			"GPU partitions, PCI Express pass-through devices, COM ports, the 3D display controller, virtual Fibre Channel ports and DVD drives.",
			hyperv.VMDevices{}, func(d *hyperv.Data, id string) (any, error) { return d.Devices(id) }),
		part("/vms/{id}/security", "Security settings",
			"Secure Boot and its template, virtual TPM, shielding, state and migration traffic encryption.",
			hyperv.VMSecurity{}, func(d *hyperv.Data, id string) (any, error) { return d.Security(id) }),
		part("/vms/{id}/metering", "Resource metering",
			"Aggregated CPU, memory, disk and network metering values when resource metering is enabled for the VM.",
			hyperv.VMMetering{}, func(d *hyperv.Data, id string) (any, error) { return d.Metering(id) }),
		{
			Operation: openapi.Operation{
				Method: "GET", Path: "/vms/{id}/disks/{diskId}/state", Tag: "vms", Auth: true,
				Summary:     "Virtual hard disk state (on demand)",
				Description: vmDesc + "{diskId} is a disk_id from /vms/{id}/disks. Asks the host to open the VHD: type, format, maximum size, file size, fragmentation, and the full parent chain for differencing disks, with chain_broken set when a parent cannot be read. Results are cached for 30 seconds.",
				Response:    hyperv.DiskState{},
				Errors:      []int{http.StatusNotFound, http.StatusServiceUnavailable, http.StatusGatewayTimeout, http.StatusBadGateway},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				st, err := s.deps.OnDemand.DiskState(r.Context(), r.PathValue("id"), r.PathValue("diskId"))
				if err != nil {
					writeDataErr(w, err)
					return
				}
				writeJSON(w, http.StatusOK, st)
			},
		},
		{
			Operation: openapi.Operation{
				Method: "GET", Path: "/vms/{id}/thumbnail", Tag: "vms", Auth: true,
				Summary:     "Console thumbnail (on demand)",
				Description: vmDesc + "A PNG screenshot of the VM's console, scaled to width by height (default 320 by 240, at most 1920 by 1200). The VM must be running.",
				Response:    "", ContentType: "image/png",
				Params: []openapi.Param{
					{Name: "width", Description: "Image width in pixels, default 320.", Type: "integer"},
					{Name: "height", Description: "Image height in pixels, default 240.", Type: "integer"},
				},
				Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusServiceUnavailable, http.StatusGatewayTimeout, http.StatusBadGateway},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				width, _ := strconv.Atoi(r.URL.Query().Get("width"))
				height, _ := strconv.Atoi(r.URL.Query().Get("height"))
				img, err := s.deps.OnDemand.Thumbnail(r.Context(), r.PathValue("id"), width, height)
				if err != nil {
					writeDataErr(w, err)
					return
				}
				w.Header().Set("Content-Type", "image/png")
				w.Header().Set("Cache-Control", "no-store")
				_, _ = w.Write(img)
			},
		},
	}
}
