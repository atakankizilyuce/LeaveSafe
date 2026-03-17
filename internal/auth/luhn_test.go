package auth

import "testing"

func TestLuhnCheckDigit(t *testing.T) {
	// Generate a key and verify the check digit makes it valid
	digits := "123456789012345"
	check := luhnCheckDigit(digits)
	full := digits + string(check)
	if !luhnValid(full) {
		t.Errorf("generated key %q with check digit %c failed validation", full, check)
	}
}

func TestLuhnValid(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"", false},
		{"1", false},
	}
	for _, tt := range tests {
		got := luhnValid(tt.input)
		if got != tt.valid {
			t.Errorf("luhnValid(%q) = %v, want %v", tt.input, got, tt.valid)
		}
	}
}

func TestGeneratedKeyPassesLuhn(t *testing.T) {
	for i := 0; i < 100; i++ {
		key, err := generatePairingKey()
		if err != nil {
			t.Fatal(err)
		}
		if len(key) != 16 {
			t.Errorf("key length = %d, want 16", len(key))
		}
		if !luhnValid(key) {
			t.Errorf("generated key %q failed Luhn check", key)
		}
	}
}
