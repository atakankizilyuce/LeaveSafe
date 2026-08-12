package push

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

// The whole of RFC 8291's worked example, section 5.
//
// This is the one test in this package that is not an opinion. Every value
// below is printed in the specification, and the body at the bottom is what a
// correct implementation produces from the ones above it. Encryption that is
// subtly wrong still produces convincing-looking ciphertext — it just arrives
// at a phone that silently discards it, which for an alarm is the same as
// having sent nothing, and is a failure nobody would ever see happen.
const (
	rfcPlaintext = "When I grow up, I want to be a watermelon"
	rfcSalt      = "DGv6ra1nlYgDCS1FRnbzlw"
	rfcAuth      = "BTBZMqHH6r4Tts7J_aSIgg"

	// The receiving phone. Only the public half ever travels.
	rfcReceiverPublic = "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"

	// The sending laptop, for this one message.
	rfcSenderPrivate = "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"

	rfcBody = "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27ml" +
		"mlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPT" +
		"pK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
)

func unb64(t *testing.T, s string) []byte {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}
	return raw
}

func TestTheEncryptionMatchesTheSpecificationsWorkedExample(t *testing.T) {
	sender, err := ecdh.P256().NewPrivateKey(unb64(t, rfcSenderPrivate))
	if err != nil {
		t.Fatalf("the specification's sending key: %v", err)
	}
	sub := Subscription{
		Endpoint: "https://push.example.net/x",
		Key:      unb64(t, rfcReceiverPublic),
		Auth:     unb64(t, rfcAuth),
	}

	got, err := encrypt([]byte(rfcPlaintext), sub, unb64(t, rfcSalt), sender)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if want := unb64(t, rfcBody); base64.RawURLEncoding.EncodeToString(got) !=
		base64.RawURLEncoding.EncodeToString(want) {
		t.Errorf("the encrypted body does not match the specification's\n got %s\nwant %s",
			base64.RawURLEncoding.EncodeToString(got), rfcBody)
	}
}

// The header travels in the clear because the phone needs all of it before it
// can decrypt anything: the salt to derive the keys, the sending public key to
// arrive at the same shared secret, and the record size to know where the
// message ends.
func TestTheHeaderCarriesWhatThePhoneNeedsToReadTheRest(t *testing.T) {
	sender, err := ecdh.P256().NewPrivateKey(unb64(t, rfcSenderPrivate))
	if err != nil {
		t.Fatalf("the specification's sending key: %v", err)
	}
	salt := unb64(t, rfcSalt)
	sub := Subscription{Key: unb64(t, rfcReceiverPublic), Auth: unb64(t, rfcAuth)}

	body, err := encrypt([]byte(rfcPlaintext), sub, salt, sender)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if got := body[:saltLen]; string(got) != string(salt) {
		t.Error("the salt is not at the front of the body")
	}
	if got := body[saltLen+4]; int(got) != publicKeyLen {
		t.Errorf("the key length octet is %d, want %d", got, publicKeyLen)
	}
	keyAt := saltLen + 4 + 1
	if got := body[keyAt : keyAt+publicKeyLen]; string(got) != string(sender.PublicKey().Bytes()) {
		t.Error("the sending public key is not in the header, so the phone cannot derive the secret")
	}
	// Everything after the header is the sealed record: the plaintext, one
	// delimiter octet, and GCM's tag.
	if want := keyAt + publicKeyLen + len(rfcPlaintext) + 1 + 16; len(body) != want {
		t.Errorf("the body is %d bytes, want %d", len(body), want)
	}
}

