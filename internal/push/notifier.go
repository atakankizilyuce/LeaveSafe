package push

import (
	"context"
	"errors"
	"time"

	log "github.com/sirupsen/logrus"
)

// notifyTimeout bounds one round of telling every phone. The alarm is already
// sounding by the time this runs and nothing waits on it, but a round that
// never ends holds goroutines for as long as the machine is up.
const notifyTimeout = 30 * time.Second

// Notifier is the whole of this package as the rest of the program uses it:
// somewhere to put a subscription, and something to say.
//
// It exists so that internal/ws can talk about telling phones without knowing
// what a VAPID key is, and so that everything about push lives behind one type
// that either works or reports why it does not.
type Notifier struct {
	identity *Identity
	store    *Store
	sender   *Sender
}

// Open prepares push notification from the files in configDir, creating a
// signing key if there is not one yet.
func Open(configDir string) (*Notifier, error) {
	identity, err := LoadOrCreateIdentity(IdentityPath(configDir))
	if err != nil {
		return nil, err
	}
	return &Notifier{
		identity: identity,
		store:    OpenStore(StorePath(configDir)),
		sender:   NewSender(identity),
	}, nil
}

// PublicKey is what a phone needs before it can subscribe.
func (n *Notifier) PublicKey() string { return n.identity.PublicKey() }

// Subscribe remembers somewhere to reach a phone.
func (n *Notifier) Subscribe(endpoint, key, auth string) error {
	sub, err := ParseSubscription(endpoint, key, auth)
	if err != nil {
		return err
	}
	return n.store.Add(sub)
}

// Count is how many phones would be told.
func (n *Notifier) Count() int { return n.store.Count() }

// Notify tells every subscribed phone, and is the reason this package exists.
//
// Every phone is attempted whatever the ones before it did. They are separate
// phones on separate networks with separate push services, and one that has
// gone away — an uninstalled app, cleared site data — must not be the reason
// the others hear nothing. That one is also forgotten as it goes: a
// subscription the service says is gone will never work again, and keeping it
// means failing to reach it once per alarm forever.
func (n *Notifier) Notify(message string) {
	subs := n.store.All()
	if len(subs) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
	defer cancel()

	var reached int
	for _, sub := range subs {
		err := n.sender.Send(ctx, sub, []byte(message))
		switch {
		case err == nil:
			reached++
		case errors.Is(err, ErrSubscriptionGone):
			log.Infof("A phone is no longer subscribed to alerts; forgetting it")
			if err := n.store.Remove(sub.Endpoint); err != nil {
				log.Warnf("Could not forget a subscription that has gone: %v", err)
			}
		default:
			log.Warnf("Could not tell a phone about the alarm: %v", err)
		}
	}

	log.Infof("Alarm pushed to %d of %d phones that are not connected", reached, len(subs))
}
