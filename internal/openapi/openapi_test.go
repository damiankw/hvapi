package openapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type inner struct {
	Name string `json:"name" doc:"The name."`
}

type sample struct {
	ID       string            `json:"id" doc:"Identifier."`
	Count    int               `json:"count"`
	Ratio    float64           `json:"ratio,omitempty"`
	When     time.Time         `json:"when"`
	Items    []inner           `json:"items"`
	Labels   map[string]string `json:"labels"`
	Nested   inner             `json:"nested" doc:"A nested object."`
	hidden   int               //nolint:unused
	Skipped  string            `json:"-"`
	Untagged bool
}

func TestBuild(t *testing.T) {
	doc := Build(
		Info{Title: "t", Version: "1"},
		[]Tag{{Name: "x", Description: "d"}},
		[]Operation{
			{Method: "GET", Path: "/health", Summary: "h", Tag: "x", Response: map[string]any{}},
			{Method: "GET", Path: "/vms/{id}/disks/{diskId}", Summary: "s", Tag: "x", Auth: true, Response: sample{}, Errors: []int{404}},
		},
	)
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{
		`"openapi":"3.0.3"`,
		`"/health"`, `"/vms/{id}/disks/{diskId}"`,
		`"operationId":"get-vms-by-id-disks-by-diskId"`,
		`"in":"path","name":"id"`, `"in":"path","name":"diskId"`,
		`"$ref":"#/components/schemas/sample"`, `"$ref":"#/components/schemas/inner"`,
		`"format":"date-time"`, `"additionalProperties"`,
		`"description":"Identifier."`, `"allOf"`,
		`"required":["Untagged","count","id","items","labels","nested","when"]`,
		`"ApiKeyAuth"`, `"404"`, `"429"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("document lacks %s\n%s", want, s)
		}
	}
	for _, unwanted := range []string{`"hidden"`, `"Skipped"`} {
		if strings.Contains(s, unwanted) {
			t.Errorf("document leaks %s", unwanted)
		}
	}

	// /health needs no key, so no 401 or 429 and empty security.
	var parsed struct {
		Paths map[string]map[string]struct {
			Security  []map[string][]string `json:"security"`
			Responses map[string]any        `json:"responses"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	h := parsed.Paths["/health"]["get"]
	if len(h.Security) != 0 || h.Responses["401"] != nil || h.Responses["429"] != nil {
		t.Errorf("/health should be open: %+v", h)
	}
	v := parsed.Paths["/vms/{id}/disks/{diskId}"]["get"]
	if len(v.Security) != 1 || v.Responses["401"] == nil {
		t.Errorf("keyed route should need a key: %+v", v)
	}
}

type loop struct {
	Next *loop `json:"next"`
}

func TestContentTypeAndQueryParams(t *testing.T) {
	doc := Build(Info{}, nil, []Operation{
		{Method: "GET", Path: "/img", Response: "", ContentType: "image/png", Params: []Param{{Name: "width", Description: "w", Type: "integer"}}},
		{Method: "GET", Path: "/txt", Response: "", ContentType: "text/plain; version=0.0.4"},
	})
	raw, _ := json.Marshal(doc)
	s := string(raw)
	for _, want := range []string{`"image/png":{"schema":{"format":"binary","type":"string"}}`, `"in":"query","name":"width"`, `"type":"integer"`, `"text/plain; version=0.0.4":{"schema":{"type":"string"}}`} {
		if !strings.Contains(s, want) {
			t.Errorf("document lacks %s\n%s", want, s)
		}
	}
}

func TestRecursiveType(t *testing.T) {
	doc := Build(Info{}, nil, []Operation{{Method: "GET", Path: "/", Response: loop{}}})
	if _, err := json.Marshal(doc); err != nil {
		t.Fatal(err)
	}
}
