package paircode

import (
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		host string
		port int
		key  string
	}{
		{"an ordinary home address", "192.168.1.24", 9443, "1111222233334448"},
		{"the lowest address and port", "0.0.0.0", 1, "0000000000000000"},
		{"the highest of both", "255.255.255.255", 65535, "9999999999999999"},
		{"a key with leading zeros", "10.0.0.7", 9443, "0000111122223333"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, err := Encode(c.host, c.port, c.key)
			if err != nil {
				t.Fatalf("Encode(%q, %d, %q) = %v", c.host, c.port, c.key, err)
			}

			host, port, key, err := Decode(code)
			if err != nil {
				t.Fatalf("Decode(%q) = %v", code, err)
			}
			if host != c.host || port != c.port || key != c.key {
				t.Errorf("round trip gave %s:%d %s, want %s:%d %s",
					host, port, key, c.host, c.port, c.key)
			}
		})
	}
}

// A code is read off one screen and typed into another, which is the whole
// reason it exists. Twenty-one characters is what thirteen bytes take in
// base32; if a change makes it longer, the change is the problem.
func TestCodeIsShortEnoughToType(t *testing.T) {
	code, err := Encode("192.168.1.24", 9443, "1111222233334448")
	if err != nil {
		t.Fatal(err)
	}

	if got := len(strings.ReplaceAll(code, "-", "")); got != 21 {
		t.Errorf("code has %d characters, want 21: %q", got, code)
	}
}

func TestGroupedForReading(t *testing.T) {
	code, err := Encode("192.168.1.24", 9443, "1111222233334448")
	if err != nil {
		t.Fatal(err)
	}

	// Four at a time, the way the pairing key itself is grouped. A run of
	// twenty-one characters is a run nobody keeps their place in.
	for _, group := range strings.Split(code, "-")[:5] {
		if len(group) != 4 {
			t.Errorf("group %q is not four characters: %q", group, code)
		}
	}
}

// Crockford's alphabet is the point of choosing it: the characters people
// confuse when copying are not in it at all.
func TestAmbiguousCharactersAreNotUsed(t *testing.T) {
	code, err := Encode("192.168.1.24", 9443, "1111222233334448")
	if err != nil {
		t.Fatal(err)
	}

	for _, bad := range []string{"I", "L", "O", "U"} {
		if strings.Contains(code, bad) {
			t.Errorf("code contains %q, which Crockford excludes: %q", bad, code)
		}
	}
}

func TestTranscriptionSlipsAreForgiven(t *testing.T) {
	code, err := Encode("192.168.1.24", 9443, "1111222233334448")
	if err != nil {
		t.Fatal(err)
	}

	// What a person actually types: lower case, the letters they think they
	// see, and the dashes left out or put in the wrong places.
	slips := []struct {
		name  string
		typed string
	}{
		{"lower case", strings.ToLower(code)},
		{"no dashes", strings.ReplaceAll(code, "-", "")},
		{"spaces instead of dashes", strings.ReplaceAll(code, "-", " ")},
		{"O typed for zero", strings.ReplaceAll(code, "0", "O")},
		{"I typed for one", strings.ReplaceAll(code, "1", "I")},
		{"l typed for one", strings.ReplaceAll(code, "1", "l")},
	}

	for _, s := range slips {
		t.Run(s.name, func(t *testing.T) {
			host, port, key, err := Decode(s.typed)
			if err != nil {
				t.Fatalf("Decode(%q) = %v", s.typed, err)
			}
			if host != "192.168.1.24" || port != 9443 || key != "1111222233334448" {
				t.Errorf("got %s:%d %s, want the original", host, port, key)
			}
		})
	}
}

func TestRefusesWhatIsNotACode(t *testing.T) {
	cases := []struct {
		name  string
		typed string
	}{
		{"empty", ""},
		{"too short", "K7M2-QX8P"},
		{"too long", "K7M2-QX8P-4TRW-9B3C-XY5Z-QQQQ"},
		{"a character outside the alphabet", "K7M2-QX8P-4TRW-9B3C-XY5$-Q"},
		{"the pairing key on its own", "1111-2222-3333-4448"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, _, _, err := Decode(c.typed); err == nil {
				t.Errorf("Decode(%q) was accepted", c.typed)
			}
		})
	}
}

func TestRefusesWhatCannotBeEncoded(t *testing.T) {
	cases := []struct {
		name string
		host string
		port int
		key  string
	}{
		{"an IPv6 address", "fe80::1", 9443, "1111222233334448"},
		{"a host name", "laptop.local", 9443, "1111222233334448"},
		{"a port out of range", "10.0.0.7", 70000, "1111222233334448"},
		{"a key that is not sixteen digits", "10.0.0.7", 9443, "111122223333444"},
		{"a key with a letter in it", "10.0.0.7", 9443, "111122223333444a"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Encode(c.host, c.port, c.key); err == nil {
				t.Errorf("Encode(%q, %d, %q) was accepted", c.host, c.port, c.key)
			}
		})
	}
}

// The seven key bytes hold sixteen digits and no more.
//
// Nothing this program issues can reach the bound — a sixteen-digit key stops
// at 9999999999999999 — which is the reason to have the check and a test for
// it rather than a comment. Without them the only thing between a longer key
// and its top bits falling off is a length check somewhere else in the file,
// and a code built from a truncated key is a working code for a *different*
// key that nothing would report.
func TestAKeyTooLargeForItsBytesIsRefused(t *testing.T) {
	// Seventeen digits: the right shape, past the bound.
	if _, err := Encode("10.0.0.7", 9443, "99999999999999999"); err == nil {
		t.Error("a key too large to carry was encoded anyway")
	}
}

func TestEveryKeySixteenDigitsCanSpellSurvivesTheRoundTrip(t *testing.T) {
	// The two ends of the range and the value one below the bound, which is
	// where a truncation would first show.
	for _, key := range []string{
		"0000000000000000",
		"9999999999999999",
		"9999999999999998",
		"1000000000000000",
	} {
		code, err := Encode("10.0.0.7", 9443, key)
		if err != nil {
			t.Fatalf("Encode(%q) = %v", key, err)
		}
		_, _, got, err := Decode(code)
		if err != nil {
			t.Fatalf("Decode(%q) = %v", code, err)
		}
		if got != key {
			t.Errorf("round trip of %q gave %q", key, got)
		}
	}
}

// Two machines on the same network differ by one octet, and two codes that
// looked alike would be two codes somebody pairs the wrong one with.
func TestNeighbouringAddressesLookDifferent(t *testing.T) {
	a, err := Encode("192.168.1.24", 9443, "1111222233334448")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encode("192.168.1.25", 9443, "1111222233334448")
	if err != nil {
		t.Fatal(err)
	}

	if a == b {
		t.Fatalf("two addresses produced the same code: %q", a)
	}
}
