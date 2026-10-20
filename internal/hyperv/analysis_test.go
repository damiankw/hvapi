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

func findingRules(d *Diagnostics) map[string]int {
	m := map[string]int{}
	for _, f := range d.Findings {
		m[f.Rule]++
	}
	return m
}

func TestDiagnose(t *testing.T) {
	fileSize = func(string) (int64, bool) { return 0, false }
	q := activityFixtures()
	now := time.Now()
	p := NewPoller(q, nil, time.Second, time.Second)
	p.SetEvents(EventSource{Reader: sampleEvents(now), Hours: 24, MaxPerLog: 500})
	p.PollOnce(context.Background())
	d := p.Data()

	diag, err := d.Diagnose(DiagOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rules := findingRules(diag)
	b, _ := json.MarshalIndent(diag, "", " ")
	if diag.Status != "critical" || diag.Thresholds.CheckpointDays != 7 || diag.Thresholds.FreeSpacePercent != 10 {
		t.Errorf("status = %s", b)
	}
	// hvhost stopped (critical), checkpoint disks missing (critical, 2), checkpoint_old (2), vm_disk_missing (3 VHDs),
	// replication warning (Archive), D: at 5% free holding Archive (warning), job failed + stuck, cluster node HV02 paused,
	// vmms log errors (warning), VM1 adapter connected so no adapter finding, heartbeat OK so none.
	for rule, want := range map[string]int{"services_stopped": 1, "checkpoint_disk_missing": 2, "checkpoint_old": 2, "vm_disk_missing": 2, "replication_health": 1,
		"volume_low_space": 1, "volume_health": 1, "job_failed": 1, "job_stuck": 1, "cluster_node_down": 1, "event_errors": 1} { // 2 realized VHDs (the ISO is not one)
		if rules[rule] != want {
			t.Errorf("rule %s: got %d want %d\n%s", rule, rules[rule], want, b)
		}
	}
	for _, unwanted := range []string{"heartbeat_missing", "adapter_disconnected", "host_memory_pressure", "host_cpu_high", "cluster_resource_failed"} {
		if rules[unwanted] != 0 {
			t.Errorf("unexpected rule %s\n%s", unwanted, b)
		}
	}
	if diag.Findings[0].Severity != "critical" || diag.Findings[len(diag.Findings)-1].Severity == "critical" {
		t.Error("findings should be sorted by severity")
	}
	if len(diag.Checks) < 15 {
		t.Errorf("checks = %v", diag.Checks)
	}

	// Thresholds change what is reported.
	loose, _ := d.Diagnose(DiagOptions{CheckpointDays: 365, FreeSpacePercent: 1})
	lr := findingRules(loose)
	if lr["checkpoint_old"] != 0 || lr["volume_low_space"] != 0 {
		t.Errorf("loose thresholds still report: %v", lr)
	}
	tight, _ := d.Diagnose(DiagOptions{CheckpointDays: 1, FreeSpacePercent: 90})
	tr := findingRules(tight)
	var crit int
	for _, f := range tight.Findings {
		if f.Rule == "volume_low_space" && f.Severity == "critical" {
			crit++
		}
	}
	if tr["volume_low_space"] != 2 || crit == 0 {
		t.Errorf("tight thresholds: %v critical=%d", tr, crit)
	}

	// A healthy host: strip the problems and expect ok.
	q2 := fixtures()
	q2.rows["Win32_Service"] = []wmi.Row{
		{"Name": "vmms", "State": "Running"}, {"Name": "vmcompute", "State": "Running"}, {"Name": "hvhost", "State": "Running"},
	}
	q2.rows["Msvm_ComputerSystem"][1]["ReplicationHealth"] = int32(1)
	q2.rows["Msvm_ResourceAllocationSettingData"] = nil
	q2.rows["Msvm_StorageAllocationSettingData"] = nil
	d2 := poll(t, q2)
	ok, _ := d2.Diagnose(DiagOptions{})
	if ok.Status != "ok" || ok.Critical != 0 || ok.Warnings != 0 {
		b, _ := json.MarshalIndent(ok, "", " ")
		t.Errorf("healthy host = %s", b)
	}
	if _, err := (&Data{}).Diagnose(DiagOptions{}); !errors.Is(err, ErrNotReady) {
		t.Error("expected not ready")
	}
}

func TestMetrics(t *testing.T) {
	fileSize = func(string) (int64, bool) { return 0, false }
	q := activityFixtures()
	p := NewPoller(q, nil, time.Second, time.Second)
	p.SetEvents(EventSource{Reader: sampleEvents(time.Now()), Hours: 24, MaxPerLog: 500})
	p.PollOnce(context.Background())
	m := p.Data().Metrics(DiagOptions{})
	for _, want := range []string{
		"hvapi_cache_ready 1",
		`hvapi_vm_running{vm_id="` + strings.ToUpper(vm1) + `",vm_name="Web Server"} 1`,
		`hvapi_vm_cpu_percent{vm_id="` + strings.ToUpper(vm1) + `",vm_name="Web Server"} 20`,
		`hvapi_vm_memory_assigned_bytes{vm_id="` + strings.ToUpper(vm1) + `",vm_name="Web Server"} 4294967296`,
		`hvapi_vm_heartbeat_ok{vm_id="` + strings.ToUpper(vm2) + `",vm_name="Archive"} 0`,
		`hvapi_vm_checkpoints{vm_id="` + strings.ToUpper(vm1) + `",vm_name="Web Server"} 2`,
		`hvapi_service_running{service="hvhost"} 0`,
		`hvapi_volume_free_bytes{drive="D",label="VMs",path="\\\\?\\Volume{bbbb}\\"}`,
		`hvapi_jobs{state="failed"} 1`,
		`hvapi_events{level="error",log="vmms"} 1`,
		`hvapi_cluster_node_up{node="HV02"} 0`,
		"hvapi_diagnostics_status 2",
		`hvapi_diagnostics_findings{severity="critical"}`,
		"# TYPE hvapi_host_cpu_percent gauge\nhvapi_host_cpu_percent 23",
	} {
		if !strings.Contains(m, want) {
			t.Errorf("metrics lack %q\n%s", want, m)
		}
	}
	if strings.Contains(m, "+Inf") || strings.Contains(m, "NaN") {
		t.Error("bad values")
	}
	if (&Data{}).Metrics(DiagOptions{}) != "# HELP hvapi_cache_ready 1 when the state poll has completed at least once.\n# TYPE hvapi_cache_ready gauge\nhvapi_cache_ready 0\n" {
		t.Error("empty cache metrics")
	}
}

func TestRawClasses(t *testing.T) {
	list := RawClasses()
	names := map[string]RawClass{}
	for _, c := range list.Classes {
		names[c.Class] = c
	}
	if list.Count < 50 || names["Msvm_ComputerSystem"].UsedBy != "computer_system" || names["Msvm_ComputerSystem"].Namespace != wmi.NSVirtualization ||
		names["MSFT_Volume"].Namespace != wmi.NSStorage || names["Msvm_ResourcePool"].Class == "" || names["Win32_DeviceGuard"].Namespace == "" {
		t.Errorf("classes = %+v", list)
	}
	q := fixtures()
	d := poll(t, q)
	od := NewOnDemand(nil, q, func() *Data { return d })
	res, err := od.RawQuery(context.Background(), "msvm_computersystem", 1)
	if err != nil || res.Count != 1 || !res.Truncated || res.Class != "Msvm_ComputerSystem" || res.Rows[0]["ElementName"] != "Web Server" {
		t.Errorf("raw = %+v %v", res, err)
	}
	if _, err := od.RawQuery(context.Background(), "Win32_Process", 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("non-allowlisted: %v", err)
	}
	if _, err := od.RawQuery(context.Background(), "MSCluster_Node", 0); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("missing class should surface the WMI error, got %v", err)
	}
	if _, err := NewOnDemand(nil, nil, nil).RawQuery(context.Background(), "x", 0); !errors.Is(err, ErrOnDemandUnavailable) {
		t.Error("nil querier")
	}
}
