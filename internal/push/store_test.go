package push

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func storeIn(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "push-subscriptions.json")
	return OpenStore(path), path
}

// The whole reason this is a file and not a map. A subscription is handed over
// while the phone is on the same network as the laptop — during pairing — and
// it has to survive until the day it is needed, which is a day the phone is
// somewhere else entirely.
func TestAPhoneIsStillKnownAfterARestart(t *testing.T) {
	store, path := storeIn(t)
	sub := Subscription{Endpoint: "https://push.example.net/a", Key: []byte("k"), Auth: []byte("a")}

	if err := store.Add(sub); err != nil {
		t.Fatalf("Add: %v", err)
	}

	reopened := OpenStore(path)
	all := reopened.All()
	if len(all) != 1 {
		t.Fatalf("%d subscriptions after reopening, want 1", len(all))
	}
	if all[0].Endpoint != sub.Endpoint || string(all[0].Key) != "k" || string(all[0].Auth) != "a" {
		t.Errorf("the subscription came back as %+v, want %+v", all[0], sub)
	}
}

// The same phone subscribing again is the same phone, not a second one to tell
// twice. A reinstall produces a new endpoint and is genuinely a different one.
func TestTheSamePhoneDoesNotAccumulate(t *testing.T) {
	store, _ := storeIn(t)
	const endpoint = "https://push.example.net/a"

	_ = store.Add(Subscription{Endpoint: endpoint, Key: []byte("first")})
	_ = store.Add(Subscription{Endpoint: endpoint, Key: []byte("second")})
	_ = store.Add(Subscription{Endpoint: "https://push.example.net/b", Key: []byte("other")})

	if got := store.Count(); got != 2 {
		t.Errorf("Count = %d, want 2", got)
	}
	for _, sub := range store.All() {
		if sub.Endpoint == endpoint && string(sub.Key) != "second" {
			t.Error("the older subscription for that phone survived the newer one")
		}
	}
}

func TestForgettingAPhoneSticks(t *testing.T) {
	store, path := storeIn(t)
	_ = store.Add(Subscription{Endpoint: "https://push.example.net/a"})

	if err := store.Remove("https://push.example.net/a"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := store.Remove("https://push.example.net/a"); err != nil {
		t.Errorf("removing a phone twice: %v", err)
	}

	if got := OpenStore(path).Count(); got != 0 {
		t.Errorf("%d subscriptions after reopening, want none", got)
	}
}

// A laptop that will not start because of a corrupt cache file is worse than a
// laptop with one fewer phone to tell: the phones subscribe again the next time
// they are on the same network.
func TestARuinedFileStartsAnEmptyStoreRatherThanRefusing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "push-subscriptions.json")
	if err := os.WriteFile(path, []byte("{not json at all"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	store := OpenStore(path)

	if got := store.Count(); got != 0 {
		t.Errorf("Count = %d, want none", got)
	}
	// And it is usable afterwards rather than stuck.
	if err := store.Add(Subscription{Endpoint: "https://push.example.net/a"}); err != nil {
		t.Errorf("Add after a ruined file: %v", err)
	}
}

func TestAStoreThatHasNeverBeenWrittenIsEmpty(t *testing.T) {
	store, _ := storeIn(t)

	if got := store.Count(); got != 0 {
		t.Errorf("Count = %d, want none", got)
	}
	if got := store.All(); len(got) != 0 {
		t.Errorf("All = %v, want none", got)
	}
}

// Each stored line is a URL that will wake somebody's phone, so the file is not
// left readable by every account on the machine.
func TestTheStoredSubscriptionsAreOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits do not describe what Windows enforces")
	}
	store, path := storeIn(t)

	if err := store.Add(Subscription{Endpoint: "https://push.example.net/a"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != storeMode {
		t.Errorf("mode is %o, want %o", got, storeMode)
	}
}

// A store it cannot write to reports rather than pretending. The alarm still
// sounds; what is lost is telling this phone about the next one.
func TestAStoreThatCannotBeWrittenSaysSo(t *testing.T) {
	dir := t.TempDir()
	// A directory where the file should be: writing over it fails on every
	// platform, without depending on permission bits Windows does not share.
	path := filepath.Join(dir, "push-subscriptions.json")
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	err := OpenStore(path).Add(Subscription{Endpoint: "https://push.example.net/a"})

	if err == nil {
		t.Fatal("a store that could not be written reported success")
	}
}

// ---- what the phone hands over ---------------------------------------------

func TestASubscriptionFromABrowserIsAccepted(t *testing.T) {
	key := strings.Repeat("A", 87) // 65 bytes as base64url
	auth := strings.Repeat("B", 22)

	sub, err := ParseSubscription("https://fcm.googleapis.com/fcm/send/abc", key, auth)
	if err != nil {
		t.Fatalf("ParseSubscription: %v", err)
	}
	if len(sub.Key) != publicKeyLen {
		t.Errorf("the key is %d bytes, want %d", len(sub.Key), publicKeyLen)
	}
	if len(sub.Auth) != authLen {
		t.Errorf("the auth secret is %d bytes, want %d", len(sub.Auth), authLen)
	}
}

// The specification says unpadded and enough code puts the padding back that
// refusing it would be refusing working phones over a formality.
func TestPaddingOnTheKeysIsAccepted(t *testing.T) {
	key := strings.Repeat("A", 87) + "="
	auth := strings.Repeat("B", 22) + "=="

	if _, err := ParseSubscription("https://push.example.net/a", key, auth); err != nil {
		t.Errorf("ParseSubscription with padding: %v", err)
	}
}

// All of this arrives over the wire from a phone, and the endpoint is a URL this
// laptop will keep making outbound requests to for as long as the phone stays
// paired.
func TestAnUnusableSubscriptionIsRefused(t *testing.T) {
	goodKey := strings.Repeat("A", 87)
	goodAuth := strings.Repeat("B", 22)

	cases := map[string][3]string{
		"plain http":        {"http://push.example.net/a", goodKey, goodAuth},
		"not a URL":         {"://nope", goodKey, goodAuth},
		"no host":           {"https:///a", goodKey, goodAuth},
		"key is not base64": {"https://push.example.net/a", "!!!!", goodAuth},
		"key is too short":  {"https://push.example.net/a", strings.Repeat("A", 20), goodAuth},
		"auth is too short": {"https://push.example.net/a", goodKey, strings.Repeat("B", 6)},
		"auth is not b64":   {"https://push.example.net/a", goodKey, "!!!!"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseSubscription(args[0], args[1], args[2])
			if !errors.Is(err, ErrBadSubscription) {
				t.Errorf("err = %v, want it refused as unusable", err)
			}
		})
	}
}

