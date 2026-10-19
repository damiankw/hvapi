// Package openapi builds an OpenAPI 3.0 document from the server's route table
// and its Go response types, so the API and its documentation cannot drift
// apart: a route is documented by the same entry that registers it.
package openapi

import (
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Operation describes one route. Path uses net/http patterns ("/vms/{id}").
type Operation struct {
	Method      string
	Path        string
	Summary     string
	Description string
	Tag         string
	Auth        bool  // needs an API key
	Response    any   // zero value of the 200 response type
	Errors      []int // extra error statuses beyond the common ones
}

// Tag groups operations in the rendered documentation.
type Tag struct {
	Name        string
	Description string
}

// Info is the document header.
type Info struct {
	Title       string
	Version     string
	Description string
}

// Error is the body of every error response.
type Error struct {
	Error string `json:"error" doc:"What went wrong."`
}

// SecurityScheme is the name of the API key scheme in the document.
const SecurityScheme = "ApiKeyAuth"

// HeaderName is the header that carries the API key.
const HeaderName = "X-API-Key"

var commonErrors = map[int]string{
	http.StatusUnauthorized:     "Missing or invalid API key.",
	http.StatusForbidden:        "Caller's address is not in allowed_cidrs.",
	http.StatusNotFound:         "No such resource.",
	http.StatusMethodNotAllowed: "Only GET is supported; the API is read-only.",
	http.StatusTooManyRequests:  "Rate limit for this key exceeded. Retry-After says when.",
}

// Build assembles the document. The result marshals to JSON with encoding/json.
func Build(info Info, tags []Tag, ops []Operation) map[string]any {
	b := &builder{schemas: map[string]any{}, named: map[string]reflect.Type{}}
	errRef := b.schema(reflect.TypeOf(Error{}))

	paths := map[string]map[string]any{}
	for _, op := range ops {
		item, ok := paths[op.Path]
		if !ok {
			item = map[string]any{}
			paths[op.Path] = item
		}
		item[strings.ToLower(op.Method)] = b.operation(op, errRef)
	}

	var tagList []map[string]any
	for _, t := range tags {
		tagList = append(tagList, map[string]any{"name": t.Name, "description": t.Description})
	}

	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       info.Title,
			"version":     info.Version,
			"description": info.Description,
		},
		"tags":  tagList,
		"paths": paths,
		"components": map[string]any{
			"schemas": b.schemas,
			"securitySchemes": map[string]any{
				SecurityScheme: map[string]any{
					"type":        "apiKey",
					"in":          "header",
					"name":        HeaderName,
					"description": "API key issued with 'hvapi hash-key'. Only its hash is stored on the host.",
				},
			},
		},
	}
}

type builder struct {
	schemas map[string]any
	named   map[string]reflect.Type
}

var pathParam = regexp.MustCompile(`\{([^}/.]+)(\.\.\.)?\}`)

func (b *builder) operation(op Operation, errRef any) map[string]any {
	responses := map[string]any{
		"200": map[string]any{
			"description": "OK",
			"content":     jsonContent(b.schema(reflect.TypeOf(op.Response))),
		},
	}
	statuses := []int{http.StatusForbidden, http.StatusMethodNotAllowed}
	if op.Auth {
		statuses = append(statuses, http.StatusUnauthorized, http.StatusTooManyRequests)
	}
	statuses = append(statuses, op.Errors...)
	for _, s := range statuses {
		desc := commonErrors[s]
		if desc == "" {
			desc = http.StatusText(s)
		}
		responses[fmt.Sprint(s)] = map[string]any{"description": desc, "content": jsonContent(errRef)}
	}

	o := map[string]any{
		"operationId": operationID(op),
		"summary":     op.Summary,
		"description": op.Description,
		"tags":        []string{op.Tag},
		"responses":   responses,
	}
	if op.Auth {
		o["security"] = []map[string][]string{{SecurityScheme: {}}}
	} else {
		o["security"] = []map[string][]string{}
	}

	var params []map[string]any
	for _, m := range pathParam.FindAllStringSubmatch(op.Path, -1) {
		params = append(params, map[string]any{
			"name":     m[1],
			"in":       "path",
			"required": true,
			"schema":   map[string]any{"type": "string"},
		})
	}
	if len(params) > 0 {
		o["parameters"] = params
	}
	return o
}

func operationID(op Operation) string {
	p := pathParam.ReplaceAllString(op.Path, "by-$1")
	p = strings.Trim(strings.ReplaceAll(p, "/", "-"), "-")
	if p == "" {
		p = "root"
	}
	return strings.ToLower(op.Method) + "-" + p
}

func jsonContent(schema any) map[string]any {
	return map[string]any{"application/json": map[string]any{"schema": schema}}
}

var timeType = reflect.TypeOf(time.Time{})

// schema returns a JSON Schema for t. Named structs are added to
// components/schemas once and referenced by name; everything else is inline.
func (b *builder) schema(t reflect.Type) any {
	if t == nil {
		return map[string]any{}
	}
	if t == timeType {
		return map[string]any{"type": "string", "format": "date-time"}
	}
	switch t.Kind() {
	case reflect.Pointer:
		return b.schema(t.Elem())
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int64, reflect.Uint, reflect.Uint64, reflect.Uintptr:
		return map[string]any{"type": "integer", "format": "int64"}
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return map[string]any{"type": "integer", "format": "int32"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string", "format": "byte"}
		}
		return map[string]any{"type": "array", "items": b.schema(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": b.schema(t.Elem())}
	case reflect.Interface:
		return map[string]any{}
	case reflect.Struct:
		if t.Name() == "" {
			return b.object(t)
		}
		name := b.componentName(t)
		if _, done := b.schemas[name]; !done {
			b.schemas[name] = map[string]any{} // placeholder guards against recursive types
			b.schemas[name] = b.object(t)
		}
		return map[string]any{"$ref": "#/components/schemas/" + name}
	}
	return map[string]any{}
}

func (b *builder) componentName(t reflect.Type) string {
	name := t.Name()
	if prev, ok := b.named[name]; ok && prev != t {
		parts := strings.Split(t.PkgPath(), "/")
		name = parts[len(parts)-1] + "." + name
	}
	b.named[name] = t
	return name
}

func (b *builder) object(t reflect.Type) map[string]any {
	props := map[string]any{}
	var required []string
	b.fields(t, props, &required)
	sort.Strings(required)
	o := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		o["required"] = required
	}
	return o
}

func (b *builder) fields(t reflect.Type, props map[string]any, required *[]string) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct && f.Tag.Get("json") == "" {
			b.fields(f.Type, props, required)
			continue
		}
		if !f.IsExported() {
			continue
		}
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		s := b.schema(f.Type)
		if doc := f.Tag.Get("doc"); doc != "" {
			if ref, ok := s.(map[string]any); ok && ref["$ref"] != nil {
				// A $ref may not carry siblings in 3.0; wrap it.
				s = map[string]any{"allOf": []any{ref}, "description": doc}
			} else {
				s.(map[string]any)["description"] = doc
			}
		}
		props[name] = s
		if !strings.Contains(opts, "omitempty") {
			*required = append(*required, name)
		}
	}
}
