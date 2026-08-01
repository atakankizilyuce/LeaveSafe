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
)

var stunMagicCookie = []byte{0x21, 0x12, 0xA4, 0x42}

// GetPublicIP discovers the public IP address using STUN, falling back to HTTP.
func GetPublicIP() (string, error) {
	if ip, err := getPublicIPviaSTUN(); err == nil {
		return ip, nil
	}
	return getPublicIPviaHTTP()
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
// txID.
func parseSTUNResponse(data []byte, txID []byte) (string, error) {
	if len(data) < stunHeaderLen {
		return "", fmt.Errorf("STUN response too short")
	}

	if msgType := uint16(data[0])<<8 | uint16(data[1]); msgType != stunBindingSuccess {
		return "", fmt.Errorf("STUN response has type 0x%04x, want a binding success", msgType)
	}
	if !bytes.Equal(data[4:8], stunMagicCookie) {
		return "", fmt.Errorf("STUN response carries the wrong magic cookie")
	}
	// Constant time is not the point here — the transaction ID is not a secret
	// and the attacker cannot see it — but the comparison must happen.
	if !bytes.Equal(data[8:stunHeaderLen], txID) {
		return "", fmt.Errorf("STUN response answers a different request")
	}

	// Parse attributes starting at byte 20
	pos := stunHeaderLen
	for pos+4 <= len(data) {
		attrType := uint16(data[pos])<<8 | uint16(data[pos+1])
		attrLen := int(uint16(data[pos+2])<<8 | uint16(data[pos+3]))
		pos += 4

		if pos+attrLen > len(data) {
			break
		}

		// XOR-MAPPED-ADDRESS (0x0020) or MAPPED-ADDRESS (0x0001)
		if attrType == 0x0020 && attrLen >= 8 {
			// XOR-MAPPED-ADDRESS: family at byte 1, port at 2-3, IP at 4-7
			ip := net.IPv4(
				data[pos+4]^0x21,
				data[pos+5]^0x12,
				data[pos+6]^0xA4,
				data[pos+7]^0x42,
			)
			return ip.String(), nil
		}
		if attrType == 0x0001 && attrLen >= 8 {
			ip := net.IPv4(data[pos+4], data[pos+5], data[pos+6], data[pos+7])
			return ip.String(), nil
		}

		// Pad to 4-byte boundary
		pos += attrLen
		if attrLen%4 != 0 {
			pos += 4 - (attrLen % 4)
		}
	}

	return "", fmt.Errorf("no mapped address in STUN response")
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

	ip := strings.TrimSpace(string(body))
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("invalid IP from HTTP: %q", ip)
	}
	return ip, nil
}
