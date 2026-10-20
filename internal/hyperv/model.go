package hyperv

import "time"

// Response types for the /vms endpoints. Every field is read from the cache.

// VM is one virtual machine as /vms lists it.
type VM struct {
	ID                 string     `json:"id" doc:"VM identifier (GUID). Stable across renames; use it in /vms/{id}."`
	Name               string     `json:"name" doc:"Display name."`
	State              string     `json:"state" doc:"Running, Off, Saved, Paused, Starting, Stopping, Saving, Pausing, Resuming, Snapshotting, FastSaved or FastSaving."`
	StateCode          int64      `json:"state_code" doc:"Raw EnabledState value."`
	OtherState         string     `json:"other_state,omitempty" doc:"Vendor state text when State is Other."`
	Health             string     `json:"health" doc:"OK, Degraded, Major Failure or Critical Failure."`
	HealthCode         int64      `json:"health_code" doc:"Raw HealthState value."`
	OperationalStatus  []string   `json:"operational_status" doc:"Operational status codes, decoded (OK, Creating Snapshot, Merging Disks ...)."`
	StatusDescriptions []string   `json:"status_descriptions" doc:"Status text as reported by Hyper-V."`
	UptimeSeconds      int64      `json:"uptime_seconds" doc:"Seconds the VM has been running; 0 when off."`
	LastStateChange    *time.Time `json:"last_state_change,omitempty" doc:"When the state last changed, UTC."`
	LastConfigChange   *time.Time `json:"last_config_change,omitempty" doc:"When the configuration last changed, UTC."`
	Created            *time.Time `json:"created,omitempty" doc:"When the VM was created, UTC."`
	ProcessID          int64      `json:"process_id" doc:"PID of the vmwp.exe worker process; 0 when not running."`
	NumaNodes          int64      `json:"numa_nodes,omitempty" doc:"Virtual NUMA nodes exposed to the VM."`
	Generation         int        `json:"generation,omitempty" doc:"1 or 2. Needs the heavy poll; 0 until it has run."`
	Version            string     `json:"version,omitempty" doc:"Configuration version, for example 9.0 or 10.0."`
	ReplicationState   string     `json:"replication_state" doc:"Disabled, Replicating, Critical ..."`
	ReplicationHealth  string     `json:"replication_health" doc:"Not applicable, OK, Warning or Critical."`
	ReplicationMode    string     `json:"replication_mode" doc:"None, Primary, Replica, Test replica or Extended replica."`
	Notes              []string   `json:"notes,omitempty" doc:"Notes set on the VM in Hyper-V Manager."`
}

// VMList is the /vms response.
type VMList struct {
	CollectedAt time.Time `json:"collected_at" doc:"When the state poll that produced this data finished, UTC."`
	Count       int       `json:"count" doc:"Number of VMs."`
	VMs         []VM      `json:"vms"`
	Notes       []string  `json:"notes,omitempty" doc:"Data sources that were unavailable, with the reason."`
}

// Guest is what the integration services report from inside the VM.
type Guest struct {
	Available           bool     `json:"available" doc:"False when the guest reports nothing (off, no integration services, or KVP disabled)."`
	Heartbeat           string   `json:"heartbeat" doc:"OK, No Contact, Lost Communication, Error, Dormant, Paused or Unknown."`
	HeartbeatCode       int64    `json:"heartbeat_code" doc:"Raw heartbeat status; 0 when unknown."`
	ApplicationState    string   `json:"application_state,omitempty" doc:"Application health reported by the guest, when available."`
	OSName              string   `json:"os_name,omitempty" doc:"Guest operating system name."`
	OSVersion           string   `json:"os_version,omitempty" doc:"Guest operating system version."`
	OSBuild             string   `json:"os_build,omitempty" doc:"Guest OS build number."`
	Hostname            string   `json:"hostname,omitempty" doc:"Fully qualified domain name reported by the guest."`
	IntegrationServices string   `json:"integration_services_version,omitempty" doc:"Version of the integration services in the guest."`
	Architecture        string   `json:"architecture,omitempty" doc:"Guest processor architecture."`
	IPv4                []string `json:"ipv4,omitempty" doc:"IPv4 addresses reported by the guest."`
	IPv6                []string `json:"ipv6,omitempty" doc:"IPv6 addresses reported by the guest."`
}

