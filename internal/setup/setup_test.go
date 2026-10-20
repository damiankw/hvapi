package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damiankw/hvapi/internal/config"
	"github.com/damiankw/hvapi/internal/tlscert"
)

const goodHash = "2bb80d537b1da3e38bd30361aa855686bde0eacd7162fef6a25fe97bf527a25b"

func TestConfigureFreshFile(t *testing.T) {
	localIPs = func() []string { return []string{"10.0.0.10"} }
	path := filepath.Join(t.TempDir(), "config.json")

	// Enter accepts the suggested listen address; one generated key named
	// "mon"; one bad CIDR then a good list; the rest default.
	in := strings.Join([]string{
		"",          // listen: default 10.0.0.10:8085
		"mon",       // key id
		"",          // generate: yes
		"",          // add another: no
		"not-a-net", // bad cidr
		"10.0.0.0/24, 192.168.1.5",
		"5",  // per second
		"",   // burst
		"",   // log file
		"",   // state
		"30", // heavy
		"2",  // HTTPS: generate self-signed
		"",   // min version
	}, "\n") + "\n"
	var out strings.Builder
	if err := Configure(strings.NewReader(in), &out, path); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	o := out.String()
	for _, want := range []string{"10.0.0.10:8085", "not a CIDR", "New API key", "Wrote " + path} {
		if !strings.Contains(o, want) {
			t.Errorf("output lacks %q:\n%s", want, o)
		}
	}

	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != "10.0.0.10:8085" || len(c.APIKeys) != 1 || c.APIKeys[0].ID != "mon" ||
		len(c.AllowedCIDRs) != 2 || c.RateLimit.PerSecond != 5 || c.RateLimit.Burst != 20 ||
		c.LogFile != "hvapi.log" || c.Poll.StateSeconds != 10 || c.Poll.HeavySeconds != 30 ||
		!c.TLSEnabled() || c.TLS.CertFile != "hvapi-cert.pem" || c.TLS.MinVersion != "1.2" || c.TLSCertPath == "" {
		t.Errorf("unexpected config: %+v", c)
	}
	if !strings.Contains(o, "self-signed certificate") || !strings.Contains(o, "https://10.0.0.10:8085/") {
		t.Errorf("output lacks certificate details:\n%s", o)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "hvapi-key.pem")); err != nil {
		t.Error("key file not written")
	}

	// The generated key printed in the output must hash to what was stored.
	i := strings.Index(o, "New API key for")
	line := strings.TrimSpace(strings.Split(o[i:], "\n")[1])
	hashed := strings.Contains(o, c.APIKeys[0].SHA256)
	if len(line) != 64 || hashed {
		t.Errorf("key %q / hash printed=%v", line, hashed)
	}
}

func TestConfigureEditsExisting(t *testing.T) {
	localIPs = func() []string { return nil }
	path := filepath.Join(t.TempDir(), "config.json")
	orig := `{"listen":"10.1.1.1:9000","api_keys":[{"id":"a","sha256":"` + goodHash + `"}],"allowed_cidrs":["10.1.1.0/24"],"log_file":"x.log"}`
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	in := strings.Join([]string{
		"",  // keep listen
		"",  // keep existing keys
		"",  // add another: no
		"",  // cidrs
		"",  // per second
		"",  // burst
		"-", // console logging
		"",  // state
		"",  // heavy
		"3", // plain HTTP
		"y", // overwrite
	}, "\n") + "\n"
	var out strings.Builder
	if err := Configure(strings.NewReader(in), &out, path); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != "10.1.1.1:9000" || c.APIKeys[0].SHA256 != goodHash || c.LogFile != "" || c.TLSEnabled() {
		t.Errorf("unexpected config: %+v", c)
	}
}

func TestConfigureStopsWhenInputEnds(t *testing.T) {
	localIPs = func() []string { return nil }
	path := filepath.Join(t.TempDir(), "config.json")
	var out strings.Builder
	// Listen accepted by default, then input ends at the key ID.
	err := Configure(strings.NewReader("\n\n\n\n"), &out, path)
	if err == nil {
		t.Fatal("expected an error when input ends")
	}
	if _, serr := os.Stat(path); serr == nil {
		t.Error("config must not be written on failure")
	}
}

func TestShowTLS(t *testing.T) {
	dir := t.TempDir()
	if _, err := tlscert.WriteFiles(filepath.Join(dir, "c.pem"), filepath.Join(dir, "k.pem"), tlscert.Options{CommonName: "hv01", DNSNames: []string{"hv01"}}, false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"api_keys":[{"id":"mon","sha256":"`+goodHash+`"}],"allowed_cidrs":["10.0.0.0/24"],"tls":{"cert_file":"c.pem","key_file":"k.pem"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := Show(&out, path); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	for _, want := range []string{"self-signed", "subject CN=hv01", "names   hv01", "expires", "sha256  "} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestShow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"api_keys":[{"id":"mon","sha256":"`+goodHash+`"}],"allowed_cidrs":["10.0.0.0/24"],"log_file":"hvapi.log"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := Show(&out, path); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	o := out.String()
	for _, want := range []string{"127.0.0.1:8085  (default)", "mon", "2bb80d537b1d...", "resolves to", "plain HTTP", "log_rotate.max_mb", "The configuration is valid."} {
		if !strings.Contains(o, want) {
			t.Errorf("output lacks %q:\n%s", want, o)
		}
	}
	if strings.Contains(o, goodHash) {
		t.Error("full hash should not be printed")
	}

	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte(`{"listen":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Show(&out, bad); err == nil {
		t.Fatal("expected error for invalid config")
	}
	if !strings.Contains(out.String(), "Problems:") || !strings.Contains(out.String(), "api_keys") {
		t.Errorf("problems not listed:\n%s", out.String())
	}
}
