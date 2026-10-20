package hyperv

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/damiankw/hvapi/internal/wmi"
)

// fileSize reads a file's size on the host. Tests replace it.
var fileSize = func(path string) (int64, bool) {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return 0, false
	}
	return st.Size(), true
}

// Integration builds the /vms/{id}/integration response.
func (d *Data) Integration(id string) (*VMIntegration, error) {
	name, at, set, err := d.header(id)
	if err != nil {
		return nil, err
	}
	st := d.State.vms[id]
	out := &VMIntegration{VMID: id, VMName: name, CollectedAt: at, Services: []IntegrationService{}}
	if set != nil && set.kvp != nil {
		out.GuestVersion = set.kvp["IntegrationServicesVersion"]
	}
	for _, c := range componentSources {
		svc := IntegrationService{Name: c.label, Status: "Unknown"}
		var runtime, settings wmi.Row
		switch c.name {
		case "heartbeat":
			runtime = st.heartbeat
		case "kvp":
			if set != nil {
				runtime = set.kvpRow
			}
		default:
			runtime = st.components[c.name]
		}
		if set != nil {
			settings = set.compSet[c.name]
		}
		svc.Present = runtime != nil || settings != nil
		switch {
		case settings != nil:
			svc.Enabled = settings.Int("EnabledState") == 2
		case runtime != nil:
			svc.Enabled = runtime.Int("EnabledState") == 2
		}
		if runtime != nil {
			if codes := runtime.Ints("OperationalStatus"); len(codes) > 0 {
				svc.StatusCode = codes[0]
				svc.Status = lookup(heartbeatStatuses, codes[0])
			}
			svc.Details = runtime.Strings("StatusDescriptions")
		}
		if !svc.Enabled && svc.Status == "Unknown" {
			svc.Status = "Disabled"
		}
		out.Services = append(out.Services, svc)
	}
	var names []string
	for _, c := range componentSources {
		names = append(names, c.name, c.settings)
	}
	out.Notes = append(d.State.errorNotes(names...), d.settingsNotes(names...)...)
	return out, nil
}

// KVP builds the /vms/{id}/kvp response.
func (d *Data) KVP(id string) (*VMKVP, error) {
	name, at, set, err := d.header(id)
	if err != nil {
		return nil, err
	}
	out := &VMKVP{VMID: id, VMName: name, CollectedAt: at, Intrinsic: map[string]string{}, Guest: map[string]string{}, Host: map[string]string{}}
	if set != nil {
		if set.kvp != nil {
			out.Intrinsic = set.kvp
		}
		if set.kvpRow != nil {
			out.Guest = ParseKVP(set.kvpRow.Strings("GuestExchangeItems"))
		}
		if set.kvpHost != nil {
			out.Host = ParseKVP(set.kvpHost.Strings("HostExchangeItems"))
		}
	}
	out.Notes = d.settingsNotes("kvp", "kvp_settings")
	if len(out.Intrinsic) == 0 {
		out.Notes = append(out.Notes, "no guest data: the VM is off, has no integration services, or data exchange is disabled")
	}
	return out, nil
}

