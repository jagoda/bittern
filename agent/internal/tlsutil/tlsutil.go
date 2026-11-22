// Package tlsutil loads the appliance UI certificate and private key.
//
// The certificate is public and lives at /etc/bittern/tls/server.crt. The
// private key lives only in /etc/bittern/secrets.yaml under tls.private_key_pem.
// There is no fallback to a plain HTTP listener when either piece is missing.
package tlsutil

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	secretstore "bittern.invalid/agent/internal/secrets"
)

// Material is the loaded certificate and its SHA-256 fingerprint.
type Material struct {
	Certificate tls.Certificate
	Leaf        *x509.Certificate
	Fingerprint string
}

// Load reads the certificate and the private key. A missing certificate, a
// missing key, or a mismatched pair is an error; callers must not fall back to
// HTTP.
func Load(certificatePath, secretsPath string) (Material, error) {
	if strings.TrimSpace(certificatePath) == "" {
		return Material{}, errors.New("tls: certificate path is empty")
	}
	certificatePEM, err := os.ReadFile(certificatePath)
	if err != nil {
		return Material{}, fmt.Errorf("tls: read certificate %s: %w", certificatePath, err)
	}
	document, err := secretstore.Load(secretsPath)
	if err != nil {
		return Material{}, fmt.Errorf("tls: read secrets %s: %w", secretsPath, err)
	}
	if strings.TrimSpace(document.TLS.PrivateKeyPEM) == "" {
		return Material{}, fmt.Errorf("tls: %s has no tls.private_key_pem", secretsPath)
	}
	certificate, err := tls.X509KeyPair(certificatePEM, []byte(document.TLS.PrivateKeyPEM))
	if err != nil {
		return Material{}, fmt.Errorf("tls: load certificate and private key: %w", err)
	}
	if len(certificate.Certificate) == 0 {
		return Material{}, errors.New("tls: certificate file contains no certificate")
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		return Material{}, fmt.Errorf("tls: parse certificate: %w", err)
	}
	certificate.Leaf = leaf
	return Material{
		Certificate: certificate,
		Leaf:        leaf,
		Fingerprint: Fingerprint(leaf.Raw),
	}, nil
}

// Fingerprint returns the SHA-256 of the DER certificate as uppercase bytes
// separated by colons.
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	parts := make([]string, len(sum))
	for index, value := range sum {
		parts[index] = strings.ToUpper(hex.EncodeToString([]byte{value}))
	}
	return strings.Join(parts, ":")
}

// TLSConfig returns a server configuration with TLS 1.2 as the floor. It does
// not disable verification on the client side and it does not add HSTS.
func (m Material) TLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{m.Certificate},
	}
}
