package hyperv

import (
	"time"

	"github.com/damiankw/hvapi/internal/evtlog"
)

// Response types for jobs, events and performance (phase 6).

// Job is one Hyper-V job (VM operations, storage merges, exports ...).
type Job struct {
	ID              string     `json:"id"`
	Name            string     `json:"name" doc:"What the job does, as Hyper-V names it."`
	Description     string     `json:"description,omitempty"`
	State           string     `json:"state" doc:"New, Starting, Running, Suspended, Shutting Down, Completed, Terminated, Killed, Exception, Service or Completed with Warnings."`
	StateCode       int64      `json:"state_code"`
	PercentComplete int64      `json:"percent_complete"`
	Started         *time.Time `json:"started,omitempty"`
	LastChange      *time.Time `json:"last_change,omitempty"`
	ElapsedSeconds  int64      `json:"elapsed_seconds"`
	Owner           string     `json:"owner,omitempty"`
	Storage         bool       `json:"storage" doc:"True for storage jobs (merge, compact, convert ...)."`
	ChildPath       string     `json:"child,omitempty" doc:"For storage jobs, the file being written."`
	ParentPath      string     `json:"parent,omitempty" doc:"For storage jobs, the source file."`
	ErrorCode       int64      `json:"error_code"`
	ErrorText       string     `json:"error,omitempty"`
	Failed          bool       `json:"failed" doc:"Terminated, killed, raised an exception or reported an error."`
	Stuck           bool       `json:"stuck" doc:"Still running after an hour; worth a look."`
}

// JobList is the /jobs response.
type JobList struct {
	CollectedAt time.Time `json:"collected_at"`
	Running     int       `json:"running"`
	Failed      int       `json:"failed"`
	Stuck       int       `json:"stuck"`
	Jobs        []Job     `json:"jobs" doc:"Running first, then by start time, newest first."`
	Notes       []string  `json:"notes,omitempty"`
}

// LogStatus summarises one event log in the cache.
type LogStatus struct {
	Name     string `json:"name" doc:"Short name to use in /events/{log}."`
	Channel  string `json:"channel"`
	Count    int    `json:"count" doc:"Events cached (last events.hours, at most events.max_per_log)."`
	Critical int    `json:"critical"`
	Errors   int    `json:"errors"`
	Warnings int    `json:"warnings"`
	Error    string `json:"error,omitempty" doc:"Why the log could not be read, if it could not."`
}

// EventList is the /events and /events/{log} response.
type EventList struct {
	CollectedAt time.Time      `json:"collected_at"`
	Hours       int            `json:"hours" doc:"How far back the cache goes."`
	Logs        []LogStatus    `json:"logs"`
	Count       int            `json:"count" doc:"Events returned after filtering."`
	Events      []evtlog.Event `json:"events" doc:"Newest first."`
	Notes       []string       `json:"notes,omitempty"`
}

// EventFilter narrows /events.
type EventFilter struct {
	Log   string // short log name, "" for all
	Level string // "critical", "error", "warning", "info": minimum severity
	Hours int    // 0 for all cached
	Limit int    // 0 for default 200
	ID    int    // event ID, 0 for any
	VMID  string // only events mentioning this VM
}

// VPPerf is one virtual processor's counters.
type VPPerf struct {
	Index             int64   `json:"index"`
	TotalPercent      float64 `json:"total_run_percent"`
	GuestPercent      float64 `json:"guest_run_percent"`
	HypervisorPercent float64 `json:"hypervisor_run_percent"`
	RemotePercent     float64 `json:"remote_run_percent,omitempty" doc:"Time run on a remote NUMA node."`
}

// NICPerf is one virtual network adapter's counters.
type NICPerf struct {
	Name             string `json:"name" doc:"Counter instance name."`
	Adapter          string `json:"adapter,omitempty" doc:"The VM adapter it was matched to, when identifiable."`
	BytesInPerSec    int64  `json:"bytes_in_per_sec"`
	BytesOutPerSec   int64  `json:"bytes_out_per_sec"`
	PacketsInPerSec  int64  `json:"packets_in_per_sec"`
	PacketsOutPerSec int64  `json:"packets_out_per_sec"`
	DroppedIn        int64  `json:"dropped_in_per_sec"`
	DroppedOut       int64  `json:"dropped_out_per_sec"`
}

// VDiskPerf is one virtual disk's counters.
type VDiskPerf struct {
	Name             string `json:"name" doc:"Counter instance name (the path with separators replaced)."`
	Path             string `json:"path,omitempty" doc:"The VHD it was matched to."`
	ReadBytesPerSec  int64  `json:"read_bytes_per_sec"`
	WriteBytesPerSec int64  `json:"write_bytes_per_sec"`
	ReadOpsPerSec    int64  `json:"read_ops_per_sec"`
	WriteOpsPerSec   int64  `json:"write_ops_per_sec"`
	QueueLength      int64  `json:"queue_length"`
	LatencyMs        *int64 `json:"latency_ms,omitempty"`
	ErrorCount       int64  `json:"error_count"`
}

// VMPerf is the /vms/{id}/perf response.
type VMPerf struct {
	VMID            string      `json:"vm_id"`
	VMName          string      `json:"vm_name"`
	CollectedAt     time.Time   `json:"collected_at"`
	CPUTotalPercent *float64    `json:"cpu_total_percent,omitempty" doc:"Average over the VM's virtual processors."`
	CPUGuestPercent *float64    `json:"cpu_guest_percent,omitempty"`
	Processors      []VPPerf    `json:"processors"`
	Adapters        []NICPerf   `json:"adapters"`
	Disks           []VDiskPerf `json:"disks"`
	Notes           []string    `json:"notes,omitempty"`
}

// HostNICPerf is a host network interface's counters.
type HostNICPerf struct {
	Name           string `json:"name"`
	BytesInPerSec  int64  `json:"bytes_in_per_sec"`
	BytesOutPerSec int64  `json:"bytes_out_per_sec"`
	PacketsPerSec  int64  `json:"packets_per_sec"`
	ErrorsIn       int64  `json:"errors_in"`
	ErrorsOut      int64  `json:"errors_out"`
	DiscardedIn    int64  `json:"discarded_in"`
	DiscardedOut   int64  `json:"discarded_out"`
	OutputQueueLen int64  `json:"output_queue_length"`
}

// HostDiskPerf is a host logical disk's counters.
type HostDiskPerf struct {
	Name             string  `json:"name" doc:"Drive letter or mount point."`
	IdlePercent      float64 `json:"idle_percent"`
	QueueLength      float64 `json:"avg_queue_length"`
	ReadBytesPerSec  int64   `json:"read_bytes_per_sec"`
	WriteBytesPerSec int64   `json:"write_bytes_per_sec"`
	ReadsPerSec      int64   `json:"reads_per_sec"`
	WritesPerSec     int64   `json:"writes_per_sec"`
	ReadLatencyMs    float64 `json:"read_latency_ms"`
	WriteLatencyMs   float64 `json:"write_latency_ms"`
	FreePercent      float64 `json:"free_percent"`
}

// SwitchPerf is a virtual switch's counters.
type SwitchPerf struct {
	Name            string `json:"name"`
	BytesPerSec     int64  `json:"bytes_per_sec"`
	PacketsPerSec   int64  `json:"packets_per_sec"`
	DroppedIn       int64  `json:"dropped_in_per_sec"`
	DroppedOut      int64  `json:"dropped_out_per_sec"`
	BroadcastPerSec int64  `json:"broadcast_packets_per_sec"`
}
