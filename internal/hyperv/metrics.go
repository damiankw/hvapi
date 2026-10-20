package hyperv

import (
	"fmt"
	"sort"
	"strings"
)

// Metrics renders the cache in the Prometheus text exposition format.
func (d *Data) Metrics(o DiagOptions) string {
	var b strings.Builder
	w := func(name, help, typ string, samples ...sample) {
		if len(samples) == 0 {
			return
		}
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
		for _, s := range samples {
			b.WriteString(name)
			if len(s.labels) > 0 {
				b.WriteByte('{')
				keys := make([]string, 0, len(s.labels))
				for k := range s.labels {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for i, k := range keys {
					if i > 0 {
						b.WriteByte(',')
					}
					fmt.Fprintf(&b, `%s="%s"`, k, escapeLabel(s.labels[k]))
				}
				b.WriteByte('}')
			}
			fmt.Fprintf(&b, " %s\n", formatFloat(s.value))
		}
	}

	ready := 0.0
	if d.State != nil {
		ready = 1
	}
	w("hvapi_cache_ready", "1 when the state poll has completed at least once.", "gauge", sample{value: ready})
	if d.State == nil {
		return b.String()
	}
	w("hvapi_poll_errors", "WMI sources that failed in the last poll.", "gauge",
		sample{labels: map[string]string{"poll": "state"}, value: float64(len(d.State.errs))},
		sample{labels: map[string]string{"poll": "settings"}, value: float64(settingsErrs(d))})

	// VMs.
	var state, running, uptime, cpu, memAssigned, memDemand, pressure, heartbeat, snapshots, oldest, repl []sample
	for _, id := range d.State.order {
		s := d.summary(id)
		l := map[string]string{"vm_id": id, "vm_name": s.Name}
		state = append(state, sample{labels: l, value: float64(d.State.vms[id].cs.Int("EnabledState"))})
		run := 0.0
		if s.State == "Running" {
			run = 1
		}
		running = append(running, sample{labels: l, value: run})
		uptime = append(uptime, sample{labels: l, value: float64(s.UptimeSeconds)})
		if s.ProcessorLoadPercent != nil {
			cpu = append(cpu, sample{labels: l, value: *s.ProcessorLoadPercent})
		}
		memAssigned = append(memAssigned, sample{labels: l, value: float64(s.MemoryAssignedMB) * 1048576})
		if s.MemoryDemandMB != nil {
			memDemand = append(memDemand, sample{labels: l, value: float64(*s.MemoryDemandMB) * 1048576})
		}
		if s.MemoryPressurePercent != nil {
			pressure = append(pressure, sample{labels: l, value: float64(*s.MemoryPressurePercent)})
		}
		hb := 0.0
		if s.Guest.HeartbeatCode == 2 {
			hb = 1
		}
		heartbeat = append(heartbeat, sample{labels: l, value: hb})
		snapshots = append(snapshots, sample{labels: l, value: float64(s.SnapshotCount)})
		if cp, err := d.Checkpoints(id); err == nil {
			oldest = append(oldest, sample{labels: l, value: cp.OldestDays})
		}
		repl = append(repl, sample{labels: l, value: float64(d.State.vms[id].cs.Int("ReplicationHealth"))})
	}
	w("hvapi_vm_state", "VM EnabledState code (2 running, 3 off, 32769 saved, 32768 paused).", "gauge", state...)
	w("hvapi_vm_running", "1 when the VM is running.", "gauge", running...)
	w("hvapi_vm_uptime_seconds", "Seconds the VM has been running.", "gauge", uptime...)
	w("hvapi_vm_cpu_percent", "Average virtual processor load.", "gauge", cpu...)
	w("hvapi_vm_memory_assigned_bytes", "Memory assigned to the VM.", "gauge", memAssigned...)
	w("hvapi_vm_memory_demand_bytes", "Memory the guest wants (dynamic memory).", "gauge", memDemand...)
	w("hvapi_vm_memory_pressure_percent", "Dynamic memory pressure; above 100 the guest wants more than it has.", "gauge", pressure...)
	w("hvapi_vm_heartbeat_ok", "1 when the guest heartbeat is OK.", "gauge", heartbeat...)
	w("hvapi_vm_checkpoints", "Number of checkpoints.", "gauge", snapshots...)
	w("hvapi_vm_checkpoint_oldest_days", "Age of the oldest checkpoint in days.", "gauge", oldest...)
	w("hvapi_vm_replication_health", "0 not applicable, 1 OK, 2 warning, 3 critical.", "gauge", repl...)

	// Host.
	if h, err := d.Host(); err == nil {
		w("hvapi_host_memory_total_bytes", "Physical memory of the host.", "gauge", sample{value: float64(h.MemoryTotalMB) * 1048576})
		w("hvapi_host_memory_free_bytes", "Free physical memory on the host.", "gauge", sample{value: float64(h.MemoryFreeMB) * 1048576})
		w("hvapi_host_uptime_seconds", "Seconds since the host booted.", "gauge", sample{value: float64(h.UptimeSeconds)})
		w("hvapi_host_vms", "VMs on the host.", "gauge", sample{labels: map[string]string{"state": "all"}, value: float64(h.VMCount)}, sample{labels: map[string]string{"state": "running"}, value: float64(h.VMRunning)})
	}
	if p, err := d.HostPerf(); err == nil {
		if p.CPUTotalPercent != nil {
			w("hvapi_host_cpu_percent", "Hypervisor logical processor total run time.", "gauge", sample{value: *p.CPUTotalPercent})
		}
		if p.BalancerPressure != nil {
			w("hvapi_host_memory_pressure_percent", "System-wide dynamic memory pressure.", "gauge", sample{value: float64(*p.BalancerPressure)})
		}
		if p.MemoryAvailableMB != nil {
			w("hvapi_host_memory_available_bytes", "Available memory from the OS counters.", "gauge", sample{value: float64(*p.MemoryAvailableMB) * 1048576})
		}
	}
	if svc, err := d.HostServices(); err == nil {
		var ss []sample
		for _, s := range svc.Services {
			v := 0.0
			if s.State == "Running" {
				v = 1
			}
			ss = append(ss, sample{labels: map[string]string{"service": s.Name}, value: v})
		}
		w("hvapi_service_running", "1 when the Hyper-V service is running.", "gauge", ss...)
	}
	if st, err := d.HostStorage(); err == nil {
		var size, free []sample
		for _, v := range st.Volumes {
			if v.SizeMB == 0 {
				continue
			}
			l := map[string]string{"path": v.Path, "label": v.Label, "drive": v.DriveLetter}
			size = append(size, sample{labels: l, value: float64(v.SizeMB) * 1048576})
			free = append(free, sample{labels: l, value: float64(v.FreeMB) * 1048576})
		}
		w("hvapi_volume_size_bytes", "Volume size.", "gauge", size...)
		w("hvapi_volume_free_bytes", "Volume free space.", "gauge", free...)
	}
	if j, err := d.Jobs(); err == nil {
		w("hvapi_jobs", "Hyper-V jobs by state.", "gauge",
			sample{labels: map[string]string{"state": "running"}, value: float64(j.Running)},
			sample{labels: map[string]string{"state": "failed"}, value: float64(j.Failed)},
			sample{labels: map[string]string{"state": "stuck"}, value: float64(j.Stuck)})
	}
	if ev, err := d.Events(EventFilter{}); err == nil && ev.Hours > 0 {
		var es []sample
		for _, l := range ev.Logs {
			es = append(es,
				sample{labels: map[string]string{"log": l.Name, "level": "critical"}, value: float64(l.Critical)},
				sample{labels: map[string]string{"log": l.Name, "level": "error"}, value: float64(l.Errors)},
				sample{labels: map[string]string{"log": l.Name, "level": "warning"}, value: float64(l.Warnings)})
		}
		w("hvapi_events", "Events cached per log and level (last events.hours).", "gauge", es...)
	}
	if c, err := d.HostCluster(); err == nil && c.Clustered {
		var ns []sample
		for _, n := range c.Nodes {
			v := 0.0
			if n.State == "Up" {
				v = 1
			}
			ns = append(ns, sample{labels: map[string]string{"node": n.Name}, value: v})
		}
		w("hvapi_cluster_node_up", "1 when the cluster node is up.", "gauge", ns...)
	}
	if diag, err := d.Diagnose(o); err == nil {
		status := map[string]float64{"ok": 0, "warning": 1, "critical": 2}[diag.Status]
		w("hvapi_diagnostics_status", "0 ok, 1 warning, 2 critical.", "gauge", sample{value: status})
		w("hvapi_diagnostics_findings", "Diagnostics findings by severity.", "gauge",
			sample{labels: map[string]string{"severity": "critical"}, value: float64(diag.Critical)},
			sample{labels: map[string]string{"severity": "warning"}, value: float64(diag.Warnings)},
			sample{labels: map[string]string{"severity": "info"}, value: float64(diag.Info)})
	}
	return b.String()
}

type sample struct {
	labels map[string]string
	value  float64
}

func settingsErrs(d *Data) int {
	if d.Settings == nil {
		return 0
	}
	return len(d.Settings.errs)
}

func escapeLabel(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s)
}

func formatFloat(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%g", f)
}
