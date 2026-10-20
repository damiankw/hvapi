// Package wmi is a strictly read-only WMI client.
//
// Queries go through ExecQuery only. Method calls are limited to the
// allowlist in Methods; nothing else can be invoked, so the API cannot change
// anything on the host however a handler is written.
package wmi

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Row is one WMI instance: property name to value. Values are Go scalars
// (string, bool, int32, uint32, float64...), []any for arrays, Row for
// embedded objects, or nil for NULL. WMI returns 64-bit integers and
// datetimes as strings; the hyperv package has helpers to parse them.
// The instance's object path is stored under PathKey.
type Row map[string]any

// PathKey is the Row key holding the instance's WMI object path.
const PathKey = "__PATH"

// Namespaces hvapi reads from.
const (
	NSVirtualization = `root\virtualization\v2`
	NSCIMv2          = `root\cimv2`
	NSStorage        = `root\Microsoft\Windows\Storage`
	NSStandardCIMv2  = `root\StandardCimv2`
	NSCluster        = `root\MSCluster`
)

// Methods is the complete list of WMI methods hvapi may invoke. Each one
// only reads; none of them changes host or VM state.
var Methods = map[string]bool{
	"GetSummaryInformation":          true,
	"GetVirtualHardDiskSettingData":  true,
	"GetVirtualHardDiskState":        true,
	"GetVirtualSystemThumbnailImage": true,
}

// ErrUnavailable is returned where WMI does not exist (any OS but Windows).
var ErrUnavailable = errors.New("WMI is only available on Windows")

// ErrNotAllowed is returned for a method outside the allowlist or a query
// that is not a plain read.
var ErrNotAllowed = errors.New("not allowed: hvapi is read-only")

// ErrInvalidClass is returned when a class does not exist in the namespace,
// which is normal on hosts without that feature (older OS, no cluster).
var ErrInvalidClass = errors.New("class not found in namespace")

// ErrAccessDenied is returned when WMI refuses the caller. For Hyper-V data
// this almost always means the process is not elevated and the account is
// not in Hyper-V Administrators.
var ErrAccessDenied = errors.New("access denied by WMI")

// ErrNamespaceNotFound is returned when the namespace does not exist, for
// example root\virtualization\v2 on a host without the Hyper-V role.
var ErrNamespaceNotFound = errors.New("WMI namespace not found")

// Querier is what the collector needs; Client implements it, tests fake it.
type Querier interface {
	Query(ctx context.Context, namespace, wql string) ([]Row, error)
}

// Caller invokes an allowlisted read-only method on an object.
type Caller interface {
	Call(ctx context.Context, namespace, objectPath, method string, in map[string]any) (Row, error)
}

var readQuery = regexp.MustCompile(`(?i)^\s*(SELECT|ASSOCIATORS\s+OF|REFERENCES\s+OF)\b`)

// CheckQuery rejects anything that is not a plain WQL read.
func CheckQuery(wql string) error {
	if !readQuery.MatchString(wql) {
		return fmt.Errorf("%w: query must start with SELECT, ASSOCIATORS OF or REFERENCES OF", ErrNotAllowed)
	}
	return nil
}

// CheckMethod rejects any method outside the allowlist.
func CheckMethod(method string) error {
	if !Methods[method] {
		return fmt.Errorf("%w: method %q is not in the read-only allowlist", ErrNotAllowed, method)
	}
	return nil
}

// Escape quotes a string for use inside a WQL string literal.
func Escape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `'`, `\'`)
}

// String returns the named property as a string, or "" if absent or NULL.
func (r Row) String(name string) string {
	switch v := r[name].(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

// Strings returns an array property as strings (a scalar becomes one element).
func (r Row) Strings(name string) []string {
	switch v := r[name].(type) {
	case nil:
		return nil
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if e != nil {
				out = append(out, fmt.Sprint(e))
			}
		}
		return out
	case []string:
		return v
	default:
		return []string{fmt.Sprint(v)}
	}
}

// Int returns a numeric property as int64. WMI uint64 values arrive as
// strings and are parsed. Returns 0 if absent or not a number.
func (r Row) Int(name string) int64 {
	n, _ := toInt64(r[name])
	return n
}

// Ints returns a numeric array property.
func (r Row) Ints(name string) []int64 {
	switch v := r[name].(type) {
	case nil:
		return nil
	case []any:
		out := make([]int64, 0, len(v))
		for _, e := range v {
			if n, ok := toInt64(e); ok {
				out = append(out, n)
			}
		}
		return out
	default:
		if n, ok := toInt64(v); ok {
			return []int64{n}
		}
		return nil
	}
}

// Float returns a numeric property as float64.
func (r Row) Float(name string) float64 {
	switch v := r[name].(type) {
	case float32:
		return float64(v)
	case float64:
		return v
	default:
		n, _ := toInt64(v)
		return float64(n)
	}
}

// Bool returns a boolean property; "true"/"1" strings count as true.
func (r Row) Bool(name string) bool {
	switch v := r[name].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true") || v == "1"
	default:
		n, _ := toInt64(v)
		return n != 0
	}
}

// Has reports whether the property is present and not NULL.
func (r Row) Has(name string) bool {
	v, ok := r[name]
	return ok && v != nil
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case uint:
		return int64(n), true
	case uint8:
		return int64(n), true
	case uint16:
		return int64(n), true
	case uint32:
		return int64(n), true
	case uint64:
		return int64(n), true
	case float32:
		return int64(n), true
	case float64:
		return int64(n), true
	case bool:
		if n {
			return 1, true
		}
		return 0, true
	case string:
		s := strings.TrimSpace(n)
		if out, err := strconv.ParseInt(s, 10, 64); err == nil {
			return out, true
		}
		if out, err := strconv.ParseUint(s, 10, 64); err == nil {
			return int64(out), true // wraps above MaxInt64, as a uint64 cast would
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return int64(f), true
		}
	}
	return 0, false
}
