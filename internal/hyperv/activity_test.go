package hyperv

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/damiankw/hvapi/internal/evtlog"
	"github.com/damiankw/hvapi/internal/wmi"
)

type fakeReader struct {
	events map[string][]evtlog.Event
	errs   map[string]error
}

func (f *fakeReader) Query(ctx context.Context, channel, xpath string, max int) ([]evtlog.Event, error) {
	if err := f.errs[channel]; err != nil {
		return nil, err
	}
	ev := f.events[channel]
	if len(ev) > max {
		ev = ev[:max]
	}
	return ev, nil
}

func activityFixtures() *fakeQuerier {
	q := deepFixtures()
	q.rows["Msvm_ConcreteJob"] = []wmi.Row{
		{"InstanceID": "J1", "ElementName": "Export Virtual Machine", "JobState": int32(4), "PercentComplete": int32(40), "StartTime": "20261020110000.000000+000", "ElapsedTime": "00000000020000.000000:000", "ErrorCode": int32(0)},
		{"InstanceID": "J2", "Caption": "Start Virtual Machine", "JobState": int32(10), "PercentComplete": int32(0), "StartTime": "20261020113000.000000+000", "ErrorCode": int32(32768), "ErrorDescription": "'Web Server' failed to start."},
		{"InstanceID": "J3", "ElementName": "Modify Resource Settings", "JobState": int32(7), "PercentComplete": int32(100), "StartTime": "20261020114000.000000+000", "ErrorCode": int32(0)},
		{"InstanceID": "S1", "ElementName": "Merge in progress", "JobState": int32(4), "PercentComplete": int32(10), "StartTime": "20261020115000.000000+000", "ElapsedTime": "00000000000500.000000:000"},
	}
	q.rows["Msvm_StorageJob"] = []wmi.Row{{"InstanceID": "S1", "ElementName": "Merge in progress", "JobState": int32(4), "PercentComplete": int32(10), "StartTime": "20261020115000.000000+000", "ElapsedTime": "00000000000500.000000:000", "Child": `C:\VMs\vm1\vm1.vhdx`, "Parent": `C:\VMs\vm1\base.vhdx`}}
	q.rows["Win32_PerfFormattedData_HvStats_HyperVHypervisorVirtualProcessor"] = []wmi.Row{
		{"Name": "Web Server:Hv VP 1", "PercentTotalRunTime": "40", "PercentGuestRunTime": "38", "PercentHypervisorRunTime": "2"},
		{"Name": "Web Server:Hv VP 0", "PercentTotalRunTime": "20", "PercentGuestRunTime": "18", "PercentHypervisorRunTime": "2"},
		{"Name": "Other:Hv VP 0", "PercentTotalRunTime": "99"},
	}
	q.rows["Win32_PerfFormattedData_NvspNicStats_HyperVVirtualNetworkAdapter"] = []wmi.Row{
		{"Name": "Web Server_Network Adapter_5A5A5A5A-0000-0000-0000-000000000001--" + sw1, "BytesReceivedPersec": "1000", "BytesSentPersec": "2000", "PacketsReceivedPersec": "10", "PacketsSentPersec": "20", "DroppedPacketsIncomingPersec": "0", "DroppedPacketsOutgoingPersec": "1"},
		{"Name": "Other_Network Adapter_X", "BytesReceivedPersec": "5"},
	}
	q.rows["Win32_PerfFormattedData_Counters_HyperVVirtualStorageDevice"] = []wmi.Row{
		{"Name": "C:-VMs-vm1-vm1.vhdx", "ReadBytesPersec": "4096", "WriteBytesPersec": "8192", "ReadOperationsPerSec": "4", "WriteOperationsPerSec": "8", "QueueLength": "1", "Latency": "2500", "ErrorCount": "0"},
		{"Name": "D:-other.vhdx", "ReadBytesPersec": "1"},
	}
	q.rows["Win32_PerfFormattedData_Tcpip_NetworkInterface"] = []wmi.Row{{"Name": "Broadcom NetXtreme", "BytesReceivedPersec": "100", "BytesSentPersec": "200", "PacketsPersec": "30", "PacketsReceivedErrors": "0", "OutputQueueLength": "0"}}
	q.rows["Win32_PerfFormattedData_PerfDisk_LogicalDisk"] = []wmi.Row{
		{"Name": "_Total", "PercentIdleTime": "90"},
		{"Name": "D:", "PercentIdleTime": "80", "AvgDiskQueueLength": "0.5", "DiskReadBytesPersec": "1024", "DiskWriteBytesPersec": "2048", "AvgDisksecPerRead": "0.002", "AvgDisksecPerWrite": "0.004", "PercentFreeSpace": "5"},
	}
	q.rows["Win32_PerfFormattedData_NvspSwitchStats_HyperVVirtualSwitch"] = []wmi.Row{{"Name": "External", "BytesPersec": "3000", "PacketsPersec": "30", "DroppedPacketsIncomingPersec": "0", "DroppedPacketsOutgoingPersec": "0", "BroadcastPacketsPersec": "2"}}
	return q
}

