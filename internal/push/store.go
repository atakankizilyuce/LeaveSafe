package push

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// storeMode keeps the stored subscriptions readable by their owner alone. They
// are not secrets in the way a key is, but each one is a URL that will wake
// somebody's phone.
const storeMode os.FileMode = 0o600

// Store remembers which phones to reach, across restarts.
//
// Across restarts is the point. A subscription is handed over while the phone
// is on the same network as the laptop — during pairing — and it has to survive
// until the day it is needed, which is a day the phone is somewhere else
// entirely. Keeping them in memory would mean a laptop that reboots on Tuesday
// and tells nobody on Wednesday.
type Store struct {
	mu   sync.Mutex
	path string
	subs map[string]Subscription
}

// OpenStore reads the subscriptions kept at path, or starts an empty one.
//
// A file that cannot be read starts an empty store rather than refusing: the
// phones will subscribe again the next time they are on the same network, and a
// laptop that will not start because of a corrupt cache file is worse than a
// laptop with one fewer phone to tell.
func OpenStore(path string) *Store {
	s := &Store{path: path, subs: map[string]Subscription{}}

	raw, err := os.ReadFile(path) //nolint:gosec // a path this program chose
	if err != nil {
		return s
	}
	var stored map[string]Subscription
	if err := json.Unmarshal(raw, &stored); err != nil || stored == nil {
		return s
	}
	s.subs = stored
	return s
}

// Add remembers a phone, replacing whatever was known about it before.
//
// Keyed by endpoint, because that is what identifies a subscription: a phone
// that subscribes twice — a reinstall, a new pairing — produces a new endpoint
// and is a different one to reach, while the same phone re-sending the same
// endpoint is the same one and must not accumulate.
func (s *Store) Add(sub Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.subs[sub.Endpoint] = sub
	return s.saveLocked()
}

// Remove forgets a phone. Safe to call for one that is not there.
func (s *Store) Remove(endpoint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.subs[endpoint]; !ok {
		return nil
	}
	delete(s.subs, endpoint)
	return s.saveLocked()
}

// All is every phone to reach, in no particular order.
func (s *Store) All() []Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Subscription, 0, len(s.subs))
	for _, sub := range s.subs {
		out = append(out, sub)
	}
	return out
}

// Count is how many phones would be told.
func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.subs)
}

// saveLocked writes the store out. The caller holds mu.
//
// Written to a temporary file and renamed over the old one, so a laptop that
// loses power mid-write still has the subscriptions it had before rather than
// half of them.
func (s *Store) saveLocked() error {
	encoded, err := json.Marshal(s.subs)
	if err != nil {
		return fmt.Errorf("encode the push subscriptions: %w", err)
	}

	temp := s.path + ".tmp"
	if err := os.WriteFile(temp, encoded, storeMode); err != nil {
		return fmt.Errorf("write the push subscriptions: %w", err)
	}
	if err := os.Chmod(temp, storeMode); err != nil {
		return fmt.Errorf("restrict the push subscriptions: %w", err)
	}
	if err := os.Rename(temp, s.path); err != nil {
		return fmt.Errorf("replace the push subscriptions: %w", err)
	}
	return nil
}

// StorePath and IdentityPath are where the two files live, beside the rest of
// the configuration.
func StorePath(configDir string) string    { return filepath.Join(configDir, "push-subscriptions.json") }
func IdentityPath(configDir string) string { return filepath.Join(configDir, "push-key") }

// ErrNoSubscriptions is what a push to nobody answers. It is not a failure —
// nobody has paired a phone that can be reached this way — but it is the
// difference between "told everyone" and "told nobody", which the caller
// reports differently.
var ErrNoSubscriptions = errors.New("no phone is subscribed to push alerts")
