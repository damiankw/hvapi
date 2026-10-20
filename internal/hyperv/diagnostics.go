package hyperv

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Finding is one thing /diagnostics noticed.
type Finding struct {
	Severity    string `json:"severity" doc:"critical, warning or info."`
	Rule        string `json:"rule" doc:"Stable identifier of the check, for alert routing."`
	Subject     string `json:"subject" doc:"vm, host, volume, service, job, switch, cluster or log."`
	SubjectID   string `json:"subject_id,omitempty"`
	SubjectName string `json:"subject_name,omitempty"`
	Message     string `json:"message"`
}

// DiagOptions are the thresholds; zero means the configured default.
type DiagOptions struct {
	CheckpointDays   int `json:"checkpoint_days"`
	FreeSpacePercent int `json:"free_space_percent"`
}

// Diagnostics is the /diagnostics response.
type Diagnostics struct {
	CollectedAt time.Time   `json:"collected_at"`
	Status      string      `json:"status" doc:"ok, warning or critical: the worst finding."`
	Critical    int         `json:"critical"`
	Warnings    int         `json:"warnings"`
	Info        int         `json:"info"`
	Findings    []Finding   `json:"findings" doc:"Most severe first."`
	Checks      []string    `json:"checks" doc:"Rules evaluated."`
	Thresholds  DiagOptions `json:"thresholds"`
	Notes       []string    `json:"notes,omitempty"`
}

var severityRank = map[string]int{"critical": 0, "warning": 1, "info": 2}

