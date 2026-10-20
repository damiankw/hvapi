// Package config loads and validates the hvapi configuration file.
package config

import (
	"bytes"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
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

// Events bounds what is read from the Hyper-V event logs on each heavy poll.
type Events struct {
	Hours     int `json:"hours"`
	MaxPerLog int `json:"max_per_log"`
}

// TLS enables HTTPS when both files are set. Paths are relative to the
// config file. Files are PEM; 'hvapi gen-cert' makes a self-signed pair.
type TLS struct {
	CertFile   string `json:"cert_file"`
	KeyFile    string `json:"key_file"`
	MinVersion string `json:"min_version"` // "1.2" (default) or "1.3"
}

// LogRotate bounds the log file: when it exceeds MaxMB it is renamed with a
// numeric suffix and a new one started; Keep old files are retained.
type LogRotate struct {
	MaxMB int `json:"max_mb"`
	Keep  int `json:"keep"`
}

type Config struct {
	Listen       string    `json:"listen"`
	APIKeys      []APIKey  `json:"api_keys"`
	AllowedCIDRs []string  `json:"allowed_cidrs"`
	RateLimit    RateLimit `json:"rate_limit"`
	LogFile      string    `json:"log_file"`
	LogRotate    LogRotate `json:"log_rotate"`
	Poll         Poll      `json:"poll"`
	Events       Events    `json:"events"`
	TLS          TLS       `json:"tls"`

	// Parsed forms, filled in by Read and Validate.
	Dir         string         `json:"-"` // directory of the config file; relative paths resolve against it
	Prefixes    []netip.Prefix `json:"-"`
	KeyIDs      []string       `json:"-"`
	KeyHash     [][32]byte     `json:"-"`
	TLSCertPath string         `json:"-"` // absolute, "" when TLS is off
	TLSKeyPath  string         `json:"-"`
}

// TLSEnabled reports whether the server should serve HTTPS.
func (c *Config) TLSEnabled() bool { return c.TLS.CertFile != "" || c.TLS.KeyFile != "" }

// Scheme is "https" or "http" depending on TLS.
func (c *Config) Scheme() string {
	if c.TLSEnabled() {
		return "https"
	}
	return "http"
}

// Resolve makes a path absolute relative to the config file's directory.
func (c *Config) Resolve(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(c.Dir, path)
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
	DefaultLogMaxMB     = 10
	DefaultLogKeep      = 5
	DefaultTLSMin       = "1.2"
	DefaultEventHours   = 24
	DefaultEventsPerLog = 500
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
	if abs, err := filepath.Abs(path); err == nil {
		c.Dir = filepath.Dir(abs)
	} else {
		c.Dir = filepath.Dir(path)
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
	if c.Events.Hours <= 0 {
		c.Events.Hours = DefaultEventHours
	}
	if c.Events.MaxPerLog <= 0 {
		c.Events.MaxPerLog = DefaultEventsPerLog
	}
	if c.LogRotate.MaxMB <= 0 {
		c.LogRotate.MaxMB = DefaultLogMaxMB
	}
	if c.LogRotate.Keep < 0 {
		c.LogRotate.Keep = 0
	} else if c.LogRotate.Keep == 0 {
		c.LogRotate.Keep = DefaultLogKeep
	}
	if c.TLSEnabled() && c.TLS.MinVersion == "" {
		c.TLS.MinVersion = DefaultTLSMin
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

	c.TLSCertPath, c.TLSKeyPath = "", ""
	if c.TLSEnabled() {
		switch {
		case c.TLS.CertFile == "":
			errs = append(errs, errors.New("tls: cert_file is required when key_file is set"))
		case c.TLS.KeyFile == "":
			errs = append(errs, errors.New("tls: key_file is required when cert_file is set"))
		default:
			cert, key := c.Resolve(c.TLS.CertFile), c.Resolve(c.TLS.KeyFile)
			if _, err := tls.LoadX509KeyPair(cert, key); err != nil {
				errs = append(errs, fmt.Errorf("tls: cannot load certificate: %w (files must be PEM; 'hvapi gen-cert' creates a self-signed pair)", err))
			} else {
				c.TLSCertPath, c.TLSKeyPath = cert, key
			}
		}
		switch c.TLS.MinVersion {
		case "", "1.2", "1.3":
		default:
			errs = append(errs, fmt.Errorf("tls: min_version must be \"1.2\" or \"1.3\", not %q", c.TLS.MinVersion))
		}
	} else if c.TLS.MinVersion != "" {
		errs = append(errs, errors.New("tls: min_version is set but cert_file and key_file are empty"))
	}

	return errors.Join(errs...)
}

// TLSMinVersion returns the crypto/tls constant for tls.min_version.
func (c *Config) TLSMinVersion() uint16 {
	if c.TLS.MinVersion == "1.3" {
		return tls.VersionTLS13
	}
	return tls.VersionTLS12
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
