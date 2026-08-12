package push

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Subscription is what a phone hands over so the laptop can reach it later.
//
// Endpoint is a URL at the browser vendor's push service — Google's for Chrome,
// Mozilla's for Firefox, Apple's for Safari — and it is the only part the
// service reads. Key and Auth belong to the phone: the first is the public half
// of a key pair whose private half never leaves the device, and the second is a
// secret it generated for this subscription. Together they are what makes the
// payload unreadable to the service carrying it.
type Subscription struct {
	Endpoint string `json:"endpoint"`
	Key      []byte `json:"key"`
	Auth     []byte `json:"auth"`
}

// ErrBadSubscription is what an unusable subscription answers.
var ErrBadSubscription = errors.New("unusable push subscription")

// ParseSubscription turns what the browser's PushManager produced into
// something this program will send to.
//
// The browser hands over base64url without padding, and the endpoint is a URL
// this laptop will make outbound requests to for as long as the phone stays
// paired. So it is checked rather than stored on trust: an endpoint that is not
// an https URL, or keys that are not the lengths the specification fixes, are
// refused here rather than at three in the morning when the alarm is the thing
// that needs to work.
func ParseSubscription(endpoint, key, auth string) (Subscription, error) {
	if err := checkEndpoint(endpoint); err != nil {
		return Subscription{}, err
	}

	rawKey, err := decodeKey(key, publicKeyLen, "p256dh")
	if err != nil {
		return Subscription{}, err
	}
	rawAuth, err := decodeKey(auth, authLen, "auth")
	if err != nil {
		return Subscription{}, err
	}

	return Subscription{Endpoint: endpoint, Key: rawKey, Auth: rawAuth}, nil
}

func checkEndpoint(endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("%w: the endpoint is not a URL: %w", ErrBadSubscription, err)
	}
	// https only, and not because of what it protects — the payload is already
	// encrypted to the phone — but because the endpoint is a URL this machine
	// will keep and keep using, and plain HTTP invites whoever is on the path to
	// point it somewhere else.
	if parsed.Scheme != "https" {
		return fmt.Errorf("%w: the endpoint is %q, want an https URL", ErrBadSubscription, endpoint)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%w: the endpoint names no host", ErrBadSubscription)
	}
	return nil
}

// decodeKey reads one of the browser's base64url values and checks its length.
//
// Both paddings are accepted because both are produced in the wild: the
// specification says unpadded, and enough code puts the padding back that
// refusing it would be refusing working phones over a formality.
func decodeKey(value string, want int, name string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(value, "="))
	if err != nil {
		return nil, fmt.Errorf("%w: %s is not base64url: %w", ErrBadSubscription, name, err)
	}
	if len(raw) != want {
		return nil, fmt.Errorf("%w: %s is %d bytes, want %d", ErrBadSubscription, name, len(raw), want)
	}
	return raw, nil
}