func sampleEvents(now time.Time) *fakeReader {
	return &fakeReader{
		events: map[string][]evtlog.Event{
			"Microsoft-Windows-Hyper-V-VMMS-Admin": {
				{ID: 16010, LevelCode: 2, Level: "Error", Provider: "Microsoft-Windows-Hyper-V-VMMS", Time: now.Add(-30 * time.Minute), Message: "'Web Server' failed to start.", VMID: strings.ToUpper(vm1)},
				{ID: 13002, LevelCode: 4, Level: "Information", Provider: "Microsoft-Windows-Hyper-V-VMMS", Time: now.Add(-20 * time.Hour), Message: "Archive was turned off."},
			},
			"Microsoft-Windows-Hyper-V-Worker-Admin": {
				{ID: 18500, LevelCode: 3, Level: "Warning", Provider: "Microsoft-Windows-Hyper-V-Worker", Time: now.Add(-5 * time.Minute), Message: "Heartbeat missed."},
			},
			schedulerChannel: {{ID: 2, LevelCode: 4, Level: "Information", Time: now, Message: "Hypervisor scheduler type is 0x3.", Data: []string{"0x3"}}},
		},
		errs: map[string]error{"Microsoft-Windows-Hyper-V-High-Availability-Admin": evtlog.ErrNoChannel},
	}
}

func TestJobs(t *testing.T) {
	d := poll(t, activityFixtures())
	j, err := d.Jobs()
	if err != nil {
		t.Fatal(err)
	}
	if len(j.Jobs) != 4 || j.Running != 2 || j.Failed != 1 || j.Stuck != 1 {
		b, _ := json.MarshalIndent(j, "", " ")
		t.Fatalf("jobs = %s", b)
	}
	if j.Jobs[0].ID != "S1" || !j.Jobs[0].Storage || j.Jobs[0].ChildPath == "" || j.Jobs[0].Stuck || j.Jobs[1].ID != "J1" || !j.Jobs[1].Stuck || j.Jobs[1].ElapsedSeconds != 7200 {
		t.Errorf("running = %+v %+v", j.Jobs[0], j.Jobs[1])
	}
	var failed *Job
	for i := range j.Jobs {
		if j.Jobs[i].ID == "J2" {
			failed = &j.Jobs[i]
		}
	}
	if failed == nil || !failed.Failed || failed.State != "Exception" || failed.Name != "Start Virtual Machine" || failed.ErrorText == "" {
		t.Errorf("failed = %+v", failed)
	}
}

