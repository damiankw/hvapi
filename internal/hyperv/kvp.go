package hyperv

import (
	"encoding/xml"
	"strings"
)

// cimInstance is a CIM instance serialised as XML (DTD 2.0), which is how
// WMI embeds objects in string properties and method results.
type cimInstance struct {
	ClassName  string `xml:"CLASSNAME,attr"`
	Properties []struct {
		Name  string `xml:"NAME,attr"`
		Value string `xml:"VALUE"`
	} `xml:"PROPERTY"`
	Arrays []struct {
		Name   string   `xml:"NAME,attr"`
		Values []string `xml:"VALUE.ARRAY>VALUE"`
	} `xml:"PROPERTY.ARRAY"`
}

// ParseCIMXML decodes one embedded instance into a property map. Array
// properties are joined with ";". Returns nil if the text is not an instance.
func ParseCIMXML(text string) map[string]string {
	var inst cimInstance
	if err := xml.Unmarshal([]byte(text), &inst); err != nil {
		return nil
	}
	out := map[string]string{}
	for _, p := range inst.Properties {
		out[p.Name] = p.Value
	}
	for _, a := range inst.Arrays {
		out[a.Name] = strings.Join(a.Values, ";")
	}
	return out
}

// ParseKVP turns the XML strings of GuestIntrinsicExchangeItems (or
// GuestExchangeItems, HostExchangeItems) into a name to value map.
func ParseKVP(items []string) map[string]string {
	out := map[string]string{}
	for _, raw := range items {
		props := ParseCIMXML(raw)
		if name := props["Name"]; name != "" {
			out[name] = props["Data"]
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
