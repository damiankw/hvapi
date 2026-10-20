package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/damiankw/hvapi/internal/config"
	"github.com/damiankw/hvapi/internal/hyperv"
	"github.com/damiankw/hvapi/internal/wmi"
)

func testConfig(burst int) *config.Config {
	h := sha256.Sum256([]byte("secret"))
	cfg := &config.Config{Prefixes: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, KeyIDs: []string{"mon"}, KeyHash: [][32]byte{h}}
	cfg.RateLimit.PerSecond, cfg.RateLimit.Burst = 1, burst
	return cfg
}

func TestMetricsAndDiagnosticsRoutes(t *testing.T) {
	cfg := testConfig(100)
	p := hyperv.NewPoller(wmi.New(), nil, time.Second, time.Second)
	p.PollOnce(context.Background())
	srv := New(cfg, slog.New(slog.NewJSONHandler(io.Discard, nil)), Deps{Data: p.Data, Status: p.Status})
	get := func(path string) (int, string, string) {
		r := httptest.NewRequest("GET", path, nil)
		r.RemoteAddr = "192.0.2.5:1"
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		srv.Handler.ServeHTTP(w, r)
		return w.Code, w.Body.String(), w.Header().Get("Content-Type")
	}
	if code, body, ct := get("/metrics"); code != 200 || !strings.HasPrefix(ct, "text/plain; version=0.0.4") || !strings.Contains(body, `hvapi_info{version="`) || !strings.Contains(body, "hvapi_cache_ready 1") {
		t.Errorf("/metrics = %d %s %s", code, ct, body)
	}
	if code, body, _ := get("/diagnostics?checkpoint_days=3"); code != 200 || !strings.Contains(body, `"checkpoint_days": 3`) || !strings.Contains(body, `"status"`) {
		t.Errorf("/diagnostics = %d %s", code, body)
	}
	if code, body, _ := get("/wmi/classes"); code != 200 || !strings.Contains(body, "Msvm_ComputerSystem") {
		t.Errorf("/wmi/classes = %d", code)
	}
	if code, _, _ := get("/wmi/Msvm_ComputerSystem"); code != 503 {
		t.Errorf("/wmi/{class} without on-demand = %d", code)
	}
	if code, _, _ := get("/events?level=loud"); code != 400 {
		t.Errorf("bad level = %d", code)
	}
	if code, _, _ := get("/events/nope"); code != 404 {
		t.Errorf("unknown log = %d", code)
	}
	if code, _, _ := get("/jobs"); code != 200 {
		t.Errorf("/jobs = %d", code)
	}
}

func testServer(t *testing.T) *http.Server {
	t.Helper()
	return New(testConfig(2), slog.New(slog.NewJSONHandler(io.Discard, nil)), Deps{})
}

func TestAuthAndRouting(t *testing.T) {
	srv := testServer(t)
	do := func(method, path, ip, key string) int {
		r := httptest.NewRequest(method, path, nil)
		r.RemoteAddr = ip + ":5555"
		if key != "" {
			r.Header.Set("X-API-Key", key)
		}
		w := httptest.NewRecorder()
		srv.Handler.ServeHTTP(w, r)
		b, _ := io.ReadAll(w.Body)
		t.Logf("%s %s %s -> %d %s", method, path, ip, w.Code, strings.TrimSpace(string(b)))
		return w.Code
	}
	check := func(got, want int) {
		t.Helper()
		if got != want {
			t.Errorf("got %d want %d", got, want)
		}
	}
	check(do("GET", "/health", "192.0.2.5", ""), 200)
	check(do("GET", "/health", "10.9.9.9", ""), 403)
	check(do("GET", "/docs", "192.0.2.5", ""), 200)
	check(do("GET", "/openapi.json", "192.0.2.5", ""), 200)
	check(do("GET", "/openapi.json", "10.9.9.9", ""), 403)
	check(do("GET", "/version", "192.0.2.5", ""), 401)
	check(do("GET", "/version", "192.0.2.5", "wrong"), 401)
	check(do("GET", "/version", "192.0.2.5", "secret"), 200)
	check(do("POST", "/version", "192.0.2.5", "secret"), 405)
	check(do("GET", "/nope", "192.0.2.5", "secret"), 404)
	check(do("GET", "/version", "192.0.2.5", "secret"), 429)
}

// With no poller wired in, VM endpoints say the data is not collected yet.
func TestVMEndpointsWithoutData(t *testing.T) {
	srv := New(testConfig(100), slog.New(slog.NewJSONHandler(io.Discard, nil)), Deps{})
	for _, path := range []string{"/vms", "/vms/summary", "/vms/x", "/vms/x/config", "/vms/x/disks"} {
		r := httptest.NewRequest("GET", path, nil)
		r.RemoteAddr = "192.0.2.5:1"
		r.Header.Set("X-API-Key", "secret")
		w := httptest.NewRecorder()
		srv.Handler.ServeHTTP(w, r)
		if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") == "" {
			t.Errorf("%s: got %d, want 503 with Retry-After", path, w.Code)
		}
	}
}

