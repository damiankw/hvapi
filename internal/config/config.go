// Package config loads and validates the hvapi configuration file.
package config

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
)

type APIKey struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
}

type RateLimit struct {
	PerSecond float64 `json:"per_second"`
	Burst     int     `json:"burst"`
}

type Poll struct {
	StateSeconds int `json:"state_seconds"`
	HeavySeconds int `json:"heavy_seconds"`
}

// TLS is reserved for later; HTTPS is not enabled yet.
type TLS struct {
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
}

type Config struct {
	Listen       string    `json:"listen"`
	APIKeys      []APIKey  `json:"api_keys"`
	AllowedCIDRs []string  `json:"allowed_cidrs"`
	RateLimit    RateLimit `json:"rate_limit"`
	LogFile      string    `json:"log_file"`
	Poll         Poll      `json:"poll"`
	TLS          TLS       `json:"tls"`

	// Parsed forms, filled in by Validate.
	Prefixes []netip.Prefix `json:"-"`
	KeyIDs   []string       `json:"-"`
	KeyHash  [][32]byte     `json:"-"`
}

// Defaults applied by ApplyDefaults to fields left empty or zero.
const (
	DefaultListen       = "127.0.0.1:8085"
	DefaultPort         = "8085"
	DefaultPerSecond    = 10
	DefaultBurst        = 20
	DefaultStateSeconds = 10
	DefaultHeavySeconds = 60
	DefaultLogFile      = "hvapi.log"
)

// Load reads, defaults and validates the config file at path.
func Load(path string) (*Config, error) {
	c, err := Read(path)
	if err != nil {
		return nil, err
	}
	c.ApplyDefaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// Read parses the config file at path as written, without applying defaults
// or validating it. Unknown fields are rejected so typos are caught early.
func Read(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	// Tolerate a UTF-8 BOM, which Notepad on older Windows adds.
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &c, nil
}

// ApplyDefaults fills in the documented defaults for empty or zero fields.
func (c *Config) ApplyDefaults() {
	if c.Listen == "" {
		c.Listen = DefaultListen
	}
	if c.RateLimit.PerSecond <= 0 {
		c.RateLimit.PerSecond = DefaultPerSecond
	}
	if c.RateLimit.Burst <= 0 {
		c.RateLimit.Burst = DefaultBurst
	}
	if c.Poll.StateSeconds <= 0 {
		c.Poll.StateSeconds = DefaultStateSeconds
	}
	if c.Poll.HeavySeconds <= 0 {
		c.Poll.HeavySeconds = DefaultHeavySeconds
	}
}

// Validate checks the config and fills in the parsed Prefixes, KeyIDs and
// KeyHash fields. It returns every problem found, joined, not just the first.
func (c *Config) Validate() error {
	var errs []error
	c.Prefixes, c.KeyIDs, c.KeyHash = nil, nil, nil

	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		errs = append(errs, fmt.Errorf("listen %q: %w", c.Listen, err))
	}

	if len(c.APIKeys) == 0 {
		errs = append(errs, errors.New("api_keys: at least one key is required"))
	}
	seen := map[string]bool{}
	for i, k := range c.APIKeys {
		if k.ID == "" {
			errs = append(errs, fmt.Errorf("api_keys[%d]: id is required", i))
		}
		if seen[k.ID] {
			errs = append(errs, fmt.Errorf("api_keys[%d]: duplicate id %q", i, k.ID))
		}
		seen[k.ID] = true
		h, err := ParseHash(k.SHA256)
		if err != nil {
			errs = append(errs, fmt.Errorf("api_keys[%d]: %w", i, err))
			continue
		}
		c.KeyIDs = append(c.KeyIDs, k.ID)
		c.KeyHash = append(c.KeyHash, h)
	}

	if len(c.AllowedCIDRs) == 0 {
		errs = append(errs, errors.New("allowed_cidrs: at least one range is required"))
	}
	for i, s := range c.AllowedCIDRs {
		p, err := ParseCIDR(s)
		if err != nil {
			errs = append(errs, fmt.Errorf("allowed_cidrs[%d]: %w", i, err))
			continue
		}
		c.Prefixes = append(c.Prefixes, p)
	}

	if c.TLS.CertFile != "" || c.TLS.KeyFile != "" {
		errs = append(errs, errors.New("tls: HTTPS is not implemented yet; leave cert_file and key_file empty"))
	}

	return errors.Join(errs...)
}

// ParseHash checks that s is a hex-encoded SHA-256 hash and returns it.
func ParseHash(s string) ([32]byte, error) {
	var h [32]byte
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 32 {
		return h, errors.New("sha256 must be 64 hex characters (generate one with 'hvapi hash-key')")
	}
	copy(h[:], b)
	return h, nil
}

// ParseCIDR parses a CIDR range, or a bare IP address as a single-host range.
func ParseCIDR(s string) (netip.Prefix, error) {
	p, err := netip.ParsePrefix(s)
	if err != nil {
		a, aerr := netip.ParseAddr(s)
		if aerr != nil {
			return netip.Prefix{}, fmt.Errorf("%q: not a CIDR range or IP address", s)
		}
		p = netip.PrefixFrom(a, a.BitLen())
	}
	return p.Masked(), nil
}
