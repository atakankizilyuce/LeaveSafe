package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"net"
	"sync"
	"time"
)

const (
	defaultMaxAttempts     = 5
	defaultLockoutPeriod   = 60 * time.Second
	defaultMaxSessions     = 3
	defaultMaxTrackedAddrs = 256
	keyDigits              = 15 // 15 random digits + 1 Luhn check = 16 total
)

// UnknownAddr is the address to pass for transports that have no network
// address of their own, such as BLE. Every such client shares one rate-limit
// bucket, which is correct: they all arrive over the same local radio.
const UnknownAddr = "local"

// Options configures session and brute-force policy. A zero value in any field
// means "use the default", so callers can override only what they care about.
type Options struct {
	// MaxSessions caps concurrent authenticated clients.
	MaxSessions int
	// MaxAttempts is how many failed keys one address may submit before it is
	// locked out.
	MaxAttempts int
	// LockoutPeriod is how long a locked-out address stays locked out.
	LockoutPeriod time.Duration
	// MaxTrackedAddrs bounds the failure table so that a stream of attempts
	// from spoofed addresses cannot grow it without limit.
	MaxTrackedAddrs int
}

func (o Options) withDefaults() Options {
	if o.MaxSessions <= 0 {
		o.MaxSessions = defaultMaxSessions
	}
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = defaultMaxAttempts
	}
	if o.LockoutPeriod <= 0 {
		o.LockoutPeriod = defaultLockoutPeriod
	}
	if o.MaxTrackedAddrs <= 0 {
		o.MaxTrackedAddrs = defaultMaxTrackedAddrs
	}
	return o
}

// attempts records the failure state of a single remote address.
type attempts struct {
	failures    int
	lockedUntil time.Time
	lastSeen    time.Time
}

// Manager handles pairing key generation, validation, rate limiting, and sessions.
type Manager struct {
	mu         sync.Mutex
	opts       Options
	pairingKey string          // 16-digit key with Luhn check
	sessions   map[string]bool // active session tokens

	// byAddr holds failures per remote address rather than one global counter.
	// A global counter is a remote kill switch once the port is reachable from
	// the internet: anyone can spend five wrong keys and lock the owner out for
	// as long as they care to keep doing it.
	byAddr map[string]*attempts
}

// NewManager creates a new auth manager with default policy and a fresh
// pairing key.
func NewManager() (*Manager, error) {
	return NewManagerWithOptions(Options{})
}

// NewManagerWithOptions creates a new auth manager with the given policy.
func NewManagerWithOptions(opts Options) (*Manager, error) {
	key, err := generatePairingKey()
	if err != nil {
		return nil, fmt.Errorf("generate pairing key: %w", err)
	}
	return &Manager{
		opts:       opts.withDefaults(),
		pairingKey: key,
		sessions:   make(map[string]bool),
		byAddr:     make(map[string]*attempts),
	}, nil
}

// MaxSessions returns the configured concurrent session cap.
func (m *Manager) MaxSessions() int { return m.opts.MaxSessions }

// MaxAttempts returns the configured per-address failure allowance.
func (m *Manager) MaxAttempts() int { return m.opts.MaxAttempts }

// PairingKey returns the current pairing key formatted as XXXX-XXXX-XXXX-XXXX.
func (m *Manager) PairingKey() string {
	k := m.pairingKey
	return fmt.Sprintf("%s-%s-%s-%s", k[0:4], k[4:8], k[8:12], k[12:16])
}

// RawPairingKey returns the unformatted 16-digit pairing key.
func (m *Manager) RawPairingKey() string {
	return m.pairingKey
}

