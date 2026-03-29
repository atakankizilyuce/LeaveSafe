package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	certFileName = "server.crt"
	keyFileName  = "server.key"
	certValidity = 365 * 24 * time.Hour // 1 year
)

// GenerateOrLoadCert loads a TLS certificate from configDir/tls/ or generates
// a new self-signed one if none exists. Returns the certificate and its
// SHA-256 fingerprint.
func GenerateOrLoadCert(configDir string) (tls.Certificate, string, error) {
	tlsDir := filepath.Join(configDir, "tls")
	certPath := filepath.Join(tlsDir, certFileName)
	keyPath := filepath.Join(tlsDir, keyFileName)

	// Try loading existing cert
	if cert, fp, err := loadCert(certPath, keyPath); err == nil {
		log.Info("Loaded existing TLS certificate")
		return cert, fp, nil
	}

	// Generate new cert
	if err := os.MkdirAll(tlsDir, 0o700); err != nil {
		return tls.Certificate{}, "", fmt.Errorf("create tls dir: %w", err)
	}

	cert, fp, err := generateCert(certPath, keyPath)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("generate cert: %w", err)
	}

	log.WithField("fingerprint", fp).Info("Generated new self-signed TLS certificate")
	return cert, fp, nil
}

func loadCert(certPath, keyPath string) (tls.Certificate, string, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return tls.Certificate{}, "", err
	}

	fp := certFingerprint(cert.Certificate[0])
	return cert, fp, nil
}

func generateCert(certPath, keyPath string) (tls.Certificate, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, "", err
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, "", err
	}

	// Collect SANs: all local IPs
	var ips []net.IP
	ips = append(ips, net.ParseIP("127.0.0.1"))
	for _, ip := range getLocalIPs() {
		ips = append(ips, ip)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"LeaveSafe"},
			CommonName:   "LeaveSafe Device Monitor",
		},
		NotBefore: time.Now().Add(-time.Hour),
		NotAfter:  time.Now().Add(certValidity),

		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,

		IPAddresses: ips,
		DNSNames:    []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, "", err
	}

	// Write cert PEM
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		return tls.Certificate{}, "", err
	}

	// Write key PEM
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, "", err
	}

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, "", err
	}

	fp := certFingerprint(certDER)
	return tlsCert, fp, nil
}

func certFingerprint(certDER []byte) string {
	hash := sha256.Sum256(certDER)
	parts := make([]string, len(hash))
	for i, b := range hash {
		parts[i] = hex.EncodeToString([]byte{b})
	}
	return strings.Join(parts, ":")
}
