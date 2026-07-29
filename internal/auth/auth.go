package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"math/big"
	"net"
	"strings"
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
	// SessionTTL is how long a session stays valid after it is created,
	// regardless of activity. Zero means sessions never expire on age.
	SessionTTL time.Duration
	// SessionIdle is how long a session may go unused before it is dropped.
	// Zero means idle sessions are kept.
	SessionIdle time.Duration
	// PairingKey fixes the key instead of generating a fresh one. It must be 16
	// digits with a valid Luhn check; anything else is rejected. Used when the
	// key is persisted so a phone stays paired across restarts.
	PairingKey string
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

// session is one paired client's authorisation to act.
type session struct {
	createdAt time.Time
	lastSeen  time.Time
}

// Manager handles pairing key generation, validation, rate limiting, and sessions.
type Manager struct {
	mu         sync.Mutex
	opts       Options
	pairingKey string              // 16-digit key with Luhn check
	sessions   map[string]*session // active session tokens

	// byAddr holds failures per remote address rather than one global counter.
	// A global counter is a remote kill switch once the port is reachable from
	// the internet: anyone can spend five wrong keys and lock the owner out for
	// as long as they care to keep doing it.
	byAddr map[string]*attempts

	// now is time.Now, replaced in tests so expiry can be exercised without
	// sleeping through a session lifetime.
	now func() time.Time
}

// NewManager creates a new auth manager with default policy and a fresh
// pairing key.
func NewManager() (*Manager, error) {
	return NewManagerWithOptions(Options{})
}

// NewManagerWithOptions creates a new auth manager with the given policy.
func NewManagerWithOptions(opts Options) (*Manager, error) {
	key := strings.ReplaceAll(opts.PairingKey, "-", "")
	if key == "" {
		generated, err := generatePairingKey()
		if err != nil {
			return nil, fmt.Errorf("generate pairing key: %w", err)
		}
		key = generated
	} else if len(key) != 16 || !luhnValid(key) {
		return nil, fmt.Errorf("supplied pairing key is not a valid 16-digit key")
	}

	return &Manager{
		opts:       opts.withDefaults(),
		pairingKey: key,
		sessions:   make(map[string]*session),
		byAddr:     make(map[string]*attempts),
		now:        time.Now,
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

	now := m.now()
	rec := m.record(host, now)

	if now.Before(rec.lockedUntil) {
		return "", 0, fmt.Errorf("locked out for %.0f seconds", now.Sub(rec.lockedUntil).Abs().Seconds())
	}

	// Expired sessions are swept before the cap is measured, so a phone whose
	// session timed out overnight does not occupy a slot the owner needs now.
	m.sweepSessionsLocked(now)

	// The session cap protects a real resource and stays global. It is checked
	// before the key so that a full server does not leak whether a key was
	// right, and it deliberately does not count as a failed attempt.
	if len(m.sessions) >= m.opts.MaxSessions {
		return "", m.opts.MaxAttempts - rec.failures, fmt.Errorf("maximum connections reached")
	}

	if subtle.ConstantTimeCompare([]byte(stripDashes(key)), []byte(m.pairingKey)) != 1 {
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
	m.sessions[token] = &session{createdAt: now, lastSeen: now}
	return token, m.opts.MaxAttempts, nil
}

// CheckPin verifies a disarm PIN against its stored hash, applying the same
// per-address lockout as pairing-key attempts. PIN and key failures share one
// bucket on purpose: both are guesses at a secret from the same source, and a
// short numeric PIN is the easier of the two to sweep without a limit.
func (m *Manager) CheckPin(addr, pin, pinHash string) error {
	host := normalizeAddr(addr)

	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	rec := m.record(host, now)

	if now.Before(rec.lockedUntil) {
		return fmt.Errorf("locked out for %.0f seconds", now.Sub(rec.lockedUntil).Abs().Seconds())
	}

	if !VerifyPinHash(pin, pinHash) {
		rec.failures++
		if rec.failures >= m.opts.MaxAttempts {
			rec.lockedUntil = now.Add(m.opts.LockoutPeriod)
			rec.failures = 0
			return fmt.Errorf("invalid PIN, locked out for %v", m.opts.LockoutPeriod)
		}
		return fmt.Errorf("invalid PIN")
	}

	delete(m.byAddr, host)
	return nil
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

// ValidateSession reports whether a session token is still valid, without
// counting the check itself as activity. An expired token is dropped as it is
// found, so a caller polling this also does the cleanup.
//
// A token is a bearer credential: whoever holds it can arm, disarm and read the
// machine's position without ever presenting the pairing key. Before the
// lifetime limits, one lived until the process restarted or the key was rotated
// by hand — so a token captured from a phone left on a desk stayed useful
// indefinitely. Both limits are configurable and either can be switched off by
// setting it to zero.
func (m *Manager) ValidateSession(token string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.validSessionLocked(token, m.now())
}

// TouchSession validates a token and, if it is still good, restarts its idle
// timer. Call it for real client activity — a message the paired phone sent —
// and never for the server's own periodic checks, which would otherwise keep an
// abandoned session alive forever.
func (m *Manager) TouchSession(token string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	if !m.validSessionLocked(token, now) {
		return false
	}
	m.sessions[token].lastSeen = now
	return true
}

// validSessionLocked reports whether token names a live session, deleting it if
// it has expired. Callers must hold m.mu.
func (m *Manager) validSessionLocked(token string, now time.Time) bool {
	sess, ok := m.sessions[token]
	if !ok {
		return false
	}
	if m.sessionExpiredLocked(sess, now) {
		delete(m.sessions, token)
		return false
	}
	return true
}

// sessionExpiredLocked reports whether sess has passed either limit. Callers
// must hold m.mu.
func (m *Manager) sessionExpiredLocked(sess *session, now time.Time) bool {
	if m.opts.SessionTTL > 0 && now.Sub(sess.createdAt) >= m.opts.SessionTTL {
		return true
	}
	if m.opts.SessionIdle > 0 && now.Sub(sess.lastSeen) >= m.opts.SessionIdle {
		return true
	}
	return false
}

// sweepSessionsLocked drops every expired session. Callers must hold m.mu.
func (m *Manager) sweepSessionsLocked(now time.Time) {
	if m.opts.SessionTTL <= 0 && m.opts.SessionIdle <= 0 {
		return
	}
	for token, sess := range m.sessions {
		if m.sessionExpiredLocked(sess, now) {
			delete(m.sessions, token)
		}
	}
}

// SweepSessions drops expired sessions and returns how many were removed. The
// hub calls it on its heartbeat so a session that goes stale while its socket
// stays open is noticed without waiting for the next message.
func (m *Manager) SweepSessions() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	before := len(m.sessions)
	m.sweepSessionsLocked(m.now())
	return before - len(m.sessions)
}

// SessionTTL returns the configured absolute session lifetime, zero if none.
func (m *Manager) SessionTTL() time.Duration { return m.opts.SessionTTL }

// SessionIdle returns the configured idle timeout, zero if none.
func (m *Manager) SessionIdle() time.Duration { return m.opts.SessionIdle }

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
	m.sessions = make(map[string]*session)
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
