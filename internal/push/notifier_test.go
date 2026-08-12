package push

import (
	"encoding/base64"
	"net/http"
	"testing"
)

// notifierFor builds the whole of this package against a fake push service:
// real key, real store on disk, real encryption, and one HTTP endpoint that
// answers however the test needs it to.
func notifierFor(t *testing.T, svc *pushService) *Notifier {
	t.Helper()

	n, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	n.sender.client = svc.Client()
	return n
}

// A subscription the browser would have produced, pointed at the fake service.
func subscribeTo(t *testing.T, n *Notifier, svc *pushService, path string) {
	t.Helper()

	sub := aSubscription(t, svc.URL+path)
	err := n.Subscribe(
		sub.Endpoint,
		b64(sub.Key),
		b64(sub.Auth),
	)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
}

func b64(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

func TestAPhoneThatSubscribedIsToldAboutTheAlarm(t *testing.T) {
	svc := newPushService(t)
	n := notifierFor(t, svc)
	subscribeTo(t, n, svc, "/sub/1")

	n.Notify("A door opened while the laptop was armed.")

	if svc.gotAuth == "" {
		t.Error("nothing was sent to the push service")
	}
	if n.Count() != 1 {
		t.Errorf("Count = %d, want the phone still subscribed", n.Count())
	}
}

// The commonest case on a fresh install, and it must not be an error or a
// warning: nobody has paired a phone that can be reached this way yet.
func TestTellingNobodyIsNotAFailure(t *testing.T) {
	svc := newPushService(t)
	n := notifierFor(t, svc)

	n.Notify("an alarm")

	if svc.gotAuth != "" {
		t.Error("something was sent with nobody subscribed")
	}
}

// A subscription the service says is gone will never work again, and keeping it
// means failing to reach it once per alarm for the life of the installation.
func TestAPhoneThatHasGoneIsForgotten(t *testing.T) {
	svc := newPushService(t)
	svc.status = http.StatusGone
	n := notifierFor(t, svc)
	subscribeTo(t, n, svc, "/sub/1")

	n.Notify("an alarm")

	if got := n.Count(); got != 0 {
		t.Errorf("Count = %d, want the phone forgotten", got)
	}
}

// A service that is merely refusing right now — rate limiting, a bad day — has
// said nothing about whether this phone still exists, so the subscription stays.
func TestAPhoneIsNotForgottenOverATemporaryRefusal(t *testing.T) {
	svc := newPushService(t)
	svc.status = http.StatusTooManyRequests
	n := notifierFor(t, svc)
	subscribeTo(t, n, svc, "/sub/1")

	n.Notify("an alarm")

	if got := n.Count(); got != 1 {
		t.Errorf("Count = %d, want the phone kept", got)
	}
}

// Separate phones on separate networks with separate push services. One that
// has gone away must not be the reason the others hear nothing.
func TestOnePhoneGoingAwayDoesNotSilenceTheOthers(t *testing.T) {
	gone := newPushService(t)
	gone.status = http.StatusNotFound
	live := newPushService(t)

	n := notifierFor(t, live)
	subscribeTo(t, n, live, "/sub/live")
	// The dead one is reached through the same client, so it needs to be
	// trusted too; pointing both at the live client and using a status is not
	// possible across two servers, so this one is added directly.
	deadSub := aSubscription(t, gone.URL+"/sub/gone")
	if err := n.Subscribe(deadSub.Endpoint, b64(deadSub.Key), b64(deadSub.Auth)); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	n.Notify("an alarm")

	// The live one was reached whatever happened to the other.
	if live.gotAuth == "" {
		t.Error("the phone that was reachable was not told")
	}
}

// The key the phone needs before it can subscribe to anything.
func TestTheNotifierPublishesTheKeyToSubscribeWith(t *testing.T) {
	svc := newPushService(t)
	n := notifierFor(t, svc)

	if n.PublicKey() == "" {
		t.Error("no public key to hand a phone")
	}
}

// What arrives over the wire from a phone is input, and an endpoint this laptop
// would keep making outbound requests to is not something to store on trust.
func TestASubscriptionThatCannotBeUsedIsRefused(t *testing.T) {
	svc := newPushService(t)
	n := notifierFor(t, svc)

	err := n.Subscribe("http://push.example.net/a", "not base64", "nor this")

	if err == nil {
		t.Fatal("an unusable subscription was stored")
	}
	if n.Count() != 0 {
		t.Errorf("Count = %d, want nothing stored", n.Count())
	}
}

// Opening against a directory that cannot hold a key file is the one failure
// that stops this before it starts, and the caller decides what to do about it.
func TestOpeningWhereNothingCanBeWrittenSaysSo(t *testing.T) {
	if _, err := Open(t.TempDir() + "/does/not/exist"); err == nil {
		t.Error("Open reported success with nowhere to keep a key")
	}
}
