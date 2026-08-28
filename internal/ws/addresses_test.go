package ws

import (
	"encoding/json"
	"strings"
	"testing"
)

// Where a phone can actually reach this machine is something only this daemon
// knows.
//
// The desktop application starts it and reads endpoint.json for the port, but
// the address it has for its own machine is 127.0.0.1 — which is correct, and
// which no phone can dial. Working out the rest means knowing which interfaces
// are up, which are loopback, and which are the Docker, WSL and Hyper-V bridges
// that would send a camera to a network it is not on. That reasoning is already
// written once, in internal/server; writing it a second time in another
// language is how the two of them start disagreeing.
//
// So the daemon says. These tests hold it to the three things that matter: it
// says it to a client that proved it holds the pairing key, it says nothing to
// one that has not, and a build with nobody to ask carries on exactly as it did
// before.

func TestAuthOKCarriesWhereAPhoneCanReachThis(t *testing.T) {
	hub := hubWithKey(t, fixedKey)
	hub.SetAddresses(func() []string {
		return []string{"http://192.168.1.16:53680", "http://10.0.0.4:53680"}
	})

	rec := &recorder{}
	client := hub.RegisterExternalClient(rec, nil)
	client.serverNonce = fixedServerNonce

	hub.handleMessage(client, ClientMessage{
		Type:  MsgTypeAuth,
		Nonce: fixedClientNonce,
		Proof: fixedClientProof,
	})

	authOK, ok := rec.saw(MsgTypeAuthOK)
	if !ok {
		t.Fatal("the worked example did not pair")
	}
	want := []string{"http://192.168.1.16:53680", "http://10.0.0.4:53680"}
	if strings.Join(authOK.Addresses, ",") != strings.Join(want, ",") {
		t.Errorf("auth_ok carried %v, want %v", authOK.Addresses, want)
	}
}

func TestTheGreetingSaysNothingAboutAddresses(t *testing.T) {
	// The greeting goes to anything that opens a socket, before it has proved
	// anything. What addresses this machine answers on is a map of somebody's
	// network, and it is not owed to a stranger who can reach one of them.
	hub := hubWithKey(t, fixedKey)
	hub.SetAddresses(func() []string { return []string{"http://192.168.1.16:53680"} })

	_, rec, _ := greeted(t, hub)

	hello, ok := rec.saw(MsgTypeHello)
	if !ok {
		t.Fatal("no greeting")
	}
	if len(hello.Addresses) != 0 {
		t.Errorf("the greeting told an unauthenticated client %v", hello.Addresses)
	}
}

func TestAHubWithNobodyToAskCarriesOnWithoutAddresses(t *testing.T) {
	// Every hub in every other test, and every build that has not been wired
	// up yet. Nothing is set, nothing is sent, and nothing fails.
	hub := hubWithKey(t, fixedKey)

	rec := &recorder{}
	client := hub.RegisterExternalClient(rec, nil)
	client.serverNonce = fixedServerNonce

	hub.handleMessage(client, ClientMessage{
		Type:  MsgTypeAuth,
		Nonce: fixedClientNonce,
		Proof: fixedClientProof,
	})

	authOK, ok := rec.saw(MsgTypeAuthOK)
	if !ok {
		t.Fatal("the worked example did not pair")
	}
	if len(authOK.Addresses) != 0 {
		t.Errorf("auth_ok carried %v with no source set", authOK.Addresses)
	}
}

func TestAnEmptyAddressListIsNotSentAsAField(t *testing.T) {
	// An older application reads this message too. A field that is present and
	// empty is a field it has to have an opinion about; omitted, the message is
	// byte for byte the one it has always been handed.
	data, err := json.Marshal(ServerMessage{Type: MsgTypeAuthOK})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "addresses") {
		t.Errorf("an auth_ok with no addresses came out as %s", data)
	}
}