// Diagnose runs every rule over the cache.
func (d *Data) Diagnose(o DiagOptions) (*Diagnostics, error) {
	if err := d.ready(false); err != nil {
		return nil, err
	}
	if o.CheckpointDays <= 0 {
		o.CheckpointDays = 7
	}
	if o.FreeSpacePercent <= 0 {
		o.FreeSpacePercent = 10
	}
	out := &Diagnostics{CollectedAt: d.State.at.UTC(), Findings: []Finding{}, Thresholds: o}
	add := func(f Finding) { out.Findings = append(out.Findings, f) }
	check := func(name string) { out.Checks = append(out.Checks, name) }

	// Host services.
	check("services_stopped")
	if svc, err := d.HostServices(); err == nil {
		for _, s := range svc.Services {
			if s.Expected && s.State != "Running" {
				add(Finding{Severity: "critical", Rule: "services_stopped", Subject: "service", SubjectID: s.Name, SubjectName: s.DisplayName, Message: fmt.Sprintf("%s is %s", s.DisplayName, s.State)})
			}
		}
		out.Notes = append(out.Notes, svc.Notes...)
	}

	// VMs.
	check("vm_health")
	check("heartbeat_missing")
	check("integration_disabled")
	check("replication_health")
	check("adapter_disconnected")
	for _, id := range d.State.order {
		vm := d.vm(id)
		if vm.HealthCode != 5 && vm.HealthCode != 0 {
			sev := "warning"
			if vm.HealthCode >= 20 {
				sev = "critical"
			}
			add(Finding{Severity: sev, Rule: "vm_health", Subject: "vm", SubjectID: id, SubjectName: vm.Name, Message: fmt.Sprintf("health is %s (%s)", vm.Health, strings.Join(vm.StatusDescriptions, "; "))})
		}
		if vm.StateCode == 2 {
			g := d.guest(id)
			switch g.HeartbeatCode {
			case 2, 0:
				if g.HeartbeatCode == 0 {
					add(Finding{Severity: "info", Rule: "heartbeat_missing", Subject: "vm", SubjectID: id, SubjectName: vm.Name, Message: "running but no heartbeat information (integration services absent?)"})
				}
			case 12, 13:
				add(Finding{Severity: "warning", Rule: "heartbeat_missing", Subject: "vm", SubjectID: id, SubjectName: vm.Name, Message: "running but the guest heartbeat is " + g.Heartbeat})
			default:
				add(Finding{Severity: "warning", Rule: "heartbeat_missing", Subject: "vm", SubjectID: id, SubjectName: vm.Name, Message: "guest heartbeat is " + g.Heartbeat})
			}
			if in, err := d.Integration(id); err == nil {
				for _, s := range in.Services {
					if s.Present && !s.Enabled && (s.Name == "Heartbeat" || s.Name == "Data exchange (key-value pairs)") {
						add(Finding{Severity: "info", Rule: "integration_disabled", Subject: "vm", SubjectID: id, SubjectName: vm.Name, Message: s.Name + " integration service is disabled"})
					}
				}
			}
			if n, err := d.Network(id); err == nil {
				for _, a := range n.Adapters {
					if !a.Connected {
						add(Finding{Severity: "info", Rule: "adapter_disconnected", Subject: "vm", SubjectID: id, SubjectName: vm.Name, Message: fmt.Sprintf("network adapter %q is not connected to a switch", a.Name)})
					}
				}
			}
		}
		switch d.State.vms[id].cs.Int("ReplicationHealth") {
		case 2:
			add(Finding{Severity: "warning", Rule: "replication_health", Subject: "vm", SubjectID: id, SubjectName: vm.Name, Message: "replication health is Warning (" + vm.ReplicationState + ")"})
		case 3:
			add(Finding{Severity: "critical", Rule: "replication_health", Subject: "vm", SubjectID: id, SubjectName: vm.Name, Message: "replication health is Critical (" + vm.ReplicationState + ")"})
		}
	}

	// Checkpoints and disk files need the settings poll.
	check("checkpoint_old")
	check("checkpoint_disk_missing")
	check("vm_disk_missing")
	if d.Settings != nil {
		for _, id := range d.State.order {
			name := d.State.vms[id].cs.String("ElementName")
			if cp, err := d.Checkpoints(id); err == nil {
				for _, c := range cp.Checkpoints {
					if c.AgeDays >= float64(o.CheckpointDays) {
						add(Finding{Severity: "warning", Rule: "checkpoint_old", Subject: "vm", SubjectID: id, SubjectName: name, Message: fmt.Sprintf("checkpoint %q is %.0f days old", c.Name, c.AgeDays)})
					}
					for _, dk := range c.Disks {
						if dk.Missing {
							add(Finding{Severity: "critical", Rule: "checkpoint_disk_missing", Subject: "vm", SubjectID: id, SubjectName: name, Message: fmt.Sprintf("checkpoint %q disk %s cannot be read: the chain may be broken", c.Name, dk.Path)})
						}
					}
				}
			}
			if set := d.Settings.vms[id]; set != nil {
				for _, s := range set.sasd {
					if s.String("ResourceSubType") != subVirtualHardDisk {
						continue
					}
					for _, p := range s.Strings("HostResource") {
						if _, ok := fileSize(p); !ok {
							add(Finding{Severity: "warning", Rule: "vm_disk_missing", Subject: "vm", SubjectID: id, SubjectName: name, Message: "virtual hard disk " + p + " cannot be read by the service account (missing, or on a share it cannot access)"})
						}
					}
				}
			}
		}
	} else {
		out.Notes = append(out.Notes, "checkpoint, disk file, storage and cluster checks need the settings poll, which has not completed")
	}

	// Volumes holding VM files.
	check("volume_low_space")
	if d.Settings != nil {
		if st, err := d.HostStorage(); err == nil {
			for _, v := range st.Volumes {
				if v.VMFiles == 0 || v.SizeMB == 0 {
					continue
				}
				label := v.DriveLetter + ":"
				if v.DriveLetter == "" {
					label = v.Path
				}
				switch {
				case v.FreePercent < float64(o.FreeSpacePercent)/2:
					add(Finding{Severity: "critical", Rule: "volume_low_space", Subject: "volume", SubjectID: v.Path, SubjectName: label, Message: fmt.Sprintf("%.1f%% free (%d MB) on a volume holding %s", v.FreePercent, v.FreeMB, strings.Join(v.VMs, ", "))})
				case v.FreePercent < float64(o.FreeSpacePercent):
					add(Finding{Severity: "warning", Rule: "volume_low_space", Subject: "volume", SubjectID: v.Path, SubjectName: label, Message: fmt.Sprintf("%.1f%% free (%d MB) on a volume holding %s", v.FreePercent, v.FreeMB, strings.Join(v.VMs, ", "))})
				}
				if v.Health != "" && v.Health != "Healthy" && v.Health != "Unknown" {
					add(Finding{Severity: "warning", Rule: "volume_health", Subject: "volume", SubjectID: v.Path, SubjectName: label, Message: "volume health is " + v.Health})
				}
			}
		}
	}

	// Jobs.
	check("job_failed")
	check("job_stuck")
	if j, err := d.Jobs(); err == nil {
		for _, job := range j.Jobs {
			if job.Failed {
				add(Finding{Severity: "warning", Rule: "job_failed", Subject: "job", SubjectID: job.ID, SubjectName: job.Name, Message: fmt.Sprintf("%s: %s %s", job.Name, job.State, job.ErrorText)})
			}
			if job.Stuck {
				add(Finding{Severity: "warning", Rule: "job_stuck", Subject: "job", SubjectID: job.ID, SubjectName: job.Name, Message: fmt.Sprintf("%s has been running for %d minutes at %d%%", job.Name, job.ElapsedSeconds/60, job.PercentComplete)})
			}
		}
	}

	// Host resources.
	check("host_memory_pressure")
	check("host_cpu_high")
	if p, err := d.HostPerf(); err == nil {
		if p.BalancerPressure != nil && *p.BalancerPressure >= 100 {
			add(Finding{Severity: "warning", Rule: "host_memory_pressure", Subject: "host", Message: fmt.Sprintf("dynamic memory pressure is %d%%: VMs want more memory than the host can give", *p.BalancerPressure)})
		}
		if p.CPUTotalPercent != nil && *p.CPUTotalPercent >= 90 {
			add(Finding{Severity: "warning", Rule: "host_cpu_high", Subject: "host", Message: fmt.Sprintf("hypervisor logical processors are %.0f%% busy", *p.CPUTotalPercent)})
		}
	}

	// Cluster.
	check("cluster_node_down")
	check("cluster_resource_failed")
	if d.Settings != nil {
		if c, err := d.HostCluster(); err == nil && c.Clustered {
			for _, n := range c.Nodes {
				if n.State != "Up" {
					add(Finding{Severity: "warning", Rule: "cluster_node_down", Subject: "cluster", SubjectID: n.Name, SubjectName: n.Name, Message: "cluster node is " + n.State})
				}
			}
			for _, r := range c.Resources {
				if r.State == "Failed" {
					add(Finding{Severity: "critical", Rule: "cluster_resource_failed", Subject: "cluster", SubjectID: r.Name, SubjectName: r.Name, Message: "cluster resource has failed (" + r.Type + ")"})
				}
			}
		}
	}

	// Event logs.
	check("event_errors")
	if d.Settings != nil && d.Settings.eventHours > 0 {
		if ev, err := d.Events(EventFilter{}); err == nil {
			for _, l := range ev.Logs {
				if l.Critical > 0 {
					add(Finding{Severity: "critical", Rule: "event_errors", Subject: "log", SubjectID: l.Name, SubjectName: l.Channel, Message: fmt.Sprintf("%d critical and %d error events in the last %d hours", l.Critical, l.Errors, ev.Hours)})
				} else if l.Errors > 0 {
					add(Finding{Severity: "warning", Rule: "event_errors", Subject: "log", SubjectID: l.Name, SubjectName: l.Channel, Message: fmt.Sprintf("%d error events in the last %d hours", l.Errors, ev.Hours)})
				}
			}
		}
	}

	sort.SliceStable(out.Findings, func(i, j int) bool {
		a, b := out.Findings[i], out.Findings[j]
		if severityRank[a.Severity] != severityRank[b.Severity] {
			return severityRank[a.Severity] < severityRank[b.Severity]
		}
		return a.Rule+a.SubjectName < b.Rule+b.SubjectName
	})
	out.Status = "ok"
	for _, f := range out.Findings {
		switch f.Severity {
		case "critical":
			out.Critical++
			out.Status = "critical"
		case "warning":
			out.Warnings++
			if out.Status == "ok" {
				out.Status = "warning"
			}
		default:
			out.Info++
		}
	}
	return out, nil
}
