// Package auth provides the IP allowlist, API key check and per-key rate limit.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"time"
)

// HeaderName is the request header that carries the API key.
const HeaderName = "X-API-Key"

type ctxKey struct{}
type slotKey struct{}

// TrackKey attaches a slot that RequireKey fills with the key ID, so outer
// middleware (the request logger) can record which key was used.
func TrackKey(r *http.Request) (*http.Request, *string) {
	s := new(string)
	return r.WithContext(context.WithValue(r.Context(), slotKey{}, s)), s
}

// KeyID returns the ID of the API key that authenticated the request, if any.
func KeyID(r *http.Request) string {
	id, _ := r.Context().Value(ctxKey{}).(string)
	return id
}

// GenerateKey returns a new random API key and its SHA-256 hash, both hex.
func GenerateKey() (key, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	key = hex.EncodeToString(b)
	sum := sha256.Sum256([]byte(key))
	return key, hex.EncodeToString(sum[:]), nil
}

// Guard holds the allowlist, key hashes and rate limiters.
type Guard struct {
	prefixes []netip.Prefix
	ids      []string
	hashes   [][32]byte
	rate     float64
	burst    float64

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

// New builds a Guard. ids and hashes must be the same length and order.
func New(prefixes []netip.Prefix, ids []string, hashes [][32]byte, perSecond float64, burst int) *Guard {
	return &Guard{
		prefixes: prefixes,
		ids:      ids,
		hashes:   hashes,
		rate:     perSecond,
		burst:    float64(burst),
		buckets:  map[string]*bucket{},
	}
}

// AllowIP rejects requests from addresses outside the allowlist.
func (g *Guard) AllowIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !g.ipAllowed(r.RemoteAddr) {
			writeErr(w, http.StatusForbidden, "address not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireKey checks the API key, then applies the per-key rate limit.
func (g *Guard) RequireKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := g.match(r.Header.Get(HeaderName))
		if !ok {
			writeErr(w, http.StatusUnauthorized, "missing or invalid API key")
			return
		}
		if s, ok := r.Context().Value(slotKey{}).(*string); ok {
			*s = id
		}
		if !g.take(id) {
			w.Header().Set("Retry-After", "1")
			writeErr(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, id)))
	})
}

func (g *Guard) ipAllowed(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	a = a.Unmap().WithZone("")
	for _, p := range g.prefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// match checks every key in constant time, so timing reveals nothing about keys.
func (g *Guard) match(key string) (string, bool) {
	if key == "" {
		return "", false
	}
	sum := sha256.Sum256([]byte(key))
	found := -1
	for i, h := range g.hashes {
		if subtle.ConstantTimeCompare(sum[:], h[:]) == 1 {
			found = i
		}
	}
	if found < 0 {
		return "", false
	}
	return g.ids[found], true
}

// take is a simple token bucket per key.
func (g *Guard) take(id string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	b, ok := g.buckets[id]
	if !ok {
		b = &bucket{tokens: g.burst, last: now}
		g.buckets[id] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * g.rate
	if b.tokens > g.burst {
		b.tokens = g.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
