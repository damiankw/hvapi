package hyperv

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/damiankw/hvapi/internal/wmi"
)

// RawClass is one class /wmi/{class} may read.
type RawClass struct {
	Class     string `json:"class"`
	Namespace string `json:"namespace"`
	UsedBy    string `json:"used_by,omitempty" doc:"The poller source that reads this class, when one does."`
}

// RawClassList is the /wmi/classes response.
type RawClassList struct {
	Count   int        `json:"count"`
	Classes []RawClass `json:"classes"`
}

// RawResult is the /wmi/{class} response.
type RawResult struct {
	Class     string    `json:"class"`
	Namespace string    `json:"namespace"`
	QueriedAt time.Time `json:"queried_at"`
	Count     int       `json:"count" doc:"Rows returned, after limit."`
	Truncated bool      `json:"truncated" doc:"True when more rows existed than limit."`
	Rows      []wmi.Row `json:"rows" doc:"Raw WMI instances; property names and value types are as WMI reports them. __PATH holds the object path."`
}

// extraRawClasses are read-only classes worth exposing that no poller
// source reads.
var extraRawClasses = []RawClass{
	{"Msvm_ResourcePool", wmi.NSVirtualization, ""},
	{"Msvm_DiskDrive", wmi.NSVirtualization, ""},
	{"Msvm_GpuPartition", wmi.NSVirtualization, ""},
	{"Msvm_PhysicalGPUInfo", wmi.NSVirtualization, ""},
	{"Msvm_VirtualSystemManagementService", wmi.NSVirtualization, ""},
	{"Msvm_SummaryInformation", wmi.NSVirtualization, ""},
	{"Win32_LogicalDisk", wmi.NSCIMv2, ""},
	{"Win32_NetworkAdapter", wmi.NSCIMv2, ""},
	{"Win32_PhysicalMemory", wmi.NSCIMv2, ""},
	{"Win32_BIOS", wmi.NSCIMv2, ""},
	{"MSFT_Disk", wmi.NSStorage, ""},
	{"MSFT_PhysicalDisk", wmi.NSStorage, ""},
	{"MSFT_NetAdapterStatistics", wmi.NSStandardCIMv2, ""},
}

var fromRe = regexp.MustCompile(`(?i)\bFROM\s+([A-Za-z0-9_]+)`)

// RawClasses lists every class /wmi/{class} accepts: all the classes the
// poller reads plus extraRawClasses. Reads are ExecQuery only.
func RawClasses() *RawClassList {
	seen := map[string]RawClass{}
	for _, srcs := range [][]source{stateSources, settingsSources} {
		for _, s := range srcs {
			m := fromRe.FindStringSubmatch(s.query)
			if m == nil {
				continue
			}
			if _, ok := seen[strings.ToLower(m[1])]; !ok {
				seen[strings.ToLower(m[1])] = RawClass{Class: m[1], Namespace: s.namespace, UsedBy: s.name}
			}
		}
	}
	for _, c := range extraRawClasses {
		if _, ok := seen[strings.ToLower(c.Class)]; !ok {
			seen[strings.ToLower(c.Class)] = c
		}
	}
	out := &RawClassList{}
	for _, c := range seen {
		out.Classes = append(out.Classes, c)
	}
	sort.Slice(out.Classes, func(i, j int) bool { return out.Classes[i].Class < out.Classes[j].Class })
	out.Count = len(out.Classes)
	return out
}

// RawQuery implements /wmi/{class}: SELECT * FROM an allowlisted class.
func (o *OnDemand) RawQuery(ctx context.Context, class string, limit int) (*RawResult, error) {
	if o == nil || o.querier == nil {
		return nil, ErrOnDemandUnavailable
	}
	var rc *RawClass
	for _, c := range RawClasses().Classes {
		if strings.EqualFold(c.Class, class) {
			cc := c
			rc = &cc
		}
	}
	if rc == nil {
		return nil, fmt.Errorf("%w: class %q is not in the allowlist; see /wmi/classes", ErrNotFound, class)
	}
	if limit <= 0 {
		limit = 1000
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	rows, err := o.querier.Query(ctx, rc.Namespace, "SELECT * FROM "+rc.Class)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rc.Class, err)
	}
	out := &RawResult{Class: rc.Class, Namespace: rc.Namespace, QueriedAt: o.now().UTC(), Rows: rows}
	if out.Rows == nil {
		out.Rows = []wmi.Row{}
	}
	if len(out.Rows) > limit {
		out.Rows = out.Rows[:limit]
		out.Truncated = true
	}
	out.Count = len(out.Rows)
	return out, nil
}