// Checkpoints builds the /vms/{id}/checkpoints response.
func (d *Data) Checkpoints(id string) (*VMCheckpoints, error) {
	name, at, set, err := d.header(id)
	if err != nil {
		return nil, err
	}
	out := &VMCheckpoints{VMID: id, VMName: name, CollectedAt: at, Checkpoints: []Checkpoint{}}
	out.Notes = d.settingsNotes("system_settings", "storage_allocation")
	if set == nil {
		return out, nil
	}
	byInstance := map[string]*Checkpoint{} // snapshot InstanceID (lower) -> checkpoint
	for _, sn := range set.snapList {
		r := sn.vssd
		c := Checkpoint{ID: NormalizeID(r.String("ConfigurationID")), Name: r.String("ElementName"), Created: CIMTime(r.String("CreationTime")), Notes: r.Strings("Notes"), Disks: []CheckpointDisk{}}
		switch {
		case r.Bool("IsAutomaticSnapshot"):
			c.Type = "Automatic"
		case strings.HasSuffix(r.String("VirtualSystemType"), ":Recovery"):
			c.Type = "Recovery"
		case strings.HasSuffix(r.String("VirtualSystemType"), ":Planned"):
			c.Type = "Planned"
		default:
			c.Type = "Checkpoint"
		}
		if c.Created != nil {
			c.AgeDays = at.Sub(*c.Created).Hours() / 24
		}
		if p := r.String("Parent"); p != "" {
			c.ParentID = strings.ToLower(PathKey(p, "InstanceID")) // resolved to a ConfigurationID below
		}
		for _, s := range sn.sasd {
			if s.String("ResourceSubType") != subVirtualHardDisk {
				continue
			}
			for _, p := range s.Strings("HostResource") {
				cd := CheckpointDisk{Path: p}
				if size, ok := fileSize(p); ok {
					mb := MB(size)
					cd.SizeMB = &mb
					c.DiskSizeMB += mb
				} else {
					cd.Missing = true
				}
				c.Disks = append(c.Disks, cd)
			}
		}
		byInstance[strings.ToLower(r.String("InstanceID"))] = &c
		out.Checkpoints = append(out.Checkpoints, c)
	}
	// Resolve parents to checkpoint IDs and mark the current one.
	current := strings.ToLower(PathKey(set.vssd.String("Parent"), "InstanceID"))
	idByInstance := map[string]string{}
	for inst, c := range byInstance {
		idByInstance[inst] = c.ID
	}
	for i := range out.Checkpoints {
		c := &out.Checkpoints[i]
		if pid, ok := idByInstance[c.ParentID]; ok {
			c.ParentID = pid
		} else {
			c.ParentID = ""
		}
	}
	for i := range out.Checkpoints {
		c := &out.Checkpoints[i]
		for j := range out.Checkpoints {
			if out.Checkpoints[j].ParentID == c.ID {
				c.Children = append(c.Children, out.Checkpoints[j].ID)
			}
		}
		sort.Strings(c.Children)
	}
	for inst, c := range byInstance {
		if inst == current && current != "" {
			for i := range out.Checkpoints {
				if out.Checkpoints[i].ID == c.ID {
					out.Checkpoints[i].Current = true
				}
			}
		}
	}
	sort.SliceStable(out.Checkpoints, func(i, j int) bool {
		a, b := out.Checkpoints[i].Created, out.Checkpoints[j].Created
		if a == nil || b == nil {
			return a != nil
		}
		return a.Before(*b)
	})
	out.Count = len(out.Checkpoints)
	for _, c := range out.Checkpoints {
		out.TotalSizeMB += c.DiskSizeMB
		if c.AgeDays > out.OldestDays {
			out.OldestDays = c.AgeDays
		}
	}
	out.Notes = append(out.Notes, set.linkNotes...)
	return out, nil
}

var replicationTypes = map[int64]string{0: "None", 1: "Regular", 2: "Resynchronize", 3: "Initial"}
var replAuth = map[int64]string{1: "Kerberos", 2: "Certificate"}

// Replication builds the /vms/{id}/replication response.
func (d *Data) Replication(id string) (*VMReplication, error) {
	name, at, set, err := d.header(id)
	if err != nil {
		return nil, err
	}
	st := d.State.vms[id]
	cs := st.cs
	out := &VMReplication{VMID: id, VMName: name, CollectedAt: at,
		State: lookup(replicationStates, cs.Int("ReplicationState")), Health: lookup(replicationHealths, cs.Int("ReplicationHealth")),
		Mode: lookup(replicationModes, cs.Int("ReplicationMode"))}
	out.Enabled = cs.Int("ReplicationState") != 0
	if r := st.replication; r != nil {
		out.LastReplication = CIMTime(r.String("LastReplicationTime"))
		out.LastAppConsistent = CIMTime(r.String("LastApplicationConsistentReplicationTime"))
		if r.Has("LastReplicationType") {
			out.LastReplicationType = lookup(replicationTypes, r.Int("LastReplicationType"))
		}
		if out.LastReplication != nil {
			m := int64(d.State.at.Sub(*out.LastReplication).Minutes())
			out.MinutesSinceLast = &m
		}
	}
	if set != nil && set.replSet != nil {
		r := set.replSet
		out.ReplicaServer = r.String("RecoveryConnectionPoint")
		out.ReplicaPort = r.Int("RecoveryServerPortNumber")
		out.PrimaryServer = r.String("PrimaryConnectionPoint")
		out.IntervalSeconds = r.Int("ReplicationInterval")
		out.RecoveryPoints = r.Int("RecoveryHistory")
		out.AppConsistentHours = r.Int("ApplicationConsistentSnapshotInterval")
		if r.Has("AuthenticationType") {
			out.Authentication = lookup(replAuth, r.Int("AuthenticationType"))
		}
		out.Compression = r.Bool("CompressionEnabled")
		out.AutoResync = r.Bool("AutoResynchronizeEnabled")
		for _, p := range r.Strings("ReplicatedDisks") {
			if hr := PathKey(p, "InstanceID"); hr != "" {
				out.ReplicatedDisks = append(out.ReplicatedDisks, hr)
			} else {
				out.ReplicatedDisks = append(out.ReplicatedDisks, p)
			}
		}
	}
	out.Notes = append(d.State.errorNotes("replication_relationships"), d.settingsNotes("replication_settings")...)
	if !out.Enabled {
		out.Notes = append(out.Notes, "replication is not enabled for this VM")
	}
	return out, nil
}

