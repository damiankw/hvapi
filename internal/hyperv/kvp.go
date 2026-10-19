package hyperv

import (
	"encoding/xml"
	"strings"
)

// kvpItem is one Msvm_KvpExchangeDataItem serialised as CIM XML, which is how
// Msvm_KvpExchangeComponent exposes guest key/value pairs.
type kvpItem struct {
	Properties []struct {
		Name  string `xml:"NAME,attr"`
		Value string `xml:"VALUE"`
	} `xml:"PROPERTY"`
}

// ParseKVP turns the XML strings of GuestIntrinsicExchangeItems (or
// GuestExchangeItems) into a name to value map.
func ParseKVP(items []string) map[string]string {
	out := map[string]string{}
	for _, raw := range items {
		var it kvpItem
		if err := xml.Unmarshal([]byte(raw), &it); err != nil {
			continue
		}
		var name, data string
		for _, p := range it.Properties {
			switch p.Name {
			case "Name":
				name = p.Value
			case "Data":
				data = p.Value
			}
		}
		if name != "" {
			out[name] = data
		}
	}
	return out
}

// splitList splits the ";"-separated address lists in KVP data.
func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ";") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
