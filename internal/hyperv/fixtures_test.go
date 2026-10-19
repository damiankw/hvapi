package hyperv

import (
	"context"
	"strings"

	"github.com/damiankw/hvapi/internal/wmi"
)

const (
	vm1  = "11111111-1111-1111-1111-111111111111" // running, generation 2
	vm2  = "22222222-2222-2222-2222-222222222222" // off, generation 1
	snap = "33333333-3333-3333-3333-333333333333" // snapshot of vm1
	sw1  = "44444444-4444-4444-4444-444444444444"
)

const ns = `\\HOST\root\virtualization\v2:`

func path(class, key, value string) string {
	return ns + class + `.` + key + `="` + strings.ReplaceAll(value, `\`, `\\`) + `"`
}

// fakeQuerier answers by class name; unknown classes fail like a missing class.
type fakeQuerier struct {
	rows   map[string][]wmi.Row
	fail   map[string]error
	called []string
}

func (f *fakeQuerier) Query(ctx context.Context, namespace, wql string) ([]wmi.Row, error) {
	f.called = append(f.called, wql)
	for class, rows := range f.rows {
		if strings.Contains(wql, "FROM "+class+" ") || strings.HasSuffix(wql, "FROM "+class) {
			if err := f.fail[class]; err != nil {
				return nil, err
			}
			return rows, nil
		}
	}
	return nil, wmi.ErrInvalidClass
}

func rasd(vm, pool, sub, name, parent string, extra wmi.Row) wmi.Row {
	id := "Microsoft:" + vm + `\` + pool
	r := wmi.Row{
		wmi.PathKey:       path("Msvm_ResourceAllocationSettingData", "InstanceID", id),
		"InstanceID":      id,
		"ResourceSubType": sub,
		"ElementName":     name,
	}
	if parent != "" {
		r["Parent"] = parent
	}
	for k, v := range extra {
		r[k] = v
	}
	return r
}

func fixtures() *fakeQuerier {
	vssd1 := path("Msvm_VirtualSystemSettingData", "InstanceID", "Microsoft:"+vm1)
	vssd2 := path("Msvm_VirtualSystemSettingData", "InstanceID", "Microsoft:"+vm2)
	vssdSnap := path("Msvm_VirtualSystemSettingData", "InstanceID", "Microsoft:"+vm1+`\`+snap)

	scsi := rasd(vm1, "D4D5B2E0-0001-0001-0001-000000000001", subSCSIController, "SCSI Controller", "", nil)
	disk := rasd(vm1, "D4D5B2E0-0001-0001-0001-000000000001\\0\\0\\D", subDiskDrive, "Hard Drive", scsi[wmi.PathKey].(string), wmi.Row{"AddressOnParent": "0"})
	dvd := rasd(vm1, "D4D5B2E0-0001-0001-0001-000000000001\\1\\0\\D", subDVDDrive, "DVD Drive", scsi[wmi.PathKey].(string), wmi.Row{"AddressOnParent": "1"})
	ide0 := rasd(vm2, "83F8638B-8DCA-4152-9EDA-2CA8B33039B4", subIDEController, "IDE Controller", "", wmi.Row{"Address": "0"})
	ide1 := rasd(vm2, "83F8638B-8DCA-4152-9EDA-2CA8B33039B5", subIDEController, "IDE Controller", "", wmi.Row{"Address": "1"})
	disk2 := rasd(vm2, "83F8638B-8DCA-4152-9EDA-2CA8B33039B4\\0\\0\\D", subDiskDrive, "Hard Drive", ide0[wmi.PathKey].(string), wmi.Row{"AddressOnParent": "0"})
	phys := rasd(vm2, "83F8638B-8DCA-4152-9EDA-2CA8B33039B5\\0\\0\\D", subPhysicalDisk, "Hard Drive", ide1[wmi.PathKey].(string), wmi.Row{
		"AddressOnParent": "0",
		"HostResource":    []any{path("Msvm_DiskDrive", "DeviceID", `Microsoft:AAAA\2`)},
	})
	// A drive that belongs to the snapshot, not the realized configuration.
	snapDisk := rasd(vm1, snap+"\\D4D5B2E0-0001-0001-0001-000000000001\\0\\0\\D", subDiskDrive, "Hard Drive", scsi[wmi.PathKey].(string), wmi.Row{"AddressOnParent": "5"})

	vhd := wmi.Row{
		wmi.PathKey:       path("Msvm_StorageAllocationSettingData", "InstanceID", "Microsoft:"+vm1+`\D4D5B2E0-0001-0001-0001-000000000001\0\0\D\L`),
		"InstanceID":      "Microsoft:" + vm1 + `\D4D5B2E0-0001-0001-0001-000000000001\0\0\D\L`,
		"ResourceSubType": subVirtualHardDisk,
		"ElementName":     "Hard Disk Image",
		"Parent":          disk[wmi.PathKey],
		"HostResource":    []any{`C:\VMs\vm1\vm1.vhdx`},
		"IOPSLimit":       "500",
		"IOPSReservation": "0",
	}
	iso := wmi.Row{
		wmi.PathKey:       path("Msvm_StorageAllocationSettingData", "InstanceID", "Microsoft:"+vm1+`\D4D5B2E0-0001-0001-0001-000000000001\1\0\D\L`),
		"InstanceID":      "Microsoft:" + vm1 + `\D4D5B2E0-0001-0001-0001-000000000001\1\0\D\L`,
		"ResourceSubType": subVirtualDVD,
		"ElementName":     "ISO Disk Image",
		"Parent":          dvd[wmi.PathKey],
		"HostResource":    []any{`C:\ISO\install.iso`},
	}
	vhd2 := wmi.Row{
		wmi.PathKey:       path("Msvm_StorageAllocationSettingData", "InstanceID", "Microsoft:"+vm2+`\83F8638B-8DCA-4152-9EDA-2CA8B33039B4\0\0\D\L`),
		"InstanceID":      "Microsoft:" + vm2 + `\83F8638B-8DCA-4152-9EDA-2CA8B33039B4\0\0\D\L`,
		"ResourceSubType": subVirtualHardDisk,
		"Parent":          disk2[wmi.PathKey],
		"HostResource":    []any{`D:\VMs\vm2.vhd`},
	}

	nicID := "Microsoft:" + vm1 + `\5A5A5A5A-0000-0000-0000-000000000001`
	nic := wmi.Row{
		wmi.PathKey:           path("Msvm_SyntheticEthernetPortSettingData", "InstanceID", nicID),
		"InstanceID":          nicID,
		"ResourceSubType":     subSyntheticEthernet,
		"ElementName":         "Network Adapter",
		"Address":             "00155D0A0B0C",
		"StaticMacAddress":    false,
		"DeviceNamingEnabled": true,
		"ClusterMonitored":    true,
	}
	connID := nicID + `\C`
	conn := wmi.Row{
		wmi.PathKey:       path("Msvm_EthernetPortAllocationSettingData", "InstanceID", connID),
		"InstanceID":      connID,
		"ResourceSubType": subEthernetConnection,
		"Parent":          nic[wmi.PathKey],
		"HostResource":    []any{ns + `Msvm_VirtualEthernetSwitch.CreationClassName="Msvm_VirtualEthernetSwitch",Name="` + sw1 + `"`},
		"EnabledState":    int32(2),
	}
	vlan := wmi.Row{
		"InstanceID":    connID + `\7B7B7B7B-0000-0000-0000-000000000001`,
		"OperationMode": int32(1),
		"AccessVlanId":  int32(42),
	}
	guestNet := wmi.Row{
		"InstanceID":      "Microsoft:GuestNetwork\\" + vm1 + "\\5A5A5A5A-0000-0000-0000-000000000001",
		"IPAddresses":     []any{"10.0.0.50", "fe80::1"},
		"Subnets":         []any{"255.255.255.0", "/64"},
		"DefaultGateways": []any{"10.0.0.1"},
		"DNSServers":      []any{"10.0.0.2"},
		"DHCPEnabled":     true,
	}
	legacyID := "Microsoft:" + vm2 + `\5A5A5A5A-0000-0000-0000-000000000002`
	legacy := wmi.Row{
		wmi.PathKey:       path("Msvm_EmulatedEthernetPortSettingData", "InstanceID", legacyID),
		"InstanceID":      legacyID,
		"ResourceSubType": subEmulatedEthernet,
		"ElementName":     "Legacy Network Adapter",
		"Address":         "00155D0A0B0D",
	}

	kvpItems := []any{
		`<INSTANCE CLASSNAME="Msvm_KvpExchangeDataItem"><PROPERTY NAME="Data" TYPE="string"><VALUE>Windows Server 2022 Datacenter</VALUE></PROPERTY><PROPERTY NAME="Name" TYPE="string"><VALUE>OSName</VALUE></PROPERTY><PROPERTY NAME="Source" TYPE="uint16"><VALUE>2</VALUE></PROPERTY></INSTANCE>`,
		`<INSTANCE CLASSNAME="Msvm_KvpExchangeDataItem"><PROPERTY NAME="Data" TYPE="string"><VALUE>10.0.0.50;</VALUE></PROPERTY><PROPERTY NAME="Name" TYPE="string"><VALUE>NetworkAddressIPv4</VALUE></PROPERTY></INSTANCE>`,
		`<INSTANCE CLASSNAME="Msvm_KvpExchangeDataItem"><PROPERTY NAME="Data" TYPE="string"><VALUE>vm1.example.local</VALUE></PROPERTY><PROPERTY NAME="Name" TYPE="string"><VALUE>FullyQualifiedDomainName</VALUE></PROPERTY></INSTANCE>`,
	}

	procSD := func(vm string, n int64) wmi.Row {
		id := "Microsoft:" + vm + `\b637f346-6a0e-4dec-af52-bd70cb80a21d\0`
		return wmi.Row{wmi.PathKey: path("Msvm_ProcessorSettingData", "InstanceID", id), "InstanceID": id,
			"VirtualQuantity": n, "Reservation": "0", "Limit": "100000", "Weight": int32(100), "HwThreadsPerCore": "0",
			"ExposeVirtualizationExtensions": false, "LimitProcessorFeatures": false, "EnableHostResourceProtection": true}
	}
	memSD := func(vm string, dynamic bool) wmi.Row {
		id := "Microsoft:" + vm + `\4764334d-e001-4176-82ee-5594ec9b530e\0`
		return wmi.Row{wmi.PathKey: path("Msvm_MemorySettingData", "InstanceID", id), "InstanceID": id,
			"VirtualQuantity": "2048", "DynamicMemoryEnabled": dynamic, "Reservation": "512", "Limit": "8192", "TargetMemoryBuffer": int32(20), "Weight": int32(5000)}
	}
	snapProc := procSD(vm1, 99)
	snapProc["InstanceID"] = "Microsoft:" + vm1 + `\` + snap + `\b637f346-6a0e-4dec-af52-bd70cb80a21d\0`
	snapProc[wmi.PathKey] = path("Msvm_ProcessorSettingData", "InstanceID", snapProc["InstanceID"].(string))

	comp := func(group, part string) wmi.Row { return wmi.Row{"GroupComponent": group, "PartComponent": part} }

	return &fakeQuerier{rows: map[string][]wmi.Row{
		"Msvm_ComputerSystem": {
			{"Name": vm1, "ElementName": "Web Server", "EnabledState": int32(2), "HealthState": int32(5),
				"OperationalStatus": []any{int32(2)}, "StatusDescriptions": []any{"Operating normally"},
				"OnTimeInMilliseconds": "3600000", "TimeOfLastStateChange": "20240102030405.000000+000",
				"InstallDate": "20230601120000.000000+600", "ProcessID": int32(4242), "NumberOfNumaNodes": int32(1),
				"ReplicationState": int32(0), "ReplicationHealth": int32(0), "ReplicationMode": int32(0)},
			{"Name": vm2, "ElementName": "Archive", "EnabledState": int32(3), "HealthState": int32(5),
				"OperationalStatus": []any{int32(2)}, "OnTimeInMilliseconds": "0", "ProcessID": int32(0),
				"ReplicationState": int32(3), "ReplicationHealth": int32(2), "ReplicationMode": int32(1)},
		},
		"Msvm_Processor": {
			{"SystemName": vm1, "DeviceID": "Microsoft:" + vm1 + `\0`, "LoadPercentage": int32(10)},
			{"SystemName": vm1, "DeviceID": "Microsoft:" + vm1 + `\1`, "LoadPercentage": int32(30)},
		},
		"Msvm_Memory": {
			{"SystemName": vm1, "NumberOfBlocks": "4096", "BlockSize": "1048576"},
			{"SystemName": "HOST", "NumberOfBlocks": "65536", "BlockSize": "1048576"},
		},
		"Msvm_HeartbeatComponent": {
			{"SystemName": vm1, "OperationalStatus": []any{int32(2), int32(2)}},
			{"SystemName": vm2, "OperationalStatus": []any{int32(12)}},
		},
		"Win32_PerfFormattedData_BalancerStats_HyperVDynamicMemoryVM": {
			{"Name": "Web Server", "CurrentPressure": "80", "AveragePressure": "75", "PhysicalMemory": "4096", "GuestVisiblePhysicalMemory": "4096", "AddedMemory": "2048", "RemovedMemory": "0"},
		},
		"Msvm_VirtualSystemSettingData": {
			{wmi.PathKey: vssd1, "InstanceID": "Microsoft:" + vm1, "VirtualSystemIdentifier": vm1, "ConfigurationID": vm1,
				"VirtualSystemType": typeRealized, "VirtualSystemSubType": "Microsoft:Hyper-V:SubType:2", "Version": "10.0",
				"ConfigurationFile": `C:\VMs\vm1\vm1.vmcx`, "ConfigurationDataRoot": `C:\VMs\vm1`, "SnapshotDataRoot": `C:\VMs\vm1\Snapshots`,
				"CreationTime": "20230601120000.000000+600", "AutomaticStartupAction": int32(3), "AutomaticStartupActionDelay": "00000000000030.000000:000",
				"AutomaticShutdownAction": int32(4), "AutomaticRecoveryAction": int32(3), "AutomaticSnapshotsEnabled": false, "UserSnapshotType": int32(3),
				"BIOSGUID": "{ABCDEF00-0000-0000-0000-000000000000}", "BootSourceOrder": []any{`\\HOST\root\virtualization\v2:Msvm_LogicalIdentity...`},
				"NetworkBootPreferredProtocol": int32(4096), "VirtualNumaEnabled": true, "EnhancedSessionTransportType": int32(0),
				"LockOnDisconnect": false, "IncrementalBackupEnabled": true, "Notes": []any{"Production web tier"}},
			{wmi.PathKey: vssd2, "InstanceID": "Microsoft:" + vm2, "VirtualSystemIdentifier": vm2, "ConfigurationID": vm2,
				"VirtualSystemType": typeRealized, "VirtualSystemSubType": "Microsoft:Hyper-V:SubType:1", "Version": "9.0",
				"AutomaticStartupAction": int32(2), "AutomaticShutdownAction": int32(3), "BootOrder": []any{int32(2), int32(1), int32(0), int32(3)}, "BIOSNumLock": true, "SecureBootEnabled": false},
			{wmi.PathKey: vssdSnap, "InstanceID": "Microsoft:" + vm1 + `\` + snap, "VirtualSystemIdentifier": vm1, "ConfigurationID": snap,
				"VirtualSystemType": "Microsoft:Hyper-V:Snapshot:Realized", "VirtualSystemSubType": "Microsoft:Hyper-V:SubType:2"},
			{"InstanceID": "Microsoft:99999999-9999-9999-9999-999999999999", "VirtualSystemIdentifier": "99999999-9999-9999-9999-999999999999",
				"VirtualSystemType": "Microsoft:Hyper-V:System:Planned"},
		},
		"Msvm_VirtualSystemSettingDataComponent": {
			comp(vssd1, procSD(vm1, 2)[wmi.PathKey].(string)), comp(vssd1, memSD(vm1, true)[wmi.PathKey].(string)),
			comp(vssd1, scsi[wmi.PathKey].(string)), comp(vssd1, disk[wmi.PathKey].(string)), comp(vssd1, dvd[wmi.PathKey].(string)),
			comp(vssd1, vhd[wmi.PathKey].(string)), comp(vssd1, iso[wmi.PathKey].(string)),
			comp(vssd1, nic[wmi.PathKey].(string)), comp(vssd1, conn[wmi.PathKey].(string)),
			comp(vssd2, procSD(vm2, 1)[wmi.PathKey].(string)), comp(vssd2, memSD(vm2, false)[wmi.PathKey].(string)),
			comp(vssd2, ide0[wmi.PathKey].(string)), comp(vssd2, ide1[wmi.PathKey].(string)), comp(vssd2, disk2[wmi.PathKey].(string)),
			comp(vssd2, phys[wmi.PathKey].(string)), comp(vssd2, vhd2[wmi.PathKey].(string)), comp(vssd2, legacy[wmi.PathKey].(string)),
			comp(vssdSnap, snapDisk[wmi.PathKey].(string)), comp(vssdSnap, snapProc[wmi.PathKey].(string)),
		},
		"Msvm_ProcessorSettingData":              {procSD(vm1, 2), procSD(vm2, 1), snapProc},
		"Msvm_MemorySettingData":                 {memSD(vm1, true), memSD(vm2, false)},
		"Msvm_ResourceAllocationSettingData":     {scsi, disk, dvd, ide0, ide1, disk2, phys, snapDisk},
		"Msvm_StorageAllocationSettingData":      {vhd, iso, vhd2},
		"Msvm_SyntheticEthernetPortSettingData":  {nic},
		"Msvm_EmulatedEthernetPortSettingData":   {legacy},
		"Msvm_EthernetPortAllocationSettingData": {conn},
		"Msvm_EthernetSwitchPortVlanSettingData": {vlan},
		"Msvm_GuestNetworkAdapterConfiguration":  {guestNet},
		"Msvm_VirtualEthernetSwitch":             {{"Name": sw1, "ElementName": "External"}},
		"Msvm_KvpExchangeComponent":              {{"SystemName": vm1, "GuestIntrinsicExchangeItems": kvpItems}},
	}, fail: map[string]error{}}
}
