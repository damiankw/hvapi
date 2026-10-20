package hyperv

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/damiankw/hvapi/internal/wmi"
)

// Data is a consistent read of the cache. State and Settings come from
// different polls and either can be nil before its first run.
type Data struct {
	State    *StateData
	Settings *SettingsData
}

// ErrNotReady means the poll that produces this data has not completed yet.
var ErrNotReady = errors.New("data not collected yet; try again shortly")

// ErrNotFound means no VM matches the given ID or name.
var ErrNotFound = errors.New("no such VM")

// Resolve finds a VM by ID (GUID, any case) or display name (exact, then
// case-insensitive if unique) and returns its ID.
func (d *Data) Resolve(key string) (string, error) {
	if d.State == nil {
		return "", ErrNotReady
	}
	key = strings.TrimSpace(key)
	if IsGUID(key) {
		if _, ok := d.State.vms[NormalizeID(key)]; ok {
			return NormalizeID(key), nil
		}
		return "", ErrNotFound
	}
	for _, id := range d.State.order {
		if d.State.vms[id].cs.String("ElementName") == key {
			return id, nil
		}
	}
	var found []string
	for _, id := range d.State.order {
		if strings.EqualFold(d.State.vms[id].cs.String("ElementName"), key) {
			found = append(found, id)
		}
	}
	if len(found) == 1 {
		return found[0], nil
	}
	if len(found) > 1 {
		return "", fmt.Errorf("%w: %d VMs are named %q; use the ID", ErrNotFound, len(found), key)
	}
	return "", ErrNotFound
}

func (d *Data) settingsFor(id string) *vmSettings {
	if d.Settings == nil {
		return nil
	}
	return d.Settings.vms[id]
}

func (d *Data) settingsNotes(names ...string) []string {
	if d.Settings == nil {
		return []string{"configuration not collected yet (the settings poll has not completed)"}
	}
	return d.Settings.errorNotes(names...)
}

// VMs builds the /vms response.
func (d *Data) VMs() (*VMList, error) {
	if d.State == nil {
		return nil, ErrNotReady
	}
	out := &VMList{CollectedAt: d.State.at.UTC(), VMs: []VM{}}
	for _, id := range d.State.order {
		out.VMs = append(out.VMs, d.vm(id))
	}
	out.Count = len(out.VMs)
	out.Notes = d.State.errorNotes("computer_system")
	if d.Settings == nil {
		out.Notes = append(out.Notes, "generation and version are filled in after the first settings poll")
	}
	return out, nil
}

func (d *Data) vm(id string) VM {
	cs := d.State.vms[id].cs
	v := VM{
		ID:                 id,
		Name:               cs.String("ElementName"),
		StateCode:          cs.Int("EnabledState"),
		OtherState:         cs.String("OtherEnabledState"),
		HealthCode:         cs.Int("HealthState"),
		StatusDescriptions: cs.Strings("StatusDescriptions"),
		LastStateChange:    CIMTime(cs.String("TimeOfLastStateChange")),
		LastConfigChange:   CIMTime(cs.String("TimeOfLastConfigurationChange")),
		Created:            CIMTime(cs.String("InstallDate")),
		ProcessID:          cs.Int("ProcessID"),
		NumaNodes:          cs.Int("NumberOfNumaNodes"),
		ReplicationState:   lookup(replicationStates, cs.Int("ReplicationState")),
		ReplicationHealth:  lookup(replicationHealths, cs.Int("ReplicationHealth")),
		ReplicationMode:    lookup(replicationModes, cs.Int("ReplicationMode")),
	}
	v.State = lookup(vmStates, v.StateCode)
	v.Health = lookup(healthStates, v.HealthCode)
	for _, code := range cs.Ints("OperationalStatus") {
		v.OperationalStatus = append(v.OperationalStatus, lookup(operationalStatuses, code))
	}
	if v.StateCode == 2 {
		v.UptimeSeconds = cs.Int("OnTimeInMilliseconds") / 1000
	}
	if s := d.settingsFor(id); s != nil {
		v.Generation = generation(s.vssd)
		v.Version = s.vssd.String("Version")
		v.Notes = s.vssd.Strings("Notes")
	}
	return v
}

func generation(vssd wmi.Row) int {
	switch vssd.String("VirtualSystemSubType") {
	case "Microsoft:Hyper-V:SubType:1":
		return 1
	case "Microsoft:Hyper-V:SubType:2":
		return 2
	}
	return 0
}

