package hyperv

import "time"

// Response types for the deep VM detail endpoints (phase 5).

// IntegrationService is one integration service of a VM.
type IntegrationService struct {
	Name       string   `json:"name" doc:"Heartbeat, Operating system shutdown, Time synchronization, Backup (volume shadow copy), Guest services, Data exchange (key-value pairs)."`
	Enabled    bool     `json:"enabled" doc:"Enabled in the VM's settings."`
	Status     string   `json:"status" doc:"OK, No Contact, Lost Communication, Error, Dormant ... as reported while running."`
	StatusCode int64    `json:"status_code"`
	Details    []string `json:"details,omitempty" doc:"Status text from Hyper-V."`
	Present    bool     `json:"present" doc:"False when the class is missing on this host."`
}

// VMIntegration is the /vms/{id}/integration response.
type VMIntegration struct {
	VMID         string               `json:"vm_id"`
	VMName       string               `json:"vm_name"`
	CollectedAt  time.Time            `json:"collected_at"`
	GuestVersion string               `json:"guest_version,omitempty" doc:"Integration services version the guest reports."`
	Services     []IntegrationService `json:"services"`
	Notes        []string             `json:"notes,omitempty"`
}

// VMKVP is the /vms/{id}/kvp response.
type VMKVP struct {
	VMID        string            `json:"vm_id"`
	VMName      string            `json:"vm_name"`
	CollectedAt time.Time         `json:"collected_at"`
	Intrinsic   map[string]string `json:"guest_intrinsic" doc:"Values the guest's integration services publish automatically: OS, hostname, addresses."`
	Guest       map[string]string `json:"guest" doc:"Values set inside the guest under HKLM\\SOFTWARE\\Microsoft\\Virtual Machine\\Guest."`
	Host        map[string]string `json:"host" doc:"Values the host sends to the guest (Set-VMKeyValuePair)."`
	Notes       []string          `json:"notes,omitempty"`
}

// CheckpointDisk is a differencing disk a checkpoint owns.
type CheckpointDisk struct {
	Path    string `json:"path"`
	SizeMB  *int64 `json:"size_mb,omitempty" doc:"File size on the host; absent when the file cannot be read."`
	Missing bool   `json:"missing" doc:"True when the file is not where the configuration says."`
}

// Checkpoint is one VM checkpoint (snapshot).
type Checkpoint struct {
	ID         string           `json:"id" doc:"Configuration ID of the checkpoint."`
	Name       string           `json:"name"`
	Created    *time.Time       `json:"created,omitempty"`
	AgeDays    float64          `json:"age_days"`
	Type       string           `json:"type" doc:"Standard, Production, Recovery (Hyper-V Replica), Planned, or Automatic."`
	ParentID   string           `json:"parent_id,omitempty" doc:"The checkpoint this one was taken from."`
	Children   []string         `json:"children,omitempty"`
	Current    bool             `json:"current" doc:"The VM is running from this checkpoint (its 'Now' parent)."`
	Notes      []string         `json:"notes,omitempty" doc:"Notes set on the checkpoint."`
	Disks      []CheckpointDisk `json:"disks"`
	DiskSizeMB int64            `json:"disk_size_mb" doc:"Total size of the checkpoint's differencing disks that could be read."`
}

// VMCheckpoints is the /vms/{id}/checkpoints response.
type VMCheckpoints struct {
	VMID        string       `json:"vm_id"`
	VMName      string       `json:"vm_name"`
	CollectedAt time.Time    `json:"collected_at"`
	Count       int          `json:"count"`
	OldestDays  float64      `json:"oldest_days"`
	TotalSizeMB int64        `json:"total_size_mb"`
	Checkpoints []Checkpoint `json:"checkpoints" doc:"Oldest first."`
	Notes       []string     `json:"notes,omitempty"`
}

