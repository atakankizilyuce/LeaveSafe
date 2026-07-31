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

// What one socket may spend on pairing attempts.
//
// Pairing needs an allowance of its own rather than a share of the one above.
// It cannot go in the same bucket — a flood from a paired phone would eat the
// allowance a stranger's guesses are counted against — but leaving it with no
// bucket at all meant an unpaired peer could send pairing attempts as fast as
// the network carried them, and every one of them wrote a line to the security
// event log.
//
// That log is size-rotated and keeps roughly six megabytes across its
// generations, so a few tens of thousands of refused attempts push out every
// genuine record: the arm, the sensor alert, the disconnect. An attacker who
// tripped the alarm could then erase the history of having done so, from an
// unauthenticated socket, without ever knowing the key. On the remote-access
// path that socket comes from the internet.
//
// The bucket is deliberately never the thing that refuses a client first. The
// per-address lockout is: it is the documented behavior, it is what the phone
// renders a countdown from, and it is what the tests prove. So the burst is
// sized above the configured attempt allowance and this only catches what
// carries on past the point where the lockout has already said no — which is
// the flood, and nothing a person does.
//
// A phone sends one pairing attempt per connection and a second if the user
// mistypes, so two per second is far above the honest case and far below what a
// script needs.
const (
	authAttemptsPerSecond = 2
	// authAttemptMargin is the headroom over max_auth_attempts, so a client
	// always reaches the lockout and hears why it was refused.
	authAttemptMargin = 3
)

// tokenBucket is a plain token-bucket limiter.
//
// It refills continuously from the clock rather than on a ticker, so an idle
// client costs nothing to keep and there is no goroutine per connection. Not
// safe for concurrent use: each one belongs to a single client, and a client's
// messages are handled on that client's own goroutine.
type tokenBucket struct {
	tokens float64
	rate   float64
	burst  float64
	last   time.Time
	now    func() time.Time // replaced in tests
}

func newBucket(rate, burst float64) *tokenBucket {
	return &tokenBucket{
		tokens: burst, // a fresh client starts with a full allowance
		rate:   rate,
		burst:  burst,
		now:    time.Now,
	}
}

func newTokenBucket() *tokenBucket {
	return newBucket(messagesPerSecond, messageBurst)
}

// newAuthBucket returns a pairing-attempt bucket with room for the whole
// attempt allowance plus the margin above.
func newAuthBucket(maxAttempts int) *tokenBucket {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	return newBucket(authAttemptsPerSecond, float64(maxAttempts+authAttemptMargin))
}

// allow reports whether one more message may be handled, spending a token if so.
func (b *tokenBucket) allow() bool {
	now := b.now()
	if b.last.IsZero() {
		b.last = now
	}

	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.last = now
		b.tokens += elapsed * b.rate
		if b.tokens > b.burst {
			b.tokens = b.burst
		}
	}

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