// Summaries builds the /vms/summary response.
func (d *Data) Summaries() (*SummaryList, error) {
	if d.State == nil {
		return nil, ErrNotReady
	}
	out := &SummaryList{CollectedAt: d.State.at.UTC(), VMs: []VMSummary{}}
	if d.Settings != nil {
		t := d.Settings.at.UTC()
		out.SettingsAt = &t
	}
	for _, id := range d.State.order {
		out.VMs = append(out.VMs, d.summary(id))
	}
	out.Count = len(out.VMs)
	out.Notes = append(d.State.errorNotes("computer_system", "processor", "memory", "heartbeat", "dynamic_memory_counters"),
		d.settingsNotes("system_settings", "processor_settings", "memory_settings", "kvp")...)
	return out, nil
}

// Summary builds one VM's summary.
func (d *Data) Summary(id string) (*VMSummary, error) {
	if d.State == nil {
		return nil, ErrNotReady
	}
	if _, ok := d.State.vms[id]; !ok {
		return nil, ErrNotFound
	}
	s := d.summary(id)
	return &s, nil
}

func (d *Data) summary(id string) VMSummary {
	v := d.vm(id)
	st := d.State.vms[id]
	s := VMSummary{
		ID: v.ID, Name: v.Name, State: v.State, Health: v.Health, UptimeSeconds: v.UptimeSeconds,
		Generation: v.Generation, Version: v.Version,
		ReplicationState: v.ReplicationState, ReplicationHealth: v.ReplicationHealth,
	}
	running := v.StateCode == 2
	s.ProcessorCount = int64(len(st.procs))
	if load := avgLoad(st.procs); load != nil && running {
		s.ProcessorLoadPercent = load
	}
	s.MemoryAssignedMB = assignedMB(st.mem)
	if set := d.settingsFor(id); set != nil {
		s.SnapshotCount = set.snapshots
		if set.proc != nil && s.ProcessorCount == 0 {
			s.ProcessorCount = set.proc.Int("VirtualQuantity")
		}
		if set.mem != nil {
			s.MemoryStartupMB = set.mem.Int("VirtualQuantity")
			s.DynamicMemory = set.mem.Bool("DynamicMemoryEnabled")
			if s.DynamicMemory {
				s.MemoryMinimumMB = set.mem.Int("Reservation")
				s.MemoryMaximumMB = set.mem.Int("Limit")
			}
		}
		if set.linkNotes != nil {
			s.Notes = append(s.Notes, set.linkNotes...)
		}
	} else {
		s.Notes = append(s.Notes, "configuration not collected yet")
	}
	if st.dynMem != nil && running {
		pressure := st.dynMem.Int("CurrentPressure")
		phys := st.dynMem.Int("PhysicalMemory")
		demand := phys * pressure / 100
		s.MemoryPressurePercent = &pressure
		s.MemoryDemandMB = &demand
	}
	s.Guest = d.guest(id)
	return s
}

func avgLoad(procs []wmi.Row) *float64 {
	if len(procs) == 0 {
		return nil
	}
	var total float64
	for _, p := range procs {
		total += p.Float("LoadPercentage")
	}
	avg := total / float64(len(procs))
	return &avg
}

func assignedMB(mem []wmi.Row) int64 {
	var bytes int64
	for _, m := range mem {
		bytes += m.Int("NumberOfBlocks") * m.Int("BlockSize")
	}
	return MB(bytes)
}

func (d *Data) guest(id string) Guest {
	g := Guest{Heartbeat: "Unknown"}
	st := d.State.vms[id]
	if st.heartbeat != nil {
		codes := st.heartbeat.Ints("OperationalStatus")
		if len(codes) > 0 {
			g.HeartbeatCode = codes[0]
			g.Heartbeat = lookup(heartbeatStatuses, codes[0])
			g.Available = codes[0] == 2
		}
		if len(codes) > 1 {
			g.ApplicationState = lookup(heartbeatAppStatuses, codes[1])
		}
	}
	if set := d.settingsFor(id); set != nil && set.kvp != nil {
		k := set.kvp
		g.OSName = k["OSName"]
		g.OSVersion = k["OSVersion"]
		g.OSBuild = k["OSBuildNumber"]
		g.Hostname = k["FullyQualifiedDomainName"]
		g.IntegrationServices = k["IntegrationServicesVersion"]
		g.Architecture = k["ProcessorArchitecture"]
		g.IPv4 = splitList(k["NetworkAddressIPv4"])
		g.IPv6 = splitList(k["NetworkAddressIPv6"])
		if g.OSName != "" {
			g.Available = true
		}
	}
	return g
}

