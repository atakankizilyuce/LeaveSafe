package auth

// luhnCheckDigit computes the Luhn check digit for a string of digits.
func luhnCheckDigit(digits string) byte {
	sum := 0
	nDigits := len(digits)
	parity := nDigits % 2

	for i := 0; i < nDigits; i++ {
		d := int(digits[i] - '0')
		if i%2 == parity {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}

	check := (10 - (sum % 10)) % 10
	return byte('0' + check)
}

// luhnValid checks if a digit string passes the Luhn check.
func luhnValid(digits string) bool {
	if len(digits) < 2 {
		return false
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return false
		}
	}
	payload := digits[:len(digits)-1]
	expected := luhnCheckDigit(payload)
	return digits[len(digits)-1] == expected
}
