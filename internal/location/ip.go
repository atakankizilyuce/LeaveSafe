package location

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultIPLookupURL resolves the caller's own public address, so no IP has to
// be discovered first or sent anywhere.
const DefaultIPLookupURL = "https://ipapi.co/json/"

// ipAccuracyM is the error radius reported for every IP-derived fix.
//
// These databases map an address to a city or an ISP presence, not to a
// building. Two well-regarded services queried for the same address during
// development returned points 17 km apart, both labeled with a confident city
// name. 25 km is a radius that actually contains the machine most of the time;
// anything tighter would draw a circle the user would wrongly trust.
const ipAccuracyM = 25000

const ipLookupTimeout = 10 * time.Second

// IPProvider locates the machine by looking up its public IP address.
type IPProvider struct {
	url    string
	client *http.Client
}

// NewIPProvider creates a provider querying the given endpoint. An empty url
// selects DefaultIPLookupURL.
func NewIPProvider(url string) *IPProvider {
	if url == "" {
		url = DefaultIPLookupURL
	}
	return &IPProvider{
		url:    url,
		client: &http.Client{Timeout: ipLookupTimeout},
	}
}

// Source implements Provider.
func (p *IPProvider) Source() Source { return SourceIP }

// Available implements Provider. The lookup needs the internet, which cannot be
// established without trying, so this reports true whenever an endpoint is
// configured and lets a failed lookup speak for itself.
func (p *IPProvider) Available() bool { return p.url != "" }

// Locate implements Provider.
func (p *IPProvider) Locate(ctx context.Context) (*Fix, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url, nil)
	if err != nil {
		return nil, fmt.Errorf("build IP lookup request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	// Some of these services return an HTML landing page to clients that do not
	// identify themselves.
	req.Header.Set("User-Agent", "LeaveSafe")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("IP lookup: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("IP lookup returned %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("read IP lookup response: %w", err)
	}

	return parseIPLookup(body)
}

// ipLookupResponse covers the fields shared by the common providers. ipapi.co
// names the country `country_name` while ipwho.is uses `country`, so both are
// read and whichever is present wins.
type ipLookupResponse struct {
	Latitude    *float64 `json:"latitude"`
	Longitude   *float64 `json:"longitude"`
	City        string   `json:"city"`
	CountryName string   `json:"country_name"`
	Country     string   `json:"country"`
	Error       bool     `json:"error"`
	Reason      string   `json:"reason"`
	Message     string   `json:"message"`
}

// parseIPLookup turns a geolocation response into a fix. It is separated from
// the request so the shape can be tested against a response a real service
// produced; see testdata/PROVENANCE.md.
func parseIPLookup(body []byte) (*Fix, error) {
	var r ipLookupResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("decode IP lookup response: %w", err)
	}

	if r.Error {
		reason := r.Reason
		if reason == "" {
			reason = r.Message
		}
		if reason == "" {
			reason = "no reason given"
		}
		return nil, fmt.Errorf("IP lookup refused: %s", reason)
	}

	// A rate-limited or unroutable address comes back as a well-formed document
	// with the coordinates simply missing, which would otherwise land on
	// null island.
	if r.Latitude == nil || r.Longitude == nil {
		return nil, fmt.Errorf("IP lookup returned no coordinates")
	}

	return &Fix{
		Latitude:  *r.Latitude,
		Longitude: *r.Longitude,
		AccuracyM: ipAccuracyM,
		Source:    SourceIP,
		Timestamp: time.Now(),
		Label:     ipLabel(r),
	}, nil
}

func ipLabel(r ipLookupResponse) string {
	country := r.CountryName
	if country == "" {
		country = r.Country
	}

	parts := make([]string, 0, 2)
	if r.City != "" {
		parts = append(parts, r.City)
	}
	if country != "" {
		parts = append(parts, country)
	}
	return strings.Join(parts, ", ")
}
