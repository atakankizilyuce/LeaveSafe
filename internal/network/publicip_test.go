package network

import (
	"testing"
)

// testTxID stands in for the transaction ID a real request would have drawn.
var testTxID = []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}

// stunResponse builds a STUN Binding Success Response carrying one attribute.
func stunResponse(attrType uint16, value []byte) []byte {
	header := make([]byte, 20)
	header[0], header[1] = 0x01, 0x01 // Binding Success Response
	length := 4 + len(value)
	header[2] = byte(length >> 8)
	header[3] = byte(length)
	copy(header[4:8], []byte{0x21, 0x12, 0xA4, 0x42}) // magic cookie
	copy(header[8:20], testTxID)

	attr := make([]byte, 4+len(value))
	attr[0] = byte(attrType >> 8)
	attr[1] = byte(attrType)
	attr[2] = byte(len(value) >> 8)
	attr[3] = byte(len(value))
	copy(attr[4:], value)

	return append(header, attr...)
}

// xorMappedIPv4 encodes an address the way XOR-MAPPED-ADDRESS does: the octets
// are XORed against the magic cookie, which is the part worth having a test for
// — get it wrong and the reported public address is plausible nonsense.
func xorMappedIPv4(a, b, c, d byte, port uint16) []byte {
	return []byte{
		0x00, 0x01, // reserved, family IPv4
		byte(port>>8) ^ 0x21, byte(port) ^ 0x12,
		a ^ 0x21, b ^ 0x12, c ^ 0xA4, d ^ 0x42,
	}
}

func TestParseXORMappedAddress(t *testing.T) {
	data := stunResponse(0x0020, xorMappedIPv4(203, 0, 113, 45, 54321))

	got, err := parseSTUNResponse(data, testTxID)
	if err != nil {
		t.Fatalf("parseSTUNResponse: %v", err)
	}
	if got != "203.0.113.45" {
		t.Errorf("parsed %q, want 203.0.113.45", got)
	}
}

func TestParsePlainMappedAddress(t *testing.T) {
	data := stunResponse(0x0001, []byte{0x00, 0x01, 0xd4, 0x31, 198, 51, 100, 7})

	got, err := parseSTUNResponse(data, testTxID)
	if err != nil {
		t.Fatalf("parseSTUNResponse: %v", err)
	}
	if got != "198.51.100.7" {
		t.Errorf("parsed %q, want 198.51.100.7", got)
	}
}

// A server may put other attributes ahead of the address, so the parser has to
// walk past them rather than only reading the first one.
func TestParseSkipsEarlierAttributes(t *testing.T) {
	// SOFTWARE (0x8022) with a 5-byte value, which also exercises the padding
	// to the next 4-byte boundary.
	software := []byte{0x80, 0x22, 0x00, 0x05, 'h', 'e', 'l', 'l', 'o', 0, 0, 0}

	data := stunResponse(0x0020, xorMappedIPv4(192, 0, 2, 33, 3478))
	// Splice the extra attribute in between the header and the address.
	withExtra := append(append([]byte{}, data[:20]...), software...)
	withExtra = append(withExtra, data[20:]...)
	length := len(withExtra) - 20
	withExtra[2] = byte(length >> 8)
	withExtra[3] = byte(length)

	got, err := parseSTUNResponse(withExtra, testTxID)
	if err != nil {
		t.Fatalf("parseSTUNResponse: %v", err)
	}
	if got != "192.0.2.33" {
		t.Errorf("parsed %q, want 192.0.2.33", got)
	}
}

func TestParseRejectsShortResponse(t *testing.T) {
	for _, data := range [][]byte{nil, {}, make([]byte, 19)} {
		if _, err := parseSTUNResponse(data, testTxID); err == nil {
			t.Errorf("a %d-byte response parsed without error", len(data))
		}
	}
}

func TestParseRejectsResponseWithNoAddress(t *testing.T) {
	// A well-formed response carrying only a SOFTWARE attribute.
	data := stunResponse(0x8022, []byte{'n', 'o', 'p', 'e'})

	if _, err := parseSTUNResponse(data, testTxID); err == nil {
		t.Error("a response with no mapped address parsed without error")
	}
}

// A truncated attribute must end the walk rather than read past the buffer.
func TestParseStopsAtATruncatedAttribute(t *testing.T) {
	data := stunResponse(0x0020, xorMappedIPv4(203, 0, 113, 45, 1234))
	truncated := data[:len(data)-3]

	if _, err := parseSTUNResponse(truncated, testTxID); err == nil {
		t.Error("a truncated attribute parsed without error")
	}
}

// An address attribute too short to hold an IPv4 address is skipped, not read
// out of bounds.
func TestParseIgnoresUndersizedAddressAttribute(t *testing.T) {
	data := stunResponse(0x0020, []byte{0x00, 0x01, 0x00, 0x00})

	if _, err := parseSTUNResponse(data, testTxID); err == nil {
		t.Error("an undersized address attribute parsed without error")
	}
}

// The socket accepts UDP from anybody, so the transaction ID is the only thing
// that says a datagram is the answer to the question this program asked. The
// address that comes back is printed as a QR code for the owner's phone to
// scan, which makes an unsolicited reply a way to choose where they pair.
func TestParseRejectsAForeignTransactionID(t *testing.T) {
	data := stunResponse(0x0020, xorMappedIPv4(203, 0, 113, 45, 1234))
	other := []byte{9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9}

	if _, err := parseSTUNResponse(data, other); err == nil {
		t.Error("a response to a different request parsed without error")
	}
}

func TestParseRejectsANonBindingSuccess(t *testing.T) {
	data := stunResponse(0x0020, xorMappedIPv4(203, 0, 113, 45, 1234))
	data[0], data[1] = 0x01, 0x11 // Binding Error Response

	if _, err := parseSTUNResponse(data, testTxID); err == nil {
		t.Error("a binding error response parsed as an address")
	}
}

func TestParseRejectsAWrongMagicCookie(t *testing.T) {
	data := stunResponse(0x0020, xorMappedIPv4(203, 0, 113, 45, 1234))
	data[4] = 0x00

	if _, err := parseSTUNResponse(data, testTxID); err == nil {
		t.Error("a response with the wrong magic cookie parsed without error")
	}
}