// Detail builds the /vms/{id} response.
func (d *Data) Detail(id string) (*VMDetail, error) {
	if d.State == nil {
		return nil, ErrNotReady
	}
	if _, ok := d.State.vms[id]; !ok {
		return nil, ErrNotFound
	}
	out := &VMDetail{VM: d.vm(id), Summary: d.summary(id)}
	if d.Settings == nil {
		out.Notes = d.settingsNotes()
		return out, nil
	}
	out.Config, _ = d.Config(id)
	out.Processor, _ = d.Processor(id)
	out.Memory, _ = d.Memory(id)
	out.Disks, _ = d.Disks(id)
	out.Network, _ = d.Network(id)
	return out, nil
}

// header is the common part of the per-VM part responses.
func (d *Data) header(id string) (name string, at time.Time, set *vmSettings, err error) {
	if d.State == nil {
		return "", time.Time{}, nil, ErrNotReady
	}
	st, ok := d.State.vms[id]
	if !ok {
		return "", time.Time{}, nil, ErrNotFound
	}
	if d.Settings == nil {
		return "", time.Time{}, nil, ErrNotReady
	}
	return st.cs.String("ElementName"), d.Settings.at.UTC(), d.Settings.vms[id], nil
}

// Config builds the /vms/{id}/config response.
func (d *Data) Config(id string) (*VMConfig, error) {
	name, at, set, err := d.header(id)
	if err != nil {
		return nil, err
	}
	out := &VMConfig{VMID: id, VMName: name, CollectedAt: at}
	if set == nil {
		out.Notes = append(d.settingsNotes("system_settings"), "no realized configuration found for this VM in the last settings poll")
		return out, nil
	}
	r := set.vssd
	out.Generation = generation(r)
	out.Version = r.String("Version")
	out.ConfigurationID = NormalizeID(r.String("ConfigurationID"))
	out.ConfigurationFile = r.String("ConfigurationFile")
	out.ConfigurationDataRoot = r.String("ConfigurationDataRoot")
	out.SnapshotDataRoot = r.String("SnapshotDataRoot")
	out.SwapFileDataRoot = r.String("SwapFileDataRoot")
	out.GuestStateDataRoot = r.String("GuestStateDataRoot")
	out.GuestStateFile = r.String("GuestStateFile")
	out.LogDataRoot = r.String("LogDataRoot")
	out.SuspendDataRoot = r.String("SuspendDataRoot")
	out.Created = CIMTime(r.String("CreationTime"))
	out.AutomaticStartAction = lookup(startupActions, r.Int("AutomaticStartupAction"))
	out.AutomaticStartDelaySec = int64(CIMInterval(r.String("AutomaticStartupActionDelay")).Seconds())
	out.AutomaticStopAction = lookup(shutdownActions, r.Int("AutomaticShutdownAction"))
	if r.Has("AutomaticRecoveryAction") {
		out.AutomaticRecoveryAct = lookup(recoveryActions, r.Int("AutomaticRecoveryAction"))
	}
	if r.Has("AutomaticSnapshotsEnabled") {
		b := r.Bool("AutomaticSnapshotsEnabled")
		out.AutomaticCheckpoints = &b
	}
	if r.Has("UserSnapshotType") {
		out.CheckpointType = lookup(map[int64]string{2: "Disabled", 3: "Production", 4: "ProductionOnly", 5: "Standard"}, r.Int("UserSnapshotType"))
	}
	out.BIOSGUID = r.String("BIOSGUID")
	out.BIOSSerialNumber = r.String("BIOSSerialNumber")
	out.BaseBoardSerialNumber = r.String("BaseBoardSerialNumber")
	out.ChassisSerialNumber = r.String("ChassisSerialNumber")
	out.ChassisAssetTag = r.String("ChassisAssetTag")
	for _, b := range r.Ints("BootOrder") {
		out.BootOrder = append(out.BootOrder, lookup(bootDevices, b))
	}
	out.BootSourceOrder = r.Strings("BootSourceOrder")
	if r.Has("NetworkBootPreferredProtocol") {
		out.NetworkBootProtocol = lookup(bootProtocols, r.Int("NetworkBootPreferredProtocol"))
	}
	if r.Has("SecureBootEnabled") {
		b := r.Bool("SecureBootEnabled")
		out.SecureBoot = &b
	}
	out.NumLock = r.Bool("BIOSNumLock")
	out.VirtualNUMA = r.Bool("VirtualNumaEnabled")
	if r.Has("ConsoleMode") {
		out.ConsoleMode = lookup(consoleModes, r.Int("ConsoleMode"))
	}
	if r.Has("EnhancedSessionTransportType") {
		out.EnhancedSessionMode = lookup(sessionTransports, r.Int("EnhancedSessionTransportType"))
	}
	out.LockOnDisconnect = r.Bool("LockOnDisconnect")
	out.IncrementalBackup = r.Bool("IncrementalBackupEnabled")
	out.GuestCacheTypes = r.Int("GuestControlledCacheTypes")
	out.DebugPortEnabled = r.Bool("DebugPortEnabled")
	out.DebugPort = r.Int("DebugPort")
	out.IsSaved = r.Bool("IsSaved")
	out.Description = strings.Join(r.Strings("Notes"), "\n")
	out.Notes = append(d.settingsNotes("system_settings"), set.linkNotes...)
	return out, nil
}