// ---- the signing key -------------------------------------------------------

// The key is kept for the life of the installation: every phone subscribes
// under its public half and the browser then refuses any message not signed by
// the private half. A laptop that generated a new one every start would have
// phones that accept nothing.
func TestTheSigningKeyIsKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "push-key")

	first, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatalf("LoadOrCreateIdentity: %v", err)
	}
	second, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatalf("LoadOrCreateIdentity again: %v", err)
	}

	if first.PublicKey() != second.PublicKey() {
		t.Error("a second start produced a different key, so every subscribed phone stops listening")
	}
	// The uncompressed point, which is the only form a browser accepts as an
	// application server key.
	if got, err := base64.RawURLEncoding.DecodeString(first.PublicKey()); err != nil {
		t.Errorf("the public key is not base64url: %v", err)
	} else if len(got) != publicKeyLen {
		t.Errorf("the public key is %d bytes, want %d", len(got), publicKeyLen)
	}
}

// Anything that can read this key can send this phone an alarm.
func TestTheSigningKeyIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits do not describe what Windows enforces")
	}
	path := filepath.Join(t.TempDir(), "push-key")

	if _, err := LoadOrCreateIdentity(path); err != nil {
		t.Fatalf("LoadOrCreateIdentity: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != keyFileMode {
		t.Errorf("mode is %o, want %o", got, keyFileMode)
	}
}

// A key file that cannot be read is replaced rather than refused. The
// alternative is a laptop that sends no alarms until somebody deletes a file,
// and nobody is at the laptop when that matters.
func TestARuinedKeyIsReplacedRatherThanRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "push-key")
	if err := os.WriteFile(path, []byte("not a key"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	id, err := LoadOrCreateIdentity(path)

	if err != nil {
		t.Fatalf("LoadOrCreateIdentity: %v", err)
	}
	if id.PublicKey() == "" {
		t.Error("no usable key came back")
	}
}

// A path that cannot be written to is a different thing from a ruined file, and
// there is nothing sensible to do but say so.
func TestAKeyThatCannotBeWrittenSaysSo(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "push-key"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if _, err := LoadOrCreateIdentity(filepath.Join(dir, "push-key")); err == nil {
		t.Error("a key that could not be written reported success")
	}
}

// Both files live beside the rest of the configuration rather than wherever the
// process happened to start.
func TestBothFilesLiveWithTheConfiguration(t *testing.T) {
	const dir = "/etc/leavesafe"

	if got := StorePath(dir); filepath.Dir(got) != filepath.Clean(dir) {
		t.Errorf("StorePath = %q, want it inside %q", got, dir)
	}
	if got := IdentityPath(dir); filepath.Dir(got) != filepath.Clean(dir) {
		t.Errorf("IdentityPath = %q, want it inside %q", got, dir)
	}
}
