//go:build !darwin

package bluetooth

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The BLE transport must not come up on a platform that cannot say which
// central sent a message.
//
// Everything above this package treats one connection as one device: a client
// pairs, and what it may do afterwards follows from that. Both of these
// platforms' stacks report a connection ID of zero for every write, so every
// central in radio range resolves to the same client — and the moment the
// owner's phone pairs, every other device nearby is authenticated too, with no
// key ever presented. Advertising anyway would offer that as a feature.
//
// If a future version of the Bluetooth library carries a real per-connection
// identity through to the write handler, this test is the thing to delete, and
// the backend can be restored from the history alongside it.
func TestStartRefusesWithoutCentralIdentity(t *testing.T) {
	srv := NewServer(nil)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := srv.Start(ctx)
	if err == nil {
		t.Fatal("the BLE server advertised on a platform that cannot identify centrals")
	}
	if !errors.Is(err, ErrNoCentralIdentity) {
		t.Errorf("Start returned %v, want ErrNoCentralIdentity", err)
	}
}

// The same reason has to reach anything that asks whether BLE can be offered,
// or the answer would contradict what Start does.
func TestAvailableIsFalseWithoutCentralIdentity(t *testing.T) {
	if Available() {
		t.Error("BLE reports itself available on a platform where Start refuses")
	}
}