// Processor builds the /vms/{id}/processor response.
func (d *Data) Processor(id string) (*VMProcessor, error) {
	name, at, set, err := d.header(id)
	if err != nil {
		return nil, err
	}
	out := &VMProcessor{VMID: id, VMName: name, CollectedAt: at}
	st := d.State.vms[id]
	if st.cs.Int("EnabledState") == 2 {
		out.LoadPercent = avgLoad(st.procs)
		for _, p := range st.procs {
			segs := instanceSegments(p.String("DeviceID"))
			idx, _ := strconv.ParseInt(segs[len(segs)-1], 10, 64)
			out.VirtualProcessors = append(out.VirtualProcessors, VProc{Index: idx, LoadPercent: p.Float("LoadPercentage")})
		}
		sort.Slice(out.VirtualProcessors, func(i, j int) bool { return out.VirtualProcessors[i].Index < out.VirtualProcessors[j].Index })
	}
	out.Notes = append(d.settingsNotes("processor_settings"), d.State.errorNotes("processor")...)
	if set == nil || set.proc == nil {
		out.Notes = append(out.Notes, "no processor settings found for this VM in the last settings poll")
		return out, nil
	}
	r := set.proc
	out.Count = r.Int("VirtualQuantity")
	out.ReservePercent = float64(r.Int("Reservation")) / 1000
	out.LimitPercent = float64(r.Int("Limit")) / 1000
	out.RelativeWeight = r.Int("Weight")
	out.HwThreadsPerCore = r.Int("HwThreadsPerCore")
	out.MaxProcessorsPerNUMA = r.Int("MaxProcessorsPerNumaNode")
	out.MaxNUMANodesPerSocket = r.Int("MaxNumaNodesPerSocket")
	out.CompatibilityMode = r.Bool("LimitProcessorFeatures")
	out.NestedVirtualization = r.Bool("ExposeVirtualizationExtensions")
	out.HostResourceProtection = r.Bool("EnableHostResourceProtection")
	out.PerfmonPMU = r.Bool("EnablePerfmonPmu")
	out.Notes = append(out.Notes, set.linkNotes...)
	return out, nil
}

// Memory builds the /vms/{id}/memory response.
func (d *Data) Memory(id string) (*VMMemory, error) {
	name, at, set, err := d.header(id)
	if err != nil {
		return nil, err
	}
	out := &VMMemory{VMID: id, VMName: name, CollectedAt: at}
	st := d.State.vms[id]
	out.AssignedMB = assignedMB(st.mem)
	if st.dynMem != nil && st.cs.Int("EnabledState") == 2 {
		dm := st.dynMem
		pressure, avg := dm.Int("CurrentPressure"), dm.Int("AveragePressure")
		phys := dm.Int("PhysicalMemory")
		demand := phys * pressure / 100
		visible, added, removed := dm.Int("GuestVisiblePhysicalMemory"), dm.Int("AddedMemory"), dm.Int("RemovedMemory")
		out.DemandMB, out.PressurePercent, out.AveragePressure = &demand, &pressure, &avg
		out.GuestVisibleMB, out.AddedMB, out.RemovedMB = &visible, &added, &removed
		if dm.Has("GuestAvailableMemory") {
			avail := dm.Int("GuestAvailableMemory")
			out.GuestAvailableMB = &avail
		}
	}
	out.Notes = append(d.settingsNotes("memory_settings"), d.State.errorNotes("memory", "dynamic_memory_counters")...)
	if set == nil || set.mem == nil {
		out.Notes = append(out.Notes, "no memory settings found for this VM in the last settings poll")
		return out, nil
	}
	r := set.mem
	out.StartupMB = r.Int("VirtualQuantity")
	out.DynamicMemory = r.Bool("DynamicMemoryEnabled")
	if out.DynamicMemory {
		out.MinimumMB = r.Int("Reservation")
		out.MaximumMB = r.Int("Limit")
		out.BufferPercent = r.Int("TargetMemoryBuffer")
	}
	out.Weight = r.Int("Weight")
	out.Notes = append(out.Notes, set.linkNotes...)
	return out, nil
}

