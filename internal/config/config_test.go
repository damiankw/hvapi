package config

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damiankw/hvapi/internal/tlscert"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const goodHash = "2bb80d537b1da3e38bd30361aa855686bde0eacd7162fef6a25fe97bf527a25b" // sha256("secret")

func TestLoadDefaultsAndBOM(t *testing.T) {
	p := write(t, "\xEF\xBB\xBF"+`{"api_keys":[{"id":"mon","sha256":"`+goodHash+`"}],"allowed_cidrs":["10.0.0.5"]}`)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != DefaultListen || c.RateLimit.PerSecond != 10 || c.RateLimit.Burst != 20 || c.Poll.StateSeconds != 10 || c.Poll.HeavySeconds != 60 {
		t.Errorf("defaults not applied: %+v", c)
	}
	if len(c.KeyIDs) != 1 || c.KeyIDs[0] != "mon" {
		t.Errorf("key ids = %v", c.KeyIDs)
	}
	if len(c.Prefixes) != 1 || c.Prefixes[0].String() != "10.0.0.5/32" {
		t.Errorf("prefixes = %v", c.Prefixes)
	}
}

func TestLoadReportsEveryError(t *testing.T) {
	p := write(t, `{"listen":"nope","api_keys":[{"id":"","sha256":"xyz"}],"allowed_cidrs":["bad"],"tls":{"cert_file":"c.pem"}}`)
	_, err := Load(p)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"listen", "id is required", "64 hex", "not a CIDR", "tls"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

func TestUnknownFieldRejected(t *testing.T) {
	p := write(t, `{"listne":"127.0.0.1:1"}`)
	if _, err := Read(p); err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestValidateIsIdempotent(t *testing.T) {
	c := &Config{Listen: "127.0.0.1:1", APIKeys: []APIKey{{ID: "a", SHA256: goodHash}}, AllowedCIDRs: []string{"10.0.0.0/8"}}
	for i := 0; i < 2; i++ {
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.KeyIDs) != 1 || len(c.Prefixes) != 1 {
		t.Errorf("parsed fields duplicated: %v %v", c.KeyIDs, c.Prefixes)
	}
}

func TestTLS(t *testing.T) {
	dir := t.TempDir()
	certPEM, keyPEM, err := tlscert.Generate(tlscert.Options{CommonName: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "c.pem"), certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "k.pem"), keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	base := `"api_keys":[{"id":"mon","sha256":"` + goodHash + `"}],"allowed_cidrs":["10.0.0.0/24"]`
	p := filepath.Join(dir, "config.json")

	// Relative paths resolve against the config directory; defaults apply.
	if err := os.WriteFile(p, []byte(`{`+base+`,"tls":{"cert_file":"c.pem","key_file":"k.pem"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !c.TLSEnabled() || c.Scheme() != "https" || c.TLSCertPath != filepath.Join(dir, "c.pem") || c.TLS.MinVersion != "1.2" || c.TLSMinVersion() != tls.VersionTLS12 {
		t.Errorf("tls config = %+v", c)
	}
	if c.LogRotate.MaxMB != DefaultLogMaxMB || c.LogRotate.Keep != DefaultLogKeep {
		t.Errorf("log rotate defaults = %+v", c.LogRotate)
	}

	for name, body := range map[string]string{
		"key only":     `{` + base + `,"tls":{"key_file":"k.pem"}}`,
		"missing file": `{` + base + `,"tls":{"cert_file":"nope.pem","key_file":"k.pem"}}`,
		"bad version":  `{` + base + `,"tls":{"cert_file":"c.pem","key_file":"k.pem","min_version":"1.1"}}`,
		"version only": `{` + base + `,"tls":{"min_version":"1.3"}}`,
	} {
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "tls") {
			t.Errorf("%s: expected a tls error, got %v", name, err)
		}
	}

	if err := os.WriteFile(p, []byte(`{`+base+`,"tls":{"cert_file":"c.pem","key_file":"k.pem","min_version":"1.3"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if c, err := Load(p); err != nil || c.TLSMinVersion() != tls.VersionTLS13 {
		t.Errorf("min 1.3: %v %v", c, err)
	}
}

func TestFieldsCoverEveryJSONKey(t *testing.T) {
	// Every documented key must have a meaning, and the well-known keys must be documented.
	for _, f := range Fields {
		if f.Meaning == "" {
			t.Errorf("%s has no meaning", f.Key)
		}
	}
	for _, k := range []string{"listen", "api_keys", "allowed_cidrs", "rate_limit.per_second", "rate_limit.burst", "log_file", "log_rotate.max_mb", "log_rotate.keep", "poll.state_seconds", "poll.heavy_seconds", "events.hours", "events.max_per_log", "diagnostics.checkpoint_days", "diagnostics.free_space_percent", "tls.cert_file", "tls.key_file", "tls.min_version"} {
		if Describe(k) == "" {
			t.Errorf("%s is not documented", k)
		}
	}
}