// DiskLink is one disk in a differencing chain.
type DiskLink struct {
	Path           string `json:"path"`
	Type           string `json:"type" doc:"Fixed, Dynamic, Differencing or Unknown."`
	Format         string `json:"format" doc:"VHD, VHDX, VHDSet or Unknown."`
	MaxSizeMB      int64  `json:"max_size_mb" doc:"Maximum size the guest can see."`
	BlockSizeKB    int64  `json:"block_size_kb,omitempty"`
	LogicalSector  int64  `json:"logical_sector_bytes,omitempty"`
	PhysicalSector int64  `json:"physical_sector_bytes,omitempty"`
	ParentPath     string `json:"parent_path,omitempty"`
	ParentMissing  bool   `json:"parent_missing" doc:"True when the parent cannot be read: a broken chain."`
	Error          string `json:"error,omitempty"`
}

// DiskState is the /vms/{id}/disks/{diskId}/state response.
type DiskState struct {
	VMID          string     `json:"vm_id"`
	VMName        string     `json:"vm_name"`
	DiskID        string     `json:"disk_id"`
	Path          string     `json:"path"`
	CheckedAt     time.Time  `json:"checked_at" doc:"When the host was asked; results are cached for 30 seconds."`
	FileSizeMB    *int64     `json:"file_size_mb,omitempty" doc:"Current size of the file on disk."`
	MinSizeMB     *int64     `json:"min_size_mb,omitempty" doc:"Smallest size the disk could be compacted to."`
	InUse         *bool      `json:"in_use,omitempty"`
	Fragmentation *int64     `json:"fragmentation_percent,omitempty"`
	Alignment     *int64     `json:"alignment,omitempty" doc:"1 when partitions are aligned to the physical sector size, 0 when not."`
	Chain         []DiskLink `json:"chain" doc:"The disk and its parents, leaf first."`
	ChainBroken   bool       `json:"chain_broken"`
	Notes         []string   `json:"notes,omitempty"`
}

// VMReplication is the /vms/{id}/replication response.
type VMReplication struct {
	VMID                string     `json:"vm_id"`
	VMName              string     `json:"vm_name"`
	CollectedAt         time.Time  `json:"collected_at"`
	Enabled             bool       `json:"enabled"`
	State               string     `json:"state"`
	Health              string     `json:"health"`
	Mode                string     `json:"mode"`
	LastReplication     *time.Time `json:"last_replication,omitempty"`
	LastReplicationType string     `json:"last_replication_type,omitempty"`
	LastAppConsistent   *time.Time `json:"last_application_consistent,omitempty"`
	MinutesSinceLast    *int64     `json:"minutes_since_last,omitempty"`
	ReplicaServer       string     `json:"replica_server,omitempty"`
	ReplicaPort         int64      `json:"replica_port,omitempty"`
	PrimaryServer       string     `json:"primary_server,omitempty"`
	IntervalSeconds     int64      `json:"interval_seconds,omitempty"`
	RecoveryPoints      int64      `json:"recovery_points,omitempty" doc:"Recovery history kept on the replica."`
	AppConsistentHours  int64      `json:"app_consistent_interval_hours,omitempty"`
	Authentication      string     `json:"authentication,omitempty" doc:"Kerberos or Certificate."`
	Compression         bool       `json:"compression"`
	AutoResync          bool       `json:"auto_resynchronize"`
	ReplicatedDisks     []string   `json:"replicated_disks,omitempty"`
	Notes               []string   `json:"notes,omitempty"`
}

// GPUPartition is a GPU partition assigned to a VM.
type GPUPartition struct {
	Name          string `json:"name,omitempty"`
	HostResource  string `json:"host_resource,omitempty" doc:"The physical GPU partition in use, when assigned."`
	MinVRAMMB     int64  `json:"min_vram_mb,omitempty"`
	MaxVRAMMB     int64  `json:"max_vram_mb,omitempty"`
	OptimalVRAMMB int64  `json:"optimal_vram_mb,omitempty"`
}

