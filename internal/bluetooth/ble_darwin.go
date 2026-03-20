//go:build darwin

package bluetooth

import (
	"context"

	log "github.com/sirupsen/logrus"
	"tinygo.org/x/bluetooth"
)

var (
	serviceUUID = mustParseUUID(ServiceUUIDString)
	txCharUUID  = mustParseUUID(TxCharUUIDString)
	rxCharUUID  = mustParseUUID(RxCharUUIDString)
)

func mustParseUUID(s string) bluetooth.UUID {
	u, err := bluetooth.ParseUUID(s)
	if err != nil {
		panic("invalid UUID: " + s)
	}
	return u
}

// Start begins BLE peripheral advertising and handles connections.
func (s *Server) Start(ctx context.Context) error {
	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return err
	}
	log.Info("BLE: adapter enabled")

	var txChar bluetooth.Characteristic

	err := adapter.AddService(&bluetooth.Service{
		UUID: serviceUUID,
		Characteristics: []bluetooth.CharacteristicConfig{
			{
				UUID:  txCharUUID,
				Flags: bluetooth.CharacteristicNotifyPermission | bluetooth.CharacteristicReadPermission,
				Handle: &txChar,
			},
			{
				UUID:  rxCharUUID,
				Flags: bluetooth.CharacteristicWritePermission | bluetooth.CharacteristicWriteWithoutResponsePermission,
				WriteEvent: func(client bluetooth.Connection, offset int, value []byte) {
					if len(value) == 0 {
						return
					}
					data := make([]byte, len(value))
					copy(data, value)
					s.handleIncoming(data)
				},
			},
		},
	})
	if err != nil {
		return err
	}

	transport := &BLETransport{
		sendFunc: func(data []byte) error {
			const maxChunk = 512
			for len(data) > 0 {
				chunk := data
				if len(chunk) > maxChunk {
					chunk = data[:maxChunk]
				}
				if _, err := txChar.Write(chunk); err != nil {
					return err
				}
				data = data[len(chunk):]
			}
			return nil
		},
	}

	s.mu.Lock()
	s.client = s.hub.RegisterExternalClient(transport)
	s.mu.Unlock()

	adv := adapter.DefaultAdvertisement()
	if err := adv.Configure(bluetooth.AdvertisementOptions{
		LocalName:    "LeaveSafe",
		ServiceUUIDs: []bluetooth.UUID{serviceUUID},
	}); err != nil {
		return err
	}
	if err := adv.Start(); err != nil {
		return err
	}
	log.Info("BLE: advertising started (LeaveSafe)")

	<-ctx.Done()
	_ = adv.Stop()
	s.disconnect()
	log.Info("BLE: server stopped")
	return nil
}

// Available checks if BLE is available on this system.
func Available() bool {
	adapter := bluetooth.DefaultAdapter
	err := adapter.Enable()
	return err == nil
}
