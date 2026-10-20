package hyperv

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/damiankw/hvapi/internal/wmi"
)

// source is one WMI query the poller runs. Optional sources may be missing on
// a host (older OS, feature not installed); their absence is a note, not an
// error.
type source struct {
	name      string
	namespace string
	query     string
	optional  bool
}

// State sources: cheap, polled often.
var stateSources = []source{
	{"computer_system", wmi.NSVirtualization, "SELECT * FROM Msvm_ComputerSystem WHERE Caption = 'Virtual Machine'", false},
	{"processor", wmi.NSVirtualization, "SELECT * FROM Msvm_Processor", true},
	{"memory", wmi.NSVirtualization, "SELECT * FROM Msvm_Memory", true},
	{"heartbeat", wmi.NSVirtualization, "SELECT * FROM Msvm_HeartbeatComponent", true},
	{"dynamic_memory_counters", wmi.NSCIMv2, "SELECT * FROM Win32_PerfFormattedData_BalancerStats_HyperVDynamicMemoryVM", true},
	// Host (phase 4): things that change by the minute.
	{"host_services", wmi.NSCIMv2, "SELECT Name, DisplayName, State, StartMode, Status, ProcessId, StartName, ExitCode FROM Win32_Service WHERE Name = 'vmms' OR Name = 'vmcompute' OR Name = 'hvhost'", false},
	{"operating_system", wmi.NSCIMv2, "SELECT * FROM Win32_OperatingSystem", false},
	{"hv_logical_processors", wmi.NSCIMv2, "SELECT * FROM Win32_PerfFormattedData_HvStats_HyperVHypervisorLogicalProcessor", true},
	{"hv_root_processors", wmi.NSCIMv2, "SELECT * FROM Win32_PerfFormattedData_HvStats_HyperVHypervisorRootVirtualProcessor", true},
	{"hypervisor_counters", wmi.NSCIMv2, "SELECT * FROM Win32_PerfFormattedData_HvStats_HyperVHypervisor", true},
	{"os_memory_counters", wmi.NSCIMv2, "SELECT * FROM Win32_PerfFormattedData_PerfOS_Memory", true},
	{"memory_balancer_counters", wmi.NSCIMv2, "SELECT * FROM Win32_PerfFormattedData_BalancerStats_HyperVDynamicMemoryBalancer", true},
	{"cluster_nodes", wmi.NSCluster, "SELECT * FROM MSCluster_Node", true},
	{"cluster_groups", wmi.NSCluster, "SELECT * FROM MSCluster_ResourceGroup", true},
	{"cluster_resources", wmi.NSCluster, "SELECT * FROM MSCluster_Resource", true},
}

