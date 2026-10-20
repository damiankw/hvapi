package hyperv

import "time"

// Response types for the /host and /switches endpoints (phase 4).

// HostInfo is the /host response.
type HostInfo struct {
	CollectedAt       time.Time  `json:"collected_at"`
	Name              string     `json:"name" doc:"Computer name."`
	Domain            string     `json:"domain,omitempty"`
	Manufacturer      string     `json:"manufacturer,omitempty"`
	Model             string     `json:"model,omitempty"`
	OS                string     `json:"os" doc:"Operating system name, for example Microsoft Windows Server 2022 Datacenter."`
	OSVersion         string     `json:"os_version" doc:"Version number such as 10.0.20348."`
	OSBuild           string     `json:"os_build"`
	OSArchitecture    string     `json:"os_architecture,omitempty"`
	InstallDate       *time.Time `json:"install_date,omitempty"`
	LastBoot          *time.Time `json:"last_boot,omitempty" doc:"When the host last booted, UTC."`
	UptimeSeconds     int64      `json:"uptime_seconds"`
	HostTime          *time.Time `json:"host_time,omitempty" doc:"The host's clock when polled, UTC; compare with your own for drift."`
	Sockets           int64      `json:"sockets" doc:"Physical processor packages."`
	Cores             int64      `json:"cores"`
	LogicalProcessors int64      `json:"logical_processors"`
	ProcessorModel    string     `json:"processor_model,omitempty"`
	ProcessorMHz      int64      `json:"processor_mhz,omitempty"`
	VirtualizationFW  *bool      `json:"virtualization_firmware_enabled,omitempty" doc:"Whether virtualization is enabled in firmware, as reported by the processor class."`
	SLAT              *bool      `json:"slat,omitempty" doc:"Second-level address translation available."`
	MemoryTotalMB     int64      `json:"memory_total_mb"`
	MemoryFreeMB      int64      `json:"memory_free_mb" doc:"Free physical memory on the host when polled."`
	HypervisorPresent bool       `json:"hypervisor_present"`
	VMCount           int        `json:"vm_count"`
	VMRunning         int        `json:"vms_running"`
	DefaultVMPath     string     `json:"default_vm_path,omitempty" doc:"Where new VM configurations go."`
	DefaultVHDPath    string     `json:"default_vhd_path,omitempty"`
	MACRangeStart     string     `json:"mac_range_start,omitempty"`
	MACRangeEnd       string     `json:"mac_range_end,omitempty"`
	NUMASpanning      *bool      `json:"numa_spanning,omitempty"`
	EnhancedSession   *bool      `json:"enhanced_session_mode,omitempty"`
	Notes             []string   `json:"notes,omitempty"`
}

// ServiceInfo is one Windows service.
type ServiceInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	State       string `json:"state" doc:"Running, Stopped, Start Pending ..."`
	StartMode   string `json:"start_mode" doc:"Auto, Manual or Disabled."`
	Status      string `json:"status,omitempty"`
	ProcessID   int64  `json:"process_id"`
	Account     string `json:"account,omitempty" doc:"The account the service runs as."`
	ExitCode    int64  `json:"exit_code"`
	Expected    bool   `json:"expected_running" doc:"True for the services Hyper-V needs: all should be Running."`
}

// HostServices is the /host/services response.
type HostServices struct {
	CollectedAt time.Time     `json:"collected_at"`
	AllRunning  bool          `json:"all_running" doc:"True when every core Hyper-V service is running."`
	Services    []ServiceInfo `json:"services"`
	Notes       []string      `json:"notes,omitempty"`
}

// Volume is one host volume.
type Volume struct {
	DriveLetter     string   `json:"drive_letter,omitempty"`
	Label           string   `json:"label,omitempty"`
	Path            string   `json:"path" doc:"Volume GUID path, or the CSV mount point."`
	FileSystem      string   `json:"file_system,omitempty"`
	SizeMB          int64    `json:"size_mb"`
	FreeMB          int64    `json:"free_mb"`
	FreePercent     float64  `json:"free_percent"`
	Health          string   `json:"health,omitempty" doc:"Healthy, Warning, Unhealthy or Unknown."`
	Type            string   `json:"type" doc:"Fixed, Removable, CD-ROM, Network, RAM disk, Cluster Shared Volume, or Unknown."`
	ClusterShared   bool     `json:"cluster_shared"`
	CSVOwner        string   `json:"csv_owner_node,omitempty"`
	CSVState        string   `json:"csv_state,omitempty"`
	VMFiles         int      `json:"vm_files" doc:"How many VM configuration roots and virtual disks known to hvapi live on this volume."`
	VMs             []string `json:"vms,omitempty" doc:"Names of VMs with files on this volume."`
	ClusterResource string   `json:"cluster_resource,omitempty" doc:"For a CSV, the cluster disk resource name."`
}

// HostStorage is the /host/storage response.
type HostStorage struct {
	CollectedAt time.Time `json:"collected_at"`
	Volumes     []Volume  `json:"volumes"`
	Notes       []string  `json:"notes,omitempty"`
}