// VMSummary is the at-a-glance view that /vms/summary returns for each VM.
type VMSummary struct {
	ID                    string   `json:"id"`
	Name                  string   `json:"name"`
	State                 string   `json:"state"`
	Health                string   `json:"health"`
	UptimeSeconds         int64    `json:"uptime_seconds"`
	Generation            int      `json:"generation,omitempty"`
	Version               string   `json:"version,omitempty"`
	ProcessorCount        int64    `json:"processor_count" doc:"Virtual processors assigned."`
	ProcessorLoadPercent  *float64 `json:"processor_load_percent,omitempty" doc:"Average load across the VM's virtual processors; absent when off."`
	MemoryAssignedMB      int64    `json:"memory_assigned_mb" doc:"Memory currently assigned to the VM."`
	MemoryStartupMB       int64    `json:"memory_startup_mb,omitempty"`
	MemoryMinimumMB       int64    `json:"memory_minimum_mb,omitempty" doc:"Dynamic memory minimum; only when dynamic memory is on."`
	MemoryMaximumMB       int64    `json:"memory_maximum_mb,omitempty" doc:"Dynamic memory maximum; only when dynamic memory is on."`
	DynamicMemory         bool     `json:"dynamic_memory"`
	MemoryDemandMB        *int64   `json:"memory_demand_mb,omitempty" doc:"Memory the guest wants, from the dynamic memory balancer; absent when counters are unavailable."`
	MemoryPressurePercent *int64   `json:"memory_pressure_percent,omitempty" doc:"Current memory pressure; above 100 means the guest wants more than it has."`
	Guest                 Guest    `json:"guest"`
	SnapshotCount         int      `json:"snapshot_count" doc:"Checkpoints (snapshots) of this VM."`
	ReplicationState      string   `json:"replication_state"`
	ReplicationHealth     string   `json:"replication_health"`
	Notes                 []string `json:"notes,omitempty" doc:"Why some fields are missing, if any."`
}

// SummaryList is the /vms/summary response.
type SummaryList struct {
	CollectedAt time.Time   `json:"collected_at"`
	SettingsAt  *time.Time  `json:"settings_at,omitempty" doc:"When the heavy poll last completed; absent before the first one."`
	Count       int         `json:"count"`
	VMs         []VMSummary `json:"vms"`
	Notes       []string    `json:"notes,omitempty"`
}

