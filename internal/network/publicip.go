package network

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	stunServer  = "stun.l.google.com:19302"
	ipifyURL    = "https://api.ipify.org"
	httpTimeout = 5 * time.Second
)

// STUN wire constants, from RFC 5389.
const (
	stunHeaderLen      = 20
	stunTxIDLen        = 12
	stunBindingSuccess = 0x0101
	// stunFamilyIPv4 is the address-family byte of a MAPPED-ADDRESS attribute
	// that carries four octets rather than sixteen.
	stunFamilyIPv4 = 0x01

	// The two attributes that can carry the answer. Both hold the family at
	// byte 1, the port at 2-3 and the address from byte 4 on; the XOR one has
	// the magic cookie folded into those bytes.
	attrMappedAddress    = 0x0001
	attrXORMappedAddress = 0x0020
	// attrAddressLen is the shortest an address attribute can be and still hold
	// a family, a port and four octets.
	attrAddressLen = 8
)

var stunMagicCookie = []byte{0x21, 0x12, 0xA4, 0x42}

// GetPublicIP discovers this machine's address on the internet.
//
// The answer is not decoration. It becomes the front of the URL list, and the
// dashboard renders the first of those as a QR code with the pairing key in its
// fragment — so whoever chooses this string chooses where the owner's phone
// tries to pair, and the key goes with it. That is the same reasoning
// PortMapping.ExternalIP already carries for the router's answer, and both
// halves of it apply here too.
//
// So: the HTTPS lookup is asked first and the STUN one second, which is the
// reverse of the order this used to use. STUN is plain UDP to a name resolved
// by whatever DNS the network handed out — a café access point that answers its
// own resolver picks the address without having to forge a packet, and one that
// can see the request can race a reply because the transaction ID is on the wire
// in clear. The HTTPS lookup is a certificate check against a public name, which
// is a claim a hostile network cannot make. STUN stays as the fallback, because
// a network that blocks the lookup is far more common than one that attacks it.
//
// Either way the result is held to what a public address can look like. Private,
// loopback, link-local, multicast and unspecified addresses are refused: none is
// reachable from the internet, so none is an honest answer to this question, and
// every one of them would aim the owner's phone somewhere on the local network.
func GetPublicIP() (string, error) {
	ip, httpErr := getPublicIPviaHTTP()
	if httpErr == nil {
		return ip, nil
	}

	ip, stunErr := getPublicIPviaSTUN()
	if stunErr == nil {
		return ip, nil
	}
	return "", fmt.Errorf("no public address: %w; %w", httpErr, stunErr)
}

func getPublicIPviaSTUN() (string, error) {
	conn, err := net.DialTimeout("udp", stunServer, httpTimeout)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	// STUN Binding Request: 20 bytes
	// Type: 0x0001 (Binding Request), Length: 0x0000
	// Magic Cookie: 0x2112A442
	// Transaction ID: 12 random bytes
	//
	// The transaction ID is drawn fresh for every request, and the reply is
	// checked against it. It is the only thing that ties an answer to this
	// question: the socket takes UDP from anyone, so a fixed ID means any
	// datagram that turns up is accepted as the answer. What that buys an
	// attacker is not academic — the address this returns is printed as a QR
	// code for the phone to scan, so choosing it means choosing where the
	// owner's phone tries to pair.
	req := []byte{
		0x00, 0x01, // Type: Binding Request
		0x00, 0x00, // Length: 0
		0x21, 0x12, 0xA4, 0x42, // Magic Cookie
	}
	txID := make([]byte, stunTxIDLen)
	if _, err := rand.Read(txID); err != nil {
		return "", fmt.Errorf("STUN transaction ID: %w", err)
	}
	req = append(req, txID...)

	if err := conn.SetDeadline(time.Now().Add(httpTimeout)); err != nil {
		return "", err
	}
	if _, err := conn.Write(req); err != nil {
		return "", err
	}

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return "", err
	}

	return parseSTUNResponse(buf[:n], txID)
}

// parseSTUNResponse extracts the mapped address from a Binding Success
// Response, rejecting anything that is not a reply to the request that carried
// txID and anything that could not be this machine's address on the internet.
//
// Nothing about a STUN exchange is authenticated: no signature, no certificate,
// and a name resolved by whatever DNS the network handed out. What comes back is
// therefore a claim, and this is where the claim is held to a shape. See
// GetPublicIP for what an unchecked one buys whoever makes it.
func parseSTUNResponse(data, txID []byte) (string, error) {
	if err := checkSTUNHeader(data, txID); err != nil {
		return "", err
	}
	return mappedAddress(data)
}