// HostAdapter is one physical or host-side network adapter.
type HostAdapter struct {
	Name           string `json:"name"`
	Description    string `json:"description,omitempty" doc:"Driver or hardware description."`
	MACAddress     string `json:"mac_address,omitempty"`
	Status         string `json:"status" doc:"Up, Down, Disconnected, Disabled or the raw status."`
	LinkSpeedMbps  int64  `json:"link_speed_mbps,omitempty"`
	MTU            int64  `json:"mtu,omitempty"`
	Virtual        bool   `json:"virtual" doc:"True for adapters created by Hyper-V or teaming rather than physical hardware."`
	DriverVersion  string `json:"driver_version,omitempty"`
	BoundToSwitch  string `json:"bound_to_switch,omitempty" doc:"Name of the virtual switch using this adapter, if any."`
	InterfaceIndex int64  `json:"interface_index,omitempty"`
}

// HostNetwork is the /host/network response.
type HostNetwork struct {
	CollectedAt time.Time     `json:"collected_at"`
	Adapters    []HostAdapter `json:"adapters"`
	Notes       []string      `json:"notes,omitempty"`
}

// LPCounter is one logical processor's hypervisor counters.
type LPCounter struct {
	Name            string  `json:"name"`
	TotalPercent    float64 `json:"total_run_percent"`
	GuestPercent    float64 `json:"guest_run_percent"`
	HypervisorPct   float64 `json:"hypervisor_run_percent"`
	IdlePercent     float64 `json:"idle_percent"`
	ContextSwitches int64   `json:"context_switches_per_sec"`
}

// HostPerf is the /host/perf response.
type HostPerf struct {
	CollectedAt          time.Time      `json:"collected_at"`
	CPUTotalPercent      *float64       `json:"cpu_total_percent,omitempty" doc:"Hypervisor logical processor total run time, _Total. The best single number for host CPU use under Hyper-V."`
	CPUGuestPercent      *float64       `json:"cpu_guest_percent,omitempty"`
	CPUHypervisorPercent *float64       `json:"cpu_hypervisor_percent,omitempty"`
	CPURootPercent       *float64       `json:"cpu_root_percent,omitempty" doc:"Time spent in the management OS's own virtual processors."`
	LogicalProcessors    []LPCounter    `json:"logical_processors,omitempty"`
	Partitions           *int64         `json:"partitions,omitempty" doc:"Running partitions including the root."`
	VirtualProcessors    *int64         `json:"virtual_processors,omitempty"`
	MemoryAvailableMB    *int64         `json:"memory_available_mb,omitempty"`
	MemoryCommittedMB    *int64         `json:"memory_committed_mb,omitempty"`
	MemoryCommitLimitMB  *int64         `json:"memory_commit_limit_mb,omitempty"`
	PagesPerSec          *int64         `json:"pages_per_sec,omitempty"`
	BalancerAvailableMB  *int64         `json:"balancer_available_mb,omitempty" doc:"Memory the dynamic memory balancer can still hand to VMs."`
	BalancerPressure     *int64         `json:"balancer_pressure_percent,omitempty" doc:"System-wide memory pressure; above 100 means VMs want more than the host can give."`
	Interfaces           []HostNICPerf  `json:"interfaces,omitempty" doc:"Host network interfaces."`
	Disks                []HostDiskPerf `json:"disks,omitempty" doc:"Host logical disks."`
	Switches             []SwitchPerf   `json:"switches,omitempty" doc:"Virtual switch throughput."`
	Notes                []string       `json:"notes,omitempty"`
}

// NUMANode is one host NUMA node.
type NUMANode struct {
	ID                string `json:"id"`
	Name              string `json:"name,omitempty"`
	LogicalProcessors int64  `json:"logical_processors"`
	Cores             int64  `json:"cores,omitempty"`
	MemoryMB          int64  `json:"memory_mb" doc:"Memory the node can currently give to VMs."`
}

// HostNUMA is the /host/numa response.
type HostNUMA struct {
	CollectedAt time.Time  `json:"collected_at"`
	Nodes       []NUMANode `json:"nodes"`
	Notes       []string   `json:"notes,omitempty"`
}

// HostHypervisor is the /host/hypervisor response.
type HostHypervisor struct {
	CollectedAt       time.Time `json:"collected_at"`
	Present           bool      `json:"present"`
	OSVersion         string    `json:"os_version"`
	OSBuild           string    `json:"os_build"`
	SchedulerType     string    `json:"scheduler_type,omitempty" doc:"Classic, Core or Root. Read from event ID 2 in the Hypervisor-Operational log (from the events phase); empty until then."`
	LogicalProcessors *int64    `json:"logical_processors,omitempty"`
	VirtualProcessors *int64    `json:"virtual_processors,omitempty"`
	Partitions        *int64    `json:"partitions,omitempty"`
	VBSStatus         string    `json:"virtualization_based_security,omitempty" doc:"Off, Enabled but not running, or Running."`
	VBSServices       []string  `json:"vbs_services_running,omitempty" doc:"Credential Guard, HVCI and other services running under VBS."`
	Notes             []string  `json:"notes,omitempty"`
}

