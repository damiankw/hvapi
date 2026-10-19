package setup

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"

	"github.com/damiankw/hvapi/internal/auth"
	"github.com/damiankw/hvapi/internal/config"
)

// localIPs lists candidate listen addresses. Tests replace it.
var localIPs = func() []string {
	var out []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
				out = append(out, n.IP.String())
			}
		}
	}
	return out
}

// Configure walks the user through every setting and writes config.json to
// path. An existing file at path supplies the defaults, so it also edits.
func Configure(in io.Reader, out io.Writer, path string) error {
	p := &prompter{in: bufio.NewReader(in), out: out}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	cfg := &config.Config{}
	existing := false
	if old, err := config.Read(abs); err == nil {
		cfg = old
		existing = true
	} else if !errors.Is(err, os.ErrNotExist) {
		p.say("Note: the existing file could not be read and will be replaced: %v\n\n", err)
	}
	cfg.ApplyDefaults()

	p.say("hvapi configuration\n")
	p.say("===================\n\n")
	if existing {
		p.say("Editing %s. The current values are the defaults.\n", abs)
	} else {
		p.say("Writing %s.\n", abs)
	}
	p.say("Press Enter to accept the value in [brackets]. Ctrl+C abandons without writing.\n\n")

	// 1. Listen address.
	p.say("1. Listen address\n   %s\n", config.Describe("listen"))
	if ips := localIPs(); len(ips) > 0 {
		p.say("   Addresses on this machine: %s\n", strings.Join(ips, ", "))
		if !existing {
			cfg.Listen = net.JoinHostPort(ips[0], config.DefaultPort)
		}
	}
	cfg.Listen, err = p.askValid("   Listen address (ip:port)", cfg.Listen, func(s string) error {
		host, port, err := net.SplitHostPort(s)
		if err != nil {
			return errors.New("use the form ip:port, for example 10.0.0.10:8085")
		}
		if _, err := netip.ParseAddr(host); err != nil && host != "" {
			return fmt.Errorf("%q is not an IP address", host)
		}
		if port == "" {
			return errors.New("a port is required")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if host, _, _ := net.SplitHostPort(cfg.Listen); host == "" || host == "0.0.0.0" || host == "::" {
		p.say("   Warning: this listens on every interface. The IP allowlist still applies.\n")
	}
	p.say("\n")

	// 2. API keys.
	p.say("2. API keys\n   %s\n", config.Describe("api_keys"))
	var generated []config.APIKey
	var keys []config.APIKey
	if len(cfg.APIKeys) > 0 {
		keep, err := p.askYesNo(fmt.Sprintf("   Keep the %d existing key(s) (%s)?", len(cfg.APIKeys), keyIDs(cfg.APIKeys)), true)
		if err != nil {
			return err
		}
		if keep {
			keys = cfg.APIKeys
		}
	}
	for {
		if len(keys) > 0 {
			more, err := p.askYesNo("   Add another key?", false)
			if err != nil {
				return err
			}
			if !more {
				break
			}
		}
		def := "monitoring"
		if len(keys) > 0 {
			def = ""
		}
		id, err := p.askValid("   Key ID (a short name for the client, shown in logs)", def, func(s string) error {
			for _, k := range keys {
				if k.ID == s {
					return fmt.Errorf("a key with id %q already exists", s)
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		gen, err := p.askYesNo("   Generate a new key for it now? (no = paste an existing hash)", true)
		if err != nil {
			return err
		}
		k := config.APIKey{ID: id}
		if gen {
			key, hash, err := auth.GenerateKey()
			if err != nil {
				return err
			}
			k.SHA256 = hash
			p.say("   New API key for %q (give this to the client; it is not stored anywhere):\n     %s\n", id, key)
			generated = append(generated, config.APIKey{ID: id, SHA256: key})
		} else {
			k.SHA256, err = p.askValid("   SHA-256 hash of the key (64 hex characters)", "", func(s string) error {
				_, err := config.ParseHash(s)
				return err
			})
			if err != nil {
				return err
			}
		}
		keys = append(keys, k)
	}
	cfg.APIKeys = keys
	p.say("\n")

	// 3. Allowed addresses.
	p.say("3. Allowed client addresses\n   %s\n", config.Describe("allowed_cidrs"))
	p.say("   Separate several with commas. A bare IP allows just that host.\n")
	defCIDRs := strings.Join(cfg.AllowedCIDRs, ", ")
	if defCIDRs == "" {
		if host, _, _ := net.SplitHostPort(cfg.Listen); host != "" {
			if a, err := netip.ParseAddr(host); err == nil && a.Is4() && !a.IsLoopback() && !a.IsUnspecified() {
				defCIDRs = netip.PrefixFrom(a, 24).Masked().String()
			}
		}
	}
	list, err := p.askValid("   Allowed ranges", defCIDRs, func(s string) error {
		for _, part := range splitList(s) {
			if _, err := config.ParseCIDR(part); err != nil {
				return err
			}
		}
		if len(splitList(s)) == 0 {
			return errors.New("at least one range is required")
		}
		return nil
	})
	if err != nil {
		return err
	}
	cfg.AllowedCIDRs = splitList(list)
	p.say("\n")

	// 4. Rate limit.
	p.say("4. Rate limit (per key)\n   %s\n   %s\n", config.Describe("rate_limit.per_second"), config.Describe("rate_limit.burst"))
	if cfg.RateLimit.PerSecond, err = p.askFloat("   Requests per second", cfg.RateLimit.PerSecond); err != nil {
		return err
	}
	if cfg.RateLimit.Burst, err = p.askInt("   Burst", cfg.RateLimit.Burst, 1); err != nil {
		return err
	}
	p.say("\n")

	// 5. Log file.
	p.say("5. Log file\n   %s\n", config.Describe("log_file"))
	logDef := cfg.LogFile
	if logDef == "" {
		logDef = config.DefaultLogFile
	}
	v, err := p.ask("   Log file ('-' for console only)", logDef)
	if err != nil {
		return err
	}
	if v == "-" {
		v = ""
	}
	cfg.LogFile = v
	p.say("\n")

	// 6. Poll intervals.
	p.say("6. Poll intervals\n   %s\n   %s\n", config.Describe("poll.state_seconds"), config.Describe("poll.heavy_seconds"))
	if cfg.Poll.StateSeconds, err = p.askInt("   State refresh (seconds)", cfg.Poll.StateSeconds, 1); err != nil {
		return err
	}
	if cfg.Poll.HeavySeconds, err = p.askInt("   Heavy refresh (seconds)", cfg.Poll.HeavySeconds, 1); err != nil {
		return err
	}
	p.say("\n")

	// 7. TLS.
	p.say("7. TLS\n   %s Left empty.\n\n", config.Describe("tls.cert_file"))
	cfg.TLS = config.TLS{}

	// Validate, show and write.
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("the configuration is not valid:\n%w", err)
	}
	p.say("Summary\n-------\n")
	writeSummary(p.out, abs, cfg, nil)
	p.say("\n")
	if _, err := os.Stat(abs); err == nil {
		ok, err := p.askYesNo(fmt.Sprintf("Overwrite %s?", abs), true)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("not written")
		}
	}
	if err := Save(abs, cfg); err != nil {
		return err
	}
	p.say("Wrote %s\n", abs)

	if len(generated) > 0 {
		p.say("\nNew API keys (shown once, give them to the clients):\n")
		for _, g := range generated {
			p.say("  %-14s %s\n", g.ID, g.SHA256)
		}
	}
	p.say("\nNext steps:\n  hvapi run -config %q      test in this console\n  hvapi install -config %q  install as a service\n", abs, abs)
	return nil
}

// Save writes cfg to path as indented JSON.
func Save(path string, cfg *config.Config) error {
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o640)
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func keyIDs(keys []config.APIKey) string {
	ids := make([]string, len(keys))
	for i, k := range keys {
		ids[i] = k.ID
	}
	return strings.Join(ids, ", ")
}