// Settings sources: heavier, polled less often.
var settingsSources = []source{
	{"system_settings", wmi.NSVirtualization, "SELECT * FROM Msvm_VirtualSystemSettingData", false},
	{"settings_components", wmi.NSVirtualization, "SELECT GroupComponent, PartComponent FROM Msvm_VirtualSystemSettingDataComponent", true},
	{"processor_settings", wmi.NSVirtualization, "SELECT * FROM Msvm_ProcessorSettingData", true},
	{"memory_settings", wmi.NSVirtualization, "SELECT * FROM Msvm_MemorySettingData", true},
	{"resource_allocation", wmi.NSVirtualization, "SELECT * FROM Msvm_ResourceAllocationSettingData", true},
	{"storage_allocation", wmi.NSVirtualization, "SELECT * FROM Msvm_StorageAllocationSettingData", true},
	{"synthetic_ethernet_ports", wmi.NSVirtualization, "SELECT * FROM Msvm_SyntheticEthernetPortSettingData", true},
	{"emulated_ethernet_ports", wmi.NSVirtualization, "SELECT * FROM Msvm_EmulatedEthernetPortSettingData", true},
	{"ethernet_connections", wmi.NSVirtualization, "SELECT * FROM Msvm_EthernetPortAllocationSettingData", true},
	{"vlan_settings", wmi.NSVirtualization, "SELECT * FROM Msvm_EthernetSwitchPortVlanSettingData", true},
	{"guest_network", wmi.NSVirtualization, "SELECT * FROM Msvm_GuestNetworkAdapterConfiguration", true},
	{"switches", wmi.NSVirtualization, "SELECT * FROM Msvm_VirtualEthernetSwitch", true},
	{"kvp", wmi.NSVirtualization, "SELECT * FROM Msvm_KvpExchangeComponent", true},
	// Host (phase 4): hardware, OS, Hyper-V settings, storage, networking.
	{"host_computer", wmi.NSCIMv2, "SELECT * FROM Win32_ComputerSystem", false},
	{"host_processors", wmi.NSCIMv2, "SELECT * FROM Win32_Processor", true},
	{"management_settings", wmi.NSVirtualization, "SELECT * FROM Msvm_VirtualSystemManagementServiceSettingData", true},
	{"volumes", wmi.NSStorage, "SELECT * FROM MSFT_Volume", true},
	{"cluster_shared_volumes", wmi.NSCluster, "SELECT * FROM MSCluster_ClusterSharedVolume", true},
	{"net_adapters", wmi.NSStandardCIMv2, "SELECT * FROM MSFT_NetAdapter", true},
	{"external_ports", wmi.NSVirtualization, "SELECT * FROM Msvm_ExternalEthernetPort", true},
	{"internal_ports", wmi.NSVirtualization, "SELECT * FROM Msvm_InternalEthernetPort", true},
	{"numa_nodes", wmi.NSVirtualization, "SELECT * FROM Msvm_NumaNode", true},
	{"migration_settings", wmi.NSVirtualization, "SELECT * FROM Msvm_VirtualSystemMigrationServiceSettingData", true},
	{"migration_networks", wmi.NSVirtualization, "SELECT * FROM Msvm_VirtualSystemMigrationNetworkSettingData", true},
	{"cluster", wmi.NSCluster, "SELECT * FROM MSCluster_Cluster", true},
	{"switch_settings", wmi.NSVirtualization, "SELECT * FROM Msvm_VirtualEthernetSwitchSettingData", true},
	{"switch_ports", wmi.NSVirtualization, "SELECT * FROM Msvm_EthernetSwitchPort", true},
	{"switch_extensions", wmi.NSVirtualization, "SELECT * FROM Msvm_EthernetSwitchExtension", true},
	{"device_guard", `root\Microsoft\Windows\DeviceGuard`, "SELECT * FROM Win32_DeviceGuard", true},
}

// rowSet is the raw result of one poll: rows and errors per source.
type rowSet struct {
	at       time.Time
	duration time.Duration
	rows     map[string][]wmi.Row
	errs     map[string]error
	status   []SourceStatus
}

// collect runs every source and keeps going past failures.
func collect(ctx context.Context, q wmi.Querier, sources []source, perQuery time.Duration) *rowSet {
	start := time.Now()
	rs := &rowSet{rows: map[string][]wmi.Row{}, errs: map[string]error{}}
	for _, s := range sources {
		qctx, cancel := context.WithTimeout(ctx, perQuery)
		rows, err := q.Query(qctx, s.namespace, s.query)
		cancel()
		st := SourceStatus{Name: s.name, Namespace: s.namespace, Query: s.query, Optional: s.optional, Rows: len(rows)}
		if err != nil {
			rs.errs[s.name] = err
			st.Error = err.Error()
		} else {
			rs.rows[s.name] = rows
		}
		rs.status = append(rs.status, st)
		if ctx.Err() != nil {
			break
		}
	}
	rs.at = time.Now()
	rs.duration = rs.at.Sub(start)
	return rs
}

