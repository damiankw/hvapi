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

const snap2 = "77777777-7777-7777-7777-777777777777" // child of snap

func deepFixtures() *fakeQuerier {
	q := hostFixtures()
	vssdSnap := path("Msvm_VirtualSystemSettingData", "InstanceID", "Microsoft:"+vm1+`\`+snap)
	vssdSnap2 := path("Msvm_VirtualSystemSettingData", "InstanceID", "Microsoft:"+vm1+`\`+snap2)
	// Second checkpoint, child of the first; the VM runs from it.
	q.rows["Msvm_VirtualSystemSettingData"] = append(q.rows["Msvm_VirtualSystemSettingData"], wmi.Row{
		wmi.PathKey: vssdSnap2, "InstanceID": "Microsoft:" + vm1 + `\` + snap2, "VirtualSystemIdentifier": vm1, "ConfigurationID": snap2,
		"VirtualSystemType": "Microsoft:Hyper-V:Snapshot:Realized", "ElementName": "Before patching", "CreationTime": "20261001000000.000000+000",
		"Parent": vssdSnap, "IsAutomaticSnapshot": false,
	})
	for _, r := range q.rows["Msvm_VirtualSystemSettingData"] {
		switch r["ConfigurationID"] {
		case snap:
			r["ElementName"], r["CreationTime"] = "Clean install", "20260901000000.000000+000"
		case vm1:
			r["Parent"] = vssdSnap2
			r["SecureBootEnabled"], r["SecureBootTemplateId"] = true, "{1734c6e8-3154-4dda-ba5f-a874cc483422}"
		}
	}
	snapVHD := wmi.Row{
		wmi.PathKey: path("Msvm_StorageAllocationSettingData", "InstanceID", "Microsoft:"+vm1+`\`+snap+`\D\L`), "InstanceID": "Microsoft:" + vm1 + `\` + snap + `\D\L`,
		"ResourceSubType": subVirtualHardDisk, "HostResource": []any{`C:\VMs\vm1\vm1_A.avhdx`},
	}
	snap2VHD := wmi.Row{
		wmi.PathKey: path("Msvm_StorageAllocationSettingData", "InstanceID", "Microsoft:"+vm1+`\`+snap2+`\D\L`), "InstanceID": "Microsoft:" + vm1 + `\` + snap2 + `\D\L`,
		"ResourceSubType": subVirtualHardDisk, "HostResource": []any{`C:\VMs\vm1\vm1_B.avhdx`},
	}
	q.rows["Msvm_StorageAllocationSettingData"] = append(q.rows["Msvm_StorageAllocationSettingData"], snapVHD, snap2VHD)
	q.rows["Msvm_VirtualSystemSettingDataComponent"] = append(q.rows["Msvm_VirtualSystemSettingDataComponent"],
		wmi.Row{"GroupComponent": vssdSnap, "PartComponent": snapVHD[wmi.PathKey]},
		wmi.Row{"GroupComponent": vssdSnap2, "PartComponent": snap2VHD[wmi.PathKey]},
	)
	// Integration components and their settings.
	q.rows["Msvm_ShutdownComponent"] = []wmi.Row{{"SystemName": vm1, "EnabledState": int32(2), "OperationalStatus": []any{int32(2)}, "StatusDescriptions": []any{"OK"}}}
	q.rows["Msvm_TimeSyncComponent"] = []wmi.Row{{"SystemName": vm1, "EnabledState": int32(2), "OperationalStatus": []any{int32(2)}}}
	q.rows["Msvm_VssComponent"] = []wmi.Row{{"SystemName": vm1, "EnabledState": int32(2), "OperationalStatus": []any{int32(12)}}}
	q.rows["Msvm_GuestServiceInterfaceComponent"] = []wmi.Row{{"SystemName": vm1, "EnabledState": int32(3), "OperationalStatus": []any{int32(15)}}}
	compSet := func(class, vm string, enabled int32) wmi.Row {
		id := "Microsoft:" + vm + `\` + class
		return wmi.Row{wmi.PathKey: path(class, "InstanceID", id), "InstanceID": id, "EnabledState": enabled}
	}
	q.rows["Msvm_HeartbeatComponentSettingData"] = []wmi.Row{compSet("Msvm_HeartbeatComponentSettingData", vm1, 2)}
	q.rows["Msvm_GuestServiceInterfaceComponentSettingData"] = []wmi.Row{compSet("Msvm_GuestServiceInterfaceComponentSettingData", vm1, 3)}
	q.rows["Msvm_KvpExchangeComponentSettingData"] = []wmi.Row{func() wmi.Row {
		r := compSet("Msvm_KvpExchangeComponentSettingData", vm1, 2)
		r["HostExchangeItems"] = []any{`<INSTANCE CLASSNAME="Msvm_KvpExchangeDataItem"><PROPERTY NAME="Data" TYPE="string"><VALUE>prod</VALUE></PROPERTY><PROPERTY NAME="Name" TYPE="string"><VALUE>Environment</VALUE></PROPERTY></INSTANCE>`}
		return r
	}()}
	q.rows["Msvm_KvpExchangeComponent"][0]["GuestExchangeItems"] = []any{`<INSTANCE CLASSNAME="Msvm_KvpExchangeDataItem"><PROPERTY NAME="Data" TYPE="string"><VALUE>2.1</VALUE></PROPERTY><PROPERTY NAME="Name" TYPE="string"><VALUE>AppVersion</VALUE></PROPERTY></INSTANCE>`}
	for _, r := range q.rows["Msvm_VirtualSystemSettingDataComponent"] {
		_ = r
	}
	vssd1 := path("Msvm_VirtualSystemSettingData", "InstanceID", "Microsoft:"+vm1)
	for _, cls := range []string{"Msvm_HeartbeatComponentSettingData", "Msvm_GuestServiceInterfaceComponentSettingData", "Msvm_KvpExchangeComponentSettingData"} {
		q.rows["Msvm_VirtualSystemSettingDataComponent"] = append(q.rows["Msvm_VirtualSystemSettingDataComponent"], wmi.Row{"GroupComponent": vssd1, "PartComponent": q.rows[cls][0][wmi.PathKey]})
	}
	// Replication of vm2 (Archive, state Replicating in the base fixtures).
	q.rows["Msvm_ReplicationRelationship"] = []wmi.Row{{"InstanceID": "Microsoft:" + vm2 + `\RR`, "LastReplicationTime": "20261019113000.000000+000", "LastReplicationType": int32(1), "LastApplicationConsistentReplicationTime": "20261019090000.000000+000"}}
	q.rows["Msvm_ReplicationSettingData"] = []wmi.Row{{"InstanceID": "Microsoft:" + vm2 + `\RS`, "RecoveryConnectionPoint": "hv-dr.example.local", "RecoveryServerPortNumber": int32(443),
		"ReplicationInterval": int32(300), "RecoveryHistory": int32(4), "ApplicationConsistentSnapshotInterval": int32(1), "AuthenticationType": int32(2), "CompressionEnabled": true, "AutoResynchronizeEnabled": true,
		"ReplicatedDisks": []any{path("Msvm_StorageAllocationSettingData", "InstanceID", `Microsoft:`+vm2+`\D`)}}}
	// Security, devices, metering for vm1.
	sec := wmi.Row{wmi.PathKey: path("Msvm_SecuritySettingData", "InstanceID", "Microsoft:"+vm1+`\SEC`), "InstanceID": "Microsoft:" + vm1 + `\SEC`, "TpmEnabled": true, "Shielded": false, "EncryptStateAndVmMigrationTraffic": true, "DataProtectionRequested": false}
	q.rows["Msvm_SecuritySettingData"] = []wmi.Row{sec}
	gpu := wmi.Row{wmi.PathKey: path("Msvm_GpuPartitionSettingData", "InstanceID", "Microsoft:"+vm1+`\GPU`), "InstanceID": "Microsoft:" + vm1 + `\GPU`, "ElementName": "GPU Partition", "MinPartitionVRAM": "1073741824", "MaxPartitionVRAM": "4294967296", "OptimalPartitionVRAM": "2147483648", "HostResource": []any{`\\HV01\root\virtualization\v2:Msvm_GpuPartition.Name="GPU-0-P1"`}}
	serial := wmi.Row{wmi.PathKey: path("Msvm_SerialPortSettingData", "InstanceID", "Microsoft:"+vm1+`\COM1`), "InstanceID": "Microsoft:" + vm1 + `\COM1`, "ElementName": "COM 1", "Connection": []any{`\\.\pipe\vm1`}, "DebuggerMode": false}
	q.rows["Msvm_GpuPartitionSettingData"], q.rows["Msvm_SerialPortSettingData"] = []wmi.Row{gpu}, []wmi.Row{serial}
	for _, r := range []wmi.Row{sec, gpu, serial} {
		q.rows["Msvm_VirtualSystemSettingDataComponent"] = append(q.rows["Msvm_VirtualSystemSettingDataComponent"], wmi.Row{"GroupComponent": vssd1, "PartComponent": r[wmi.PathKey]})
	}
	mv := wmi.Row{wmi.PathKey: path("Msvm_AggregationMetricValue", "InstanceID", "Microsoft:"+vm1+`\M1`), "MetricDefinitionId": "Microsoft:CPU", "MetricValue": "12.5", "TimeStamp": "20261001000000.000000+000", "Duration": "00000000180000.000000:000", "MeasuredElementName": "Web Server"}
	q.rows["Msvm_AggregationMetricValue"] = []wmi.Row{mv}
	q.rows["Msvm_AggregationMetricDefinition"] = []wmi.Row{{"Id": "Microsoft:CPU", "ElementName": "Aggregated Average CPU Utilization", "Units": "MHz"}}
	q.rows["Msvm_MetricForME"] = []wmi.Row{{"Antecedent": path("Msvm_ComputerSystem", "Name", vm1), "Dependent": mv[wmi.PathKey]}}
	q.rows["Msvm_ImageManagementService"] = []wmi.Row{{wmi.PathKey: ns + `Msvm_ImageManagementService.Name="vhdsvc"`, "Name": "vhdsvc"}}
	q.rows["Msvm_VirtualSystemManagementService"] = []wmi.Row{{wmi.PathKey: ns + `Msvm_VirtualSystemManagementService.Name="vmms"`, "Name": "vmms"}}
	return q
}

func TestIntegrationAndKVP(t *testing.T) {
	d := poll(t, deepFixtures())
	id := strings.ToUpper(vm1)
	in, err := d.Integration(id)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]IntegrationService{}
	for _, s := range in.Services {
		by[s.Name] = s
	}
	if len(in.Services) != 6 || !by["Heartbeat"].Enabled || by["Heartbeat"].Status != "OK" || by["Backup (volume shadow copy)"].Status != "No Contact" ||
		by["Guest services"].Enabled || by["Guest services"].Status != "Dormant" || !by["Data exchange (key-value pairs)"].Present || in.GuestVersion != "" {
		b, _ := json.MarshalIndent(in, "", " ")
		t.Errorf("integration = %s", b)
	}
	kvp, _ := d.KVP(id)
	if kvp.Intrinsic["OSName"] == "" || kvp.Guest["AppVersion"] != "2.1" || kvp.Host["Environment"] != "prod" {
		t.Errorf("kvp = %+v", kvp)
	}
	kvp2, _ := d.KVP(strings.ToUpper(vm2))
	if len(kvp2.Intrinsic) != 0 || len(kvp2.Notes) == 0 {
		t.Errorf("kvp2 = %+v", kvp2)
	}
}

func TestCheckpoints(t *testing.T) {
	fileSize = func(p string) (int64, bool) {
		if strings.HasSuffix(p, "vm1_A.avhdx") {
			return 3 << 30, true
		}
		return 0, false
	}
	defer func() { fileSize = func(string) (int64, bool) { return 0, false } }()
	d := poll(t, deepFixtures())
	c, err := d.Checkpoints(strings.ToUpper(vm1))
	if err != nil {
		t.Fatal(err)
	}
	if c.Count != 2 || c.Checkpoints[0].Name != "Clean install" || c.Checkpoints[1].Name != "Before patching" {
		b, _ := json.MarshalIndent(c, "", " ")
		t.Fatalf("checkpoints = %s", b)
	}
	a, b := c.Checkpoints[0], c.Checkpoints[1]
	if a.ParentID != "" || len(a.Children) != 1 || a.Children[0] != snap2 || a.Current || a.DiskSizeMB != 3072 || a.Disks[0].Missing || a.AgeDays < 30 {
		t.Errorf("first = %+v", a)
	}
	if b.ParentID != snap || !b.Current || b.Type != "Checkpoint" || !b.Disks[0].Missing || b.DiskSizeMB != 0 {
		t.Errorf("second = %+v", b)
	}
	if c.TotalSizeMB != 3072 || c.OldestDays < 30 {
		t.Errorf("totals = %+v", c)
	}
	// A VM without checkpoints.
	c2, _ := d.Checkpoints(strings.ToUpper(vm2))
	if c2.Count != 0 || len(c2.Checkpoints) != 0 {
		t.Errorf("vm2 checkpoints = %+v", c2)
	}
}

func TestReplicationSecurityDevicesMetering(t *testing.T) {
	d := poll(t, deepFixtures())
	r, err := d.Replication(strings.ToUpper(vm2))
	if err != nil {
		t.Fatal(err)
	}
	if !r.Enabled || r.State != "Replicating" || r.Health != "Warning" || r.ReplicaServer != "hv-dr.example.local" || r.ReplicaPort != 443 || r.IntervalSeconds != 300 ||
		r.Authentication != "Certificate" || !r.Compression || r.LastReplication == nil || r.LastReplicationType != "Regular" || r.MinutesSinceLast == nil || len(r.ReplicatedDisks) != 1 {
		b, _ := json.MarshalIndent(r, "", " ")
		t.Errorf("replication = %s", b)
	}
	r1, _ := d.Replication(strings.ToUpper(vm1))
	if r1.Enabled || len(r1.Notes) == 0 {
		t.Errorf("vm1 replication = %+v", r1)
	}

	sec, _ := d.Security(strings.ToUpper(vm1))
	if sec.Generation != 2 || sec.SecureBoot == nil || !*sec.SecureBoot || sec.SecureBootTemplate != "Microsoft Windows" || sec.TPM == nil || !*sec.TPM || sec.EncryptStateAndTraffic == nil || !*sec.EncryptStateAndTraffic {
		t.Errorf("security = %+v", sec)
	}
	sec2, _ := d.Security(strings.ToUpper(vm2))
	if sec2.Generation != 1 || sec2.SecureBoot != nil || sec2.TPM != nil || len(sec2.Notes) == 0 {
		t.Errorf("security2 = %+v", sec2)
	}

	dev, _ := d.Devices(strings.ToUpper(vm1))
	if len(dev.GPUPartitions) != 1 || dev.GPUPartitions[0].MaxVRAMMB != 4096 || dev.GPUPartitions[0].HostResource == "" || len(dev.SerialPorts) != 1 || dev.SerialPorts[0].Connection != `\\.\pipe\vm1` || len(dev.DVDDrives) != 1 || dev.DVDDrives[0].Path != `C:\ISO\install.iso` {
		b, _ := json.MarshalIndent(dev, "", " ")
		t.Errorf("devices = %s", b)
	}

	m, _ := d.Metering(strings.ToUpper(vm1))
	if !m.Enabled || len(m.Metrics) != 1 || m.Metrics[0].Name != "Aggregated Average CPU Utilization" || m.Metrics[0].Value != 12.5 || m.Metrics[0].Units != "MHz" || m.Metrics[0].DurationSecs != 18*3600 || m.Metrics[0].Element != "" {
		t.Errorf("metering = %+v", m)
	}
	m2, _ := d.Metering(strings.ToUpper(vm2))
	if m2.Enabled || len(m2.Notes) == 0 {
		t.Errorf("metering2 = %+v", m2)
	}

	det, _ := d.Detail(strings.ToUpper(vm1))
	if det.Integration == nil || det.Checkpoints == nil || det.Replication == nil || det.Security == nil || det.Devices == nil {
		t.Error("detail lacks deep parts")
	}
}

// fakeCaller answers method calls with canned rows keyed by method and Path.
type fakeCaller struct {
	settings map[string]string // path -> SettingData XML
	state    map[string]string // path -> State XML
	calls    int
	thumb    []byte
}

func (f *fakeCaller) Call(ctx context.Context, ns, obj, method string, in map[string]any) (wmi.Row, error) {
	f.calls++
	if err := wmi.CheckMethod(method); err != nil {
		return nil, err
	}
	switch method {
	case "GetVirtualHardDiskSettingData":
		p, _ := in["Path"].(string)
		if x, ok := f.settings[p]; ok {
			return wmi.Row{"ReturnValue": int32(0), "SettingData": x}, nil
		}
		return nil, errors.New("file not found")
	case "GetVirtualHardDiskState":
		p, _ := in["Path"].(string)
		if x, ok := f.state[p]; ok {
			return wmi.Row{"ReturnValue": int32(0), "State": x}, nil
		}
		return wmi.Row{"ReturnValue": int32(32768)}, nil
	case "GetVirtualSystemThumbnailImage":
		if obj == "" || in["TargetSystem"] == "" {
			return nil, errors.New("bad target")
		}
		vals := make([]any, len(f.thumb))
		for i, b := range f.thumb {
			vals[i] = b
		}
		return wmi.Row{"ReturnValue": int32(0), "ImageData": vals}, nil
	}
	return nil, errors.New("unexpected method " + method)
}

func vhdXML(path, parent, typ string) string {
	return `<INSTANCE CLASSNAME="Msvm_VirtualHardDiskSettingData"><PROPERTY NAME="Path" TYPE="string"><VALUE>` + path + `</VALUE></PROPERTY>` +
		`<PROPERTY NAME="ParentPath" TYPE="string"><VALUE>` + parent + `</VALUE></PROPERTY><PROPERTY NAME="Type" TYPE="uint16"><VALUE>` + typ + `</VALUE></PROPERTY>` +
		`<PROPERTY NAME="Format" TYPE="uint16"><VALUE>3</VALUE></PROPERTY><PROPERTY NAME="MaxInternalSize" TYPE="uint64"><VALUE>137438953472</VALUE></PROPERTY>` +
		`<PROPERTY NAME="BlockSize" TYPE="uint32"><VALUE>33554432</VALUE></PROPERTY><PROPERTY NAME="LogicalSectorSize" TYPE="uint32"><VALUE>512</VALUE></PROPERTY></INSTANCE>`
}

func TestDiskState(t *testing.T) {
	d := poll(t, deepFixtures())
	leaf := `C:\VMs\vm1\vm1.vhdx`
	fc := &fakeCaller{
		settings: map[string]string{leaf: vhdXML(leaf, `C:\VMs\vm1\base.vhdx`, "4"), `C:\VMs\vm1\base.vhdx`: vhdXML(`C:\VMs\vm1\base.vhdx`, "", "3")},
		state:    map[string]string{leaf: `<INSTANCE CLASSNAME="Msvm_VirtualHardDiskState"><PROPERTY NAME="FileSize" TYPE="uint64"><VALUE>4294967296</VALUE></PROPERTY><PROPERTY NAME="InUse" TYPE="boolean"><VALUE>true</VALUE></PROPERTY><PROPERTY NAME="FragmentationPercentage" TYPE="uint32"><VALUE>7</VALUE></PROPERTY><PROPERTY NAME="MinInternalSize" TYPE="uint64"><VALUE>2147483648</VALUE></PROPERTY></INSTANCE>`},
	}
	now := time.Now()
	od := NewOnDemand(fc, func() *Data { return d })
	od.now = func() time.Time { return now }
	st, err := od.DiskState(context.Background(), "Web Server", "scsi0-0")
	if err != nil {
		t.Fatal(err)
	}
	if st.VMName != "Web Server" || st.DiskID != "scsi0-0" || len(st.Chain) != 2 || st.Chain[0].Type != "Differencing" || st.Chain[0].Format != "VHDX" || st.Chain[0].MaxSizeMB != 131072 ||
		st.Chain[0].BlockSizeKB != 32768 || st.Chain[1].Type != "Dynamic" || st.ChainBroken || st.FileSizeMB == nil || *st.FileSizeMB != 4096 || st.InUse == nil || !*st.InUse || st.Fragmentation == nil || *st.Fragmentation != 7 {
		b, _ := json.MarshalIndent(st, "", " ")
		t.Errorf("state = %s", b)
	}
	calls := fc.calls
	if _, err := od.DiskState(context.Background(), vm1, "scsi0-0"); err != nil || fc.calls != calls {
		t.Errorf("second call should be served from cache (calls %d -> %d, %v)", calls, fc.calls, err)
	}
	now = now.Add(time.Minute)
	if _, err := od.DiskState(context.Background(), vm1, "scsi0-0"); err != nil || fc.calls == calls {
		t.Error("cache should expire")
	}

	// Broken chain: the parent cannot be opened.
	delete(fc.settings, `C:\VMs\vm1\base.vhdx`)
	now = now.Add(time.Minute)
	st, err = od.DiskState(context.Background(), vm1, "scsi0-0")
	if err != nil || !st.ChainBroken || !st.Chain[0].ParentMissing || len(st.Chain) != 2 || st.Chain[1].Error == "" {
		t.Errorf("broken chain = %+v %v", st, err)
	}

	// Errors.
	if _, err := od.DiskState(context.Background(), vm1, "scsi0-1"); !errors.Is(err, ErrNotFound) { // DVD, not a VHD
		t.Errorf("dvd: %v", err)
	}
	if _, err := od.DiskState(context.Background(), vm1, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown disk: %v", err)
	}
	if _, err := od.DiskState(context.Background(), "nope", "scsi0-0"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown vm: %v", err)
	}
	var none *OnDemand
	if _, err := none.DiskState(context.Background(), vm1, "scsi0-0"); !errors.Is(err, ErrOnDemandUnavailable) {
		t.Errorf("nil: %v", err)
	}
}

func TestThumbnail(t *testing.T) {
	d := poll(t, deepFixtures())
	// 2x1 image: pure red (0xF800) then pure blue (0x001F), little endian.
	fc := &fakeCaller{thumb: []byte{0x00, 0xF8, 0x1F, 0x00}}
	od := NewOnDemand(fc, func() *Data { return d })
	pngBytes, err := od.Thumbnail(context.Background(), "Web Server", 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(pngBytes) < 8 || string(pngBytes[1:4]) != "PNG" {
		t.Errorf("not a PNG: %x", pngBytes[:8])
	}
	if _, err := od.Thumbnail(context.Background(), "Archive", 2, 1); !errors.Is(err, ErrNotRunning) {
		t.Errorf("off VM: %v", err)
	}
	if _, err := od.Thumbnail(context.Background(), "Web Server", 1000, 1000); err == nil {
		t.Error("short data should fail")
	}
	img, err := rgb565ToPNG([]byte{0x00, 0xF8, 0x1F, 0x00}, 2, 1)
	if err != nil || len(img) == 0 {
		t.Error(err)
	}
}

func TestParseCIMXML(t *testing.T) {
	m := ParseCIMXML(`<INSTANCE CLASSNAME="X"><PROPERTY NAME="A" TYPE="string"><VALUE>1</VALUE></PROPERTY><PROPERTY.ARRAY NAME="B" TYPE="string"><VALUE.ARRAY><VALUE>x</VALUE><VALUE>y</VALUE></VALUE.ARRAY></PROPERTY.ARRAY></INSTANCE>`)
	if m["A"] != "1" || m["B"] != "x;y" {
		t.Errorf("parsed = %v", m)
	}
	if ParseCIMXML("nope") != nil {
		t.Error("garbage should be nil")
	}
}