// Disks builds the /vms/{id}/disks response from the controller, drive and
// media resource settings and their Parent links.
func (d *Data) Disks(id string) (*VMDisks, error) {
	name, at, set, err := d.header(id)
	if err != nil {
		return nil, err
	}
	out := &VMDisks{VMID: id, VMName: name, CollectedAt: at, Controllers: []Controller{}}
	out.Notes = d.settingsNotes("resource_allocation", "storage_allocation")
	if set == nil {
		return out, nil
	}
	out.Notes = append(out.Notes, set.linkNotes...)

	type ctl struct {
		c   *Controller
		row wmi.Row
	}
	var ctls []ctl
	byInstance := map[string]*Controller{}
	counts := map[string]int{}
	for _, r := range set.rasd {
		var typ string
		switch r.String("ResourceSubType") {
		case subIDEController:
			typ = "IDE"
		case subSCSIController:
			typ = "SCSI"
		case subFCPort:
			typ = "Fibre Channel"
		default:
			continue
		}
		c := &Controller{Type: typ, Name: r.String("ElementName"), InstanceID: r.String("InstanceID"), Drives: []Drive{}}
		c.Number = r.String("Address")
		if c.Number == "" {
			c.Number = strconv.Itoa(counts[typ])
		}
		counts[typ]++
		byInstance[strings.ToLower(c.InstanceID)] = c
		ctls = append(ctls, ctl{c, r})
	}

	type drv struct {
		d   *Drive
		c   *Controller
		row wmi.Row
	}
	drives := map[string]*drv{}
	for _, r := range set.rasd {
		var typ string
		switch r.String("ResourceSubType") {
		case subDiskDrive:
			typ = "Hard Drive"
		case subDVDDrive:
			typ = "DVD Drive"
		case subPhysicalDisk:
			typ = "Physical Disk"
		case subFloppyDrive:
			typ = "Floppy Drive"
		default:
			continue
		}
		parent := strings.ToLower(PathKey(r.String("Parent"), "InstanceID"))
		c := byInstance[parent]
		dr := &Drive{Type: typ, Location: r.String("AddressOnParent"), Name: r.String("ElementName"), Media: "None", InstanceID: r.String("InstanceID")}
		if typ == "Physical Disk" {
			dr.Media = "Physical"
			if hr := r.Strings("HostResource"); len(hr) > 0 {
				dr.Path = PathKey(hr[0], "DeviceID")
				if dr.Path == "" {
					dr.Path = hr[0]
				}
			}
		}
		if c != nil {
			dr.DiskID = strings.ToLower(strings.ReplaceAll(c.Type, " ", "")) + c.Number + "-" + dr.Location
		} else {
			dr.Notes = append(dr.Notes, "controller not found: "+r.String("Parent"))
		}
		drives[strings.ToLower(dr.InstanceID)] = &drv{dr, c, r}
	}
	for _, r := range set.sasd {
		parent := strings.ToLower(PathKey(r.String("Parent"), "InstanceID"))
		dv := drives[parent]
		if dv == nil {
			continue
		}
		switch r.String("ResourceSubType") {
		case subVirtualHardDisk:
			dv.d.Media = "VHD"
		case subVirtualDVD:
			dv.d.Media = "ISO"
		case subVirtualFloppy:
			dv.d.Media = "Floppy image"
		default:
			dv.d.Media = r.String("ResourceSubType")
		}
		if hr := r.Strings("HostResource"); len(hr) > 0 {
			dv.d.Path = hr[0]
		}
		dv.d.MediaName = r.String("ElementName")
		dv.d.IOPSMin = r.Int("IOPSReservation")
		dv.d.IOPSMax = r.Int("IOPSLimit")
	}

	var orphans []Drive
	for _, dv := range drives {
		if dv.c != nil {
			dv.c.Drives = append(dv.c.Drives, *dv.d)
		} else {
			orphans = append(orphans, *dv.d)
		}
	}
	for _, c := range ctls {
		sort.Slice(c.c.Drives, func(i, j int) bool { return locLess(c.c.Drives[i].Location, c.c.Drives[j].Location) })
	}
	sort.SliceStable(ctls, func(i, j int) bool {
		if ctls[i].c.Type != ctls[j].c.Type {
			return ctls[i].c.Type == "IDE" // IDE first, as Hyper-V Manager shows it
		}
		return locLess(ctls[i].c.Number, ctls[j].c.Number)
	})
	for _, c := range ctls {
		out.Controllers = append(out.Controllers, *c.c)
	}
	if len(orphans) > 0 {
		out.Controllers = append(out.Controllers, Controller{Type: "Unknown", Name: "Drives without a known controller", Drives: orphans})
	}
	return out, nil
}