// VMConfig is the realized configuration (Msvm_VirtualSystemSettingData).
type VMConfig struct {
	VMID                   string     `json:"vm_id"`
	VMName                 string     `json:"vm_name"`
	CollectedAt            time.Time  `json:"collected_at"`
	Generation             int        `json:"generation"`
	Version                string     `json:"version" doc:"Configuration version."`
	ConfigurationID        string     `json:"configuration_id" doc:"GUID of the configuration; equals the VM ID for the realized configuration."`
	ConfigurationFile      string     `json:"configuration_file,omitempty" doc:"Path of the .vmcx file."`
	ConfigurationDataRoot  string     `json:"configuration_data_root,omitempty"`
	SnapshotDataRoot       string     `json:"snapshot_data_root,omitempty"`
	SwapFileDataRoot       string     `json:"swap_file_data_root,omitempty"`
	GuestStateDataRoot     string     `json:"guest_state_data_root,omitempty"`
	GuestStateFile         string     `json:"guest_state_file,omitempty"`
	LogDataRoot            string     `json:"log_data_root,omitempty"`
	SuspendDataRoot        string     `json:"suspend_data_root,omitempty"`
	Created                *time.Time `json:"created,omitempty"`
	AutomaticStartAction   string     `json:"automatic_start_action" doc:"None, Restart if previously running, or Always start."`
	AutomaticStartDelaySec int64      `json:"automatic_start_delay_seconds"`
	AutomaticStopAction    string     `json:"automatic_stop_action" doc:"Turn off, Save, or Shut down."`
	AutomaticRecoveryAct   string     `json:"automatic_recovery_action,omitempty" doc:"What happens when the VM crashes: None, Restart or Revert to snapshot."`
	AutomaticCheckpoints   *bool      `json:"automatic_checkpoints,omitempty"`
	CheckpointType         string     `json:"checkpoint_type,omitempty" doc:"Disabled, Production, ProductionOnly or Standard."`
	BIOSGUID               string     `json:"bios_guid,omitempty"`
	BIOSSerialNumber       string     `json:"bios_serial_number,omitempty"`
	BaseBoardSerialNumber  string     `json:"base_board_serial_number,omitempty"`
	ChassisSerialNumber    string     `json:"chassis_serial_number,omitempty"`
	ChassisAssetTag        string     `json:"chassis_asset_tag,omitempty"`
	BootOrder              []string   `json:"boot_order,omitempty" doc:"Generation 1 boot device order (Floppy, CD, IDE, Network)."`
	BootSourceOrder        []string   `json:"boot_source_order,omitempty" doc:"Generation 2 boot entries, as device paths."`
	NetworkBootProtocol    string     `json:"network_boot_protocol,omitempty" doc:"IPv4 or IPv6 (generation 2)."`
	SecureBoot             *bool      `json:"secure_boot,omitempty" doc:"Reported by this class on older hosts only; see /vms/{id}/security (phase 4) for the full picture."`
	NumLock                bool       `json:"bios_numlock"`
	VirtualNUMA            bool       `json:"virtual_numa_enabled"`
	ConsoleMode            string     `json:"console_mode,omitempty"`
	EnhancedSessionMode    string     `json:"enhanced_session_transport,omitempty" doc:"VMBus or HvSocket."`
	LockOnDisconnect       bool       `json:"lock_on_disconnect"`
	IncrementalBackup      bool       `json:"incremental_backup_enabled"`
	GuestCacheTypes        int64      `json:"guest_controlled_cache_types,omitempty"`
	DebugPortEnabled       bool       `json:"debug_port_enabled"`
	DebugPort              int64      `json:"debug_port,omitempty"`
	IsSaved                bool       `json:"is_saved" doc:"A saved state exists for the VM."`
	Notes                  []string   `json:"notes,omitempty"`
	Description            string     `json:"description,omitempty" doc:"Notes set on the VM in Hyper-V Manager."`
}

// VProc is one virtual processor's load.
type VProc struct {
	Index       int64   `json:"index"`
	LoadPercent float64 `json:"load_percent"`
}

// VMProcessor is the /vms/{id}/processor response.
type VMProcessor struct {
	VMID                   string    `json:"vm_id"`
	VMName                 string    `json:"vm_name"`
	CollectedAt            time.Time `json:"collected_at"`
	Count                  int64     `json:"count" doc:"Virtual processors configured."`
	ReservePercent         float64   `json:"reserve_percent" doc:"Share of the host's processors reserved for this VM, 0 to 100."`
	LimitPercent           float64   `json:"limit_percent" doc:"Maximum share of the host's processors this VM may use, 0 to 100."`
	RelativeWeight         int64     `json:"relative_weight" doc:"Scheduling weight, default 100."`
	HwThreadsPerCore       int64     `json:"hw_threads_per_core" doc:"SMT threads per core exposed to the guest; 0 means inherit from the host."`
	MaxProcessorsPerNUMA   int64     `json:"max_processors_per_numa_node,omitempty"`
	MaxNUMANodesPerSocket  int64     `json:"max_numa_nodes_per_socket,omitempty"`
	CompatibilityMode      bool      `json:"compatibility_mode" doc:"Processor compatibility for migration (limits CPUID features)."`
	NestedVirtualization   bool      `json:"nested_virtualization" doc:"Virtualization extensions exposed to the guest."`
	HostResourceProtection bool      `json:"host_resource_protection"`
	PerfmonPMU             bool      `json:"perfmon_pmu,omitempty"`
	LoadPercent            *float64  `json:"load_percent,omitempty" doc:"Average load across virtual processors; absent when off."`
	VirtualProcessors      []VProc   `json:"virtual_processors,omitempty" doc:"Per virtual processor load; present while running."`
	Notes                  []string  `json:"notes,omitempty"`
}

