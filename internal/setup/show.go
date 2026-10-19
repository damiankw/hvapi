package setup

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/damiankw/hvapi/internal/config"
)

// Show prints the config at path with every setting explained, the defaults
// that apply, and any validation problems.
func Show(out io.Writer, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	raw, err := config.Read(abs)
	if err != nil {
		return err
	}
	eff := *raw
	eff.ApplyDefaults()
	verr := eff.Validate()

	fmt.Fprintf(out, "Configuration: %s\n\n", abs)
	writeSummary(out, abs, &eff, raw)
	fmt.Fprintln(out)
	if verr != nil {
		fmt.Fprintf(out, "Problems:\n")
		for _, line := range strings.Split(verr.Error(), "\n") {
			fmt.Fprintf(out, "  - %s\n", line)
		}
		return fmt.Errorf("configuration is not valid")
	}
	fmt.Fprintln(out, "The configuration is valid.")
	return nil
}

// writeSummary prints each setting with its effective value and meaning.
// raw, if given, is the file as written; values it leaves empty are marked
// as defaults.
func writeSummary(out io.Writer, cfgPath string, c *config.Config, raw *config.Config) {
	mark := func(isDefault bool) string {
		if raw != nil && isDefault {
			return "  (default)"
		}
		return ""
	}
	setting := func(key, value string) {
		fmt.Fprintf(out, "%-22s %s\n", key, value)
		fmt.Fprintf(out, "%-22s %s\n", "", config.Describe(key))
	}

	setting("listen", c.Listen+mark(raw != nil && raw.Listen == ""))

	fmt.Fprintf(out, "%-22s %d key(s)\n", "api_keys", len(c.APIKeys))
	for _, k := range c.APIKeys {
		hash := k.SHA256
		if len(hash) > 12 {
			hash = hash[:12] + "..."
		}
		fmt.Fprintf(out, "%-22s   %-14s sha256 %s\n", "", k.ID, hash)
	}
	fmt.Fprintf(out, "%-22s %s\n", "", config.Describe("api_keys"))

	setting("allowed_cidrs", strings.Join(c.AllowedCIDRs, ", "))
	setting("rate_limit.per_second", fmt.Sprintf("%g", c.RateLimit.PerSecond)+mark(raw != nil && raw.RateLimit.PerSecond <= 0))
	setting("rate_limit.burst", fmt.Sprint(c.RateLimit.Burst)+mark(raw != nil && raw.RateLimit.Burst <= 0))

	logVal := c.LogFile
	switch {
	case logVal == "":
		logVal = "(console; hvapi.log when run as a service)"
	case !filepath.IsAbs(logVal):
		logVal = fmt.Sprintf("%s  (resolves to %s)", logVal, filepath.Join(filepath.Dir(cfgPath), logVal))
	}
	setting("log_file", logVal)

	setting("poll.state_seconds", fmt.Sprint(c.Poll.StateSeconds)+mark(raw != nil && raw.Poll.StateSeconds <= 0))
	setting("poll.heavy_seconds", fmt.Sprint(c.Poll.HeavySeconds)+mark(raw != nil && raw.Poll.HeavySeconds <= 0))

	tlsVal := "(empty)"
	if c.TLS.CertFile != "" || c.TLS.KeyFile != "" {
		tlsVal = fmt.Sprintf("cert_file=%q key_file=%q", c.TLS.CertFile, c.TLS.KeyFile)
	}
	fmt.Fprintf(out, "%-22s %s\n", "tls", tlsVal)
	fmt.Fprintf(out, "%-22s %s\n", "", config.Describe("tls.cert_file"))
}
