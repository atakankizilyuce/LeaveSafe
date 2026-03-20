package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"sync"
	"time"
)

const (
	maxAttempts    = 5
	lockoutPeriod  = 60 * time.Second
	maxConnections = 3
	keyDigits      = 15 // 15 random digits + 1 Luhn check = 16 total
)

// Manager handles pairing key generation, validation, rate limiting, and sessions.
type Manager struct {
	mu           sync.Mutex
	pairingKey   string            // 16-digit key with Luhn check
	sessions     map[string]bool   // active session tokens
	attempts     int               // failed attempts counter
	lockedUntil  time.Time         // lockout expiry
}

// NewManager creates a new auth manager with a fresh pairing key.
func NewManager() (*Manager, error) {
	key, err := generatePairingKey()
	if err != nil {
		return nil, fmt.Errorf("generate pairing key: %w", err)
	}
	return &Manager{
		pairingKey: key,
		sessions:   make(map[string]bool),
	}, nil
}

// PairingKey returns the current pairing key formatted as XXXX-XXXX-XXXX-XXXX.
func (m *Manager) PairingKey() string {
	k := m.pairingKey
	return fmt.Sprintf("%s-%s-%s-%s", k[0:4], k[4:8], k[8:12], k[12:16])
}

// RawPairingKey returns the unformatted 16-digit pairing key.
func (m *Manager) RawPairingKey() string {
	return m.pairingKey
}

// Authenticate validates a pairing key and returns a session token if valid.
// Returns token, remaining attempts, and error.
func (m *Manager) Authenticate(key string) (string, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check lockout
	if time.Now().Before(m.lockedUntil) {
		remaining := time.Until(m.lockedUntil).Seconds()
		return "", 0, fmt.Errorf("locked out for %.0f seconds", remaining)
	}

	// Check connection limit
	if len(m.sessions) >= maxConnections {
		return "", maxAttempts - m.attempts, fmt.Errorf("maximum connections reached")
	}

	// Strip dashes from input key
	stripped := stripDashes(key)

	if stripped != m.pairingKey {
		m.attempts++
		remaining := maxAttempts - m.attempts
		if m.attempts >= maxAttempts {
			m.lockedUntil = time.Now().Add(lockoutPeriod)
			m.attempts = 0
			return "", 0, fmt.Errorf("invalid key, locked out for %v", lockoutPeriod)
		}
		return "", remaining, fmt.Errorf("invalid key")
	}

	// Success - reset attempts and generate session token
	m.attempts = 0
	token, err := generateSessionToken()
	if err != nil {
		return "", maxAttempts, fmt.Errorf("generate session token: %w", err)
	}
	m.sessions[token] = true
	return token, maxAttempts, nil
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
	m.attempts = 0
	m.lockedUntil = time.Time{}

	k := m.pairingKey
	return fmt.Sprintf("%s-%s-%s-%s", k[0:4], k[4:8], k[8:12], k[12:16]), nil
}

// SessionCount returns the number of active sessions.
func (m *Manager) SessionCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// generatePairingKey creates a 16-digit numeric key with Luhn check digit.
func generatePairingKey() (string, error) {
	digits := make([]byte, keyDigits)
	for i := 0; i < keyDigits; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		digits[i] = byte('0' + n.Int64())
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
