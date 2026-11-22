package firstboot

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"strings"
	"time"
)

type certificateInfo struct {
	Path        string
	Fingerprint string
	IPAddresses int
}

func ensureCertificate(
	path string,
	identity identityRecord,
	secrets secretsDocument,
	hostname string,
	lanAddress string,
	refreshAddress bool,
	random io.Reader,
	now time.Time,
) (certificateInfo, error) {
	key, err := parsePrivateKey(secrets.TLS.PrivateKeyPEM)
	if err != nil {
		return certificateInfo{}, err
	}

	if data, readErr := os.ReadFile(path); readErr == nil {
		certificate, parseErr := parseCertificate(data)
		if parseErr == nil && certificateMatches(certificate, identity, key) {
			addressMissing := len(certificate.IPAddresses) == 0 && lanAddress != ""
			if !(refreshAddress && addressMissing) {
				_ = os.Chmod(path, 0o644)
				return certificateDetails(path, certificate), nil
			}
		}
	} else if !os.IsNotExist(readErr) {
		return certificateInfo{}, readErr
	}

	if random == nil {
		random = rand.Reader
	}
	serial, err := randomSerial(random)
	if err != nil {
		return certificateInfo{}, err
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   hostname,
			Organization: []string{"Bittern"},
			// The identity id makes a reset visible without a private file.
			SerialNumber: identity.ID,
		},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{hostname},
	}
	if address := net.ParseIP(lanAddress); address != nil && address.To4() != nil {
		template.IPAddresses = []net.IP{address.To4()}
	}
	der, err := x509.CreateCertificate(random, template, template, &key.PublicKey, key)
	if err != nil {
		return certificateInfo{}, err
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		return certificateInfo{}, err
	}
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := writeFileAtomic(path, data, 0o644); err != nil {
		return certificateInfo{}, err
	}
	return certificateDetails(path, certificate), nil
}

func parseCertificate(data []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("no PEM certificate block")
	}
	return x509.ParseCertificate(block.Bytes)
}

func certificateMatches(certificate *x509.Certificate, identity identityRecord, key *ecdsa.PrivateKey) bool {
	if certificate.Subject.SerialNumber != identity.ID {
		return false
	}
	want, err := x509.MarshalPKIXPublicKey(key.Public())
	if err != nil {
		return false
	}
	got, err := x509.MarshalPKIXPublicKey(certificate.PublicKey)
	if err != nil {
		return false
	}
	return string(want) == string(got)
}

func certificateDetails(path string, certificate *x509.Certificate) certificateInfo {
	sum := sha256.Sum256(certificate.Raw)
	parts := make([]string, len(sum))
	for index, value := range sum {
		parts[index] = strings.ToUpper(hex.EncodeToString([]byte{value}))
	}
	return certificateInfo{
		Path:        path,
		Fingerprint: strings.Join(parts, ":"),
		IPAddresses: len(certificate.IPAddresses),
	}
}

func randomSerial(reader io.Reader) (*big.Int, error) {
	var value [16]byte
	if _, err := io.ReadFull(reader, value[:]); err != nil {
		return nil, err
	}
	value[0] &= 0x7f
	serial := new(big.Int).SetBytes(value[:])
	if serial.Sign() == 0 {
		serial.SetInt64(1)
	}
	return serial, nil
}