// Authenticate validates a pairing key presented by addr and returns a session
// token if valid. Rate limiting is applied per address. Returns token,
// remaining attempts for that address, and error.
func (m *Manager) Authenticate(addr, key string) (string, int, error) {
	host := normalizeAddr(addr)

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	rec := m.record(host, now)

	if now.Before(rec.lockedUntil) {
		return "", 0, fmt.Errorf("locked out for %.0f seconds", time.Until(rec.lockedUntil).Seconds())
	}

	// The session cap protects a real resource and stays global. It is checked
	// before the key so that a full server does not leak whether a key was
	// right, and it deliberately does not count as a failed attempt.
	if len(m.sessions) >= m.opts.MaxSessions {
		return "", m.opts.MaxAttempts - rec.failures, fmt.Errorf("maximum connections reached")
	}

	if stripDashes(key) != m.pairingKey {
		rec.failures++
		if rec.failures >= m.opts.MaxAttempts {
			rec.lockedUntil = now.Add(m.opts.LockoutPeriod)
			rec.failures = 0
			return "", 0, fmt.Errorf("invalid key, locked out for %v", m.opts.LockoutPeriod)
		}
		return "", m.opts.MaxAttempts - rec.failures, fmt.Errorf("invalid key")
	}

	// Success clears this address's failure history.
	delete(m.byAddr, host)

	token, err := generateSessionToken()
	if err != nil {
		return "", m.opts.MaxAttempts, fmt.Errorf("generate session token: %w", err)
	}
	m.sessions[token] = true
	return token, m.opts.MaxAttempts, nil
}

// record returns the failure record for host, creating it if needed and
// evicting stale entries when the table is full. Callers must hold m.mu.
func (m *Manager) record(host string, now time.Time) *attempts {
	if rec, ok := m.byAddr[host]; ok {
		rec.lastSeen = now
		return rec
	}

	if len(m.byAddr) >= m.opts.MaxTrackedAddrs {
		m.evictLocked(now)
	}

	rec := &attempts{lastSeen: now}
	m.byAddr[host] = rec
	return rec
}

// evictLocked drops expired records, and if that frees nothing, the least
// recently seen one. Callers must hold m.mu.
func (m *Manager) evictLocked(now time.Time) {
	freed := false
	for host, rec := range m.byAddr {
		if now.After(rec.lockedUntil) && now.Sub(rec.lastSeen) > m.opts.LockoutPeriod {
			delete(m.byAddr, host)
			freed = true
		}
	}
	if freed {
		return
	}

	var oldestHost string
	var oldestSeen time.Time
	for host, rec := range m.byAddr {
		if oldestHost == "" || rec.lastSeen.Before(oldestSeen) {
			oldestHost, oldestSeen = host, rec.lastSeen
		}
	}
	if oldestHost != "" {
		delete(m.byAddr, oldestHost)
	}
}

// ValidateSession checks if a session token is valid.
func (m *Manager) ValidateSession(token string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[token]
}

// RemoveSession removes a session token.
func (m *Manager) RemoveSession(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, token)
}

// Regenerate creates a new pairing key and invalidates all sessions.
// Returns the new formatted key.
func (m *Manager) Regenerate() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key, err := generatePairingKey()
	if err != nil {
		return "", fmt.Errorf("generate pairing key: %w", err)
	}
	m.pairingKey = key
	m.sessions = make(map[string]bool)
	m.byAddr = make(map[string]*attempts)

	k := m.pairingKey
	return fmt.Sprintf("%s-%s-%s-%s", k[0:4], k[4:8], k[8:12], k[12:16]), nil
}

// SessionCount returns the number of active sessions.
func (m *Manager) SessionCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// TrackedAddrs returns how many addresses currently have failure history.
func (m *Manager) TrackedAddrs() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.byAddr)
}

// normalizeAddr reduces a transport address to the identity we rate-limit on.
// A host:port pair becomes the bare host, because the port changes on every
// TCP connection and would give each attempt a fresh allowance.
func normalizeAddr(addr string) string {
	if addr == "" {
		return UnknownAddr
	}
	if host, _, err := net.SplitHostPort(addr); err == nil && host != "" {
		return host
	}
	return addr
}

// generatePairingKey creates a 16-digit numeric key with Luhn check digit.
func generatePairingKey() (string, error) {
	digits := make([]byte, keyDigits)
	for i := 0; i < keyDigits; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		digits[i] = "0123456789"[n.Int64()]
	}
	check := luhnCheckDigit(string(digits))
	return string(digits) + string(check), nil
}

// generateSessionToken creates a 256-bit random hex token.
func generateSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// stripDashes removes all dashes from a string.
func stripDashes(s string) string {
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '-' {
			result = append(result, s[i])
		}
	}
	return string(result)
}
