// Package paircode turns the three things a phone needs to reach this machine
// into one string somebody can read off a screen and type into a phone.
//
// The QR code beside it carries the same three things as a URL, and that URL
// stays what it is: it opens this machine's own web client in a browser, which
// is the path for a phone with no application installed. This is the other
// path — the one for the application, where a person is typing rather than
// pointing a camera.
//
// Thirteen bytes go in:
//
//	[4] the IPv4 address
//	[2] the port, big-endian
//	[7] the pairing key, as the number its sixteen digits spell
//
// and twenty-one characters come out. That is longer than the sixteen digits
// the key alone used to be, and shorter than the sixteen digits plus an address
// which is what it actually replaces.
package paircode

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// alphabet is Crockford's base32, and the reason for choosing it is the four
// characters missing from it: I, L, O and U. The first three are what people
// type when they mean 1, 1 and 0 — and this string is read off one screen and
// typed into another, which is the exact situation those mistakes belong to.
// U is left out to keep an accidental obscenity from appearing in a code.
const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// payloadBytes is the address, the port and the key.
const payloadBytes = 4 + 2 + 7

// codeChars is what payloadBytes takes in base32, rounded up: 13*8 = 104 bits,
// and 104/5 is 20.8.
const codeChars = 21

// keyDigits is the length of a pairing key. It matches internal/auth.
const keyDigits = 16

// ErrNotACode says the string handed over is not one of these at all.
var ErrNotACode = errors.New("not a pairing code")

// Encode builds the code this machine shows.
func Encode(host string, port int, key string) (string, error) {
	ip := net.ParseIP(host)
	if ip == nil {
		return "", fmt.Errorf("%w: %q is not an address", ErrNotACode, host)
	}
	// IPv4 only, and deliberately. The daemon reports IPv4 addresses to the
	// application already, and nobody types an IPv6 literal off a screen.
	v4 := ip.To4()
	if v4 == nil {
		return "", fmt.Errorf("%w: %q is not an IPv4 address", ErrNotACode, host)
	}

	if port < 0 || port > 65535 {
		return "", fmt.Errorf("%w: port %d is out of range", ErrNotACode, port)
	}

	digits, err := keyNumber(key)
	if err != nil {
		return "", err
	}

	payload := make([]byte, payloadBytes)
	copy(payload[0:4], v4)
	payload[4] = byte(port >> 8)
	payload[5] = byte(port)
	for i := 6; i >= 0; i-- {
		payload[6+i] = byte(digits)
		digits >>= 8
	}

	return group(encode32(payload)), nil
}

// Decode reads a code back, forgiving everything a person does to one on the
// way from a screen to a keyboard.
func Decode(typed string) (host string, port int, key string, err error) {
	payload, err := decode32(typed)
	if err != nil {
		return "", 0, "", err
	}

	host = net.IP(payload[0:4]).String()
	port = int(payload[4])<<8 | int(payload[5])

	var number uint64
	for _, b := range payload[6:] {
		number = number<<8 | uint64(b)
	}
	if number >= 1e16 {
		return "", 0, "", fmt.Errorf("%w: the key in it is not sixteen digits", ErrNotACode)
	}

	return host, port, fmt.Sprintf("%016d", number), nil
}

// keyNumber reads the sixteen digits as the number they spell.
//
// The check digit rides along rather than being recomputed on arrival. It is
// part of the key the machine will be asked for, and a code that carried
// fifteen digits and worked out the sixteenth would agree with itself about a
// key it had got wrong.
func keyNumber(key string) (uint64, error) {
	if len(key) != keyDigits {
		return 0, fmt.Errorf("%w: a key is %d digits, not %d", ErrNotACode, keyDigits, len(key))
	}
	n, err := strconv.ParseUint(key, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: a key is digits only", ErrNotACode)
	}
	return n, nil
}

// encode32 writes bytes as base32 characters, most significant bit first.
//
// Hand-rolled rather than encoding/base32, because that package's alphabet is
// RFC 4648's — which contains I, L and O, the three characters this whole
// choice exists to avoid — and its padding would put '=' in something a person
// has to type.
func encode32(payload []byte) string {
	out := make([]byte, 0, codeChars)

	var buffer uint16
	var bits uint

	for _, b := range payload {
		buffer = buffer<<8 | uint16(b)
		bits += 8
		for bits >= 5 {
			bits -= 5
			out = append(out, alphabet[(buffer>>bits)&0x1f])
		}
	}
	if bits > 0 {
		// The last four bits, padded out rather than dropped.
		out = append(out, alphabet[(buffer<<(5-bits))&0x1f])
	}

	return string(out)
}

func decode32(typed string) ([]byte, error) {
	cleaned := clean(typed)
	if len(cleaned) != codeChars {
		return nil, fmt.Errorf("%w: a code is %d characters, not %d", ErrNotACode, codeChars, len(cleaned))
	}

	payload := make([]byte, 0, payloadBytes)

	var buffer uint16
	var bits uint

	for _, c := range cleaned {
		value := strings.IndexRune(alphabet, c)
		if value < 0 {
			return nil, fmt.Errorf("%w: %q is not a character a code uses", ErrNotACode, c)
		}
		buffer = buffer<<5 | uint16(value)
		bits += 5
		if bits >= 8 {
			bits -= 8
			payload = append(payload, byte(buffer>>bits))
		}
	}

	if len(payload) != payloadBytes {
		return nil, fmt.Errorf("%w: it does not carry an address and a key", ErrNotACode)
	}
	return payload, nil
}

// clean turns what somebody typed into what they meant.
//
// Case is thrown away because a code shown in capitals gets typed in whatever
// the keyboard was in. Separators go because people put them where they like
// or leave them out. And the three characters Crockford excludes are mapped to
// the digits they are mistaken for rather than refused — somebody who typed O
// for 0 made the mistake this alphabet was chosen to survive, and answering
// "not a code" would be the one response that does not survive it.
func clean(typed string) string {
	var out strings.Builder
	out.Grow(len(typed))

	for _, c := range strings.ToUpper(typed) {
		switch c {
		case '-', ' ', '\t', '\n', '\r', '_':
			continue
		case 'O':
			out.WriteRune('0')
		case 'I', 'L':
			out.WriteRune('1')
		default:
			out.WriteRune(c)
		}
	}

	return out.String()
}

// group breaks the code up four characters at a time, the way the pairing key
// itself is broken up. Twenty-one unbroken characters is a run nobody keeps
// their place in halfway through typing.
func group(code string) string {
	var out strings.Builder
	for i, c := range code {
		if i > 0 && i%4 == 0 {
			out.WriteByte('-')
		}
		out.WriteRune(c)
	}
	return out.String()
}