// MigrationNetwork is one live migration network.
type MigrationNetwork struct {
	Subnet string `json:"subnet"`
	Metric int64  `json:"metric"`
}

// HostMigration is the /host/migration response.
type HostMigration struct {
	CollectedAt          time.Time          `json:"collected_at"`
	Enabled              bool               `json:"enabled"`
	MaxActiveMigrations  int64              `json:"max_active_migrations"`
	MaxStorageMigrations int64              `json:"max_storage_migrations"`
	Authentication       string             `json:"authentication" doc:"CredSSP or Kerberos."`
	PerformanceOption    string             `json:"performance_option,omitempty" doc:"TCP/IP, Compression or SMB."`
	UseAnyNetwork        *bool              `json:"use_any_network,omitempty"`
	Networks             []MigrationNetwork `json:"networks"`
	Notes                []string           `json:"notes,omitempty"`
}

// ClusterNode is a failover cluster node.
type ClusterNode struct {
	Name          string `json:"name"`
	State         string `json:"state" doc:"Up, Down, Paused, Joining or Unknown."`
	Weight        int64  `json:"weight"`
	DynamicWeight int64  `json:"dynamic_weight"`
	Build         string `json:"build,omitempty"`
}

// ClusterResource is a cluster resource.
type ClusterResource struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	State     string `json:"state"`
	Group     string `json:"group,omitempty"`
	OwnerNode string `json:"owner_node,omitempty"`
	VMID      string `json:"vm_id,omitempty" doc:"For Virtual Machine resources, the VM's ID."`
}

// ClusterGroup is a cluster role (group).
type ClusterGroup struct {
	Name      string `json:"name"`
	State     string `json:"state"`
	OwnerNode string `json:"owner_node,omitempty"`
}

// HostCluster is the /host/cluster response.
type HostCluster struct {
	CollectedAt time.Time         `json:"collected_at"`
	Clustered   bool              `json:"clustered" doc:"False when the host is not a failover cluster member; the rest is then empty."`
	Name        string            `json:"name,omitempty"`
	Nodes       []ClusterNode     `json:"nodes"`
	Groups      []ClusterGroup    `json:"groups"`
	Resources   []ClusterResource `json:"resources"`
	Notes       []string          `json:"notes,omitempty"`
}

// SwitchExtension is a virtual switch extension.
type SwitchExtension struct {
	Name    string `json:"name"`
	Type    string `json:"type" doc:"Capture, Filter, Forwarding or Monitoring."`
	Enabled bool   `json:"enabled"`
	Running bool   `json:"running"`
	Version string `json:"version,omitempty"`
	Vendor  string `json:"vendor,omitempty"`
}

// Switch is one virtual switch.
type Switch struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	Type               string            `json:"type" doc:"External, Internal or Private."`
	Health             string            `json:"health"`
	State              string            `json:"state"`
	ExternalAdapters   []string          `json:"external_adapters,omitempty" doc:"Physical adapters bound to the switch."`
	ManagementOSAccess bool              `json:"management_os_access" doc:"The host shares the adapter (an internal port exists)."`
	EmbeddedTeaming    bool              `json:"embedded_teaming" doc:"Switch Embedded Teaming (SET) in use."`
	SRIOV              bool              `json:"sriov"`
	BandwidthMode      string            `json:"bandwidth_mode,omitempty" doc:"Default, Weight, Absolute or None."`
	PacketDirect       bool              `json:"packet_direct"`
	VMPorts            int               `json:"vm_ports" doc:"Number of VM adapters connected."`
	Extensions         []SwitchExtension `json:"extensions,omitempty"`
	Notes              []string          `json:"notes,omitempty"`
}

// SwitchList is the /switches response.
type SwitchList struct {
	CollectedAt time.Time `json:"collected_at"`
	Count       int       `json:"count"`
	Switches    []Switch  `json:"switches"`
	Notes       []string  `json:"notes,omitempty"`
}

// SwitchPort is one port on a switch: a host-side port or a VM adapter.
type SwitchPort struct {
	Kind       string  `json:"kind" doc:"External (physical adapter), Internal (management OS) or VM."`
	Name       string  `json:"name"`
	VMID       string  `json:"vm_id,omitempty"`
	VMName     string  `json:"vm_name,omitempty"`
	MACAddress string  `json:"mac_address,omitempty"`
	Connected  bool    `json:"connected"`
	VLANMode   string  `json:"vlan_mode,omitempty"`
	VLANID     int64   `json:"vlan_id,omitempty"`
	TrunkVLANs []int64 `json:"trunk_vlans,omitempty"`
}

// SwitchDetail is the /switches/{id} response.
type SwitchDetail struct {
	Switch
	Ports []SwitchPort `json:"ports"`
}

// SwitchPorts is the /switches/{id}/ports response.
type SwitchPorts struct {
	CollectedAt time.Time    `json:"collected_at"`
	SwitchID    string       `json:"switch_id"`
	SwitchName  string       `json:"switch_name"`
	Ports       []SwitchPort `json:"ports"`
	Notes       []string     `json:"notes,omitempty"`
}
