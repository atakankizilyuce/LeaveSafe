package network

import (
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
	req := []byte{
		0x00, 0x01, // Type: Binding Request
		0x00, 0x00, // Length: 0
		0x21, 0x12, 0xA4, 0x42, // Magic Cookie
		0x01, 0x02, 0x03, 0x04, // Transaction ID (static, good enough)
		0x05, 0x06, 0x07, 0x08,
		0x09, 0x0A, 0x0B, 0x0C,
	}

	conn.SetDeadline(time.Now().Add(httpTimeout))
	if _, err := conn.Write(req); err != nil {
		return "", err
	}

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return "", err
	}

	return parseSTUNResponse(buf[:n])
}

func parseSTUNResponse(data []byte) (string, error) {
	if len(data) < 20 {
		return "", fmt.Errorf("STUN response too short")
	}

	// Parse attributes starting at byte 20
	pos := 20
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
