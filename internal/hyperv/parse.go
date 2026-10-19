// Package hyperv collects Hyper-V data over WMI into an in-memory model and
// builds the API responses from it.
package hyperv

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CIMTime parses a CIM_DATETIME ("20240131235959.123456+600"). The offset is
// minutes east of UTC; "***" means unknown and is treated as UTC. Returns nil
// for empty or unparseable input.
func CIMTime(s string) *time.Time {
	s = strings.TrimSpace(s)
	if len(s) < 21 || s == "00000000000000.000000+000" {
		return nil
	}
	y, err1 := strconv.Atoi(s[0:4])
	mo, err2 := strconv.Atoi(s[4:6])
	d, err3 := strconv.Atoi(s[6:8])
	h, err4 := strconv.Atoi(s[8:10])
	mi, err5 := strconv.Atoi(s[10:12])
	sec, err6 := strconv.Atoi(s[12:14])
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil || err6 != nil || y == 0 {
		return nil
	}
	micro := 0
	if s[14] == '.' {
		frac := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return '0'
		}, s[15:21])
		micro, _ = strconv.Atoi(frac)
	}
	offset := 0
	if len(s) >= 25 {
		if m, err := strconv.Atoi(s[22:25]); err == nil {
			offset = m * 60
			if s[21] == '-' {
				offset = -offset
			}
		}
	}
	t := time.Date(y, time.Month(mo), d, h, mi, sec, micro*1000, time.FixedZone("", offset)).UTC()
	return &t
}

// CIMInterval parses a CIM interval ("00000000000030.000000:000", that is
// ddddddddHHMMSS.ffffff:000) into a Duration. Returns 0 if unparseable.
func CIMInterval(s string) time.Duration {
	s = strings.TrimSpace(s)
	if len(s) < 14 {
		return 0
	}
	days, err1 := strconv.Atoi(s[0:8])
	h, err2 := strconv.Atoi(s[8:10])
	m, err3 := strconv.Atoi(s[10:12])
	sec, err4 := strconv.Atoi(s[12:14])
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return 0
	}
	d := time.Duration(days)*24*time.Hour + time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(sec)*time.Second
	if len(s) >= 21 && s[14] == '.' {
		if micro, err := strconv.Atoi(s[15:21]); err == nil {
			d += time.Duration(micro) * time.Microsecond
		}
	}
	return d
}

var guidRe = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// IsGUID reports whether s looks like a GUID.
func IsGUID(s string) bool { return guidRe.MatchString(s) }

// NormalizeID upper-cases a GUID so IDs compare reliably.
func NormalizeID(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// VMIDFromInstanceID extracts the VM GUID from a settings InstanceID such as
// "Microsoft:GUID\\pool\\0" or "Microsoft:GuestNetwork\\GUID\\adapter".
func VMIDFromInstanceID(id string) string {
	for _, seg := range instanceSegments(id) {
		if IsGUID(seg) {
			return NormalizeID(seg)
		}
	}
	return ""
}

// instanceSegments splits an InstanceID into its backslash-separated parts
// with the "Microsoft:" prefix removed.
func instanceSegments(id string) []string {
	id = strings.TrimPrefix(id, "Microsoft:")
	return strings.Split(id, `\`)
}

// PathKey returns the value of key="..." in a WMI object path, unescaped.
// Returns "" if the key is absent.
func PathKey(path, key string) string {
	lower := strings.ToLower(path)
	needle := strings.ToLower(key) + `="`
	i := -1
	for _, sep := range []string{".", ","} {
		if j := strings.Index(lower, sep+needle); j >= 0 && (i < 0 || j < i) {
			i = j + 1
		}
	}
	if i < 0 {
		return ""
	}
	rest := path[i+len(key)+2:]
	var b strings.Builder
	for j := 0; j < len(rest); j++ {
		c := rest[j]
		if c == '\\' && j+1 < len(rest) {
			j++
			b.WriteByte(rest[j])
			continue
		}
		if c == '"' {
			break
		}
		b.WriteByte(c)
	}
	return b.String()
}

// PathClass returns the class name in a WMI object path.
func PathClass(path string) string {
	p := path
	if strings.HasPrefix(p, `\\`) {
		// \\server\namespace:Class.key="value" -- neither server nor
		// namespace can contain a colon, values can.
		if i := strings.Index(p, ":"); i >= 0 {
			p = p[i+1:]
		}
	}
	if i := strings.Index(p, "."); i >= 0 {
		p = p[:i]
	}
	return p
}

// NormPath makes an object path comparable: lower-cased, with the server
// prefix removed so references from different sources match.
func NormPath(path string) string {
	p := strings.ToLower(strings.TrimSpace(path))
	if strings.HasPrefix(p, `\\`) {
		if i := strings.Index(p[2:], `\`); i >= 0 {
			p = p[i+3:]
		}
	}
	return p
}

// MB converts a byte count to whole megabytes.
func MB(bytes int64) int64 { return bytes / (1024 * 1024) }

// lookup maps a code to a label, falling back to the number itself.
func lookup(m map[int64]string, code int64) string {
	if s, ok := m[code]; ok {
		return s
	}
	return strconv.FormatInt(code, 10)
}
