package push

import (
	"context"
	"crypto/ecdh"
	cryptorand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pushService is a stand-in for Google's or Mozilla's: it records what arrived
// and answers however the test needs it to.
type pushService struct {
	*httptest.Server
	status int

	gotAuth     string
	gotEncoding string
	gotTTL      string
	gotBody     []byte
}

func newPushService(t *testing.T) *pushService {
	t.Helper()

	svc := &pushService{status: http.StatusCreated}
	svc.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		svc.gotAuth = r.Header.Get("Authorization")
		svc.gotEncoding = r.Header.Get("Content-Encoding")
		svc.gotTTL = r.Header.Get("TTL")
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		svc.gotBody = body[:n]
		w.WriteHeader(svc.status)
	}))
	t.Cleanup(svc.Close)
	return svc
}

// senderFor wires a Sender to the fake service, trusting its certificate the
// way a real one's would be trusted by the system store.
func senderFor(t *testing.T, svc *pushService) *Sender {
	t.Helper()

	id, err := LoadOrCreateIdentity(filepath.Join(t.TempDir(), "push-key"))
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	s := NewSender(id)
	s.client = svc.Client()
	return s
}

func aSubscription(t *testing.T, endpoint string) Subscription {
	t.Helper()

	phone, err := ecdh.P256().GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("the phone's key: %v", err)
	}
	auth := make([]byte, authLen)
	if _, err := cryptorand.Read(auth); err != nil {
		t.Fatalf("the phone's auth secret: %v", err)
	}
	return Subscription{Endpoint: endpoint, Key: phone.PublicKey().Bytes(), Auth: auth}
}

// What actually leaves the machine: a signed token, the content coding the
// phone's browser expects, and a body that is not the message in the clear.
func TestWhatIsSentIsSignedAndUnreadable(t *testing.T) {
	svc := newPushService(t)

	const message = "A door opened while the laptop was armed."
	err := senderFor(t, svc).Send(t.Context(), aSubscription(t, svc.URL+"/sub/1"), []byte(message))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if !strings.HasPrefix(svc.gotAuth, "vapid t=") || !strings.Contains(svc.gotAuth, ", k=") {
		t.Errorf("Authorization = %q, want a vapid token and key", svc.gotAuth)
	}
	if svc.gotEncoding != "aes128gcm" {
		t.Errorf("Content-Encoding = %q, want aes128gcm", svc.gotEncoding)
	}
	if svc.gotTTL == "" {
		t.Error("no TTL, so a phone that is switched off is told nothing when it comes back")
	}
	// The push service stores this until the phone connects. It must not be
	// able to read what it is storing.
	if strings.Contains(string(svc.gotBody), "door") {
		t.Error("the message is in the clear in the body")
	}
}

// The token is what stops somebody who has learned the endpoint — a URL, not a
// secret — from sending this phone alarms of their own. It has to name the
// service it is for and say when it stops being good.
func TestTheTokenNamesTheServiceAndExpires(t *testing.T) {
	svc := newPushService(t)
	s := senderFor(t, svc)
	at := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return at }

	if err := s.Send(t.Context(), aSubscription(t, svc.URL+"/sub/1"), []byte("an alarm")); err != nil {
		t.Fatalf("Send: %v", err)
	}

	claims := claimsOf(t, svc.gotAuth)
	if got, want := claims["aud"], svc.URL; got != want {
		t.Errorf("aud = %v, want the push service's origin %q", got, want)
	}
	if got, want := claims["exp"], float64(at.Add(tokenLifetime).Unix()); got != want {
		t.Errorf("exp = %v, want %v", got, want)
	}
	// The path identifies the phone, so it has no business in a token the
	// service reads before it has read the request.
	if aud, _ := claims["aud"].(string); strings.Contains(aud, "/sub/1") {
		t.Errorf("aud = %q, which tells the service which subscription this is", aud)
	}
}

func claimsOf(t *testing.T, authorization string) map[string]any {
	t.Helper()

	token := strings.TrimPrefix(authorization, "vapid t=")
	token, _, _ = strings.Cut(token, ",")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("the token has %d parts, want 3: %q", len(parts), token)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode the claims: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("parse the claims: %v", err)
	}
	return claims
}

// A phone that has uninstalled the app or cleared its data is the one failure
// worth telling apart: it is not worth retrying, and the subscription should be
// forgotten rather than pushed to for the life of the installation.
func TestAPhoneThatIsGoneIsSaidToBeGone(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusGone} {
		svc := newPushService(t)
		svc.status = status

		err := senderFor(t, svc).Send(t.Context(), aSubscription(t, svc.URL+"/sub/1"), []byte("an alarm"))

		if !errors.Is(err, ErrSubscriptionGone) {
			t.Errorf("status %d gave %v, want it reported as gone", status, err)
		}
	}
}

func TestAServiceThatRefusesIsReported(t *testing.T) {
	svc := newPushService(t)
	svc.status = http.StatusTooManyRequests

	err := senderFor(t, svc).Send(t.Context(), aSubscription(t, svc.URL+"/sub/1"), []byte("an alarm"))

	if err == nil {
		t.Fatal("a refused push was reported as delivered")
	}
	if errors.Is(err, ErrSubscriptionGone) {
		t.Errorf("err = %v, want it kept rather than treated as a phone that is gone", err)
	}
}

func TestAnUnreachableServiceIsReported(t *testing.T) {
	svc := newPushService(t)
	s := senderFor(t, svc)
	svc.Close()

	err := s.Send(t.Context(), aSubscription(t, svc.URL+"/sub/1"), []byte("an alarm"))

	if err == nil {
		t.Fatal("a push to a service that is not there was reported as delivered")
	}
}

// The alarm is already sounding on the laptop by the time this runs, so nothing
// waits on it — but a caller that gives up has to be able to stop it.
func TestGivingUpStopsTheSend(t *testing.T) {
	svc := newPushService(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := senderFor(t, svc).Send(ctx, aSubscription(t, svc.URL+"/sub/1"), []byte("an alarm"))

	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want the cancellation", err)
	}
}

// An endpoint that is not a URL cannot be addressed and must not be sent to.
func TestAnEndpointThatIsNotAURLIsRefused(t *testing.T) {
	svc := newPushService(t)
	sub := aSubscription(t, "://not a url")

	if err := senderFor(t, svc).Send(t.Context(), sub, []byte("an alarm")); err == nil {
		t.Error("a push was sent to something that is not a URL")
	}
}