// PCIDevice is a PCI Express device passed through to a VM.
type PCIDevice struct {
	Name             string   `json:"name,omitempty"`
	HostResource     []string `json:"host_resource,omitempty" doc:"Host device path(s)."`
	VirtualFunctions int64    `json:"virtual_functions,omitempty"`
}

// SerialPort is a COM port.
type SerialPort struct {
	Name       string `json:"name"`
	Connection string `json:"connection,omitempty" doc:"Named pipe path, or empty when not connected."`
	Debugger   bool   `json:"debugger"`
}

// Display is the VM's synthetic 3D display controller, if any.
type Display struct {
	MaxMonitors   int64  `json:"max_monitors"`
	MaxResolution string `json:"max_resolution,omitempty"`
	VRAMMB        int64  `json:"vram_mb,omitempty"`
}

// FibreChannelPort is a virtual Fibre Channel adapter.
type FibreChannelPort struct {
	Name          string `json:"name"`
	WWNN          string `json:"wwnn,omitempty"`
	WWPNPrimary   string `json:"wwpn_primary,omitempty"`
	WWPNSecondary string `json:"wwpn_secondary,omitempty"`
	SAN           string `json:"san,omitempty"`
}

// VMDevices is the /vms/{id}/devices response.
type VMDevices struct {
	VMID          string             `json:"vm_id"`
	VMName        string             `json:"vm_name"`
	CollectedAt   time.Time          `json:"collected_at"`
	GPUPartitions []GPUPartition     `json:"gpu_partitions"`
	PCIDevices    []PCIDevice        `json:"pci_devices"`
	SerialPorts   []SerialPort       `json:"serial_ports"`
	Display       *Display           `json:"display_3d,omitempty"`
	FibreChannel  []FibreChannelPort `json:"fibre_channel_ports"`
	DVDDrives     []Drive            `json:"dvd_drives"`
	Notes         []string           `json:"notes,omitempty"`
}

// VMSecurity is the /vms/{id}/security response.
type VMSecurity struct {
	VMID                   string    `json:"vm_id"`
	VMName                 string    `json:"vm_name"`
	CollectedAt            time.Time `json:"collected_at"`
	Generation             int       `json:"generation"`
	SecureBoot             *bool     `json:"secure_boot,omitempty" doc:"Generation 2 only."`
	SecureBootTemplate     string    `json:"secure_boot_template,omitempty" doc:"Microsoft Windows, Microsoft UEFI Certificate Authority, Open Source Shielded VM, or the template GUID."`
	TPM                    *bool     `json:"tpm,omitempty" doc:"Virtual TPM present."`
	Shielded               *bool     `json:"shielded,omitempty"`
	EncryptStateAndTraffic *bool     `json:"encrypt_state_and_migration_traffic,omitempty"`
	DataProtection         *bool     `json:"data_protection_requested,omitempty"`
	VBSOptOut              *bool     `json:"virtualization_based_security_opt_out,omitempty"`
	BindToHostTPM          *bool     `json:"bind_to_host_tpm,omitempty"`
	Notes                  []string  `json:"notes,omitempty"`
}

// Metric is one resource metering value.
type Metric struct {
	Name         string     `json:"name" doc:"For example Aggregated Average CPU Utilization."`
	Value        float64    `json:"value"`
	Units        string     `json:"units,omitempty"`
	Since        *time.Time `json:"since,omitempty" doc:"Start of the metering period."`
	DurationSecs int64      `json:"duration_seconds,omitempty"`
	Element      string     `json:"element,omitempty" doc:"Disk or adapter the metric applies to, when not the whole VM."`
}

// VMMetering is the /vms/{id}/metering response.
type VMMetering struct {
	VMID        string    `json:"vm_id"`
	VMName      string    `json:"vm_name"`
	CollectedAt time.Time `json:"collected_at"`
	Enabled     bool      `json:"enabled" doc:"False when resource metering is not turned on for this VM (Enable-VMResourceMetering)."`
	Metrics     []Metric  `json:"metrics"`
	Notes       []string  `json:"notes,omitempty"`
}