func TestEvents(t *testing.T) {
	now := time.Now()
	p := NewPoller(activityFixtures(), nil, time.Second, time.Second)
	p.SetEvents(EventSource{Reader: sampleEvents(now), Hours: 24, MaxPerLog: 500})
	p.PollOnce(context.Background())
	d := p.Data()

	all, err := d.Events(EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if all.Count != 3 || all.Events[0].ID != 18500 || all.Events[2].ID != 13002 || len(all.Logs) != len(EventLogs) || all.Hours != 24 {
		b, _ := json.MarshalIndent(all, "", " ")
		t.Fatalf("events = %s", b)
	}
	byName := map[string]LogStatus{}
	for _, l := range all.Logs {
		byName[l.Name] = l
	}
	if byName["vmms"].Count != 2 || byName["vmms"].Errors != 1 || byName["worker"].Warnings != 1 || byName["high-availability"].Error == "" || byName["compute"].Count != 0 {
		t.Errorf("logs = %+v", all.Logs)
	}
	errs, _ := d.Events(EventFilter{Level: "error"})
	if errs.Count != 1 || errs.Events[0].ID != 16010 {
		t.Errorf("errors = %+v", errs.Events)
	}
	warn, _ := d.Events(EventFilter{Level: "warning", Hours: 1})
	if warn.Count != 2 {
		t.Errorf("warnings last hour = %d", warn.Count)
	}
	one, _ := d.Events(EventFilter{Log: "VMMS", Limit: 1})
	if one.Count != 1 || one.Events[0].Log != "vmms" {
		t.Errorf("vmms limit 1 = %+v", one.Events)
	}
	vm, _ := d.Events(EventFilter{VMID: strings.ToUpper(vm1)})
	if vm.Count != 1 {
		t.Errorf("vm filter = %d", vm.Count)
	}
	if _, err := d.Events(EventFilter{Log: "nope"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown log: %v", err)
	}
	if _, err := d.Events(EventFilter{Level: "loud"}); !errors.Is(err, ErrBadRequest) {
		t.Errorf("bad level: %v", err)
	}
	hv, _ := d.HostHypervisor()
	if hv.SchedulerType != "Core" {
		t.Errorf("scheduler = %q", hv.SchedulerType)
	}

	// Without a reader, events are empty with a note; no scheduler type.
	d2 := poll(t, activityFixtures())
	none, _ := d2.Events(EventFilter{})
	if none.Count != 0 || len(none.Notes) == 0 {
		t.Errorf("no reader = %+v", none)
	}
	if schedulerTypeFrom(evtlog.Event{Message: "Hypervisor scheduler type is 0x2."}) != "Classic" || schedulerTypeFrom(evtlog.Event{Data: []string{"4"}}) != "Root" || schedulerTypeFrom(evtlog.Event{}) != "" {
		t.Error("schedulerTypeFrom")
	}
}

func TestVMPerfAndHostPerfExtras(t *testing.T) {
	d := poll(t, activityFixtures())
	p, err := d.VMPerf(strings.ToUpper(vm1))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Processors) != 2 || p.Processors[0].Index != 0 || p.Processors[1].TotalPercent != 40 || p.CPUTotalPercent == nil || *p.CPUTotalPercent != 30 ||
		len(p.Adapters) != 1 || p.Adapters[0].Adapter != "Network Adapter" || p.Adapters[0].BytesOutPerSec != 2000 || p.Adapters[0].DroppedOut != 1 ||
		len(p.Disks) != 1 || p.Disks[0].Path != `C:\VMs\vm1\vm1.vhdx` || p.Disks[0].WriteBytesPerSec != 8192 || p.Disks[0].LatencyMs == nil || *p.Disks[0].LatencyMs != 2 {
		b, _ := json.MarshalIndent(p, "", " ")
		t.Errorf("vm perf = %s", b)
	}
	p2, _ := d.VMPerf(strings.ToUpper(vm2))
	if len(p2.Processors) != 0 || len(p2.Notes) == 0 {
		t.Errorf("off vm perf = %+v", p2)
	}
	if _, err := d.VMPerf("nope"); !errors.Is(err, ErrNotFound) {
		t.Error("expected not found")
	}

	h, _ := d.HostPerf()
	if len(h.Interfaces) != 1 || h.Interfaces[0].BytesOutPerSec != 200 || len(h.Disks) != 1 || h.Disks[0].Name != "D:" || h.Disks[0].ReadLatencyMs != 2 || h.Disks[0].FreePercent != 5 ||
		len(h.Switches) != 1 || h.Switches[0].BytesPerSec != 3000 {
		b, _ := json.MarshalIndent(h, "", " ")
		t.Errorf("host perf = %s", b)
	}
}