// With the poller fed by the non-Windows WMI stub, the endpoints answer with
// empty data and notes, and unknown VMs are 404.
func TestVMEndpointsWithStubPoller(t *testing.T) {
	cfg := testConfig(100)
	p := hyperv.NewPoller(wmi.New(), nil, time.Second, time.Second)
	p.PollOnce(context.Background())
	srv := New(cfg, slog.New(slog.NewJSONHandler(io.Discard, nil)), Deps{Data: p.Data, Status: p.Status})
	get := func(path string) (int, string) {
		r := httptest.NewRequest("GET", path, nil)
		r.RemoteAddr = "192.0.2.5:1"
		r.Header.Set("X-API-Key", "secret")
		w := httptest.NewRecorder()
		srv.Handler.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	if code, body := get("/vms"); code != 200 || !strings.Contains(body, `"count": 0`) || !strings.Contains(body, "notes") {
		t.Errorf("/vms = %d %s", code, body)
	}
	if code, _ := get("/vms/summary"); code != 200 {
		t.Errorf("/vms/summary = %d", code)
	}
	if code, _ := get("/vms/does-not-exist"); code != 404 {
		t.Errorf("/vms/does-not-exist = %d", code)
	}
	if code, body := get("/status"); code != 200 || !strings.Contains(body, `"wmi_available": false`) {
		t.Errorf("/status = %d %s", code, body)
	}
	// On-demand endpoints without a caller wired in are unavailable, not errors.
	if code, _ := get("/vms/x/disks/scsi0-0/state"); code != 503 {
		t.Errorf("disk state = %d", code)
	}
	if code, _ := get("/vms/x/thumbnail"); code != 503 {
		t.Errorf("thumbnail = %d", code)
	}
}

// Every route the server registers must be in the OpenAPI document with the
// right security, and vice versa. Spec() is what the CLI and CI publish.
func TestSpecMatchesRoutes(t *testing.T) {
	var doc struct {
		OpenAPI string `json:"openapi"`
		Paths   map[string]map[string]struct {
			Security []map[string][]string `json:"security"`
			Summary  string                `json:"summary"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(Spec(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.OpenAPI != "3.0.3" {
		t.Errorf("openapi version %q", doc.OpenAPI)
	}
	routes := (&state{}).routes()
	if len(doc.Paths) != len(routes) {
		t.Errorf("%d routes but %d documented paths", len(routes), len(doc.Paths))
	}
	for _, r := range routes {
		op, ok := doc.Paths[r.Path][strings.ToLower(r.Method)]
		if !ok {
			t.Errorf("%s %s is not documented", r.Method, r.Path)
			continue
		}
		if op.Summary == "" {
			t.Errorf("%s %s has no summary", r.Method, r.Path)
		}
		if (len(op.Security) > 0) != r.Auth {
			t.Errorf("%s %s: documented security %v, Auth=%v", r.Method, r.Path, op.Security, r.Auth)
		}
	}
}

// The OpenAPI document marks every field without omitempty as required.
// Required must mean never null: encode each route's zero-value response
// the way the handlers do and walk the document checking.
func TestRequiredFieldsAreNeverNull(t *testing.T) {
	var doc struct {
		Paths map[string]map[string]struct {
			Responses map[string]struct {
				Content map[string]struct {
					Schema map[string]any `json:"schema"`
				} `json:"content"`
			} `json:"responses"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]map[string]any `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(Spec(), &doc); err != nil {
		t.Fatal(err)
	}
	var walk func(path string, schema map[string]any, value any)
	walk = func(path string, schema map[string]any, value any) {
		if ref, ok := schema["$ref"].(string); ok {
			name := strings.TrimPrefix(ref, "#/components/schemas/")
			schema = doc.Components.Schemas[name]
		}
		if all, ok := schema["allOf"].([]any); ok && len(all) > 0 {
			walk(path, all[0].(map[string]any), value)
			return
		}
		switch schema["type"] {
		case "object":
			obj, ok := value.(map[string]any)
			if !ok {
				if value == nil && path != "" {
					t.Errorf("%s: required object is null", path)
				}
				return
			}
			req, _ := schema["required"].([]any)
			props, _ := schema["properties"].(map[string]any)
			for _, r := range req {
				name := r.(string)
				v, present := obj[name]
				if !present {
					t.Errorf("%s.%s: required field missing", path, name)
					continue
				}
				if v == nil {
					t.Errorf("%s.%s: required field is null", path, name)
					continue
				}
				if ps, ok := props[name].(map[string]any); ok {
					walk(path+"."+name, ps, v)
				}
			}
		case "array":
			if value == nil {
				t.Errorf("%s: required array is null", path)
			}
		}
	}
	for p, ops := range doc.Paths {
		for _, op := range ops {
			c, ok := op.Responses["200"].Content["application/json"]
			if !ok {
				continue
			}
			var zero any
			for _, r := range (&state{}).routes() {
				if r.Path == p {
					zero = r.Response
				}
			}
			raw, err := json.Marshal(normalize(zero))
			if err != nil {
				t.Fatal(err)
			}
			var decoded any
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			walk(p, c.Schema, decoded)
		}
	}
}

func TestNormalize(t *testing.T) {
	type inner struct {
		Tags []string          `json:"tags"`
		M    map[string]int    `json:"m"`
		P    *[]int            `json:"p,omitempty"`
		Any  any               `json:"any,omitempty"` // an empty interface has nothing to fill; such fields must be optional
		List []inner           `json:"list"`
		Deep map[string][]bool `json:"deep"`
	}
	v := inner{List: []inner{{}}, Deep: map[string][]bool{"k": nil}}
	raw, _ := json.Marshal(normalize(v))
	if strings.Contains(string(raw), "null") {
		t.Errorf("nulls remain: %s", raw)
	}
	ptr := &inner{}
	normalize(ptr)
	if ptr.Tags == nil || ptr.M == nil {
		t.Error("pointer not normalized in place")
	}
	if normalize(nil) != nil {
		t.Error("nil")
	}
}

func TestServedSpecIsTheSame(t *testing.T) {
	srv := testServer(t)
	r := httptest.NewRequest("GET", "/openapi.json", nil)
	r.RemoteAddr = "192.0.2.5:1"
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, r)
	if w.Body.String() != string(Spec()) {
		t.Error("served document differs from Spec()")
	}
}