// checkSTUNHeader rejects everything that says this datagram is not the reply to
// the request that carried txID.
func checkSTUNHeader(data, txID []byte) error {
	if len(data) < stunHeaderLen {
		return fmt.Errorf("STUN response too short")
	}

	if msgType := uint16(data[0])<<8 | uint16(data[1]); msgType != stunBindingSuccess {
		return fmt.Errorf("STUN response has type 0x%04x, want a binding success", msgType)
	}
	if !bytes.Equal(data[4:8], stunMagicCookie) {
		return fmt.Errorf("STUN response carries the wrong magic cookie")
	}
	// Constant time is not the point here — the transaction ID is not a secret
	// and the attacker cannot see it — but the comparison must happen.
	if !bytes.Equal(data[8:stunHeaderLen], txID) {
		return fmt.Errorf("STUN response answers a different request")
	}
	return nil
}

// mappedAddress walks the attributes after the header and returns the address in
// the first one that carries it.
//
// A server is free to put other attributes first, so this cannot simply read the
// one at byte 20. An attribute that runs off the end of the datagram ends the
// walk, and one too short to hold an address is stepped over rather than read.
func mappedAddress(data []byte) (string, error) {
	pos := stunHeaderLen
	for pos+4 <= len(data) {
		attrType := uint16(data[pos])<<8 | uint16(data[pos+1])
		attrLen := int(uint16(data[pos+2])<<8 | uint16(data[pos+3]))
		pos += 4

		if pos+attrLen > len(data) {
			break
		}

		if isAddressAttribute(attrType) && attrLen >= attrAddressLen {
			return addressFrom(attrType, data[pos:pos+attrLen])
		}

		// Pad to 4-byte boundary
		pos += attrLen
		if attrLen%4 != 0 {
			pos += 4 - (attrLen % 4)
		}
	}

	return "", fmt.Errorf("no mapped address in STUN response")
}

func isAddressAttribute(attrType uint16) bool {
	return attrType == attrXORMappedAddress || attrType == attrMappedAddress
}

// addressFrom reads the four octets out of an address attribute's value.
//
// The family is checked rather than assumed. An IPv6 attribute is sixteen bytes
// of address, and reading its first four as an IPv4 one produces a
// plausible-looking address that belongs to nobody — which, for a value the
// owner's phone is about to be pointed at, is worse than having no answer at all.
func addressFrom(attrType uint16, value []byte) (string, error) {
	if value[1] != stunFamilyIPv4 {
		return "", fmt.Errorf("STUN response carries a non-IPv4 mapped address")
	}
	if attrType == attrXORMappedAddress {
		ip := net.IPv4(
			value[4]^0x21,
			value[5]^0x12,
			value[6]^0xA4,
			value[7]^0x42,
		)
		return publicAddr(ip.String())
	}
	ip := net.IPv4(value[4], value[5], value[6], value[7])
	return publicAddr(ip.String())
}

// cgnatBlock is RFC 6598 shared address space, the range an ISP assigns to a
// subscriber while keeping the routable address for itself.
var cgnatBlock = &net.IPNet{
	IP:   net.IPv4(100, 64, 0, 0),
	Mask: net.CIDRMask(10, 32),
}

// IsCarrierGradeNAT reports whether ip is inside 100.64.0.0/10.
//
// An address in this range is the answer to "what does the internet see" only
// in the sense that the ISP's NAT sees it. Nothing the user can do to their own
// router makes such a machine reachable from outside, so remote access is not
// merely misconfigured here — it cannot work, and saying so is more useful than
// leaving the user to forward ports at a problem that is not theirs.
//
// publicAddr deliberately accepts these addresses; refusing a syntactically
// valid public address is not its job. This is the separate question.
func IsCarrierGradeNAT(ip string) bool {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return false
	}
	v4 := parsed.To4()
	if v4 == nil {
		return false
	}
	return cgnatBlock.Contains(v4)
}

func getPublicIPviaHTTP() (string, error) {
	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Get(ipifyURL)
	if err != nil {
		return "", fmt.Errorf("HTTP IP lookup failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return "", err
	}

	// The transport is authenticated, so this is the trustworthy of the two
	// sources — but the endpoint is still a third party answering a question
	// about the owner's machine, and the answer still ends up in a QR code. It
	// is held to the same shape as the other two.
	return publicAddr(strings.TrimSpace(string(body)))
}