// Devices builds the /vms/{id}/devices response.
func (d *Data) Devices(id string) (*VMDevices, error) {
	name, at, set, err := d.header(id)
	if err != nil {
		return nil, err
	}
	out := &VMDevices{VMID: id, VMName: name, CollectedAt: at, GPUPartitions: []GPUPartition{}, PCIDevices: []PCIDevice{}, SerialPorts: []SerialPort{}, FibreChannel: []FibreChannelPort{}, DVDDrives: []Drive{}}
	out.Notes = d.settingsNotes("gpu_partitions", "pci_express", "serial_ports", "display_3d", "fibre_channel_ports")
	if set == nil {
		return out, nil
	}
	for _, r := range set.devices["gpu_partitions"] {
		g := GPUPartition{Name: r.String("ElementName"), MinVRAMMB: MB(r.Int("MinPartitionVRAM")), MaxVRAMMB: MB(r.Int("MaxPartitionVRAM")), OptimalVRAMMB: MB(r.Int("OptimalPartitionVRAM"))}
		if hr := r.Strings("HostResource"); len(hr) > 0 {
			g.HostResource = hr[0]
		}
		out.GPUPartitions = append(out.GPUPartitions, g)
	}
	for _, r := range set.devices["pci_express"] {
		out.PCIDevices = append(out.PCIDevices, PCIDevice{Name: r.String("ElementName"), HostResource: r.Strings("HostResource"), VirtualFunctions: r.Int("VirtualFunctions")})
	}
	for _, r := range set.devices["serial_ports"] {
		p := SerialPort{Name: r.String("ElementName"), Debugger: r.Bool("DebuggerMode")}
		if c := r.Strings("Connection"); len(c) > 0 {
			p.Connection = c[0]
		}
		out.SerialPorts = append(out.SerialPorts, p)
	}
	sort.Slice(out.SerialPorts, func(i, j int) bool { return out.SerialPorts[i].Name < out.SerialPorts[j].Name })
	if r := first(set.devices["display_3d"]); r != nil {
		out.Display = &Display{MaxMonitors: r.Int("MaximumMonitors"), MaxResolution: r.String("MaximumScreenResolution"), VRAMMB: MB(r.Int("VRAMSizeBytes"))}
	}
	for _, r := range set.devices["fibre_channel_ports"] {
		fc := FibreChannelPort{Name: r.String("ElementName"), WWNN: r.String("VirtualPortWWNN"), WWPNPrimary: r.String("VirtualPortWWPN"), WWPNSecondary: r.String("SecondaryWWPN")}
		if hr := r.Strings("Connection"); len(hr) > 0 {
			fc.SAN = hr[0]
		}
		out.FibreChannel = append(out.FibreChannel, fc)
	}
	if disks, err := d.Disks(id); err == nil {
		for _, c := range disks.Controllers {
			for _, dr := range c.Drives {
				if dr.Type == "DVD Drive" {
					out.DVDDrives = append(out.DVDDrives, dr)
				}
			}
		}
	}
	return out, nil
}

