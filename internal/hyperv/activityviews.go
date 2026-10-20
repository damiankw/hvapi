package hyperv

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/damiankw/hvapi/internal/evtlog"
	"github.com/damiankw/hvapi/internal/wmi"
)

var jobStates = map[int64]string{
	2: "New", 3: "Starting", 4: "Running", 5: "Suspended", 6: "Shutting Down", 7: "Completed",
	8: "Terminated", 9: "Killed", 10: "Exception", 11: "Service", 32768: "Completed with Warnings",
}

const stuckAfter = time.Hour

// Jobs builds the /jobs response.
func (d *Data) Jobs() (*JobList, error) {
	if err := d.ready(false); err != nil {
		return nil, err
	}
	out := &JobList{CollectedAt: d.State.at.UTC(), Jobs: []Job{}}
	seen := map[string]bool{}
	add := func(r wmi.Row, storage bool) {
		id := r.String("InstanceID")
		if seen[id] {
			return
		}
		seen[id] = true
		j := Job{
			ID: id, Name: r.String("ElementName"), Description: r.String("Description"), StateCode: r.Int("JobState"),
			PercentComplete: r.Int("PercentComplete"), Started: CIMTime(r.String("StartTime")), LastChange: CIMTime(r.String("TimeOfLastStateChange")),
			ElapsedSeconds: int64(CIMInterval(r.String("ElapsedTime")).Seconds()), Owner: r.String("Owner"), Storage: storage,
			ErrorCode: r.Int("ErrorCode"), ErrorText: r.String("ErrorDescription"),
		}
		if j.Name == "" {
			j.Name = r.String("Caption")
		}
		if j.ErrorText == "" {
			j.ErrorText = r.String("ErrorSummaryDescription")
		}
		if storage {
			j.ChildPath, j.ParentPath = r.String("Child"), r.String("Parent")
		}
		j.State = lookup(jobStates, j.StateCode)
		switch j.StateCode {
		case 8, 9, 10:
			j.Failed = true
		}
		if j.ErrorCode != 0 && j.StateCode != 4 && j.StateCode != 3 && j.StateCode != 2 {
			j.Failed = true
		}
		if j.StateCode == 4 || j.StateCode == 3 {
			out.Running++
			elapsed := time.Duration(j.ElapsedSeconds) * time.Second
			if elapsed == 0 && j.Started != nil {
				elapsed = d.State.at.Sub(*j.Started)
			}
			if elapsed > stuckAfter {
				j.Stuck = true
				out.Stuck++
			}
		}
		if j.Failed {
			out.Failed++
		}
		out.Jobs = append(out.Jobs, j)
	}
	for _, r := range d.State.rows["storage_jobs"] {
		add(r, true)
	}
	for _, r := range d.State.rows["jobs"] {
		add(r, false)
	}
	sort.SliceStable(out.Jobs, func(i, j int) bool {
		a, b := out.Jobs[i], out.Jobs[j]
		ar, br := a.StateCode == 4 || a.StateCode == 3, b.StateCode == 4 || b.StateCode == 3
		if ar != br {
			return ar
		}
		if a.Started != nil && b.Started != nil && !a.Started.Equal(*b.Started) {
			return a.Started.After(*b.Started)
		}
		return a.ID < b.ID
	})
	out.Notes = d.State.errorNotes("jobs", "storage_jobs")
	return out, nil
}

var levelRank = map[string]int{"critical": 1, "error": 2, "warning": 3, "info": 4, "information": 4, "verbose": 5}

