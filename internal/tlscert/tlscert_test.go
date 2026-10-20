package tlscert

import (
	"crypto/tls"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGenerateAndDescribe(t *testing.T) {
	certPEM, keyPEM, err := Generate(Options{CommonName: "hv01", DNSNames: []string{"hv01", "hv01.example.local", "hv01"}, IPs: []net.IP{net.ParseIP("10.0.0.10"), net.ParseIP("10.0.0.10")}, Validity: 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	info, err := Describe(&pair)
	if err != nil {
		t.Fatal(err)
	}
	if !info.SelfSigned || len(info.DNSNames) != 2 || len(info.IPAddresses) != 1 || info.IPAddresses[0] != "10.0.0.10" || info.Subject == "" ||
		time.Until(info.NotAfter) > 25*time.Hour || len(info.Fingerprint) != 95 {
		t.Errorf("info = %+v", info)
	}
}

func TestReloader(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "c.pem"), filepath.Join(dir, "k.pem")
	write := func(cn string) {
		c, k, err := Generate(Options{CommonName: cn})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(certFile, c, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(keyFile, k, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewReloader(certFile, keyFile); err == nil {
		t.Fatal("missing files should fail")
	}
	write("first")
	r, err := NewReloader(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	r.checkEvery = 0
	if c, _ := r.GetCertificate(nil); c.Leaf.Subject.CommonName != "first" {
		t.Fatal("initial cert")
	}

	// Replace the pair; the next call picks it up.
	write("second")
	past := time.Now().Add(-time.Hour)
	_ = os.Chtimes(certFile, past, past) // make sure the mtime differs even on coarse filesystems
	if c := r.Current(); c.Leaf.Subject.CommonName != "second" {
		t.Errorf("after rewrite got %q", c.Leaf.Subject.CommonName)
	}

	// A broken replacement keeps the last good pair and records the error.
	if err := os.WriteFile(keyFile, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	_ = os.Chtimes(keyFile, later, later)
	if c := r.Current(); c.Leaf.Subject.CommonName != "second" || r.LastError() == nil {
		t.Errorf("broken file: cn=%q err=%v", c.Leaf.Subject.CommonName, r.LastError())
	}
}
