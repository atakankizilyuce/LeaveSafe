//go:build darwin

package bluetooth

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/tinygo-org/cbgo"
)

var (
	serviceUUID = cbgo.MustParseUUID(ServiceUUIDString)
	txCharUUID  = cbgo.MustParseUUID(TxCharUUIDString)
	rxCharUUID  = cbgo.MustParseUUID(RxCharUUIDString)
)

// darwinDelegate implements cbgo.PeripheralManagerDelegate for BLE peripheral mode.
type darwinDelegate struct {
	cbgo.PeripheralManagerDelegateBase

	server  *Server
	pm      cbgo.PeripheralManager
	txChar  cbgo.MutableCharacteristic
	ready   chan struct{}
	svcDone chan struct{}

	mu       sync.Mutex
	centrals map[string]cbgo.Central // keyed by Identifier()
}

func (d *darwinDelegate) PeripheralManagerDidUpdateState(pm cbgo.PeripheralManager) {
	if pm.State() == cbgo.ManagerStatePoweredOn {
		select {
		case <-d.ready:
		default:
			close(d.ready)
		}
	}
}

func (d *darwinDelegate) DidAddService(pm cbgo.PeripheralManager, svc cbgo.Service, err error) {
	if err != nil {
		log.Errorf("BLE: failed to add service: %v", err)
	}
	select {
	case <-d.svcDone:
	default:
		close(d.svcDone)
	}
}

func (d *darwinDelegate) DidStartAdvertising(pm cbgo.PeripheralManager, err error) {
	if err != nil {
		log.Errorf("BLE: advertising error: %v", err)
	} else {
		log.Info("BLE: advertising started (LeaveSafe)")
	}
}

func (d *darwinDelegate) CentralDidSubscribe(pm cbgo.PeripheralManager, cent cbgo.Central, chr cbgo.Characteristic) {
	id := cent.Identifier().String()
	d.mu.Lock()
	d.centrals[id] = cent
	d.mu.Unlock()
	log.WithField("central", id).Info("BLE: central subscribed")
}

func (d *darwinDelegate) CentralDidUnsubscribe(pm cbgo.PeripheralManager, cent cbgo.Central, chr cbgo.Characteristic) {
	id := cent.Identifier().String()
	d.mu.Lock()
	delete(d.centrals, id)
	d.mu.Unlock()
	d.server.removeClient(id)
	log.WithField("central", id).Info("BLE: central unsubscribed")
}

func (d *darwinDelegate) DidReceiveReadRequest(pm cbgo.PeripheralManager, req cbgo.ATTRequest) {
	pm.RespondToRequest(req, cbgo.ATTErrorSuccess)
}

func (d *darwinDelegate) DidReceiveWriteRequests(pm cbgo.PeripheralManager, reqs []cbgo.ATTRequest) {
	for _, req := range reqs {
		if bytes.Equal(req.Characteristic().UUID(), rxCharUUID) {
			value := req.Value()
			if len(value) > 0 {
				data := make([]byte, len(value))
				copy(data, value)
				centralID := req.Central().Identifier().String()
				go d.server.handleIncoming(centralID, data, func() *BLETransport {
					return d.newTransportFor(centralID)
				})
			}
		}
		pm.RespondToRequest(req, cbgo.ATTErrorSuccess)
	}
}

// newTransportFor creates a BLETransport that sends notifications only to
// the specified central.
func (d *darwinDelegate) newTransportFor(centralID string) *BLETransport {
	return &BLETransport{
		sendFunc: func(data []byte) error {
			const maxChunk = 182 // macOS BLE safe MTU
			d.mu.Lock()
			cent, ok := d.centrals[centralID]
			d.mu.Unlock()
			if !ok {
				return fmt.Errorf("central %s not connected", centralID)
			}
			for len(data) > 0 {
				chunk := data
				if len(chunk) > maxChunk {
					chunk = data[:maxChunk]
				}
				d.pm.UpdateValue(chunk, d.txChar.Characteristic(), []cbgo.Central{cent})
				data = data[len(chunk):]
			}
			return nil
		},
	}
}

// Start begins BLE peripheral advertising and handles connections.
func (s *Server) Start(ctx context.Context) error {
	dlg := &darwinDelegate{
		server:   s,
		ready:    make(chan struct{}),
		svcDone:  make(chan struct{}),
		centrals: make(map[string]cbgo.Central),
	}

	pm := cbgo.NewPeripheralManager(&cbgo.ManagerOpts{})
	pm.SetDelegate(dlg)
	dlg.pm = pm

	// Wait for adapter to power on.
	select {
	case <-dlg.ready:
	case <-time.After(10 * time.Second):
		return errTimeout("BLE adapter did not power on")
	case <-ctx.Done():
		return ctx.Err()
	}
	log.Info("BLE: adapter enabled")

	// Create TX characteristic (notify/read).
	txChar := cbgo.NewMutableCharacteristic(
		txCharUUID,
		cbgo.CharacteristicPropertyNotify|cbgo.CharacteristicPropertyRead,
		nil,
		cbgo.AttributePermissionsReadable,
	)
	dlg.txChar = txChar

	// Create RX characteristic (write).
	rxChar := cbgo.NewMutableCharacteristic(
		rxCharUUID,
		cbgo.CharacteristicPropertyWrite|cbgo.CharacteristicPropertyWriteWithoutResponse,
		nil,
		cbgo.AttributePermissionsWriteable,
	)

	// Create and add service.
	svc := cbgo.NewMutableService(serviceUUID, true)
	svc.SetCharacteristics([]cbgo.MutableCharacteristic{txChar, rxChar})
	pm.AddService(svc)

	select {
	case <-dlg.svcDone:
	case <-time.After(5 * time.Second):
		return errTimeout("BLE service registration timed out")
	case <-ctx.Done():
		return ctx.Err()
	}

	// Start advertising.
	pm.StartAdvertising(cbgo.AdvData{
		LocalName:    "LeaveSafe",
		ServiceUUIDs: []cbgo.UUID{serviceUUID},
	})

	<-ctx.Done()
	pm.StopAdvertising()
	pm.RemoveAllServices()
	s.disconnectAll()
	log.Info("BLE: server stopped")
	return nil
}

// Available checks if BLE peripheral mode is available on this system.
func Available() bool {
	ready := make(chan struct{})
	dlg := &availCheckDelegate{ready: ready}
	pm := cbgo.NewPeripheralManager(&cbgo.ManagerOpts{})
	pm.SetDelegate(dlg)
	select {
	case <-ready:
		return pm.State() == cbgo.ManagerStatePoweredOn
	case <-time.After(3 * time.Second):
		return false
	}
}

type availCheckDelegate struct {
	cbgo.PeripheralManagerDelegateBase
	ready chan struct{}
}

func (d *availCheckDelegate) PeripheralManagerDidUpdateState(_ cbgo.PeripheralManager) {
	select {
	case <-d.ready:
	default:
		close(d.ready)
	}
}

type bleTimeoutError string

func errTimeout(msg string) bleTimeoutError { return bleTimeoutError(msg) }
func (e bleTimeoutError) Error() string     { return string(e) }
