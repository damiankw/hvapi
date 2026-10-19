package hyperv

// Code tables from the Hyper-V WMI v2 provider documentation.

var vmStates = map[int64]string{
	0: "Unknown", 1: "Other", 2: "Running", 3: "Off", 4: "Shutting Down", 5: "Not Applicable",
	6: "Saved", 7: "In Test", 8: "Deferred", 9: "Quiesce", 10: "Starting",
	32768: "Paused", 32769: "Saved", 32770: "Starting", 32771: "Snapshotting", 32773: "Saving",
	32774: "Stopping", 32776: "Pausing", 32777: "Resuming", 32779: "FastSaved", 32780: "FastSaving",
}

var healthStates = map[int64]string{
	0: "Unknown", 5: "OK", 10: "Degraded", 15: "Minor Failure", 20: "Major Failure", 25: "Critical Failure", 30: "Non-recoverable Error",
}

var operationalStatuses = map[int64]string{
	0: "Unknown", 1: "Other", 2: "OK", 3: "Degraded", 4: "Stressed", 5: "Predictive Failure", 6: "Error",
	7: "Non-Recoverable Error", 8: "Starting", 9: "Stopping", 10: "Stopped", 11: "In Service", 12: "No Contact",
	13: "Lost Communication", 14: "Aborted", 15: "Dormant", 16: "Supporting Entity in Error", 17: "Completed",
	18: "Power Mode", 32768: "Creating Snapshot", 32769: "Applying Snapshot", 32770: "Deleting Snapshot",
	32771: "Waiting to Start", 32772: "Merging Disks", 32773: "Exporting Virtual Machine", 32774: "Importing Virtual Machine",
}

// Heartbeat (Msvm_HeartbeatComponent.OperationalStatus[0]).
var heartbeatStatuses = map[int64]string{
	2: "OK", 6: "Error", 12: "No Contact", 13: "Lost Communication", 15: "Dormant", 32775: "Paused",
}

// Application state reported by the guest (OperationalStatus[1]).
var heartbeatAppStatuses = map[int64]string{
	2: "OK", 3: "Degraded (Application Critical)", 32775: "Paused",
}

var replicationStates = map[int64]string{
	0: "Disabled", 1: "Ready for replication", 2: "Waiting to complete initial replication", 3: "Replicating",
	4: "Synced replication complete", 5: "Recovered", 6: "Committed", 7: "Suspended", 8: "Critical",
	9: "Waiting for start resynchronize", 10: "Resynchronizing", 11: "Resynchronize suspended",
	12: "Failover in progress", 13: "Failback in progress", 14: "Failback complete",
}

var replicationHealths = map[int64]string{0: "Not applicable", 1: "OK", 2: "Warning", 3: "Critical"}

var replicationModes = map[int64]string{0: "None", 1: "Primary", 2: "Replica", 3: "Test replica", 4: "Extended replica"}

var startupActions = map[int64]string{2: "None", 3: "Restart if previously running", 4: "Always start"}
var shutdownActions = map[int64]string{2: "Turn off", 3: "Save", 4: "Shut down"}
var recoveryActions = map[int64]string{2: "None", 3: "Restart", 4: "Revert to snapshot"}
var bootDevices = map[int64]string{0: "Floppy", 1: "CD", 2: "IDE", 3: "Network"}
var bootProtocols = map[int64]string{4096: "IPv4", 4097: "IPv6"}
var consoleModes = map[int64]string{0: "Default", 1: "COM1", 2: "COM2", 3: "None"}
var sessionTransports = map[int64]string{0: "VMBus", 1: "HvSocket"}

var vlanModes = map[int64]string{0: "Unknown", 1: "Access", 2: "Trunk", 3: "Private", 4: "Isolated"}

// Resource sub types.
const (
	subIDEController      = "Microsoft:Hyper-V:Emulated IDE Controller"
	subSCSIController     = "Microsoft:Hyper-V:Synthetic SCSI Controller"
	subFCPort             = "Microsoft:Hyper-V:Synthetic Fibre Channel Port"
	subDiskDrive          = "Microsoft:Hyper-V:Synthetic Disk Drive"
	subDVDDrive           = "Microsoft:Hyper-V:Synthetic DVD Drive"
	subPhysicalDisk       = "Microsoft:Hyper-V:Physical Disk Drive"
	subFloppyDrive        = "Microsoft:Hyper-V:Diskette Drive"
	subVirtualHardDisk    = "Microsoft:Hyper-V:Virtual Hard Disk"
	subVirtualDVD         = "Microsoft:Hyper-V:Virtual CD/DVD Disk"
	subVirtualFloppy      = "Microsoft:Hyper-V:Virtual Floppy Disk"
	subSyntheticEthernet  = "Microsoft:Hyper-V:Synthetic Ethernet Port"
	subEmulatedEthernet   = "Microsoft:Hyper-V:Emulated Ethernet Port"
	subEthernetConnection = "Microsoft:Hyper-V:Ethernet Connection"
)
