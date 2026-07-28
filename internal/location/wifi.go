package location

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"
)

// maxAccessPoints caps how many radios are sent for resolution. Geolocation
// services do not get more accurate past a couple of dozen, and every extra one
// is another neighbor's router in an outbound request.
const maxAccessPoints = 24

// WiFiProvider locates the machine by scanning nearby Wi-Fi radios and asking a
// geolocation service where that combination of radios is.
//
// This is the only source that is both precise and still correct after the
// machine has been carried somewhere. It is also the only one that costs money
// and talks to a third party, so it is off unless explicitly configured.
type WiFiProvider struct {
	geo     *geolocateClient
	scan    func(context.Context) ([]AccessPoint, error)
	enabled bool
}

// NewWiFiProvider creates a provider that scans with the platform scanner and
// resolves through the given endpoint. It reports itself unavailable unless an
// API key is configured, because the endpoints that accept one require it.
func NewWiFiProvider(url, apiKey string) *WiFiProvider {
	return &WiFiProvider{
		geo:     newGeolocateClient(url, apiKey),
		scan:    scanAccessPoints,
		enabled: apiKey != "",
	}
}

// Source implements Provider.
func (p *WiFiProvider) Source() Source { return SourceWiFi }

// Available implements Provider.
func (p *WiFiProvider) Available() bool {
	if !p.enabled {
		return false
	}
	if !wifiScanSupported() {
		log.Debugf("Wi-Fi location unavailable: %s", wifiScanUnsupportedReason())
		return false
	}
	return true
}

// Locate implements Provider.
func (p *WiFiProvider) Locate(ctx context.Context) (*Fix, error) {
	aps, err := p.scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("wi-fi scan: %w", err)
	}
	if len(aps) == 0 {
		return nil, fmt.Errorf("wi-fi scan found no access points")
	}
	if len(aps) > maxAccessPoints {
		aps = aps[:maxAccessPoints]
	}

	log.WithField("access_points", len(aps)).Info("Resolving position from Wi-Fi scan")
	return p.geo.Resolve(ctx, aps)
}
