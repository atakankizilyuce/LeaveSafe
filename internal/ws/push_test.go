package ws

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// rememberingNotifier stands in for internal/push. What this package owes is
// that subscriptions reach it and alerts reach it; what it does with them is
// tested where it is written.
type rememberingNotifier struct {
	mu       sync.Mutex
	subs     [][3]string
	messages []string
	refuse   error
}

func (n *rememberingNotifier) PublicKey() string { return "the-public-key" }

func (n *rememberingNotifier) Subscribe(endpoint, key, auth string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.refuse != nil {
		return n.refuse
	}
	n.subs = append(n.subs, [3]string{endpoint, key, auth})
	return nil
}

func (n *rememberingNotifier) Notify(message string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.messages = append(n.messages, message)
}

func (n *rememberingNotifier) told() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.messages...)
}

func (n *rememberingNotifier) subscriptions() [][3]string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([][3]string(nil), n.subs...)
}

// waitFor gives the push, which is deliberately handed to a goroutine, a moment
// to happen. Nothing waits on it in the running program: an outbound request to
// a push service on the other side of the world must not sit between a sensor
// firing and the siren starting.
func waitFor(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal("the push never happened")
		}
		time.Sleep(time.Millisecond)
	}
}

// The whole point. A phone that is not connected is the normal case for the
// alarm that matters — the laptop is being carried out of the building and the
// owner is somewhere else — and until now that phone was told nothing at all.
func TestAnAlertReachesPhonesThatAreNotConnected(t *testing.T) {
	hub := testHub(t)
	notifier := &rememberingNotifier{}
	hub.SetPushNotifier(notifier)

	hub.PushAlert(NewAlert("lid", "critical", "The lid was closed while armed."))

	waitFor(t, func() bool { return len(notifier.told()) == 1 })
	if got := notifier.told()[0]; got != "The lid was closed while armed." {
		t.Errorf("the phone was told %q, want what the alert said", got)
	}
}

// Without one, this hub behaves exactly as it did before. That is the whole of
// the off switch, and it must not be a crash.
func TestAnAlertWithNowhereToPushItIsStillDelivered(t *testing.T) {
	hub := testHub(t)

	hub.PushAlert(NewAlert("lid", "critical", "The lid was closed while armed."))
}

// A status change is not worth waking somebody's phone for, and a message with
// nothing in it is not worth a notification that says nothing.
func TestOnlyAlertsThatSaySomethingAreWorthWakingAPhoneFor(t *testing.T) {
	hub := testHub(t)
	notifier := &rememberingNotifier{}
	hub.SetPushNotifier(notifier)

	hub.PushAlert(ServerMessage{Type: MsgTypeStatus})
	hub.PushAlert(NewAlert("lid", "critical", ""))

	time.Sleep(50 * time.Millisecond)
	if got := notifier.told(); len(got) != 0 {
		t.Errorf("phones were woken for %v", got)
	}
}

// The subscription arrives from a paired phone and has to reach the thing that
// will use it, months later, when that phone is somewhere else.
func TestAPairedPhoneCanHandOverSomewhereToReachIt(t *testing.T) {
	hub := testHub(t)
	notifier := &rememberingNotifier{}
	hub.SetPushNotifier(notifier)
	client, _ := hub.pairRecorder(t)

	hub.handleMessage(client, ClientMessage{
		Type: MsgTypePushSubscribe,
		Push: &PushSub{Endpoint: "https://push.example.net/a", Key: "k", Auth: "a"},
	})

	subs := notifier.subscriptions()
	if len(subs) != 1 {
		t.Fatalf("%d subscriptions reached the notifier, want 1", len(subs))
	}
	if subs[0] != [3]string{"https://push.example.net/a", "k", "a"} {
		t.Errorf("the subscription arrived as %v", subs[0])
	}
}

// A phone that has not paired must not be able to point this laptop at a URL to
// make outbound requests to.
func TestAnUnpairedPhoneCannotHandOverAnything(t *testing.T) {
	hub := testHub(t)
	notifier := &rememberingNotifier{}
	hub.SetPushNotifier(notifier)
	client := hub.RegisterExternalClient(&recorder{}, nil)

	hub.handleMessage(client, ClientMessage{
		Type: MsgTypePushSubscribe,
		Push: &PushSub{Endpoint: "https://push.example.net/a", Key: "k", Auth: "a"},
	})

	if got := notifier.subscriptions(); len(got) != 0 {
		t.Errorf("an unpaired phone got %v through", got)
	}
}

// The phone is connected right now — it is how the message arrived — so there
// is nothing broken to report to it, and the one thing that must not happen is
// a bad subscription taking down the connection that is working.
func TestASubscriptionThatCannotBeUsedIsNotFatal(t *testing.T) {
	hub := testHub(t)
	hub.SetPushNotifier(&rememberingNotifier{refuse: errors.New("not an https endpoint")})
	client, _ := hub.pairRecorder(t)

	hub.handleMessage(client, ClientMessage{
		Type: MsgTypePushSubscribe,
		Push: &PushSub{Endpoint: "http://push.example.net/a", Key: "k", Auth: "a"},
	})

	hub.handleMessage(client, ClientMessage{Type: MsgTypePing})
	if !client.authenticated {
		t.Error("the connection was lost over a subscription it could not use")
	}
}

// A message with no subscription in it at all, which is what a truncated or
// hand-written one looks like.
func TestAPushMessageWithNothingInItIsIgnored(t *testing.T) {
	hub := testHub(t)
	notifier := &rememberingNotifier{}
	hub.SetPushNotifier(notifier)
	client, _ := hub.pairRecorder(t)

	hub.handleMessage(client, ClientMessage{Type: MsgTypePushSubscribe})

	if got := notifier.subscriptions(); len(got) != 0 {
		t.Errorf("something got through: %v", got)
	}
}

// The phone cannot subscribe to anything without it, and it arrives with the
// successful pairing rather than the greeting: not a secret, but there is no
// reason to answer it to a connection that has not proved it holds the key.
func TestThePairedPhoneIsToldWhichKeyToSubscribeWith(t *testing.T) {
	hub := testHub(t)
	hub.SetPushNotifier(&rememberingNotifier{})
	_, rec := hub.pairRecorder(t)

	msg, ok := rec.saw(MsgTypeAuthOK)
	if !ok {
		t.Fatal("the phone was never told it had paired")
	}
	if msg.PushKey != "the-public-key" {
		t.Errorf("auth_ok carried push key %q, want the notifier's", msg.PushKey)
	}
}
