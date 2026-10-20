package hyperv

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/damiankw/hvapi/internal/wmi"
)

// ready checks that the polls the host views need have run.
func (d *Data) ready(settings bool) error {
	if d.State == nil || (settings && d.Settings == nil) {
		return ErrNotReady
	}
	return nil
}

func first(rows []wmi.Row) wmi.Row {
	if len(rows) == 0 {
		return nil
	}
	return rows[0]
}

func boolPtr(r wmi.Row, name string) *bool {
	if r == nil || !r.Has(name) {
		return nil
	}
	b := r.Bool(name)
	return &b
}

func intPtr(r wmi.Row, name string) *int64 {
	if r == nil || !r.Has(name) {
		return nil
	}
	n := r.Int(name)
	return &n
}

func floatPtr(r wmi.Row, name string) *float64 {
	if r == nil || !r.Has(name) {
		return nil
	}
	f := r.Float(name)
	return &f
}

// Host builds the /host response.
func (d *Data) Host() (*HostInfo, error) {
	if err := d.ready(true); err != nil {
		return nil, err
	}
	cs := first(d.Settings.rows["host_computer"])
	os := first(d.State.rows["operating_system"])
	vsms := first(d.Settings.rows["management_settings"])
	out := &HostInfo{CollectedAt: d.Settings.at.UTC(), VMCount: len(d.State.vms)}
	for _, v := range d.State.vms {
		if v.cs.Int("EnabledState") == 2 {
			out.VMRunning++
		}
	}
	if cs != nil {
		out.Name = cs.String("Name")
		out.Domain = cs.String("Domain")
		out.Manufacturer = cs.String("Manufacturer")
		out.Model = cs.String("Model")
		out.Sockets = cs.Int("NumberOfProcessors")
		out.LogicalProcessors = cs.Int("NumberOfLogicalProcessors")
		out.MemoryTotalMB = MB(cs.Int("TotalPhysicalMemory"))
		out.HypervisorPresent = cs.Bool("HypervisorPresent")
	}
	if os != nil {
		out.OS = os.String("Caption")
		out.OSVersion = os.String("Version")
		out.OSBuild = os.String("BuildNumber")
		out.OSArchitecture = os.String("OSArchitecture")
		out.InstallDate = CIMTime(os.String("InstallDate"))
		out.LastBoot = CIMTime(os.String("LastBootUpTime"))
		out.HostTime = CIMTime(os.String("LocalDateTime"))
		if out.LastBoot != nil {
			ref := d.State.at
			if out.HostTime != nil {
				ref = *out.HostTime
			}
			out.UptimeSeconds = int64(ref.Sub(*out.LastBoot).Seconds())
		}
		out.MemoryFreeMB = os.Int("FreePhysicalMemory") / 1024
		if out.MemoryTotalMB == 0 {
			out.MemoryTotalMB = os.Int("TotalVisibleMemorySize") / 1024
		}
	}
	for i, p := range d.Settings.rows["host_processors"] {
		out.Cores += p.Int("NumberOfCores")
		if i == 0 {
			out.ProcessorModel = strings.Join(strings.Fields(p.String("Name")), " ")
			out.ProcessorMHz = p.Int("MaxClockSpeed")
			out.VirtualizationFW = boolPtr(p, "VirtualizationFirmwareEnabled")
			out.SLAT = boolPtr(p, "SecondLevelAddressTranslationExtensions")
		}
	}
	if vsms != nil {
		out.DefaultVMPath = vsms.String("DefaultExternalDataRoot")
		out.DefaultVHDPath = vsms.String("DefaultVirtualHardDiskPath")
		out.MACRangeStart = formatMAC(vsms.String("MinimumMacAddress"))
		out.MACRangeEnd = formatMAC(vsms.String("MaximumMacAddress"))
		out.NUMASpanning = boolPtr(vsms, "NumaSpanningEnabled")
		out.EnhancedSession = boolPtr(vsms, "EnhancedSessionModeEnabled")
	}
	out.Notes = append(d.Settings.errorNotes("host_computer", "host_processors", "management_settings"), d.State.errorNotes("operating_system")...)
	return out, nil
}

var coreServices = map[string]bool{"vmms": true, "vmcompute": true, "hvhost": true}