// VMMemory is the /vms/{id}/memory response.
type VMMemory struct {
	VMID             string    `json:"vm_id"`
	VMName           string    `json:"vm_name"`
	CollectedAt      time.Time `json:"collected_at"`
	StartupMB        int64     `json:"startup_mb"`
	DynamicMemory    bool      `json:"dynamic_memory"`
	MinimumMB        int64     `json:"minimum_mb,omitempty"`
	MaximumMB        int64     `json:"maximum_mb,omitempty"`
	BufferPercent    int64     `json:"buffer_percent,omitempty" doc:"Memory buffer kept free for the guest, dynamic memory only."`
	Weight           int64     `json:"weight" doc:"Memory weight, 0 to 10000."`
	AssignedMB       int64     `json:"assigned_mb" doc:"Memory currently assigned to the VM; 0 when off."`
	DemandMB         *int64    `json:"demand_mb,omitempty" doc:"Memory demand from the dynamic memory balancer."`
	PressurePercent  *int64    `json:"pressure_percent,omitempty"`
	AveragePressure  *int64    `json:"average_pressure_percent,omitempty"`
	GuestVisibleMB   *int64    `json:"guest_visible_mb,omitempty" doc:"Physical memory the guest sees."`
	GuestAvailableMB *int64    `json:"guest_available_mb,omitempty" doc:"Memory the guest reports as available."`
	AddedMB          *int64    `json:"added_mb,omitempty" doc:"Memory hot-added by the balancer since start."`
	RemovedMB        *int64    `json:"removed_mb,omitempty" doc:"Memory removed by the balancer since start."`
	Notes            []string  `json:"notes,omitempty"`
}

// Drive is a disk, DVD or floppy drive attached to a controller.
type Drive struct {
	Type       string   `json:"type" doc:"Hard Drive, DVD Drive, Physical Disk, Floppy Drive or the raw sub type."`
	Location   string   `json:"location" doc:"Slot on the controller (AddressOnParent)."`
	Name       string   `json:"name,omitempty"`
	Path       string   `json:"path,omitempty" doc:"VHD/VHDX/ISO path, or the physical disk path for pass-through."`
	Media      string   `json:"media" doc:"VHD, ISO, Floppy image, Physical or None."`
	MediaName  string   `json:"media_name,omitempty"`
	IOPSMin    int64    `json:"iops_minimum,omitempty" doc:"QoS minimum IOPS, 0 when not set."`
	IOPSMax    int64    `json:"iops_maximum,omitempty" doc:"QoS maximum IOPS, 0 when not set."`
	DiskID     string   `json:"disk_id,omitempty" doc:"ID to use with /vms/{id}/disks/{diskId}/state (phase 4)."`
	InstanceID string   `json:"instance_id,omitempty" doc:"Drive InstanceID."`
	Notes      []string `json:"notes,omitempty"`
}

// Controller is an IDE or SCSI controller with its drives.
type Controller struct {
	Type       string  `json:"type" doc:"IDE, SCSI, Fibre Channel or the raw sub type."`
	Name       string  `json:"name,omitempty"`
	Number     string  `json:"number,omitempty" doc:"Controller number (IDE 0/1, SCSI 0..3)."`
	InstanceID string  `json:"instance_id,omitempty"`
	Drives     []Drive `json:"drives"`
}

// VMDisks is the /vms/{id}/disks response.
type VMDisks struct {
	VMID        string       `json:"vm_id"`
	VMName      string       `json:"vm_name"`
	CollectedAt time.Time    `json:"collected_at"`
	Controllers []Controller `json:"controllers"`
	Notes       []string     `json:"notes,omitempty"`
}

