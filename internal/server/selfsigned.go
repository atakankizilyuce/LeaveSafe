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
	"errors"
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
	} else if !errors.Is(err, errCertUnusable) {
		log.Debugf("No usable TLS certificate on disk: %v", err)
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

// errCertUnusable marks a certificate that was read successfully but must not
// be served — expired, or about to be. It is distinguished from a read failure
// so the caller can say which happened.
var errCertUnusable = errors.New("stored certificate is not usable")

// certRenewBefore is how long before expiry a certificate is replaced. A
// machine that is started once a month must not be the one to discover, while
// its owner is away from it, that the certificate ran out yesterday.
const certRenewBefore = 30 * 24 * time.Hour

func loadCert(certPath, keyPath string) (tls.Certificate, string, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return tls.Certificate{}, "", err
	}

	// LoadX509KeyPair checks that the key matches the certificate; it says
	// nothing about the dates. Serving an expired certificate means every phone
	// meets a warning the fingerprint cannot explain away, on the one screen
	// the user has while they are away from the laptop.
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("%w: %w", errCertUnusable, err)
	}
	if remaining := time.Until(parsed.NotAfter); remaining < certRenewBefore {
		if remaining <= 0 {
			log.Warnf("Stored TLS certificate expired on %s — generating a new one",
				parsed.NotAfter.Format("2006-01-02"))
		} else {
			log.Infof("Stored TLS certificate expires on %s — generating a new one now",
				parsed.NotAfter.Format("2006-01-02"))
		}
		// The fingerprint changes with the certificate, which is exactly what
		// the QR code and the greeting exist to carry: the phone compares what
		// this run is serving, so a rescan pairs against the new one.
		return tls.Certificate{}, "", fmt.Errorf("%w: expires %s", errCertUnusable, parsed.NotAfter)
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
	ips = append(ips, getLocalIPs()...)

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
	// WriteFile applies the mode only when it creates the file, so a key
	// restored from a backup or left by an older version keeps whatever
	// permissions it arrived with. This is the private key for the certificate
	// guarding remote access; internal/auth/keyfile.go does the same for the
	// pairing key and for the same reason.
	if err := os.Chmod(keyPath, 0o600); err != nil {
		return tls.Certificate{}, "", fmt.Errorf("restrict TLS key permissions: %w", err)
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
