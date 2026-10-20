package hyperv

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/damiankw/hvapi/internal/wmi"
)

const sw2 = "66666666-6666-6666-6666-666666666666" // private switch

// hostFixtures adds host, cluster and switch rows to the VM fixtures.
func hostFixtures() *fakeQuerier {
	q := fixtures()
	extDev := `Microsoft:` + sw1 + `\EXT-DEVICE`
	q.rows["Win32_ComputerSystem"] = []wmi.Row{{"Name": "HV01", "Domain": "example.local", "Manufacturer": "Dell Inc.", "Model": "PowerEdge R750",
		"NumberOfProcessors": int32(2), "NumberOfLogicalProcessors": int32(64), "TotalPhysicalMemory": "274877906944", "HypervisorPresent": true}}
	q.rows["Win32_OperatingSystem"] = []wmi.Row{{"Caption": "Microsoft Windows Server 2022 Datacenter", "Version": "10.0.20348", "BuildNumber": "20348",
		"OSArchitecture": "64-bit", "InstallDate": "20230101000000.000000+000", "LastBootUpTime": "20260919000000.000000+000", "LocalDateTime": "20261019120000.000000+000",
		"TotalVisibleMemorySize": "268435456", "FreePhysicalMemory": "104857600"}}
	q.rows["Win32_Processor"] = []wmi.Row{
		{"Name": "Intel(R) Xeon(R)  Gold 6338", "NumberOfCores": int32(16), "MaxClockSpeed": int32(2000), "VirtualizationFirmwareEnabled": false, "SecondLevelAddressTranslationExtensions": false},
		{"Name": "Intel(R) Xeon(R)  Gold 6338", "NumberOfCores": int32(16), "MaxClockSpeed": int32(2000)},
	}
	q.rows["Msvm_VirtualSystemManagementServiceSettingData"] = []wmi.Row{{"DefaultExternalDataRoot": `C:\VMs`, "DefaultVirtualHardDiskPath": `D:\VHDs`,
		"MinimumMacAddress": "00155D0A0B00", "MaximumMacAddress": "00155D0A0BFF", "NumaSpanningEnabled": true, "EnhancedSessionModeEnabled": false}}
	q.rows["Win32_Service"] = []wmi.Row{
		{"Name": "vmms", "DisplayName": "Hyper-V Virtual Machine Management", "State": "Running", "StartMode": "Auto", "Status": "OK", "ProcessId": int32(1234), "StartName": "LocalSystem", "ExitCode": int32(0)},
		{"Name": "vmcompute", "DisplayName": "Hyper-V Host Compute Service", "State": "Running", "StartMode": "Auto", "ProcessId": int32(1235), "ExitCode": int32(0)},
		{"Name": "hvhost", "DisplayName": "HV Host Service", "State": "Stopped", "StartMode": "Manual", "ProcessId": int32(0), "ExitCode": int32(1077)},
	}
	q.rows["MSFT_Volume"] = []wmi.Row{
		{"DriveLetter": "C", "FileSystemLabel": "OS", "FileSystem": "NTFS", "Size": "214748364800", "SizeRemaining": "53687091200", "HealthStatus": int32(0), "DriveType": int32(3), "Path": `\\?\Volume{aaaa}\`},
		{"DriveLetter": int32('D'), "FileSystemLabel": "VMs", "FileSystem": "ReFS", "Size": "4398046511104", "SizeRemaining": "219902325555", "HealthStatus": int32(1), "DriveType": int32(3), "Path": `\\?\Volume{bbbb}\`},
		{"DriveLetter": nil, "FileSystemLabel": "", "FileSystem": "CSVFS", "Size": "1099511627776", "SizeRemaining": "549755813888", "HealthStatus": int32(0), "DriveType": int32(3), "Path": `C:\ClusterStorage\Volume1\`},
	}
	q.rows["MSCluster_ClusterSharedVolume"] = []wmi.Row{{"Name": "Cluster Disk 1", "VolumeName": `C:\ClusterStorage\Volume1`,
		"SharedVolumeInfo": wmi.Row{"FriendlyVolumeName": `C:\ClusterStorage\Volume1`, "Partition": wmi.Row{"Size": "1099511627776", "FreeSpace": "549755813888", "FileSystem": "CSVFS", "VolumeLabel": "CSV1"}}}}
	q.rows["MSFT_NetAdapter"] = []wmi.Row{
		{"Name": "NIC1", "InterfaceDescription": "Broadcom NetXtreme", "MacAddress": "00-11-22-33-44-55", "Status": "Up", "ReceiveLinkSpeed": "10000000000", "MtuSize": int32(1500), "Virtual": false, "DriverVersionString": "1.2.3", "InterfaceIndex": int32(5)},
		{"Name": "vEthernet (External)", "InterfaceDescription": "Hyper-V Virtual Ethernet Adapter", "MacAddress": "00-11-22-33-44-55", "Status": "Up", "ReceiveLinkSpeed": "10000000000", "Virtual": true, "InterfaceIndex": int32(9)},
		{"Name": "NIC2", "InterfaceDescription": "Broadcom NetXtreme #2", "MacAddress": "00-11-22-33-44-56", "InterfaceOperationalStatus": int32(2), "Virtual": false, "InterfaceIndex": int32(6)},
	}
	q.rows["Msvm_ExternalEthernetPort"] = []wmi.Row{{"DeviceID": extDev, "ElementName": "Broadcom NetXtreme", "PermanentAddress": "001122334455", "IsBound": true}}
	q.rows["Msvm_InternalEthernetPort"] = []wmi.Row{{"DeviceID": `Microsoft:` + sw1 + `\INT`, "ElementName": "External"}}
	q.rows["Msvm_NumaNode"] = []wmi.Row{
		{"NodeID": "Microsoft:HV01\\1", "ElementName": "Node 1", "NumberOfLogicalProcessors": int32(32), "NumberOfProcessorCores": int32(16), "CurrentlyConsumableMemoryBlocks": "120000"},
		{"NodeID": "Microsoft:HV01\\0", "ElementName": "Node 0", "NumberOfLogicalProcessors": int32(32), "NumberOfProcessorCores": int32(16), "CurrentlyConsumableMemoryBlocks": "130000"},
	}
	q.rows["Msvm_VirtualSystemMigrationServiceSettingData"] = []wmi.Row{{"EnableVirtualSystemMigration": true, "MaximumActiveVirtualSystemMigration": "2", "MaximumActiveStorageMigration": "2",
		"AuthenticationType": int32(1), "EnableCompression": true, "EnableSmbTransport": false, "UseAnyNetworkForMigration": false}}
	q.rows["Msvm_VirtualSystemMigrationNetworkSettingData"] = []wmi.Row{
		{"SubnetNumber": "10.0.1.0", "PrefixLength": int32(24), "Metric": "200"},
		{"SubnetNumber": "10.0.0.0", "PrefixLength": int32(24), "Metric": "100"},
	}
	q.rows["MSCluster_Cluster"] = []wmi.Row{{"Name": "HVCLUSTER"}}
	q.rows["MSCluster_Node"] = []wmi.Row{
		{"Name": "HV02", "State": int32(2), "NodeWeight": int32(1), "DynamicWeight": int32(1), "MajorVersion": int32(10), "MinorVersion": int32(0), "BuildNumber": int32(20348)},
		{"Name": "HV01", "State": int32(0), "NodeWeight": int32(1), "DynamicWeight": int32(1), "MajorVersion": int32(10), "MinorVersion": int32(0), "BuildNumber": int32(20348)},
	}
	q.rows["MSCluster_ResourceGroup"] = []wmi.Row{{"Name": "Web Server", "State": int32(0), "OwnerNode": "HV01"}}
	q.rows["MSCluster_Resource"] = []wmi.Row{
		{"Name": "Virtual Machine Web Server", "Type": "Virtual Machine", "State": int32(2), "OwnerGroup": "Web Server", "OwnerNode": "HV01", "PrivateProperties": wmi.Row{"VmID": vm1}},
		{"Name": "Cluster Disk 1", "Type": "Physical Disk", "State": int32(2), "OwnerGroup": "Cluster Group", "OwnerNode": "HV02"},
	}
	q.rows["Win32_PerfFormattedData_HvStats_HyperVHypervisorLogicalProcessor"] = []wmi.Row{
		{"Name": "_Total", "PercentTotalRunTime": "23", "PercentGuestRunTime": "20", "PercentHypervisorRunTime": "3", "PercentIdleTime": "77", "ContextSwitchesPersec": "5000"},
		{"Name": "Hv LP 10", "PercentTotalRunTime": "10", "PercentIdleTime": "90"},
		{"Name": "Hv LP 2", "PercentTotalRunTime": "50", "PercentIdleTime": "50"},
	}
	q.rows["Win32_PerfFormattedData_HvStats_HyperVHypervisorRootVirtualProcessor"] = []wmi.Row{{"Name": "_Total", "PercentTotalRunTime": "4"}}
	q.rows["Win32_PerfFormattedData_HvStats_HyperVHypervisor"] = []wmi.Row{{"LogicalProcessors": "64", "VirtualProcessors": "70", "Partitions": "3"}}
	q.rows["Win32_PerfFormattedData_PerfOS_Memory"] = []wmi.Row{{"AvailableMBytes": "102400", "CommittedBytes": "171798691840", "CommitLimit": "300647710720", "PagesPersec": "12"}}
	q.rows["Win32_PerfFormattedData_BalancerStats_HyperVDynamicMemoryBalancer"] = []wmi.Row{{"Name": "System Balancer", "AvailableMemory": "98000", "SystemCurrentPressure": "45"}}
	q.rows["Win32_DeviceGuard"] = []wmi.Row{{"VirtualizationBasedSecurityStatus": int32(2), "SecurityServicesRunning": []any{int32(2)}}}
	q.rows["Msvm_VirtualEthernetSwitch"] = []wmi.Row{
		{"Name": sw1, "ElementName": "External", "HealthState": int32(5), "EnabledState": int32(2)},
		{"Name": sw2, "ElementName": "Private", "HealthState": int32(5), "EnabledState": int32(2)},
	}
	q.rows["Msvm_VirtualEthernetSwitchSettingData"] = []wmi.Row{{"VirtualSystemIdentifier": sw1, "IOVPreferred": false, "TeamingEnabled": true, "BandwidthReservationMode": int32(1), "PacketDirectEnabled": false}}
	q.rows["Msvm_EthernetSwitchExtension"] = []wmi.Row{
		{"SystemName": sw1, "ElementName": "Microsoft Windows Filtering Platform", "ExtensionType": int32(2), "EnabledState": int32(2), "OperationalStatus": []any{int32(2)}, "Version": "10.0", "Vendor": "Microsoft"},
		{"SystemName": sw1, "ElementName": "Microsoft NDIS Capture", "ExtensionType": int32(1), "EnabledState": int32(3), "OperationalStatus": []any{int32(15)}},
	}
	// Host-side switch ports share the EPASD class with VM connections.
	q.rows["Msvm_EthernetPortAllocationSettingData"] = append(q.rows["Msvm_EthernetPortAllocationSettingData"],
		wmi.Row{"InstanceID": "Microsoft:" + sw1 + `\EXTPORT`, "HostResource": []any{path("Msvm_ExternalEthernetPort", "DeviceID", extDev)}, "EnabledState": int32(2)},
		wmi.Row{"InstanceID": "Microsoft:" + sw1 + `\INTPORT`, "HostResource": []any{path("Msvm_InternalEthernetPort", "DeviceID", `Microsoft:`+sw1+`\INT`)}, "EnabledState": int32(2)},
	)
	return q
}

func TestHost(t *testing.T) {
	d := poll(t, hostFixtures())
	h, err := d.Host()
	if err != nil {
		t.Fatal(err)
	}
	if h.Name != "HV01" || h.OS != "Microsoft Windows Server 2022 Datacenter" || h.Sockets != 2 || h.Cores != 32 || h.LogicalProcessors != 64 ||
		h.MemoryTotalMB != 262144 || h.MemoryFreeMB != 102400 || h.UptimeSeconds != 30*24*3600+12*3600 || h.VMCount != 2 || h.VMRunning != 1 ||
		h.ProcessorModel != "Intel(R) Xeon(R) Gold 6338" || h.DefaultVHDPath != `D:\VHDs` || h.MACRangeStart != "00:15:5D:0A:0B:00" ||
		h.NUMASpanning == nil || !*h.NUMASpanning || h.VirtualizationFW == nil || *h.VirtualizationFW || !h.HypervisorPresent {
		b, _ := json.MarshalIndent(h, "", " ")
		t.Errorf("host = %s", b)
	}
}

func TestHostServices(t *testing.T) {
	d := poll(t, hostFixtures())
	s, err := d.HostServices()
	if err != nil {
		t.Fatal(err)
	}
	if s.AllRunning || len(s.Services) != 3 || s.Services[0].Name != "hvhost" || s.Services[0].State != "Stopped" || !s.Services[0].Expected || s.Services[2].Account != "LocalSystem" {
		t.Errorf("services = %+v", s)
	}
}

func TestHostStorage(t *testing.T) {
	d := poll(t, hostFixtures())
	st, err := d.HostStorage()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Volumes) != 3 {
		b, _ := json.MarshalIndent(st, "", " ")
		t.Fatalf("volumes = %s", b)
	}
	c, dv, csv := st.Volumes[0], st.Volumes[1], st.Volumes[2]
	if c.DriveLetter != "C" || c.SizeMB != 204800 || c.FreeMB != 51200 || c.FreePercent != 25 || c.Health != "Healthy" || c.Type != "Fixed" ||
		c.VMFiles != 4 || len(c.VMs) != 1 || c.VMs[0] != "Web Server" { // config root, snapshot root, vhdx, iso
		t.Errorf("C: = %+v", c)
	}
	if dv.DriveLetter != "D" || dv.Health != "Warning" || dv.VMFiles != 1 || dv.VMs[0] != "Archive" {
		t.Errorf("D: = %+v", dv)
	}
	if !csv.ClusterShared || csv.Label != `C:\ClusterStorage\Volume1` || csv.CSVOwner != "HV02" || csv.CSVState != "Online" || csv.FreePercent != 50 || csv.DriveLetter != "" {
		t.Errorf("csv = %+v", csv)
	}
}

func TestHostNetworkAndSwitches(t *testing.T) {
	d := poll(t, hostFixtures())
	n, err := d.HostNetwork()
	if err != nil {
		t.Fatal(err)
	}
	if len(n.Adapters) != 3 || n.Adapters[0].Name != "NIC1" || n.Adapters[0].BoundToSwitch != "External" || n.Adapters[0].LinkSpeedMbps != 10000 ||
		n.Adapters[0].MACAddress != "00:11:22:33:44:55" || n.Adapters[1].Status != "Down" || n.Adapters[1].BoundToSwitch != "" || !n.Adapters[2].Virtual {
		b, _ := json.MarshalIndent(n, "", " ")
		t.Errorf("network = %s", b)
	}

	sw, err := d.Switches()
	if err != nil {
		t.Fatal(err)
	}
	if sw.Count != 2 || sw.Switches[0].Name != "External" || sw.Switches[0].Type != "External" || sw.Switches[0].ExternalAdapters[0] != "Broadcom NetXtreme" ||
		!sw.Switches[0].ManagementOSAccess || !sw.Switches[0].EmbeddedTeaming || sw.Switches[0].BandwidthMode != "Weight" || sw.Switches[0].VMPorts != 1 ||
		len(sw.Switches[0].Extensions) != 2 || !sw.Switches[0].Extensions[0].Running || sw.Switches[0].Extensions[1].Enabled ||
		sw.Switches[1].Type != "Private" || sw.Switches[1].VMPorts != 0 {
		b, _ := json.MarshalIndent(sw, "", " ")
		t.Errorf("switches = %s", b)
	}

	det, err := d.Switch("external")
	if err != nil {
		t.Fatal(err)
	}
	if det.ID != sw1 || len(det.Ports) != 3 {
		b, _ := json.MarshalIndent(det, "", " ")
		t.Fatalf("detail = %s", b)
	}
	if p := det.Ports[2]; p.Kind != "VM" || p.VMName != "Web Server" || p.Name != "Network Adapter" || p.MACAddress != "00:15:5D:0A:0B:0C" || !p.Connected || p.VLANMode != "Access" || p.VLANID != 42 {
		t.Errorf("vm port = %+v", p)
	}
	if det.Ports[0].Kind != "External" || det.Ports[1].Kind != "Internal" {
		t.Errorf("host ports = %+v", det.Ports[:2])
	}
	ports, err := d.SwitchPorts(sw1)
	if err != nil || len(ports.Ports) != 3 || ports.SwitchName != "External" {
		t.Errorf("ports = %+v %v", ports, err)
	}
	if _, err := d.Switch("nope"); !errors.Is(err, ErrNotFound) {
		t.Error("expected not found")
	}
}

func TestHostPerfNUMAHypervisorMigrationCluster(t *testing.T) {
	d := poll(t, hostFixtures())
	p, err := d.HostPerf()
	if err != nil {
		t.Fatal(err)
	}
	if p.CPUTotalPercent == nil || *p.CPUTotalPercent != 23 || p.CPURootPercent == nil || *p.CPURootPercent != 4 || len(p.LogicalProcessors) != 2 ||
		p.LogicalProcessors[0].Name != "Hv LP 2" || p.Partitions == nil || *p.Partitions != 3 || p.MemoryAvailableMB == nil || *p.MemoryAvailableMB != 102400 ||
		p.MemoryCommittedMB == nil || *p.MemoryCommittedMB != 163840 || p.BalancerPressure == nil || *p.BalancerPressure != 45 {
		b, _ := json.MarshalIndent(p, "", " ")
		t.Errorf("perf = %s", b)
	}

	numa, _ := d.HostNUMA()
	if len(numa.Nodes) != 2 || numa.Nodes[0].Name != "Node 0" || numa.Nodes[0].MemoryMB != 130000 {
		t.Errorf("numa = %+v", numa)
	}

	hv, _ := d.HostHypervisor()
	if !hv.Present || hv.OSBuild != "20348" || hv.VBSStatus != "Running" || len(hv.VBSServices) != 1 || !strings.Contains(hv.VBSServices[0], "Code Integrity") || hv.SchedulerType != "" || len(hv.Notes) == 0 {
		t.Errorf("hypervisor = %+v", hv)
	}

	m, _ := d.HostMigration()
	if !m.Enabled || m.MaxActiveMigrations != 2 || m.Authentication != "Kerberos" || m.PerformanceOption != "Compression" || len(m.Networks) != 2 || m.Networks[0].Subnet != "10.0.0.0/24" {
		t.Errorf("migration = %+v", m)
	}

	c, _ := d.HostCluster()
	if !c.Clustered || c.Name != "HVCLUSTER" || len(c.Nodes) != 2 || c.Nodes[0].Name != "HV01" || c.Nodes[1].State != "Paused" || c.Nodes[0].Build != "10.0.20348" ||
		len(c.Groups) != 1 || c.Groups[0].State != "Online" || len(c.Resources) != 2 || c.Resources[1].VMID != strings.ToUpper(vm1) || c.Resources[1].State != "Online" {
		b, _ := json.MarshalIndent(c, "", " ")
		t.Errorf("cluster = %s", b)
	}
}

func TestHostNotClustered(t *testing.T) {
	q := fixtures() // no cluster rows at all: the namespace does not exist
	d := poll(t, q)
	c, err := d.HostCluster()
	if err != nil {
		t.Fatal(err)
	}
	if c.Clustered || len(c.Notes) != 1 || !strings.Contains(c.Notes[0], "not a member") {
		t.Errorf("cluster = %+v", c)
	}
	st, _ := d.HostStorage()
	for _, n := range st.Notes {
		if strings.Contains(n, "cluster") {
			t.Errorf("storage should not complain about a missing cluster: %v", st.Notes)
		}
	}
	if _, err := (&Data{}).Host(); !errors.Is(err, ErrNotReady) {
		t.Error("expected not ready")
	}
}