// errorNotes turns the errors of the named sources into notes. An absent
// class on this host is reported as such rather than as a failure.
func (rs *rowSet) errorNotes(names ...string) []string {
	var notes []string
	for _, n := range names {
		err, ok := rs.errs[n]
		if !ok {
			continue
		}
		switch {
		case errors.Is(err, wmi.ErrInvalidClass):
			notes = append(notes, n+": not available on this host (class does not exist)")
		case errors.Is(err, wmi.ErrUnavailable):
			notes = append(notes, n+": "+err.Error())
		default:
			notes = append(notes, n+": query failed: "+err.Error())
		}
	}
	return notes
}

// StateData is the result of one state poll, grouped by VM.
type StateData struct {
	*rowSet
	vms   map[string]*vmState
	order []string // IDs sorted by name
}

type vmState struct {
	cs        wmi.Row
	procs     []wmi.Row
	mem       []wmi.Row
	heartbeat wmi.Row
	dynMem    wmi.Row
}

func buildState(rs *rowSet) *StateData {
	d := &StateData{rowSet: rs, vms: map[string]*vmState{}}
	for _, r := range rs.rows["computer_system"] {
		id := NormalizeID(r.String("Name"))
		if !IsGUID(id) {
			continue
		}
		d.vms[id] = &vmState{cs: r}
		d.order = append(d.order, id)
	}
	sort.Slice(d.order, func(i, j int) bool {
		a, b := d.vms[d.order[i]].cs.String("ElementName"), d.vms[d.order[j]].cs.String("ElementName")
		if strings.EqualFold(a, b) {
			return d.order[i] < d.order[j]
		}
		return strings.ToLower(a) < strings.ToLower(b)
	})
	for _, r := range rs.rows["processor"] {
		if v := d.vms[NormalizeID(r.String("SystemName"))]; v != nil {
			v.procs = append(v.procs, r)
		}
	}
	for _, r := range rs.rows["memory"] {
		if v := d.vms[NormalizeID(r.String("SystemName"))]; v != nil {
			v.mem = append(v.mem, r)
		}
	}
	for _, r := range rs.rows["heartbeat"] {
		if v := d.vms[NormalizeID(r.String("SystemName"))]; v != nil {
			v.heartbeat = r
		}
	}
	// Performance counters are keyed by display name, not ID.
	byName := map[string]*vmState{}
	for _, id := range d.order {
		byName[strings.ToLower(d.vms[id].cs.String("ElementName"))] = d.vms[id]
	}
	for _, r := range rs.rows["dynamic_memory_counters"] {
		if v := byName[strings.ToLower(r.String("Name"))]; v != nil {
			v.dynMem = r
		}
	}
	return d
}

// SettingsData is the result of one settings poll, grouped by VM.
type SettingsData struct {
	*rowSet
	vms           map[string]*vmSettings
	switches      map[string]string // switch GUID -> name
	schedulerType string            // from the hypervisor event log, when collected
}

type vmSettings struct {
	vssd      wmi.Row
	snapshots int
	proc      wmi.Row
	mem       wmi.Row
	rasd      []wmi.Row
	sasd      []wmi.Row
	ports     []wmi.Row // synthetic and emulated ethernet ports
	conns     []wmi.Row // ethernet port allocation (switch connections)
	vlans     []wmi.Row
	guestNet  []wmi.Row
	kvp       map[string]string
	linkNotes []string
}

const (
	typeRealized = "Microsoft:Hyper-V:System:Realized"
	typeSnapshot = "Microsoft:Hyper-V:Snapshot:"
)