func locLess(a, b string) bool {
	ai, aerr := strconv.Atoi(a)
	bi, berr := strconv.Atoi(b)
	if aerr == nil && berr == nil {
		return ai < bi
	}
	return a < b
}

// Network builds the /vms/{id}/network response.
func (d *Data) Network(id string) (*VMNetwork, error) {
	name, at, set, err := d.header(id)
	if err != nil {
		return nil, err
	}
	out := &VMNetwork{VMID: id, VMName: name, CollectedAt: at, Adapters: []Adapter{}}
	out.Notes = d.settingsNotes("synthetic_ethernet_ports", "ethernet_connections", "vlan_settings", "guest_network", "switches")
	if set == nil {
		return out, nil
	}
	out.Notes = append(out.Notes, set.linkNotes...)

	for _, p := range set.ports {
		a := Adapter{
			Name:           p.String("ElementName"),
			Type:           "Synthetic",
			MACAddress:     formatMAC(p.String("Address")),
			StaticMAC:      p.Bool("StaticMacAddress"),
			DeviceNaming:   p.Bool("DeviceNamingEnabled"),
			ClusterMonitor: p.Bool("ClusterMonitored"),
			InstanceID:     p.String("InstanceID"),
		}
		if p.String("ResourceSubType") == subEmulatedEthernet {
			a.Type = "Legacy"
		}
		for _, c := range set.conns {
			if !strings.EqualFold(PathKey(c.String("Parent"), "InstanceID"), a.InstanceID) {
				continue
			}
			if hr := c.Strings("HostResource"); len(hr) > 0 {
				a.SwitchID = NormalizeID(PathKey(hr[0], "Name"))
				a.Switch = d.Settings.switches[a.SwitchID]
				if a.Switch == "" {
					a.Switch = a.SwitchID
				}
			}
			a.Connected = c.Int("EnabledState") == 2 && a.SwitchID != ""
			a.VLANMode, a.VLANID, a.TrunkVLANs = vlanFor(set.vlans, c.String("InstanceID"))
		}
		segs := instanceSegments(a.InstanceID)
		adapterGUID := strings.ToLower(segs[len(segs)-1])
		for _, g := range set.guestNet {
			gs := instanceSegments(g.String("InstanceID"))
			if strings.ToLower(gs[len(gs)-1]) != adapterGUID {
				continue
			}
			a.GuestIPs = g.Strings("IPAddresses")
			a.GuestSubnets = g.Strings("Subnets")
			a.GuestGateways = g.Strings("DefaultGateways")
			a.GuestDNSServers = g.Strings("DNSServers")
			if g.Has("DHCPEnabled") {
				b := g.Bool("DHCPEnabled")
				a.GuestDHCP = &b
			}
		}
		out.Adapters = append(out.Adapters, a)
	}
	sort.SliceStable(out.Adapters, func(i, j int) bool { return out.Adapters[i].InstanceID < out.Adapters[j].InstanceID })
	return out, nil
}

// formatMAC turns "00155D0A0B0C" into "00:15:5D:0A:0B:0C".
func formatMAC(s string) string {
	s = strings.ToUpper(strings.NewReplacer(":", "", "-", "").Replace(strings.TrimSpace(s)))
	if len(s) != 12 {
		return s
	}
	parts := make([]string, 6)
	for i := range parts {
		parts[i] = s[i*2 : i*2+2]
	}
	return strings.Join(parts, ":")
}
