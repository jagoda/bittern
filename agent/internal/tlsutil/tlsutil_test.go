package tlsutil

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	secretstore "bittern.invalid/agent/internal/secrets"
)

type fixture struct {
	certificatePath string
	secretsPath     string
	certificatePEM  []byte
	keyPEM          []byte
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(42),
		Subject:               pkix.Name{CommonName: "bittern"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"bittern"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	fixture := fixture{
		certificatePEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		keyPEM:         pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
	}
	base := t.TempDir()
	fixture.certificatePath = filepath.Join(base, "tls", "server.crt")
	fixture.secretsPath = filepath.Join(base, "secrets.yaml")
	if err := os.MkdirAll(filepath.Dir(fixture.certificatePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.certificatePath, fixture.certificatePEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := secretstore.Save(fixture.secretsPath, secretstore.Document{
		SchemaVersion: 1,
		SetupToken:    "setup-token-value",
		TLS:           secretstore.TLS{PrivateKeyPEM: string(fixture.keyPEM)},
	}); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestLoadCertificateAndHandshake(t *testing.T) {
	fixture := newFixture(t)
	material, err := Load(fixture.certificatePath, fixture.secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(material.Leaf.Raw)
	if material.Fingerprint != Fingerprint(material.Leaf.Raw) || material.Fingerprint == "" {
		t.Fatal("fingerprint does not identify the certificate")
	}
	if strings.ReplaceAll(material.Fingerprint, ":", "") != strings.ToUpper(hex.EncodeToString(sum[:])) {
		t.Fatal("fingerprint is not the SHA-256 of the certificate")
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "ui-body")
		}),
		TLSConfig: material.TLSConfig(),
	}
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.ServeTLS(listener, "", "") }()
	t.Cleanup(func() {
		_ = server.Shutdown(context.Background())
		_ = listener.Close()
	})

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(fixture.certificatePEM) {
		t.Fatal("could not add the fixture certificate to the client roots")
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
		ServerName: "bittern",
	}}}
	response, err := client.Get("https://" + listener.Addr().String() + "/ui/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if string(body) != "ui-body" {
		t.Fatalf("TLS response body = %q", body)
	}

	plain, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	_ = plain.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = io.WriteString(plain, "GET /ui/ HTTP/1.1\r\nHost: appliance\r\nConnection: close\r\n\r\n")
	plainResponse, _ := io.ReadAll(bufio.NewReader(plain))
	if strings.Contains(string(plainResponse), "ui-body") {
		t.Fatal("the TLS listener served the UI over plain HTTP")
	}
}

func TestLoadRefusesMissingCertificateOrKey(t *testing.T) {
	fixture := newFixture(t)
	if _, err := Load(filepath.Join(t.TempDir(), "missing.crt"), fixture.secretsPath); err == nil {
		t.Fatal("a missing certificate was accepted")
	}
	if err := secretstore.Save(fixture.secretsPath, secretstore.Document{SchemaVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(fixture.certificatePath, fixture.secretsPath); err == nil ||
		!strings.Contains(err.Error(), "private_key_pem") {
		t.Fatalf("a missing private key error = %v", err)
	}
	if _, err := Load(fixture.certificatePath, filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("a missing secrets file was accepted")
	}
}