// Events builds the /events response from the cache.
func (d *Data) Events(f EventFilter) (*EventList, error) {
	if d.Settings == nil {
		return nil, ErrNotReady
	}
	out := &EventList{CollectedAt: d.Settings.at.UTC(), Hours: d.Settings.eventHours, Logs: []LogStatus{}, Events: []evtlog.Event{}}
	if f.Log != "" {
		known := false
		for _, l := range EventLogs {
			if strings.EqualFold(l.name, f.Log) {
				known = true
				f.Log = l.name
			}
		}
		if !known {
			var names []string
			for _, l := range EventLogs {
				names = append(names, l.name)
			}
			return nil, fmt.Errorf("%w: no event log %q; logs are %s", ErrNotFound, f.Log, strings.Join(names, ", "))
		}
	}
	minRank := 5
	if f.Level != "" {
		r, ok := levelRank[strings.ToLower(f.Level)]
		if !ok {
			return nil, fmt.Errorf("%w: level must be critical, error, warning, info or verbose", ErrBadRequest)
		}
		minRank = r
	}
	var cutoff time.Time
	if f.Hours > 0 {
		cutoff = d.Settings.at.Add(-time.Duration(f.Hours) * time.Hour)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 200
	}
	for _, l := range EventLogs {
		st := LogStatus{Name: l.name, Channel: l.channel}
		if err := d.Settings.eventErrs[l.name]; err != nil {
			switch {
			case errors.Is(err, evtlog.ErrNoChannel):
				st.Error = "log does not exist on this host"
			case errors.Is(err, evtlog.ErrUnavailable):
				st.Error = err.Error()
			default:
				st.Error = "read failed: " + err.Error()
			}
		}
		for _, e := range d.Settings.events[l.name] {
			st.Count++
			switch e.LevelCode {
			case 1:
				st.Critical++
			case 2:
				st.Errors++
			case 3:
				st.Warnings++
			}
			if f.Log != "" && l.name != f.Log {
				continue
			}
			if rank := levelRank[strings.ToLower(e.Level)]; rank == 0 || rank > minRank {
				continue
			}
			if !cutoff.IsZero() && e.Time.Before(cutoff) {
				continue
			}
			if f.ID != 0 && e.ID != f.ID {
				continue
			}
			if f.VMID != "" && !strings.EqualFold(e.VMID, f.VMID) {
				continue
			}
			out.Events = append(out.Events, e)
		}
		out.Logs = append(out.Logs, st)
	}
	sort.SliceStable(out.Events, func(i, j int) bool { return out.Events[i].Time.After(out.Events[j].Time) })
	if len(out.Events) > limit {
		out.Events = out.Events[:limit]
	}
	out.Count = len(out.Events)
	if d.Settings.eventHours == 0 {
		out.Notes = append(out.Notes, "event log collection is not enabled")
	}
	return out, nil
}

// ErrBadRequest is returned for an invalid filter value.
var ErrBadRequest = errors.New("bad request")

