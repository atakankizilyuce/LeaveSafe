package location

import "testing"

// The API key is a secret whose bytes this package does not choose. Pasting it
// after a "?" assumed it needed no escaping and that the endpoint carried no
// query string of its own; neither holds, and both send the key somewhere it
// was not meant to go.
func TestTheAPIKeyIsAttachedAsAParameter(t *testing.T) {
	cases := []struct {
		endpoint, key, want string
	}{
		{"https://example.test/geolocate", "plain", "https://example.test/geolocate?key=plain"},
		{"https://example.test/geolocate", "a&b=c", "https://example.test/geolocate?key=a%26b%3Dc"},
		{"https://example.test/geolocate", "a#b", "https://example.test/geolocate?key=a%23b"},
		{"https://example.test/g?v=2", "plain", "https://example.test/g?key=plain&v=2"},
	}
	for _, c := range cases {
		got, err := newGeolocateClient(c.endpoint, c.key).requestURL()
		if err != nil {
			t.Errorf("requestURL for %q: %v", c.endpoint, err)
			continue
		}
		if got != c.want {
			t.Errorf("endpoint %q with key %q became %q, want %q", c.endpoint, c.key, got, c.want)
		}
	}
}

// With no key configured the endpoint is used exactly as it stands, which is
// what an endpoint that authenticates some other way needs.
func TestNoKeyMeansNoParameter(t *testing.T) {
	got, err := newGeolocateClient("https://example.test/geolocate", "").requestURL()
	if err != nil {
		t.Fatalf("requestURL: %v", err)
	}
	if got != "https://example.test/geolocate" {
		t.Errorf("endpoint became %q with no key set", got)
	}
}