// Adapter is one network adapter of a VM.
type Adapter struct {
	Name            string   `json:"name"`
	Type            string   `json:"type" doc:"Synthetic or Legacy (emulated)."`
	MACAddress      string   `json:"mac_address,omitempty"`
	StaticMAC       bool     `json:"static_mac"`
	Connected       bool     `json:"connected" doc:"Attached to a switch and enabled."`
	Switch          string   `json:"switch,omitempty" doc:"Name of the virtual switch, when connected."`
	SwitchID        string   `json:"switch_id,omitempty"`
	VLANMode        string   `json:"vlan_mode,omitempty" doc:"Access, Trunk, Private or Isolated; absent when no VLAN setting."`
	VLANID          int64    `json:"vlan_id,omitempty" doc:"Access VLAN, or native VLAN in trunk mode."`
	TrunkVLANs      []int64  `json:"trunk_vlans,omitempty"`
	DeviceNaming    bool     `json:"device_naming"`
	ClusterMonitor  bool     `json:"cluster_monitored" doc:"Protected network: the cluster moves the VM if this network fails."`
	GuestIPs        []string `json:"guest_ips,omitempty" doc:"Addresses reported by the guest for this adapter."`
	GuestSubnets    []string `json:"guest_subnets,omitempty"`
	GuestGateways   []string `json:"guest_gateways,omitempty"`
	GuestDNSServers []string `json:"guest_dns_servers,omitempty"`
	GuestDHCP       *bool    `json:"guest_dhcp,omitempty"`
	InstanceID      string   `json:"instance_id,omitempty"`
}

// VMNetwork is the /vms/{id}/network response.
type VMNetwork struct {
	VMID        string    `json:"vm_id"`
	VMName      string    `json:"vm_name"`
	CollectedAt time.Time `json:"collected_at"`
	Adapters    []Adapter `json:"adapters"`
	Notes       []string  `json:"notes,omitempty"`
}

// VMDetail is the /vms/{id} response: everything known about one VM.
type VMDetail struct {
	VM          VM             `json:"vm"`
	Summary     VMSummary      `json:"summary"`
	Config      *VMConfig      `json:"config,omitempty" doc:"Absent until the heavy poll has run."`
	Processor   *VMProcessor   `json:"processor,omitempty"`
	Memory      *VMMemory      `json:"memory,omitempty"`
	Disks       *VMDisks       `json:"disks,omitempty"`
	Network     *VMNetwork     `json:"network,omitempty"`
	Integration *VMIntegration `json:"integration,omitempty"`
	Checkpoints *VMCheckpoints `json:"checkpoints,omitempty"`
	Replication *VMReplication `json:"replication,omitempty"`
	Security    *VMSecurity    `json:"security,omitempty"`
	Devices     *VMDevices     `json:"devices,omitempty"`
	Notes       []string       `json:"notes,omitempty"`
}

// SourceStatus describes one WMI query the poller runs.
type SourceStatus struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Query     string `json:"query"`
	Optional  bool   `json:"optional" doc:"True for data that may not exist on every host; a failure is a note, not a problem."`
	Rows      int    `json:"rows"`
	Error     string `json:"error,omitempty"`
}

// PollStatus is the state of one poll loop.
type PollStatus struct {
	IntervalSeconds int            `json:"interval_seconds"`
	LastRun         *time.Time     `json:"last_run,omitempty"`
	LastDurationMs  int64          `json:"last_duration_ms"`
	Runs            int64          `json:"runs"`
	Sources         []SourceStatus `json:"sources"`
}

// Status is the /status response.
type Status struct {
	WMIAvailable bool       `json:"wmi_available" doc:"False on a build or host without WMI; every VM endpoint is then empty."`
	Warnings     []string   `json:"warnings,omitempty" doc:"Start-up problems, such as Hyper-V WMI refusing access to the service account. Fix these first."`
	VMCount      int        `json:"vm_count"`
	State        PollStatus `json:"state_poll" doc:"The fast loop: VM state, load, heartbeat."`
	Settings     PollStatus `json:"settings_poll" doc:"The slow loop: configuration, disks, network."`
}
