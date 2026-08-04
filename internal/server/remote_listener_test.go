package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// testCert generates a throwaway certificate through the same path production
// uses, so the test exercises the real loader rather than a hand-rolled one.
func testCert(t *testing.T) (tls.Certificate, string) {
	t.Helper()
	cert, fp, err := GenerateOrLoadCert(t.TempDir())
	if err != nil {
		t.Fatalf("generate cert: %v", err)
	}
	return cert, fp
}

// This is the central claim of the whole design: turning remote access on and
// off must not touch the local listener. A phone connected over Wi-Fi is what
// flips the switch, and if flipping it kills that phone's own socket then
// "no restart required" has bought the user nothing.
func TestLocalWebSocketSurvivesTheRemoteListenerComingAndGoing(t *testing.T) {
	srv := startTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, fmt.Sprintf("ws://127.0.0.1:%d/ws", srv.Port()), nil) //nolint:bodyclose // closed below when non-nil
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial local socket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// The hub greets every socket before asking for anything. Reading it proves
	// the connection is live before anything else happens to the server.
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("greeting before remote listener started: %v", err)
	}

	cert, fp := testCert(t)
	remotePort, err := srv.StartRemote(cert, fp, 0)
	if err != nil {
		t.Fatalf("start remote listener: %v", err)
	}
	if remotePort == 0 {
		t.Fatal("StartRemote returned port 0, want the bound port")
	}
	if srv.RemoteCertFP() != fp {
		t.Errorf("RemoteCertFP() = %q, want %q", srv.RemoteCertFP(), fp)
	}

	srv.StopRemote()

	if srv.RemotePort() != 0 {
		t.Errorf("RemotePort() = %d after StopRemote, want 0", srv.RemotePort())
	}

	// The socket opened before any of that must still carry traffic.
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`)); err != nil {
		t.Fatalf("local socket died while the remote listener came and went: %v", err)
	}
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("no answer on the local socket afterwards: %v", err)
	}
}

// The remote listener is the one exposed to the internet. A plain-HTTP request
// to it must not be served — that is the whole reason the design refuses to
// publish a port without a certificate.
//
// The connection is not simply dropped: net/http notices a plaintext request on
// a TLS port and answers it in cleartext with a 400 saying so. That is a
// courtesy to a person who typed http:// by hand, and it is fine — what matters
// is that no page and no socket is on the other side of it. So the assertion is
// on what came back, not on whether the call returned an error.
func TestRemoteListenerRefusesPlainHTTP(t *testing.T) {
	srv := startTestServer(t)

	cert, fp := testCert(t)
	port, err := srv.StartRemote(cert, fp, 0)
	if err != nil {
		t.Fatalf("start remote listener: %v", err)
	}
	defer srv.StopRemote()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("http://127.0.0.1:%d/", port), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// A refused or reset connection is an equally correct refusal.
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Fatalf("plain HTTP was served on the remote listener: got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	if err != nil {
		t.Fatalf("read refusal body: %v", err)
	}
	if !strings.Contains(string(body), "HTTP request to an HTTPS server") {
		t.Errorf("the refusal did not name the reason, got %d: %q", resp.StatusCode, body)
	}
}

// Both listeners serve the same application. A phone that pairs over the public
// URL has to reach the same hub as one on the LAN, or remote access would be a
// second, empty copy of the app.
func TestBothListenersServeTheSameApplication(t *testing.T) {
	srv := startTestServer(t)

	cert, fp := testCert(t)
	port, err := srv.StartRemote(cert, fp, 0)
	if err != nil {
		t.Fatalf("start remote listener: %v", err)
	}
	defer srv.StopRemote()

	// The certificate is self-signed, but it is not unknown: this test generated
	// it. Trusting exactly that one and verifying against it is both closer to
	// what the phone is asked to do and a stronger assertion than skipping
	// verification — it proves the listener serves the certificate it was
	// handed, and that the SANs cover the address being dialed.
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse generated certificate: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)

	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		},
	}
	defer client.CloseIdleConnections()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("https://127.0.0.1:%d/", port), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("remote listener did not serve the UI: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("remote listener returned %d, want 200", resp.StatusCode)
	}
	// HSTS is only sent by the TLS listener, so its presence here is what says
	// the handler chain was built for this listener rather than borrowed from
	// the plain-HTTP one.
	if got := resp.Header.Get("Strict-Transport-Security"); got == "" {
		t.Error("remote listener served no HSTS header, but it is the TLS one")
	}
}

// Enable is driven by three callers that do not coordinate — startup, the
// phone's settings screen and the console command — so starting an already
// running listener must be a no-op rather than a second bind.
func TestStartRemoteTwiceKeepsOneListener(t *testing.T) {
	srv := startTestServer(t)

	cert, fp := testCert(t)
	first, err := srv.StartRemote(cert, fp, 0)
	if err != nil {
		t.Fatalf("first StartRemote: %v", err)
	}
	second, err := srv.StartRemote(cert, fp, 0)
	if err != nil {
		t.Fatalf("second StartRemote: %v", err)
	}
	defer srv.StopRemote()

	if first != second {
		t.Errorf("second StartRemote bound a different port (%d then %d)", first, second)
	}
}

// StopRemote is called on shutdown and on every toggle, including when nothing
// is running.
func TestStopRemoteWithoutStartIsSafe(t *testing.T) {
	srv := startTestServer(t)

	srv.StopRemote()
	srv.StopRemote()

	if srv.RemotePort() != 0 {
		t.Errorf("RemotePort() = %d, want 0", srv.RemotePort())
	}
}