var secureBootTemplates = map[string]string{
	"1734C6E8-3154-4DDA-BA5F-A874CC483422": "Microsoft Windows",
	"272E7447-90A4-4563-A4B9-8E4AB00526CE": "Microsoft UEFI Certificate Authority",
	"4292AE9C-5A68-4D44-A2E2-C4A5E3C4F8A1": "Open Source Shielded VM",
}

// Security builds the /vms/{id}/security response.
func (d *Data) Security(id string) (*VMSecurity, error) {
	name, at, set, err := d.header(id)
	if err != nil {
		return nil, err
	}
	out := &VMSecurity{VMID: id, VMName: name, CollectedAt: at}
	out.Notes = d.settingsNotes("security_settings", "system_settings")
	if set == nil {
		return out, nil
	}
	out.Generation = generation(set.vssd)
	if out.Generation == 2 {
		out.SecureBoot = boolPtr(set.vssd, "SecureBootEnabled")
		if t := strings.Trim(strings.ToUpper(set.vssd.String("SecureBootTemplateId")), "{}"); t != "" {
			if n, ok := secureBootTemplates[t]; ok {
				out.SecureBootTemplate = n
			} else {
				out.SecureBootTemplate = t
			}
		}
	}
	if r := set.security; r != nil {
		out.TPM = boolPtr(r, "TpmEnabled")
		out.Shielded = boolPtr(r, "Shielded")
		out.EncryptStateAndTraffic = boolPtr(r, "EncryptStateAndVmMigrationTraffic")
		out.DataProtection = boolPtr(r, "DataProtectionRequested")
		out.VBSOptOut = boolPtr(r, "VirtualizationBasedSecurityOptOut")
		out.BindToHostTPM = boolPtr(r, "BindToHostTpm")
	} else {
		out.Notes = append(out.Notes, "no Msvm_SecuritySettingData for this VM (older host or configuration version)")
	}
	return out, nil
}

// Metering builds the /vms/{id}/metering response.
func (d *Data) Metering(id string) (*VMMetering, error) {
	name, at, set, err := d.header(id)
	if err != nil {
		return nil, err
	}
	out := &VMMetering{VMID: id, VMName: name, CollectedAt: at, Metrics: []Metric{}}
	out.Notes = d.settingsNotes("metric_values", "metric_definitions", "metric_links")
	if set == nil || len(set.metrics) == 0 {
		out.Notes = append(out.Notes, "no metering data: resource metering is not enabled for this VM")
		return out, nil
	}
	defs := map[string]wmi.Row{}
	for _, r := range d.Settings.rows["metric_definitions"] {
		defs[strings.ToLower(r.String("Id"))] = r
	}
	out.Enabled = true
	for _, v := range set.metrics {
		m := Metric{Value: v.Float("MetricValue"), Since: CIMTime(v.String("TimeStamp")), DurationSecs: int64(CIMInterval(v.String("Duration")).Seconds())}
		if def := defs[strings.ToLower(v.String("MetricDefinitionId"))]; def != nil {
			m.Name, m.Units = def.String("ElementName"), def.String("Units")
		} else {
			m.Name = v.String("MetricDefinitionId")
		}
		if el := v.String("MeasuredElementName"); el != "" && !strings.EqualFold(el, name) {
			m.Element = el
		}
		out.Metrics = append(out.Metrics, m)
	}
	sort.SliceStable(out.Metrics, func(i, j int) bool {
		return out.Metrics[i].Name+out.Metrics[i].Element < out.Metrics[j].Name+out.Metrics[j].Element
	})
	return out, nil
}

// servicePath returns the object path of a singleton service class row.
func (d *Data) servicePath(source string) (string, error) {
	if d.Settings == nil {
		return "", ErrNotReady
	}
	r := first(d.Settings.rows[source])
	if r == nil || r.String(wmi.PathKey) == "" {
		if err := d.Settings.errs[source]; err != nil {
			return "", fmt.Errorf("%s: %w", source, err)
		}
		return "", fmt.Errorf("%s not found on this host", source)
	}
	return r.String(wmi.PathKey), nil
}

var _ = time.Now
