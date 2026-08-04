package location

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// DefaultGeolocateURL is Google's Geolocation API. The request and response
// shapes below are that API's, which several other services also implement, so
// pointing geolocate_url elsewhere works without code changes.
const DefaultGeolocateURL = "https://www.googleapis.com/geolocation/v1/geolocate"

const geolocateTimeout = 15 * time.Second

// AccessPoint is one Wi-Fi radio observed by a scan.
type AccessPoint struct {
	// BSSID is the access point's MAC address in colon-separated form.
	BSSID string `json:"macAddress"`
	// SignalDBM is the received signal strength in dBm, a negative number.
	SignalDBM int `json:"signalStrength,omitempty"`
	// Channel is the Wi-Fi channel, zero when the scanner did not report one.
	Channel int `json:"channel,omitempty"`
}

type geolocateRequest struct {
	// ConsiderIP is always false. Falling back to the IP silently would return
	// a city-wide guess wearing the accuracy of a Wi-Fi fix, which is exactly
	// the kind of unearned precision this package exists to avoid. The IP
	// provider covers that case openly instead.
	ConsiderIP       bool          `json:"considerIp"`
	WiFiAccessPoints []AccessPoint `json:"wifiAccessPoints"`
}

type geolocateResponse struct {
	Location *struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	} `json:"location"`
	Accuracy float64 `json:"accuracy"`
	Error    *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// geolocateClient resolves observed access points into a position.
type geolocateClient struct {
	url    string
	apiKey string
	client *http.Client
}

func newGeolocateClient(endpoint, apiKey string) *geolocateClient {
	if endpoint == "" {
		endpoint = DefaultGeolocateURL
	}
	return &geolocateClient{
		url:    endpoint,
		apiKey: apiKey,
		client: &http.Client{Timeout: geolocateTimeout},
	}
}

// requestURL is the configured endpoint with the API key attached.
//
// Built through net/url rather than by concatenation. The key is a secret whose
// bytes this package does not choose — it arrives from the phone's settings
// screen or from a hand-edited config file — and pasting it after a "?" assumes
// two things that do not hold. It assumes the key needs no escaping, when a '#'
// in one silently truncates it and a '&' splits it into a second parameter that
// the endpoint will log rather than read; and it assumes the endpoint carries no
// query string of its own, when one that does gets a second '?' and the key
// becomes part of a value instead of a parameter. Either way the request goes
// out unauthenticated and the key goes somewhere it was not meant to.
func (c *geolocateClient) requestURL() (string, error) {
	if c.apiKey == "" {
		return c.url, nil
	}
	u, err := url.Parse(c.url)
	if err != nil {
		return "", fmt.Errorf("geolocation endpoint is not a URL: %w", err)
	}
	q := u.Query()
	q.Set("key", c.apiKey)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Resolve sends the access points to the geolocation service.
func (c *geolocateClient) Resolve(ctx context.Context, aps []AccessPoint) (*Fix, error) {
	if len(aps) == 0 {
		return nil, fmt.Errorf("no access points to resolve")
	}

	payload, err := json.Marshal(geolocateRequest{WiFiAccessPoints: aps})
	if err != nil {
		return nil, fmt.Errorf("encode geolocation request: %w", err)
	}

	endpoint, err := c.requestURL()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build geolocation request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("geolocation request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("read geolocation response: %w", err)
	}

	return parseGeolocateResponse(body, resp.StatusCode, len(aps))
}

// parseGeolocateResponse is separated from the request so it can be tested
// against recorded service output.
func parseGeolocateResponse(body []byte, statusCode, apCount int) (*Fix, error) {
	var r geolocateResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("decode geolocation response (HTTP %d): %w", statusCode, err)
	}

	if r.Error != nil {
		return nil, fmt.Errorf("geolocation service refused the request: %s (code %d)",
			r.Error.Message, r.Error.Code)
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("geolocation service returned HTTP %d", statusCode)
	}
	if r.Location == nil {
		return nil, fmt.Errorf("geolocation response carried no location")
	}
	// The endpoint is configurable, so this is not always the service the
	// default names — and even when it is, it is a third party answering a
	// question about where the owner's laptop is. Its answer is held to the same
	// range the phone's own is.
	if !ValidCoordinates(r.Location.Lat, r.Location.Lng) {
		return nil, fmt.Errorf("geolocation response carried coordinates outside the valid range")
	}

	// The service reports its own accuracy, and it is the only party that knows
	// how good the fix is. A missing value means we cannot make that claim, so
	// fall back to something deliberately wide rather than to zero, which would
	// render as pinpoint certainty.
	accuracy := r.Accuracy
	if accuracy <= 0 {
		accuracy = 500
	}

	return &Fix{
		Latitude:  r.Location.Lat,
		Longitude: r.Location.Lng,
		AccuracyM: accuracy,
		Source:    SourceWiFi,
		Timestamp: time.Now(),
		Label:     fmt.Sprintf("%d access points", apCount),
	}, nil
}
