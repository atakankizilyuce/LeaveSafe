package ws

import "time"

// What one client may ask for per second, and how much of a burst is forgiven.
//
// A paired phone sends almost nothing: a ping every fifteen seconds, and a
// message when a thumb touches the screen. The allowance is set far above that
// so nothing a person can do reaches it, and far below what a program can do.
//
// The cost of not having it is not evenly spread. `update_config` writes
// config.json to disk on every message, `trigger_sensor` sounds the siren and
// broadcasts to every paired phone, and both are reachable by anyone holding a
// session — a stolen phone, or a token lifted from one. None of that needs the
// PIN, so the only thing bounding the damage is how fast messages can arrive.
const (
	messagesPerSecond = 8
	messageBurst      = 24
)

// tokenBucket is a plain token-bucket limiter over the constants above.
//
// It refills continuously from the clock rather than on a ticker, so an idle
// client costs nothing to keep and there is no goroutine per connection. Not
// safe for concurrent use: each one belongs to a single client, and a client's
// messages are handled on that client's own goroutine.
type tokenBucket struct {
	tokens float64
	last   time.Time
	now    func() time.Time // replaced in tests
}

func newTokenBucket() *tokenBucket {
	return &tokenBucket{
		tokens: messageBurst, // a fresh client starts with a full allowance
		now:    time.Now,
	}
}

// allow reports whether one more message may be handled, spending a token if so.
func (b *tokenBucket) allow() bool {
	now := b.now()
	if b.last.IsZero() {
		b.last = now
	}

	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.last = now
		b.tokens += elapsed * messagesPerSecond
		if b.tokens > messageBurst {
			b.tokens = messageBurst
		}
	}

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