// HostServices builds the /host/services response.
func (d *Data) HostServices() (*HostServices, error) {
	if err := d.ready(false); err != nil {
		return nil, err
	}
	out := &HostServices{CollectedAt: d.State.at.UTC(), Services: []ServiceInfo{}, AllRunning: true}
	seen := map[string]bool{}
	for _, r := range d.State.rows["host_services"] {
		name := strings.ToLower(r.String("Name"))
		s := ServiceInfo{
			Name: r.String("Name"), DisplayName: r.String("DisplayName"), State: r.String("State"),
			StartMode: r.String("StartMode"), Status: r.String("Status"), ProcessID: r.Int("ProcessId"),
			Account: r.String("StartName"), ExitCode: r.Int("ExitCode"), Expected: coreServices[name],
		}
		seen[name] = true
		if s.Expected && s.State != "Running" {
			out.AllRunning = false
		}
		out.Services = append(out.Services, s)
	}
	for name := range coreServices {
		if !seen[name] {
			out.AllRunning = false
			out.Notes = append(out.Notes, "service "+name+" is not installed on this host")
		}
	}
	sort.Slice(out.Services, func(i, j int) bool { return out.Services[i].Name < out.Services[j].Name })
	sort.Strings(out.Notes)
	out.Notes = append(out.Notes, d.State.errorNotes("host_services")...)
	if len(d.State.rows["host_services"]) == 0 {
		out.AllRunning = false
	}
	return out, nil
}

var volumeHealth = map[int64]string{0: "Healthy", 1: "Warning", 2: "Unhealthy", 5: "Unknown"}
var driveTypes = map[int64]string{0: "Unknown", 1: "Invalid root path", 2: "Removable", 3: "Fixed", 4: "Network", 5: "CD-ROM", 6: "RAM disk"}

// driveLetter normalises MSFT_Volume.DriveLetter, which may arrive as a
// character or its numeric code.
func driveLetter(r wmi.Row) string {
	switch v := r["DriveLetter"].(type) {
	case nil:
		return ""
	case string:
		if v == "" || v == "\x00" {
			return ""
		}
		return strings.ToUpper(v[:1])
	default:
		n := r.Int("DriveLetter")
		if n >= 'A' && n <= 'z' {
			return strings.ToUpper(string(rune(n)))
		}
		return ""
	}
}