func buildSettings(rs *rowSet) *SettingsData {
	d := &SettingsData{rowSet: rs, vms: map[string]*vmSettings{}, switches: map[string]string{}}

	// Realized system settings define the VMs; snapshot settings are counted.
	vssdPathToVM := map[string]string{}
	snapshotIDs := map[string]bool{}
	for _, r := range rs.rows["system_settings"] {
		t := r.String("VirtualSystemType")
		switch {
		case t == typeRealized:
			id := NormalizeID(r.String("VirtualSystemIdentifier"))
			if id == "" {
				id = VMIDFromInstanceID(r.String("InstanceID"))
			}
			if id == "" {
				continue
			}
			d.vms[id] = &vmSettings{vssd: r}
			vssdPathToVM[NormPath(r.String(wmi.PathKey))] = id
		case strings.HasPrefix(t, typeSnapshot):
			snapshotIDs[NormalizeID(r.String("ConfigurationID"))] = true
		}
	}
	for _, r := range rs.rows["system_settings"] {
		if strings.HasPrefix(r.String("VirtualSystemType"), typeSnapshot) {
			if v := d.vms[NormalizeID(r.String("VirtualSystemIdentifier"))]; v != nil {
				v.snapshots++
			}
		}
	}

	// Which VM a resource setting belongs to. Preferred: the association
	// class, which is exact. Fallback: the InstanceID, which starts with
	// the VM GUID; a second segment naming a snapshot means it is not the
	// realized configuration.
	partToVM := map[string]string{}
	for _, r := range rs.rows["settings_components"] {
		if id, ok := vssdPathToVM[NormPath(r.String("GroupComponent"))]; ok {
			partToVM[NormPath(r.String("PartComponent"))] = id
		}
	}
	useAssoc := len(partToVM) > 0
	owner := func(r wmi.Row) *vmSettings {
		if useAssoc {
			if id, ok := partToVM[NormPath(r.String(wmi.PathKey))]; ok {
				return d.vms[id]
			}
			return nil
		}
		segs := instanceSegments(r.String("InstanceID"))
		if len(segs) == 0 || !IsGUID(segs[0]) {
			return nil
		}
		if len(segs) > 1 && snapshotIDs[NormalizeID(segs[1])] {
			return nil
		}
		return d.vms[NormalizeID(segs[0])]
	}

	for _, r := range rs.rows["processor_settings"] {
		if v := owner(r); v != nil {
			v.proc = r
		}
	}
	for _, r := range rs.rows["memory_settings"] {
		if v := owner(r); v != nil {
			v.mem = r
		}
	}
	for _, r := range rs.rows["resource_allocation"] {
		if v := owner(r); v != nil {
			v.rasd = append(v.rasd, r)
		}
	}
	for _, r := range rs.rows["storage_allocation"] {
		if v := owner(r); v != nil {
			v.sasd = append(v.sasd, r)
		}
	}
	for _, name := range []string{"synthetic_ethernet_ports", "emulated_ethernet_ports"} {
		for _, r := range rs.rows[name] {
			if v := owner(r); v != nil {
				v.ports = append(v.ports, r)
			}
		}
	}
	for _, r := range rs.rows["ethernet_connections"] {
		if v := owner(r); v != nil {
			v.conns = append(v.conns, r)
		}
	}
	// Feature settings hang off the connection, not the VM; link by prefix.
	for _, r := range rs.rows["vlan_settings"] {
		if v := d.vms[VMIDFromInstanceID(r.String("InstanceID"))]; v != nil {
			v.vlans = append(v.vlans, r)
		}
	}
	for _, r := range rs.rows["guest_network"] {
		if v := d.vms[VMIDFromInstanceID(r.String("InstanceID"))]; v != nil {
			v.guestNet = append(v.guestNet, r)
		}
	}
	for _, r := range rs.rows["switches"] {
		d.switches[NormalizeID(r.String("Name"))] = r.String("ElementName")
	}
	for _, r := range rs.rows["kvp"] {
		if v := d.vms[NormalizeID(r.String("SystemName"))]; v != nil {
			v.kvp = ParseKVP(r.Strings("GuestIntrinsicExchangeItems"))
		}
	}
	if !useAssoc && len(rs.rows["system_settings"]) > 0 {
		for _, v := range d.vms {
			v.linkNotes = append(v.linkNotes, "settings were matched to the VM by InstanceID because Msvm_VirtualSystemSettingDataComponent returned nothing")
		}
	}
	return d
}
