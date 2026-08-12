package push

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	// sendTimeout bounds the outbound request. The alarm is already sounding on
	// the laptop by the time this runs, so nothing waits on it — but a request
	// that hangs holds a goroutine for as long as the alarm lasts.
	sendTimeout = 10 * time.Second

	// messageTTL is how long the push service should hold the message for a
	// phone that is switched off or out of signal, in seconds. Four hours: long
	// enough to cover a flight or a meeting, short enough that nobody is told
	// about a break-in that was dealt with yesterday.
	messageTTL = 4 * 60 * 60
)

// ErrSubscriptionGone means the push service says this phone is no longer
// reachable at this endpoint — it uninstalled the app, cleared its site data,
// or the browser rotated the subscription.
//
// It is worth its own error because it is the one failure that is not worth
// retrying and is worth acting on: the subscription should be forgotten rather
// than kept and pushed to for the life of the installation.
var ErrSubscriptionGone = errors.New("the phone is no longer subscribed")

// Sender delivers messages to push services.
type Sender struct {
	identity *Identity
	client   *http.Client
	// now is time.Now, as a field so a test can sign a token at a known moment.
	now func() time.Time
}

// NewSender returns a Sender that signs with id.
func NewSender(id *Identity) *Sender {
	return &Sender{
		identity: id,
		client:   &http.Client{Timeout: sendTimeout},
		now:      time.Now,
	}
}

// Send delivers one message to one phone.
//
// The message is encrypted to the subscription before it goes anywhere, so what
// travels — and what the push service stores until the phone next connects — is
// ciphertext it has no key for. What the service does learn is that a message
// was sent, and when: that much is unavoidable in any arrangement where
// somebody else carries the message, and it is why the payload says an alarm
// fired rather than what tripped it or where the laptop is.
func (s *Sender) Send(ctx context.Context, sub Subscription, message []byte) error {
	sender, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate a sending key: %w", err)
	}
	body, err := encrypt(message, sub, newSalt(), sender)
	if err != nil {
		return err
	}

	req, err := s.request(ctx, sub, body)
	if err != nil {
		return err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("reach the push service: %w", err)
	}
	defer func() {
		// Drained before closing so the connection can be reused: an alarm may
		// be pushed to several phones at once, and to the same phone again when
		// escalation raises it.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
	}()

	return statusError(resp.StatusCode)
}

func (s *Sender) request(ctx context.Context, sub Subscription, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build the push request: %w", err)
	}

	authorization, err := s.identity.authorization(sub.Endpoint, s.now())
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", authorization)
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Length", strconv.Itoa(len(body)))
	req.Header.Set("TTL", strconv.Itoa(messageTTL))
	// The phone should wake for this. It is the whole point of the message.
	req.Header.Set("Urgency", "high")
	return req, nil
}

// statusError turns a push service's answer into something the caller can act
// on. Only two outcomes matter to it: the message was accepted, or this phone
// will never be reachable here again.
func statusError(status int) error {
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == http.StatusNotFound || status == http.StatusGone:
		return fmt.Errorf("%w (%d)", ErrSubscriptionGone, status)
	default:
		return fmt.Errorf("the push service answered %d", status)
	}
}
