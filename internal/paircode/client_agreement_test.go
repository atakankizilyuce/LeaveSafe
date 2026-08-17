package paircode

import "testing"

// Codes the Dart client holds verbatim, in its test/link/pair_code_test.dart.
//
// Two separate implementations of one encoding — this one and the client's
// decoder — can each be internally consistent and still disagree, and only
// fixtures that crossed the gap catch that. If this fails, the two ends have
// stopped agreeing and the application cannot read a code this machine shows.
func TestFixturesTheClientHolds(t *testing.T) {
	cases := []struct {
		host      string
		port      int
		key, want string
	}{
		{"192.168.1.24", 9443, "1111222233334448", "R2M0-2614-WC1Z-59MP-FG1B-0"},
		{"10.0.0.7", 9443, "1111222233334448", "1800-01S4-WC1Z-59MP-FG1B-0"},
		{"255.255.255.255", 65535, "9999999999999995", "ZZZZ-ZZZZ-ZWHR-DWKF-R3ZZ-P"},
	}
	for _, c := range cases {
		got, err := Encode(c.host, c.port, c.key)
		if err != nil {
			t.Fatalf("Encode(%s) = %v", c.host, err)
		}
		if got != c.want {
			t.Errorf("Encode(%s:%d, %s) = %q, want %q", c.host, c.port, c.key, got, c.want)
		}
	}
}