// A fresh key pair and a fresh salt for every message, so that two alarms to
// the same phone share no key material. Reusing either is how one recovered
// message becomes all of them.
func TestNoTwoMessagesShareTheirKeyMaterial(t *testing.T) {
	sub := Subscription{Key: unb64(t, rfcReceiverPublic), Auth: unb64(t, rfcAuth)}

	first, second := sealTwice(t, sub)

	if string(first[:saltLen]) == string(second[:saltLen]) {
		t.Error("two messages carry the same salt")
	}
	keyAt := saltLen + 4 + 1
	if string(first[keyAt:keyAt+publicKeyLen]) == string(second[keyAt:keyAt+publicKeyLen]) {
		t.Error("two messages carry the same sending key")
	}
}

func sealTwice(t *testing.T, sub Subscription) (first, second []byte) {
	t.Helper()
	for _, out := range []*[]byte{&first, &second} {
		salt := newSalt()
		sender, err := ecdh.P256().GenerateKey(cryptorand.Reader)
		if err != nil {
			t.Fatalf("sender key: %v", err)
		}
		body, err := encrypt([]byte("an alarm"), sub, salt, sender)
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		*out = body
	}
	return first, second
}

// A subscription whose key is not a point on the curve is refused rather than
// sent to. The keys arrive over the wire from a phone, so this is input.
func TestAKeyThatIsNotAPointIsRefused(t *testing.T) {
	sender, err := ecdh.P256().GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("sender key: %v", err)
	}
	sub := Subscription{Key: make([]byte, publicKeyLen), Auth: unb64(t, rfcAuth)}

	if _, err := encrypt([]byte("an alarm"), sub, make([]byte, saltLen), sender); err == nil {
		t.Error("sixty-five zero bytes were accepted as a public key")
	}
}

// And the phone can read it.
//
// Matching the specification's bytes says this agrees with the specification;
// this says a holder of the private half gets the message back out, with keys
// neither the specification nor this test chose in advance. Between them there
// is no room left for an encryption that looks right and arrives unreadable.
func TestThePhoneCanReadWhatWasSentToIt(t *testing.T) {
	phone, err := ecdh.P256().GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("the phone's key: %v", err)
	}
	auth := make([]byte, authLen)
	if _, err := cryptorand.Read(auth); err != nil {
		t.Fatalf("the phone's auth secret: %v", err)
	}
	sub := Subscription{Key: phone.PublicKey().Bytes(), Auth: auth}

	salt := newSalt()
	sender, err := ecdh.P256().GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("sender key: %v", err)
	}

	const message = "A door opened while the laptop was armed."
	body, err := encrypt([]byte(message), sub, salt, sender)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	got, err := openAsThePhoneWould(t, body, phone, auth)
	if err != nil {
		t.Fatalf("the phone could not read the message: %v", err)
	}
	if got != message {
		t.Errorf("the phone read %q, want %q", got, message)
	}
}

// openAsThePhoneWould undoes RFC 8291 the way a browser does, reading the
// sending key and the salt out of the header exactly as one would arrive.
func openAsThePhoneWould(t *testing.T, body []byte, phone *ecdh.PrivateKey, auth []byte) (string, error) {
	t.Helper()

	salt := body[:saltLen]
	keyAt := saltLen + 4 + 1
	senderPublic := body[keyAt : keyAt+publicKeyLen]
	sealed := body[keyAt+publicKeyLen:]

	sender, err := ecdh.P256().NewPublicKey(senderPublic)
	if err != nil {
		return "", err
	}
	shared, err := phone.ECDH(sender)
	if err != nil {
		return "", err
	}

	keyInfo := append(append([]byte(keyInfoPrefix), phone.PublicKey().Bytes()...), senderPublic...)
	ikm, err := hkdf.Key(sha256.New, shared, auth, string(keyInfo), sha256.Size)
	if err != nil {
		return "", err
	}
	key, err := hkdf.Key(sha256.New, ikm, salt, cekInfo, aes.BlockSize)
	if err != nil {
		return "", err
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, nonceInfo, gcmNonceLen)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	record, err := aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", err
	}
	// The last octet is the delimiter the sender appended.
	return string(record[:len(record)-1]), nil
}
