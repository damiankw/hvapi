// Package tlscert loads the server certificate, reloads it when the files
// change, and generates self-signed certificates for hosts without a CA.
package tlscert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// Reloader serves a certificate from PEM files and re-reads them when their
// modification time changes, so a renewed certificate takes effect without
// a restart. A broken new file is ignored and the last good pair kept.
type Reloader struct {
	certFile, keyFile string
	checkEvery        time.Duration

	mu        sync.Mutex
	cert      *tls.Certificate
	modCert   time.Time
	modKey    time.Time
	lastCheck time.Time
	lastErr   error
}

// NewReloader loads the pair once and fails if it cannot.
func NewReloader(certFile, keyFile string) (*Reloader, error) {
	r := &Reloader{certFile: certFile, keyFile: keyFile, checkEvery: 30 * time.Second}
	if err := r.load(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Reloader) load() error {
	cert, err := tls.LoadX509KeyPair(r.certFile, r.keyFile)
	if err != nil {
		return err
	}
	if cert.Leaf == nil {
		if cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0]); err != nil {
			return err
		}
	}
	r.cert = &cert
	r.modCert, r.modKey = mtime(r.certFile), mtime(r.keyFile)
	r.lastCheck = time.Now()
	r.lastErr = nil
	return nil
}

func mtime(path string) time.Time {
	st, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return st.ModTime()
}

// GetCertificate is for tls.Config.GetCertificate.
func (r *Reloader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return r.Current(), nil
}

// Current returns the certificate in use, reloading first if the files
// changed and the check interval has passed.
func (r *Reloader) Current() *tls.Certificate {
	r.mu.Lock()
	defer r.mu.Unlock()
	if time.Since(r.lastCheck) >= r.checkEvery {
		r.lastCheck = time.Now()
		if mc, mk := mtime(r.certFile), mtime(r.keyFile); !mc.Equal(r.modCert) || !mk.Equal(r.modKey) {
			old := r.cert
			if err := r.load(); err != nil {
				r.cert, r.lastErr = old, err
				r.modCert, r.modKey = mc, mk // do not retry until they change again
			}
		}
	}
	return r.cert
}

// LastError reports why the most recent reload failed, if it did.
func (r *Reloader) LastError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastErr
}

// Info summarises a certificate for show-config and /health.
type Info struct {
	Subject     string    `json:"subject"`
	Issuer      string    `json:"issuer"`
	NotBefore   time.Time `json:"not_before"`
	NotAfter    time.Time `json:"not_after"`
	DNSNames    []string  `json:"dns_names,omitempty"`
	IPAddresses []string  `json:"ip_addresses,omitempty"`
	SelfSigned  bool      `json:"self_signed"`
	Fingerprint string    `json:"sha256_fingerprint"`
}

// Describe summarises the leaf certificate of a loaded pair.
func Describe(cert *tls.Certificate) (Info, error) {
	if cert == nil || len(cert.Certificate) == 0 {
		return Info{}, errors.New("no certificate")
	}
	leaf := cert.Leaf
	if leaf == nil {
		var err error
		if leaf, err = x509.ParseCertificate(cert.Certificate[0]); err != nil {
			return Info{}, err
		}
	}
	sum := sha256.Sum256(leaf.Raw)
	info := Info{
		Subject:     leaf.Subject.String(),
		Issuer:      leaf.Issuer.String(),
		NotBefore:   leaf.NotBefore.UTC(),
		NotAfter:    leaf.NotAfter.UTC(),
		DNSNames:    leaf.DNSNames,
		SelfSigned:  leaf.Issuer.String() == leaf.Subject.String(),
		Fingerprint: fingerprint(sum[:]),
	}
	for _, ip := range leaf.IPAddresses {
		info.IPAddresses = append(info.IPAddresses, ip.String())
	}
	return info, nil
}

func fingerprint(b []byte) string {
	h := strings.ToUpper(hex.EncodeToString(b))
	parts := make([]string, 0, len(h)/2)
	for i := 0; i+2 <= len(h); i += 2 {
		parts = append(parts, h[i:i+2])
	}
	return strings.Join(parts, ":")
}

// Options for Generate.
type Options struct {
	CommonName string        // subject CN, usually the host name
	DNSNames   []string      // subject alternative names
	IPs        []net.IP      // subject alternative names
	Validity   time.Duration // default 10 years
}

// Generate makes a self-signed ECDSA P-256 server certificate and returns
// the certificate and key as PEM. The key is never written here; the caller
// decides where it goes and with what permissions.
func Generate(o Options) (certPEM, keyPEM []byte, err error) {
	if o.Validity <= 0 {
		o.Validity = 10 * 365 * 24 * time.Hour
	}
	if o.CommonName == "" {
		o.CommonName = "hvapi"
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: o.CommonName, Organization: []string{"hvapi"}},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(o.Validity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dedupe(o.DNSNames),
		IPAddresses:           dedupeIPs(o.IPs),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("create certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

// WriteFiles generates a self-signed pair and writes it to certFile and
// keyFile (key with owner-only permissions). Existing files are only
// replaced when overwrite is set.
func WriteFiles(certFile, keyFile string, o Options, overwrite bool) (Info, error) {
	if !overwrite {
		for _, f := range []string{certFile, keyFile} {
			if _, err := os.Stat(f); err == nil {
				return Info{}, fmt.Errorf("%s already exists (use -force to replace it)", f)
			}
		}
	}
	certPEM, keyPEM, err := Generate(o)
	if err != nil {
		return Info{}, err
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		return Info{}, fmt.Errorf("write key: %w", err)
	}
	if err := os.WriteFile(certFile, certPEM, 0o644); err != nil {
		return Info{}, fmt.Errorf("write certificate: %w", err)
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return Info{}, err
	}
	return Describe(&pair)
}

// DefaultSANs returns the names a self-signed certificate for this machine
// should carry: its host name, the given addresses, and loopback.
func DefaultSANs(ips []string) (dns []string, addrs []net.IP) {
	if h, err := os.Hostname(); err == nil && h != "" {
		dns = append(dns, h)
	}
	dns = append(dns, "localhost")
	for _, s := range append(ips, "127.0.0.1") {
		if ip := net.ParseIP(strings.TrimSpace(s)); ip != nil {
			addrs = append(addrs, ip)
		}
	}
	return dns, addrs
}

// ParseSANs splits "a.example.com,10.0.0.5" into DNS names and IPs.
func ParseSANs(list string) (dns []string, ips []net.IP) {
	for _, s := range strings.Split(list, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if ip := net.ParseIP(s); ip != nil {
			ips = append(ips, ip)
		} else {
			dns = append(dns, s)
		}
	}
	return dns, ips
}

func dedupeIPs(in []net.IP) []net.IP {
	seen := map[string]bool{}
	var out []net.IP
	for _, ip := range in {
		if ip == nil || seen[ip.String()] {
			continue
		}
		seen[ip.String()] = true
		out = append(out, ip)
	}
	return out
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[strings.ToLower(s)] {
			continue
		}
		seen[strings.ToLower(s)] = true
		out = append(out, s)
	}
	return out
}