// VMPerf builds the /vms/{id}/perf response from the performance counters.
func (d *Data) VMPerf(id string) (*VMPerf, error) {
	if err := d.ready(false); err != nil {
		return nil, err
	}
	st, ok := d.State.vms[id]
	if !ok {
		return nil, ErrNotFound
	}
	name := st.cs.String("ElementName")
	out := &VMPerf{VMID: id, VMName: name, CollectedAt: d.State.at.UTC(), Processors: []VPPerf{}, Adapters: []NICPerf{}, Disks: []VDiskPerf{}}
	lname := strings.ToLower(name)

	// Virtual processors: "<VM name>:Hv VP <n>".
	var total, guest float64
	for _, r := range d.State.rows["vp_counters"] {
		n := r.String("Name")
		if !strings.HasPrefix(strings.ToLower(n), lname+":") {
			continue
		}
		idx, _ := strconv.ParseInt(strings.TrimSpace(n[strings.LastIndex(n, " ")+1:]), 10, 64)
		vp := VPPerf{Index: idx, TotalPercent: r.Float("PercentTotalRunTime"), GuestPercent: r.Float("PercentGuestRunTime"), HypervisorPercent: r.Float("PercentHypervisorRunTime"), RemotePercent: r.Float("PercentRemoteRunTime")}
		total += vp.TotalPercent
		guest += vp.GuestPercent
		out.Processors = append(out.Processors, vp)
	}
	sort.Slice(out.Processors, func(i, j int) bool { return out.Processors[i].Index < out.Processors[j].Index })
	if n := float64(len(out.Processors)); n > 0 {
		t, g := total/n, guest/n
		out.CPUTotalPercent, out.CPUGuestPercent = &t, &g
	}

	// Virtual network adapters: instance names start with the VM name, or
	// carry the adapter's GUID.
	var adapterGUIDs map[string]string
	if set := d.settingsFor(id); set != nil {
		adapterGUIDs = map[string]string{}
		for _, p := range set.ports {
			segs := instanceSegments(p.String("InstanceID"))
			adapterGUIDs[strings.ToLower(segs[len(segs)-1])] = p.String("ElementName")
		}
	}
	for _, r := range d.State.rows["vnic_counters"] {
		n := r.String("Name")
		ln := strings.ToLower(n)
		adapter := ""
		matched := strings.HasPrefix(ln, lname+"_")
		for g, aname := range adapterGUIDs {
			if strings.Contains(ln, g) {
				matched, adapter = true, aname
			}
		}
		if !matched {
			continue
		}
		out.Adapters = append(out.Adapters, NICPerf{
			Name: n, Adapter: adapter, BytesInPerSec: r.Int("BytesReceivedPersec"), BytesOutPerSec: r.Int("BytesSentPersec"),
			PacketsInPerSec: r.Int("PacketsReceivedPersec"), PacketsOutPerSec: r.Int("PacketsSentPersec"),
			DroppedIn: r.Int("DroppedPacketsIncomingPersec"), DroppedOut: r.Int("DroppedPacketsOutgoingPersec"),
		})
	}

	// Virtual disks: the instance name is the path with "\" turned into "-".
	paths := map[string]string{}
	if disks, err := d.Disks(id); err == nil {
		for _, c := range disks.Controllers {
			for _, dr := range c.Drives {
				if dr.Path != "" {
					paths[strings.ToLower(strings.ReplaceAll(dr.Path, `\`, "-"))] = dr.Path
				}
			}
		}
	}
	for _, r := range d.State.rows["vdisk_counters"] {
		n := r.String("Name")
		p, ok := paths[strings.ToLower(n)]
		if !ok {
			continue
		}
		dp := VDiskPerf{
			Name: n, Path: p, ReadBytesPerSec: r.Int("ReadBytesPersec"), WriteBytesPerSec: r.Int("WriteBytesPersec"),
			ReadOpsPerSec: r.Int("ReadOperationsPerSec"), WriteOpsPerSec: r.Int("WriteOperationsPerSec"), QueueLength: r.Int("QueueLength"), ErrorCount: r.Int("ErrorCount"),
		}
		if r.Has("Latency") {
			v := r.Int("Latency") / 1000 // the counter is in microseconds
			dp.LatencyMs = &v
		}
		out.Disks = append(out.Disks, dp)
	}
	out.Notes = d.State.errorNotes("vp_counters", "vnic_counters", "vdisk_counters")
	if st.cs.Int("EnabledState") != 2 {
		out.Notes = append(out.Notes, "the VM is not running; counters exist only while it runs")
	}
	return out, nil
}

// hostPerfExtras fills the interface, disk and switch counters of HostPerf.
func (d *Data) hostPerfExtras(out *HostPerf) {
	for _, r := range d.State.rows["host_nic_counters"] {
		out.Interfaces = append(out.Interfaces, HostNICPerf{
			Name: r.String("Name"), BytesInPerSec: r.Int("BytesReceivedPersec"), BytesOutPerSec: r.Int("BytesSentPersec"), PacketsPerSec: r.Int("PacketsPersec"),
			ErrorsIn: r.Int("PacketsReceivedErrors"), ErrorsOut: r.Int("PacketsOutboundErrors"), DiscardedIn: r.Int("PacketsReceivedDiscarded"), DiscardedOut: r.Int("PacketsOutboundDiscarded"),
			OutputQueueLen: r.Int("OutputQueueLength"),
		})
	}
	for _, r := range d.State.rows["host_disk_counters"] {
		if r.String("Name") == "_Total" {
			continue
		}
		out.Disks = append(out.Disks, HostDiskPerf{
			Name: r.String("Name"), IdlePercent: r.Float("PercentIdleTime"), QueueLength: r.Float("AvgDiskQueueLength"),
			ReadBytesPerSec: r.Int("DiskReadBytesPersec"), WriteBytesPerSec: r.Int("DiskWriteBytesPersec"), ReadsPerSec: r.Int("DiskReadsPersec"), WritesPerSec: r.Int("DiskWritesPersec"),
			ReadLatencyMs: r.Float("AvgDisksecPerRead") * 1000, WriteLatencyMs: r.Float("AvgDisksecPerWrite") * 1000, FreePercent: r.Float("PercentFreeSpace"),
		})
	}
	for _, r := range d.State.rows["vswitch_counters"] {
		out.Switches = append(out.Switches, SwitchPerf{
			Name: r.String("Name"), BytesPerSec: r.Int("BytesPersec"), PacketsPerSec: r.Int("PacketsPersec"),
			DroppedIn: r.Int("DroppedPacketsIncomingPersec"), DroppedOut: r.Int("DroppedPacketsOutgoingPersec"), BroadcastPerSec: r.Int("BroadcastPacketsPersec"),
		})
	}
	for _, s := range []*[]string{} {
		_ = s
	}
	out.Notes = append(out.Notes, d.State.errorNotes("host_nic_counters", "host_disk_counters", "vswitch_counters")...)
}