// HostStorage builds the /host/storage response.
func (d *Data) HostStorage() (*HostStorage, error) {
	if err := d.ready(true); err != nil {
		return nil, err
	}
	out := &HostStorage{CollectedAt: d.Settings.at.UTC(), Volumes: []Volume{}}

	// CSV ownership and state come from the cluster resource of the same name.
	resByName := map[string]wmi.Row{}
	for _, r := range d.State.rows["cluster_resources"] {
		resByName[strings.ToLower(r.String("Name"))] = r
	}

	for _, r := range d.Settings.rows["volumes"] {
		v := Volume{
			DriveLetter: driveLetter(r), Label: r.String("FileSystemLabel"), Path: r.String("Path"),
			FileSystem: r.String("FileSystem"), SizeMB: MB(r.Int("Size")), FreeMB: MB(r.Int("SizeRemaining")),
			Health: lookup(volumeHealth, r.Int("HealthStatus")), Type: lookup(driveTypes, r.Int("DriveType")),
		}
		if v.FileSystem == "" {
			v.FileSystem = r.String("FileSystemType")
		}
		if v.FileSystem == "CSVFS" || strings.Contains(strings.ToLower(v.Path), `\clusterstorage\`) {
			v.ClusterShared = true
			v.Type = "Cluster Shared Volume"
		}
		if v.SizeMB > 0 {
			v.FreePercent = float64(v.FreeMB) * 100 / float64(v.SizeMB)
		}
		out.Volumes = append(out.Volumes, v)
	}
	for _, r := range d.Settings.rows["cluster_shared_volumes"] {
		v := Volume{Path: r.String("VolumeName"), ClusterResource: r.String("Name"), ClusterShared: true, Type: "Cluster Shared Volume"}
		if info, ok := r["SharedVolumeInfo"].(wmi.Row); ok {
			v.Label = info.String("FriendlyVolumeName")
			if part, ok := info["Partition"].(wmi.Row); ok {
				v.SizeMB, v.FreeMB = MB(part.Int("Size")), MB(part.Int("FreeSpace"))
				v.FileSystem = part.String("FileSystem")
				if v.Label == "" {
					v.Label = part.String("VolumeLabel")
				}
			}
		}
		if v.SizeMB > 0 {
			v.FreePercent = float64(v.FreeMB) * 100 / float64(v.SizeMB)
		}
		if res := resByName[strings.ToLower(r.String("Name"))]; res != nil {
			v.CSVOwner = res.String("OwnerNode")
			v.CSVState = lookup(resourceStates, res.Int("State"))
		}
		// Prefer the CSV entry over a duplicate MSFT_Volume row for the same path.
		replaced := false
		for i := range out.Volumes {
			if out.Volumes[i].ClusterShared && samePath(out.Volumes[i].Path, v.Path) {
				if out.Volumes[i].Label == "" {
					out.Volumes[i].Label = v.Label
				}
				out.Volumes[i].CSVOwner, out.Volumes[i].CSVState = v.CSVOwner, v.CSVState
				replaced = true
			}
		}
		if !replaced {
			out.Volumes = append(out.Volumes, v)
		}
	}

	// Which VMs keep files on which volume: longest mount point prefix wins.
	type mount struct {
		prefix string
		idx    int
	}
	var mounts []mount
	for i, v := range out.Volumes {
		if v.DriveLetter != "" {
			mounts = append(mounts, mount{strings.ToLower(v.DriveLetter + `:\`), i})
		}
		if v.Path != "" && !strings.HasPrefix(v.Path, `\\?\`) {
			p := strings.ToLower(v.Path)
			if !strings.HasSuffix(p, `\`) {
				p += `\`
			}
			mounts = append(mounts, mount{p, i})
		}
	}
	vmNames := make([]map[string]bool, len(out.Volumes))
	for id, s := range d.Settings.vms {
		name := id
		if st := d.State.vms[id]; st != nil {
			name = st.cs.String("ElementName")
		}
		var paths []string
		paths = append(paths, s.vssd.String("ConfigurationDataRoot"), s.vssd.String("SnapshotDataRoot"))
		for _, r := range s.sasd {
			paths = append(paths, r.Strings("HostResource")...)
		}
		for _, p := range paths {
			p = strings.ToLower(p)
			best := -1
			bestLen := 0
			for _, m := range mounts {
				if strings.HasPrefix(p, m.prefix) && len(m.prefix) > bestLen {
					best, bestLen = m.idx, len(m.prefix)
				}
			}
			if best >= 0 {
				out.Volumes[best].VMFiles++
				if vmNames[best] == nil {
					vmNames[best] = map[string]bool{}
				}
				vmNames[best][name] = true
			}
		}
	}
	for i := range out.Volumes {
		for n := range vmNames[i] {
			out.Volumes[i].VMs = append(out.Volumes[i].VMs, n)
		}
		sort.Strings(out.Volumes[i].VMs)
	}
	sort.SliceStable(out.Volumes, func(i, j int) bool {
		a, b := out.Volumes[i], out.Volumes[j]
		if (a.DriveLetter != "") != (b.DriveLetter != "") {
			return a.DriveLetter != ""
		}
		if a.DriveLetter != b.DriveLetter {
			return a.DriveLetter < b.DriveLetter
		}
		return a.Path < b.Path
	})
	out.Notes = append(d.Settings.errorNotes("volumes"), clusterNotes(d.Settings.rowSet, "cluster_shared_volumes")...)
	return out, nil
}

func samePath(a, b string) bool {
	return strings.TrimSuffix(strings.ToLower(a), `\`) == strings.TrimSuffix(strings.ToLower(b), `\`)
}

// clusterNotes reports a missing cluster namespace as "not clustered"
// rather than as a failure.
func clusterNotes(rs *rowSet, names ...string) []string {
	var notes []string
	for _, n := range names {
		err, ok := rs.errs[n]
		if !ok {
			continue
		}
		if !isClusterAbsent(err) {
			notes = append(notes, rs.errorNotes(n)...)
		}
	}
	return notes
}

func isClusterAbsent(err error) bool {
	return err != nil && (errorsIs(err, wmi.ErrNamespaceNotFound) || errorsIs(err, wmi.ErrInvalidClass))
}

// switchTopology links switches, their host-side ports and their VM ports
// from the EPASD rows, which describe both ends.
type switchTopology struct {
	external map[string][]string // switch GUID -> bound physical adapter names
	internal map[string]bool     // switch GUID -> has a management OS port
	vmConns  map[string][]wmi.Row
	extToSw  map[string]string // external port DeviceID (lower) -> switch name
}

func (d *Data) topology() *switchTopology {
	t := &switchTopology{external: map[string][]string{}, internal: map[string]bool{}, vmConns: map[string][]wmi.Row{}, extToSw: map[string]string{}}
	swName := map[string]string{}
	for _, r := range d.Settings.rows["switches"] {
		swName[NormalizeID(r.String("Name"))] = r.String("ElementName")
	}
	extName := map[string]string{}
	for _, r := range d.Settings.rows["external_ports"] {
		extName[strings.ToLower(r.String("DeviceID"))] = r.String("ElementName")
	}
	for _, r := range d.Settings.rows["ethernet_connections"] {
		hr := r.Strings("HostResource")
		if len(hr) == 0 {
			continue
		}
		owner := VMIDFromInstanceID(r.String("InstanceID"))
		switch PathClass(hr[0]) {
		case "Msvm_VirtualEthernetSwitch":
			sw := NormalizeID(PathKey(hr[0], "Name"))
			if _, isVM := d.Settings.vms[owner]; isVM || owner != sw {
				t.vmConns[sw] = append(t.vmConns[sw], r)
			}
		case "Msvm_ExternalEthernetPort":
			dev := strings.ToLower(PathKey(hr[0], "DeviceID"))
			name := extName[dev]
			if name == "" {
				name = dev
			}
			t.external[owner] = append(t.external[owner], name)
			t.extToSw[dev] = swName[owner]
		case "Msvm_InternalEthernetPort":
			t.internal[owner] = true
		}
	}
	return t
}

// HostNetwork builds the /host/network response.
func (d *Data) HostNetwork() (*HostNetwork, error) {
	if err := d.ready(true); err != nil {
		return nil, err
	}
	out := &HostNetwork{CollectedAt: d.Settings.at.UTC(), Adapters: []HostAdapter{}}
	t := d.topology()
	swByMAC := map[string]string{}
	for _, r := range d.Settings.rows["external_ports"] {
		if sw := t.extToSw[strings.ToLower(r.String("DeviceID"))]; sw != "" {
			swByMAC[formatMAC(r.String("PermanentAddress"))] = sw
		}
	}
	for _, r := range d.Settings.rows["net_adapters"] {
		a := HostAdapter{
			Name: r.String("Name"), Description: r.String("InterfaceDescription"), MACAddress: formatMAC(r.String("MacAddress")),
			Status: r.String("Status"), MTU: r.Int("MtuSize"), Virtual: r.Bool("Virtual"), DriverVersion: r.String("DriverVersionString"),
			InterfaceIndex: r.Int("InterfaceIndex"),
		}
		if a.DriverVersion == "" {
			a.DriverVersion = r.String("DriverVersion")
		}
		if a.Status == "" {
			a.Status = lookup(map[int64]string{1: "Up", 2: "Down", 3: "Testing", 4: "Unknown", 5: "Dormant", 6: "Not Present", 7: "Lower Layer Down"}, r.Int("InterfaceOperationalStatus"))
		}
		if speed := r.Int("ReceiveLinkSpeed"); speed > 0 {
			a.LinkSpeedMbps = speed / 1_000_000
		} else if speed := r.Int("Speed"); speed > 0 {
			a.LinkSpeedMbps = speed / 1_000_000
		}
		a.BoundToSwitch = swByMAC[a.MACAddress]
		out.Adapters = append(out.Adapters, a)
	}
	sort.SliceStable(out.Adapters, func(i, j int) bool { return out.Adapters[i].Name < out.Adapters[j].Name })
	out.Notes = d.Settings.errorNotes("net_adapters", "external_ports")
	return out, nil
}

// HostPerf builds the /host/perf response.
func (d *Data) HostPerf() (*HostPerf, error) {
	if err := d.ready(false); err != nil {
		return nil, err
	}
	out := &HostPerf{CollectedAt: d.State.at.UTC()}
	for _, r := range d.State.rows["hv_logical_processors"] {
		lp := LPCounter{
			Name: r.String("Name"), TotalPercent: r.Float("PercentTotalRunTime"), GuestPercent: r.Float("PercentGuestRunTime"),
			HypervisorPct: r.Float("PercentHypervisorRunTime"), IdlePercent: r.Float("PercentIdleTime"), ContextSwitches: r.Int("ContextSwitchesPersec"),
		}
		if lp.Name == "_Total" {
			out.CPUTotalPercent, out.CPUGuestPercent, out.CPUHypervisorPercent = &lp.TotalPercent, &lp.GuestPercent, &lp.HypervisorPct
			continue
		}
		out.LogicalProcessors = append(out.LogicalProcessors, lp)
	}
	sort.SliceStable(out.LogicalProcessors, func(i, j int) bool { return lpLess(out.LogicalProcessors[i].Name, out.LogicalProcessors[j].Name) })
	for _, r := range d.State.rows["hv_root_processors"] {
		if r.String("Name") == "_Total" {
			out.CPURootPercent = floatPtr(r, "PercentTotalRunTime")
		}
	}
	if hv := first(d.State.rows["hypervisor_counters"]); hv != nil {
		out.Partitions, out.VirtualProcessors = intPtr(hv, "Partitions"), intPtr(hv, "VirtualProcessors")
	}
	if m := first(d.State.rows["os_memory_counters"]); m != nil {
		out.MemoryAvailableMB = intPtr(m, "AvailableMBytes")
		if m.Has("CommittedBytes") {
			v := MB(m.Int("CommittedBytes"))
			out.MemoryCommittedMB = &v
		}
		if m.Has("CommitLimit") {
			v := MB(m.Int("CommitLimit"))
			out.MemoryCommitLimitMB = &v
		}
		out.PagesPerSec = intPtr(m, "PagesPersec")
	}
	if b := first(d.State.rows["memory_balancer_counters"]); b != nil {
		out.BalancerAvailableMB, out.BalancerPressure = intPtr(b, "AvailableMemory"), intPtr(b, "SystemCurrentPressure")
	}
	out.Notes = d.State.errorNotes("hv_logical_processors", "hv_root_processors", "hypervisor_counters", "os_memory_counters", "memory_balancer_counters")
	d.hostPerfExtras(out)
	return out, nil
}

// lpLess orders "Hv LP 2" before "Hv LP 10".
func lpLess(a, b string) bool {
	fa, fb := strings.Fields(a), strings.Fields(b)
	if len(fa) > 0 && len(fb) > 0 {
		ia, erra := strconv.Atoi(fa[len(fa)-1])
		ib, errb := strconv.Atoi(fb[len(fb)-1])
		if erra == nil && errb == nil {
			return ia < ib
		}
	}
	return a < b
}

// HostNUMA builds the /host/numa response.
func (d *Data) HostNUMA() (*HostNUMA, error) {
	if err := d.ready(true); err != nil {
		return nil, err
	}
	out := &HostNUMA{CollectedAt: d.Settings.at.UTC(), Nodes: []NUMANode{}}
	for _, r := range d.Settings.rows["numa_nodes"] {
		out.Nodes = append(out.Nodes, NUMANode{
			ID: r.String("NodeID"), Name: r.String("ElementName"), LogicalProcessors: r.Int("NumberOfLogicalProcessors"),
			Cores: r.Int("NumberOfProcessorCores"), MemoryMB: r.Int("CurrentlyConsumableMemoryBlocks"),
		})
	}
	sort.Slice(out.Nodes, func(i, j int) bool { return lpLess(out.Nodes[i].ID, out.Nodes[j].ID) })
	out.Notes = d.Settings.errorNotes("numa_nodes")
	return out, nil
}

var vbsStatus = map[int64]string{0: "Off", 1: "Enabled but not running", 2: "Running"}
var vbsServices = map[int64]string{1: "Credential Guard", 2: "Hypervisor enforced Code Integrity", 3: "System Guard Secure Launch", 4: "SMM Firmware Measurement", 5: "Kernel-mode Hardware-enforced Stack Protection", 6: "Kernel-mode Hardware-enforced Stack Protection (audit)", 7: "Hypervisor-Enforced Paging Translation"}

// HostHypervisor builds the /host/hypervisor response.
func (d *Data) HostHypervisor() (*HostHypervisor, error) {
	if err := d.ready(true); err != nil {
		return nil, err
	}
	out := &HostHypervisor{CollectedAt: d.Settings.at.UTC(), SchedulerType: d.Settings.schedulerType}
	if cs := first(d.Settings.rows["host_computer"]); cs != nil {
		out.Present = cs.Bool("HypervisorPresent")
	}
	if os := first(d.State.rows["operating_system"]); os != nil {
		out.OSVersion, out.OSBuild = os.String("Version"), os.String("BuildNumber")
	}
	if hv := first(d.State.rows["hypervisor_counters"]); hv != nil {
		out.LogicalProcessors, out.VirtualProcessors, out.Partitions = intPtr(hv, "LogicalProcessors"), intPtr(hv, "VirtualProcessors"), intPtr(hv, "Partitions")
		out.Present = true
	}
	if dg := first(d.Settings.rows["device_guard"]); dg != nil {
		out.VBSStatus = lookup(vbsStatus, dg.Int("VirtualizationBasedSecurityStatus"))
		for _, code := range dg.Ints("SecurityServicesRunning") {
			out.VBSServices = append(out.VBSServices, lookup(vbsServices, code))
		}
	}
	out.Notes = append(d.Settings.errorNotes("host_computer", "device_guard"), d.State.errorNotes("operating_system", "hypervisor_counters")...)
	if out.SchedulerType == "" {
		out.Notes = append(out.Notes, "scheduler_type is read from the Hypervisor-Operational event log, which is not collected yet")
	}
	return out, nil
}

// HostMigration builds the /host/migration response.
func (d *Data) HostMigration() (*HostMigration, error) {
	if err := d.ready(true); err != nil {
		return nil, err
	}
	out := &HostMigration{CollectedAt: d.Settings.at.UTC(), Networks: []MigrationNetwork{}}
	if m := first(d.Settings.rows["migration_settings"]); m != nil {
		out.Enabled = m.Bool("EnableVirtualSystemMigration")
		out.MaxActiveMigrations = m.Int("MaximumActiveVirtualSystemMigration")
		out.MaxStorageMigrations = m.Int("MaximumActiveStorageMigration")
		out.Authentication = lookup(map[int64]string{0: "CredSSP", 1: "Kerberos"}, m.Int("AuthenticationType"))
		switch {
		case m.Bool("EnableSmbTransport"):
			out.PerformanceOption = "SMB"
		case m.Bool("EnableCompression"):
			out.PerformanceOption = "Compression"
		default:
			out.PerformanceOption = "TCP/IP"
		}
		out.UseAnyNetwork = boolPtr(m, "UseAnyNetworkForMigration")
	}
	for _, n := range d.Settings.rows["migration_networks"] {
		out.Networks = append(out.Networks, MigrationNetwork{Subnet: fmt.Sprintf("%s/%d", n.String("SubnetNumber"), n.Int("PrefixLength")), Metric: n.Int("Metric")})
	}
	sort.Slice(out.Networks, func(i, j int) bool { return out.Networks[i].Metric < out.Networks[j].Metric })
	out.Notes = d.Settings.errorNotes("migration_settings", "migration_networks")
	return out, nil
}

var nodeStates = map[int64]string{-1: "Unknown", 0: "Up", 1: "Down", 2: "Paused", 3: "Joining"}
var groupStates = map[int64]string{-1: "Unknown", 0: "Online", 1: "Offline", 2: "Failed", 3: "Partial Online", 4: "Pending"}
var resourceStates = map[int64]string{-1: "Unknown", 0: "Inherited", 1: "Initializing", 2: "Online", 3: "Offline", 4: "Failed", 128: "Pending", 129: "Online Pending", 130: "Offline Pending"}

// HostCluster builds the /host/cluster response.
func (d *Data) HostCluster() (*HostCluster, error) {
	if err := d.ready(true); err != nil {
		return nil, err
	}
	out := &HostCluster{CollectedAt: d.State.at.UTC(), Nodes: []ClusterNode{}, Groups: []ClusterGroup{}, Resources: []ClusterResource{}}
	if c := first(d.Settings.rows["cluster"]); c != nil {
		out.Clustered = true
		out.Name = c.String("Name")
	}
	for _, r := range d.State.rows["cluster_nodes"] {
		out.Clustered = true
		n := ClusterNode{Name: r.String("Name"), State: lookup(nodeStates, r.Int("State")), Weight: r.Int("NodeWeight"), DynamicWeight: r.Int("DynamicWeight")}
		if r.Has("MajorVersion") {
			n.Build = fmt.Sprintf("%d.%d.%d", r.Int("MajorVersion"), r.Int("MinorVersion"), r.Int("BuildNumber"))
		}
		out.Nodes = append(out.Nodes, n)
	}
	for _, r := range d.State.rows["cluster_groups"] {
		out.Groups = append(out.Groups, ClusterGroup{Name: r.String("Name"), State: lookup(groupStates, r.Int("State")), OwnerNode: r.String("OwnerNode")})
	}
	for _, r := range d.State.rows["cluster_resources"] {
		res := ClusterResource{Name: r.String("Name"), Type: r.String("Type"), State: lookup(resourceStates, r.Int("State")), Group: r.String("OwnerGroup"), OwnerNode: r.String("OwnerNode")}
		if pp, ok := r["PrivateProperties"].(wmi.Row); ok && pp.Has("VmID") {
			res.VMID = NormalizeID(pp.String("VmID"))
		}
		out.Resources = append(out.Resources, res)
	}
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].Name < out.Nodes[j].Name })
	sort.Slice(out.Groups, func(i, j int) bool { return out.Groups[i].Name < out.Groups[j].Name })
	sort.Slice(out.Resources, func(i, j int) bool { return out.Resources[i].Name < out.Resources[j].Name })
	if !out.Clustered {
		if err := d.Settings.errs["cluster"]; isClusterAbsent(err) || err == nil {
			out.Notes = append(out.Notes, "this host is not a member of a failover cluster")
			return out, nil
		}
	}
	out.Notes = append(clusterNotes(d.Settings.rowSet, "cluster"), clusterNotes(d.State.rowSet, "cluster_nodes", "cluster_groups", "cluster_resources")...)
	return out, nil
}

var bandwidthModes = map[int64]string{0: "Default", 1: "Weight", 2: "Absolute", 3: "None"}
var extensionTypes = map[int64]string{1: "Capture", 2: "Filter", 3: "Forwarding", 4: "Monitoring"}

func (d *Data) buildSwitch(r wmi.Row, t *switchTopology) Switch {
	id := NormalizeID(r.String("Name"))
	sw := Switch{
		ID: id, Name: r.String("ElementName"), Health: lookup(healthStates, r.Int("HealthState")),
		State: lookup(vmStates, r.Int("EnabledState")), ExternalAdapters: t.external[id],
		ManagementOSAccess: t.internal[id], VMPorts: len(t.vmConns[id]),
	}
	switch {
	case len(sw.ExternalAdapters) > 0:
		sw.Type = "External"
	case sw.ManagementOSAccess:
		sw.Type = "Internal"
	default:
		sw.Type = "Private"
	}
	for _, s := range d.Settings.rows["switch_settings"] {
		if NormalizeID(s.String("VirtualSystemIdentifier")) != id {
			continue
		}
		sw.SRIOV = s.Bool("IOVPreferred")
		sw.EmbeddedTeaming = s.Bool("TeamingEnabled") || s.Bool("EmbeddedTeamingEnabled")
		sw.PacketDirect = s.Bool("PacketDirectEnabled")
		if s.Has("BandwidthReservationMode") {
			sw.BandwidthMode = lookup(bandwidthModes, s.Int("BandwidthReservationMode"))
		}
	}
	for _, e := range d.Settings.rows["switch_extensions"] {
		if NormalizeID(e.String("SystemName")) != id {
			continue
		}
		sw.Extensions = append(sw.Extensions, SwitchExtension{
			Name: e.String("ElementName"), Type: lookup(extensionTypes, e.Int("ExtensionType")),
			Enabled: e.Int("EnabledState") == 2, Running: e.Int("EnabledState") == 2 && len(e.Ints("OperationalStatus")) > 0 && e.Ints("OperationalStatus")[0] == 2,
			Version: e.String("Version"), Vendor: e.String("Vendor"),
		})
	}
	if len(sw.ExternalAdapters) > 1 && !sw.EmbeddedTeaming {
		sw.EmbeddedTeaming = true
	}
	return sw
}

// Switches builds the /switches response.
func (d *Data) Switches() (*SwitchList, error) {
	if err := d.ready(true); err != nil {
		return nil, err
	}
	out := &SwitchList{CollectedAt: d.Settings.at.UTC(), Switches: []Switch{}}
	t := d.topology()
	for _, r := range d.Settings.rows["switches"] {
		out.Switches = append(out.Switches, d.buildSwitch(r, t))
	}
	sort.Slice(out.Switches, func(i, j int) bool {
		return strings.ToLower(out.Switches[i].Name) < strings.ToLower(out.Switches[j].Name)
	})
	out.Count = len(out.Switches)
	out.Notes = d.Settings.errorNotes("switches", "switch_settings", "ethernet_connections")
	return out, nil
}

// ResolveSwitch finds a switch by GUID or name and returns its row.
func (d *Data) ResolveSwitch(key string) (wmi.Row, error) {
	if err := d.ready(true); err != nil {
		return nil, err
	}
	key = strings.TrimSpace(key)
	var byName []wmi.Row
	for _, r := range d.Settings.rows["switches"] {
		if IsGUID(key) && NormalizeID(r.String("Name")) == NormalizeID(key) {
			return r, nil
		}
		if strings.EqualFold(r.String("ElementName"), key) {
			byName = append(byName, r)
		}
	}
	if len(byName) == 1 {
		return byName[0], nil
	}
	if len(byName) > 1 {
		return nil, fmt.Errorf("%w: %d switches are named %q; use the ID", ErrNotFound, len(byName), key)
	}
	return nil, fmt.Errorf("%w: no switch %q", ErrNotFound, key)
}

// Switch builds the /switches/{id} response.
func (d *Data) Switch(key string) (*SwitchDetail, error) {
	r, err := d.ResolveSwitch(key)
	if err != nil {
		return nil, err
	}
	t := d.topology()
	out := &SwitchDetail{Switch: d.buildSwitch(r, t)}
	out.Ports = d.ports(out.ID, t)
	return out, nil
}

// SwitchPorts builds the /switches/{id}/ports response.
func (d *Data) SwitchPorts(key string) (*SwitchPorts, error) {
	r, err := d.ResolveSwitch(key)
	if err != nil {
		return nil, err
	}
	t := d.topology()
	id := NormalizeID(r.String("Name"))
	return &SwitchPorts{CollectedAt: d.Settings.at.UTC(), SwitchID: id, SwitchName: r.String("ElementName"), Ports: d.ports(id, t),
		Notes: d.Settings.errorNotes("ethernet_connections", "vlan_settings")}, nil
}

func (d *Data) ports(id string, t *switchTopology) []SwitchPort {
	ports := []SwitchPort{}
	for _, name := range t.external[id] {
		ports = append(ports, SwitchPort{Kind: "External", Name: name, Connected: true})
	}
	if t.internal[id] {
		ports = append(ports, SwitchPort{Kind: "Internal", Name: "Management OS", Connected: true})
	}
	// VM adapter rows by InstanceID, for name and MAC.
	adapters := map[string]wmi.Row{}
	for _, s := range d.Settings.vms {
		for _, p := range s.ports {
			adapters[strings.ToLower(p.String("InstanceID"))] = p
		}
	}
	for _, c := range t.vmConns[id] {
		vmID := VMIDFromInstanceID(c.String("InstanceID"))
		port := SwitchPort{Kind: "VM", VMID: vmID, Connected: c.Int("EnabledState") == 2}
		if st := d.State.vms[vmID]; st != nil {
			port.VMName = st.cs.String("ElementName")
		}
		if a := adapters[strings.ToLower(PathKey(c.String("Parent"), "InstanceID"))]; a != nil {
			port.Name = a.String("ElementName")
			port.MACAddress = formatMAC(a.String("Address"))
		}
		if s := d.Settings.vms[vmID]; s != nil {
			port.VLANMode, port.VLANID, port.TrunkVLANs = vlanFor(s.vlans, c.String("InstanceID"))
		}
		ports = append(ports, port)
	}
	sort.SliceStable(ports, func(i, j int) bool {
		if ports[i].Kind != ports[j].Kind {
			return ports[i].Kind < ports[j].Kind
		}
		return ports[i].VMName+ports[i].Name < ports[j].VMName+ports[j].Name
	})
	return ports
}

// vlanFor finds the VLAN feature setting attached to a connection.
func vlanFor(vlans []wmi.Row, connInstanceID string) (mode string, id int64, trunk []int64) {
	prefix := strings.ToLower(connInstanceID) + `\`
	for _, v := range vlans {
		if !strings.HasPrefix(strings.ToLower(v.String("InstanceID")), prefix) {
			continue
		}
		mode = lookup(vlanModes, v.Int("OperationMode"))
		if v.Int("OperationMode") == 2 {
			return mode, v.Int("NativeVlanId"), v.Ints("TrunkVlanIdArray")
		}
		return mode, v.Int("AccessVlanId"), nil
	}
	return "", 0, nil
}

// errorsIs avoids importing errors in every file for one call.
func errorsIs(err, target error) bool {
	for e := err; e != nil; {
		if e == target {
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

// schedulerType is filled by the events phase; declared here so the host
// view compiles before then.
var _ = time.Now
