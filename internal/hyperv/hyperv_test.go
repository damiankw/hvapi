package hyperv

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/damiankw/hvapi/internal/wmi"
)

func TestParsers(t *testing.T) {
	if got := CIMTime("20240102030405.123456+600"); got == nil || !got.Equal(time.Date(2024, 1, 1, 17, 4, 5, 123456000, time.UTC)) {
		t.Errorf("CIMTime = %v", got)
	}
	if got := CIMTime("20240102030405.000000-060"); got == nil || got.Hour() != 4 {
		t.Errorf("CIMTime negative offset = %v", got)
	}
	if CIMTime("") != nil || CIMTime("00000000000000.000000+000") != nil || CIMTime("garbage") != nil {
		t.Error("CIMTime should reject empty/zero/garbage")
	}
	if CIMInterval("00000000000130.500000:000") != 90*time.Second+500*time.Millisecond {
		t.Error("CIMInterval")
	}
	if CIMInterval("00000001000000.000000:000") != 24*time.Hour {
		t.Error("CIMInterval days")
	}
	p := `\\HOST\root\virtualization\v2:Msvm_ResourceAllocationSettingData.InstanceID="Microsoft:ABC\\DEF\\0"`
	if PathKey(p, "InstanceID") != `Microsoft:ABC\DEF\0` || PathKey(p, "Nope") != "" {
		t.Errorf("PathKey = %q", PathKey(p, "InstanceID"))
	}
	if PathClass(p) != "Msvm_ResourceAllocationSettingData" {
		t.Errorf("PathClass = %q", PathClass(p))
	}
	if NormPath(p) != NormPath(`\\other\root\virtualization\v2:MSVM_ResourceAllocationSettingData.InstanceID="Microsoft:ABC\\DEF\\0"`) {
		t.Error("NormPath should ignore server and case")
	}
	if VMIDFromInstanceID(`Microsoft:GuestNetwork\`+vm1+`\x`) != strings.ToUpper(vm1) || VMIDFromInstanceID(`Microsoft:`+vm2+`\y\0`) != strings.ToUpper(vm2) || VMIDFromInstanceID("x") != "" {
		t.Error("VMIDFromInstanceID")
	}
	if formatMAC("00155d0a0b0c") != "00:15:5D:0A:0B:0C" || formatMAC("") != "" {
		t.Error("formatMAC")
	}
}

func TestKVP(t *testing.T) {
	m := ParseKVP([]string{
		`<INSTANCE CLASSNAME="Msvm_KvpExchangeDataItem"><PROPERTY NAME="Data" TYPE="string"><VALUE>v</VALUE></PROPERTY><PROPERTY NAME="Name" TYPE="string"><VALUE>k</VALUE></PROPERTY></INSTANCE>`,
		`not xml`,
	})
	if m["k"] != "v" || len(m) != 1 {
		t.Errorf("ParseKVP = %v", m)
	}
}

func poll(t *testing.T, q wmi.Querier) *Data {
	t.Helper()
	p := NewPoller(q, nil, 10*time.Second, 60*time.Second)
	p.PollOnce(context.Background())
	return p.Data()
}

func TestVMsAndSummary(t *testing.T) {
	d := poll(t, fixtures())
	list, err := d.VMs()
	if err != nil {
		t.Fatal(err)
	}
	if list.Count != 2 || list.VMs[0].Name != "Archive" || list.VMs[1].Name != "Web Server" {
		t.Fatalf("list = %+v", list)
	}
	web := list.VMs[1]
	if web.ID != strings.ToUpper(vm1) || web.State != "Running" || web.Health != "OK" || web.UptimeSeconds != 3600 ||
		web.Generation != 2 || web.Version != "10.0" || web.ProcessID != 4242 || web.OperationalStatus[0] != "OK" ||
		web.LastStateChange == nil || web.Created == nil || web.Notes[0] != "Production web tier" {
		t.Errorf("web = %+v", web)
	}
	arc := list.VMs[0]
	if arc.State != "Off" || arc.UptimeSeconds != 0 || arc.Generation != 1 || arc.ReplicationState != "Replicating" || arc.ReplicationHealth != "Warning" || arc.ReplicationMode != "Primary" {
		t.Errorf("archive = %+v", arc)
	}
	if len(list.Notes) != 0 {
		t.Errorf("unexpected notes %v", list.Notes)
	}

	sums, err := d.Summaries()
	if err != nil {
		t.Fatal(err)
	}
	s := sums.VMs[1]
	if s.ProcessorCount != 2 || s.ProcessorLoadPercent == nil || *s.ProcessorLoadPercent != 20 ||
		s.MemoryAssignedMB != 4096 || s.MemoryStartupMB != 2048 || !s.DynamicMemory || s.MemoryMinimumMB != 512 || s.MemoryMaximumMB != 8192 ||
		s.MemoryDemandMB == nil || *s.MemoryDemandMB != 3276 || s.SnapshotCount != 1 ||
		!s.Guest.Available || s.Guest.Heartbeat != "OK" || s.Guest.OSName != "Windows Server 2022 Datacenter" ||
		s.Guest.Hostname != "vm1.example.local" || len(s.Guest.IPv4) != 1 || s.Guest.IPv4[0] != "10.0.0.50" {
		b, _ := json.MarshalIndent(s, "", " ")
		t.Errorf("summary = %s", b)
	}
	a := sums.VMs[0]
	if a.ProcessorCount != 1 || a.ProcessorLoadPercent != nil || a.Guest.Heartbeat != "No Contact" || a.Guest.Available || a.DynamicMemory || a.MemoryDemandMB != nil {
		t.Errorf("archive summary = %+v", a)
	}
	if sums.SettingsAt == nil {
		t.Error("settings_at missing")
	}
}

func TestResolve(t *testing.T) {
	d := poll(t, fixtures())
	for _, key := range []string{vm1, strings.ToUpper(vm1), "Web Server", "web server"} {
		if id, err := d.Resolve(key); err != nil || id != strings.ToUpper(vm1) {
			t.Errorf("Resolve(%q) = %q, %v", key, id, err)
		}
	}
	if _, err := d.Resolve("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected not found, got %v", err)
	}
	if _, err := d.Resolve("55555555-5555-5555-5555-555555555555"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected not found, got %v", err)
	}
}

func TestConfigProcessorMemory(t *testing.T) {
	d := poll(t, fixtures())
	id := strings.ToUpper(vm1)
	c, err := d.Config(id)
	if err != nil {
		t.Fatal(err)
	}
	if c.Generation != 2 || c.ConfigurationFile != `C:\VMs\vm1\vm1.vmcx` || c.AutomaticStartAction != "Restart if previously running" ||
		c.AutomaticStartDelaySec != 30 || c.AutomaticStopAction != "Shut down" || c.AutomaticRecoveryAct != "Restart" ||
		c.CheckpointType != "Production" || c.NetworkBootProtocol != "IPv4" || !c.VirtualNUMA || c.Created == nil || c.Description != "Production web tier" ||
		c.AutomaticCheckpoints == nil || *c.AutomaticCheckpoints {
		b, _ := json.MarshalIndent(c, "", " ")
		t.Errorf("config = %s", b)
	}
	c2, _ := d.Config(strings.ToUpper(vm2))
	if len(c2.BootOrder) != 4 || c2.BootOrder[0] != "IDE" || c2.SecureBoot == nil || *c2.SecureBoot || !c2.NumLock {
		t.Errorf("config2 = %+v", c2)
	}

	p, err := d.Processor(id)
	if err != nil {
		t.Fatal(err)
	}
	if p.Count != 2 || p.LimitPercent != 100 || p.ReservePercent != 0 || p.RelativeWeight != 100 || !p.HostResourceProtection ||
		p.LoadPercent == nil || *p.LoadPercent != 20 || len(p.VirtualProcessors) != 2 || p.VirtualProcessors[1].Index != 1 || p.VirtualProcessors[1].LoadPercent != 30 {
		t.Errorf("processor = %+v", p)
	}
	p2, _ := d.Processor(strings.ToUpper(vm2))
	if p2.Count != 1 || p2.LoadPercent != nil || len(p2.VirtualProcessors) != 0 {
		t.Errorf("processor2 = %+v", p2)
	}

	m, err := d.Memory(id)
	if err != nil {
		t.Fatal(err)
	}
	if m.StartupMB != 2048 || !m.DynamicMemory || m.MinimumMB != 512 || m.MaximumMB != 8192 || m.BufferPercent != 20 || m.Weight != 5000 ||
		m.AssignedMB != 4096 || m.DemandMB == nil || *m.DemandMB != 3276 || m.PressurePercent == nil || *m.PressurePercent != 80 || m.AddedMB == nil || *m.AddedMB != 2048 || m.GuestAvailableMB != nil {
		t.Errorf("memory = %+v", m)
	}
}

func TestDisks(t *testing.T) {
	d := poll(t, fixtures())
	disks, err := d.Disks(strings.ToUpper(vm1))
	if err != nil {
		t.Fatal(err)
	}
	if len(disks.Controllers) != 1 {
		b, _ := json.MarshalIndent(disks, "", " ")
		t.Fatalf("disks = %s", b)
	}
	c := disks.Controllers[0]
	if c.Type != "SCSI" || c.Number != "0" || len(c.Drives) != 2 {
		t.Fatalf("controller = %+v", c)
	}
	if c.Drives[0].Type != "Hard Drive" || c.Drives[0].Media != "VHD" || c.Drives[0].Path != `C:\VMs\vm1\vm1.vhdx` || c.Drives[0].IOPSMax != 500 || c.Drives[0].DiskID != "scsi0-0" {
		t.Errorf("drive0 = %+v", c.Drives[0])
	}
	if c.Drives[1].Type != "DVD Drive" || c.Drives[1].Media != "ISO" || c.Drives[1].Path != `C:\ISO\install.iso` || c.Drives[1].DiskID != "scsi0-1" {
		t.Errorf("drive1 = %+v", c.Drives[1])
	}

	disks2, _ := d.Disks(strings.ToUpper(vm2))
	if len(disks2.Controllers) != 2 || disks2.Controllers[0].Type != "IDE" || disks2.Controllers[0].Number != "0" || disks2.Controllers[1].Number != "1" {
		b, _ := json.MarshalIndent(disks2, "", " ")
		t.Fatalf("disks2 = %s", b)
	}
	if dr := disks2.Controllers[0].Drives[0]; dr.Media != "VHD" || dr.Path != `D:\VMs\vm2.vhd` || dr.DiskID != "ide0-0" {
		t.Errorf("ide0 drive = %+v", dr)
	}
	if dr := disks2.Controllers[1].Drives[0]; dr.Type != "Physical Disk" || dr.Media != "Physical" || dr.Path != `Microsoft:AAAA\2` {
		t.Errorf("ide1 drive = %+v", dr)
	}
}

func TestNetwork(t *testing.T) {
	d := poll(t, fixtures())
	n, err := d.Network(strings.ToUpper(vm1))
	if err != nil {
		t.Fatal(err)
	}
	if len(n.Adapters) != 1 {
		t.Fatalf("adapters = %+v", n.Adapters)
	}
	a := n.Adapters[0]
	if a.Name != "Network Adapter" || a.Type != "Synthetic" || a.MACAddress != "00:15:5D:0A:0B:0C" || !a.Connected || a.Switch != "External" ||
		a.SwitchID != sw1 || a.VLANMode != "Access" || a.VLANID != 42 || !a.DeviceNaming || !a.ClusterMonitor ||
		len(a.GuestIPs) != 2 || a.GuestGateways[0] != "10.0.0.1" || a.GuestDHCP == nil || !*a.GuestDHCP {
		b, _ := json.MarshalIndent(a, "", " ")
		t.Errorf("adapter = %s", b)
	}
	n2, _ := d.Network(strings.ToUpper(vm2))
	if len(n2.Adapters) != 1 || n2.Adapters[0].Type != "Legacy" || n2.Adapters[0].Connected {
		t.Errorf("adapters2 = %+v", n2.Adapters)
	}
}

func TestDetail(t *testing.T) {
	d := poll(t, fixtures())
	det, err := d.Detail(strings.ToUpper(vm1))
	if err != nil {
		t.Fatal(err)
	}
	if det.Config == nil || det.Processor == nil || det.Memory == nil || det.Disks == nil || det.Network == nil || det.VM.Name != "Web Server" {
		t.Errorf("detail incomplete: %+v", det)
	}
	if _, err := d.Detail("nope"); !errors.Is(err, ErrNotFound) {
		t.Error("expected not found")
	}
}

// Without the association class the InstanceID fallback must still exclude
// snapshot settings.
func TestFallbackLinking(t *testing.T) {
	q := fixtures()
	delete(q.rows, "Msvm_VirtualSystemSettingDataComponent")
	d := poll(t, q)
	p, _ := d.Processor(strings.ToUpper(vm1))
	if p.Count != 2 {
		t.Errorf("fallback picked the snapshot processor settings: %+v", p)
	}
	disks, _ := d.Disks(strings.ToUpper(vm1))
	if len(disks.Controllers[0].Drives) != 2 {
		t.Errorf("fallback included the snapshot drive: %+v", disks.Controllers[0].Drives)
	}
	if len(p.Notes) == 0 || !strings.Contains(strings.Join(p.Notes, " "), "InstanceID") {
		t.Errorf("fallback should be noted: %v", p.Notes)
	}
}

func TestMissingSourcesBecomeNotes(t *testing.T) {
	q := fixtures()
	delete(q.rows, "Win32_PerfFormattedData_BalancerStats_HyperVDynamicMemoryVM")
	delete(q.rows, "Msvm_KvpExchangeComponent")
	q.fail["Msvm_HeartbeatComponent"] = errors.New("access denied")
	d := poll(t, q)
	sums, err := d.Summaries()
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(sums.Notes, "\n")
	for _, want := range []string{"dynamic_memory_counters: not available on this host", "kvp: not available", "heartbeat: query failed: access denied"} {
		if !strings.Contains(joined, want) {
			t.Errorf("notes lack %q:\n%s", want, joined)
		}
	}
	if s := sums.VMs[1]; s.MemoryDemandMB != nil || s.Guest.OSName != "" || s.Guest.Heartbeat != "Unknown" {
		t.Errorf("summary should degrade gracefully: %+v", s)
	}
}

func TestNotReadyAndUnavailable(t *testing.T) {
	d := &Data{}
	if _, err := d.VMs(); !errors.Is(err, ErrNotReady) {
		t.Error("expected not ready")
	}
	if _, err := d.Resolve("x"); !errors.Is(err, ErrNotReady) {
		t.Error("expected not ready")
	}

	// State polled, settings not yet: list works, config does not.
	q := fixtures()
	p := NewPoller(q, nil, time.Second, time.Second)
	p.pollState(context.Background())
	d = p.Data()
	if l, err := d.VMs(); err != nil || l.Count != 2 || l.VMs[0].Generation != 0 || len(l.Notes) == 0 {
		t.Errorf("list before settings: %+v %v", l, err)
	}
	if _, err := d.Config(strings.ToUpper(vm1)); !errors.Is(err, ErrNotReady) {
		t.Error("config should be not ready")
	}
	if det, err := d.Detail(strings.ToUpper(vm1)); err != nil || det.Config != nil || len(det.Notes) == 0 {
		t.Errorf("detail before settings: %+v %v", det, err)
	}

	// No WMI at all (the non-Windows stub): empty, with notes, and /status says so.
	p = NewPoller(wmi.New(), nil, time.Second, time.Second)
	p.PollOnce(context.Background())
	d = p.Data()
	l, err := d.VMs()
	if err != nil || l.Count != 0 || len(l.Notes) == 0 {
		t.Errorf("unavailable: %+v %v", l, err)
	}
	st := p.Status()
	if st.WMIAvailable || st.State.Runs != 1 || st.Settings.Runs != 1 || len(st.State.Sources) != len(stateSources) || st.State.LastRun == nil {
		t.Errorf("status = %+v", st)
	}
}

func TestPollerRunStops(t *testing.T) {
	p := NewPoller(fixtures(), nil, 10*time.Millisecond, 10*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	p.Run(ctx)
	if st := p.Status(); st.State.Runs < 2 || st.Settings.Runs < 2 || st.VMCount != 2 {
		t.Errorf("status after run = %+v", st)
	}
}
