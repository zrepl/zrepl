package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path"
	"reflect"
	"testing"
	"time"

	"github.com/zrepl/yaml-config"
)

func TestTLSConfig(t *testing.T) {
	input := `
cert: path/to/cert.pem
key: path/to/key.pem
client_ca: path/to/ca.pem
client_auth_type: RequireAndVerifyClientCert
min_version: TLS1.0
max_version: TLS1.3
cipher_suites:
  # TLS 1.2 cipher suites.
  - TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA
  - TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA
  - TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA
  - TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA
  - TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256
  - TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384
  - TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256
  - TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
  - TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256
  - TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256
  # TLS 1.3 cipher suites.
  - TLS_AES_128_GCM_SHA256
  - TLS_AES_256_GCM_SHA384
  - TLS_CHACHA20_POLY1305_SHA256
`
	parsed := TLSConfig{
		CertPath:   "path/to/cert.pem",
		KeyPath:    "path/to/key.pem",
		ClientCAs:  "path/to/ca.pem",
		ClientAuth: ClientAuthType(tls.RequireAndVerifyClientCert),
		MinVersion: tls.VersionTLS10,
		MaxVersion: tls.VersionTLS13,
		CipherSuites: []CipherSuite{
			// TLS 1.2 cipher suites.
			CipherSuite(tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA),
			CipherSuite(tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA),
			CipherSuite(tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA),
			CipherSuite(tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA),
			CipherSuite(tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256),
			CipherSuite(tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384),
			CipherSuite(tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256),
			CipherSuite(tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384),
			CipherSuite(tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256),
			CipherSuite(tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256),
			// TLS 1.3 cipher suites.
			CipherSuite(tls.TLS_AES_128_GCM_SHA256),
			CipherSuite(tls.TLS_AES_256_GCM_SHA384),
			CipherSuite(tls.TLS_CHACHA20_POLY1305_SHA256),
		},
	}

	var actual TLSConfig
	err := yaml.Unmarshal([]byte(input), &actual)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, parsed) {
		t.Fatal("parsed TLS config doesn't match expected")
	}

	workdir := t.TempDir()
	pathTo := path.Join(workdir, "path", "to")
	err = os.MkdirAll(pathTo, os.FileMode(0755))
	if err != nil {
		t.Fatal(err)
	}

	// Generate private key and x509 certificate, valid enough to make the TLS
	// code load them for testing.
	key, cert, err := genKeyCert( /* validFor */ time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	writePEMFile(t, path.Join(pathTo, "cert.pem"), os.FileMode(0644), "CERTIFICATE", cert)
	writePEMFile(t, path.Join(pathTo, "key.pem"), os.FileMode(0644), "PRIVATE KEY", key)
	// write the "leaf" cert to the CA path- it doesn't matter that it doesn't
	// actually sign the cert (we never check)- just that it's a valid
	// PEM-encoded x509 certificate
	writePEMFile(t, path.Join(pathTo, "ca.pem"), os.FileMode(0644), "CERTIFICATE", cert)

	if os.Chdir(workdir) != nil {
		t.Fatal("failed to change chdir to " + workdir)
	}
	config, err := parsed.Config()
	if err != nil {
		t.Fatal(err)
	}

	expectedCipherSuites := make([]uint16, len(parsed.CipherSuites))
	for i, cs := range parsed.CipherSuites {
		expectedCipherSuites[i] = uint16(cs)
	}

	matches := []struct {
		name             string
		expected, actual interface{}
	}{
		{"CipherSuites", expectedCipherSuites, config.CipherSuites},
		{"MinVersion", uint16(tls.VersionTLS10), config.MinVersion},
		{"MaxVersion", uint16(tls.VersionTLS13), config.MaxVersion},
	}
	for _, match := range matches {
		if !reflect.DeepEqual(match.expected, match.actual) {
			t.Fatalf("expected and actual %s do not match", match.name)
		}
	}

	actualClientCAs := x509.NewCertPool()
	actualClientCAs.AppendCertsFromPEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert}))
	if !config.ClientCAs.Equal(actualClientCAs) {
		t.Fatalf("expected and actual client CAs do not match")
	}
}

// genKeyCert generates an elliptic curve and self-signed certificate for
// testing. key and certificate are returned in DER format, as bytes.
func genKeyCert(validFor time.Duration) ([]byte, []byte, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	key, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}

	notBefore := time.Now()
	template := x509.Certificate{
		SerialNumber:          big.NewInt(0),
		Subject:               pkix.Name{CommonName: "localhost"},
		SignatureAlgorithm:    x509.ECDSAWithSHA256,
		NotBefore:             notBefore,
		NotAfter:              notBefore.Add(validFor),
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	cert, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, err
	}

	return key, cert, nil
}

func writePEMFile(t *testing.T, path string, mode os.FileMode, typ string, cert []byte) {
	open, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		t.Fatal(err)
	}
	err = pem.Encode(open, &pem.Block{Type: typ, Bytes: cert})
	if err != nil {
		t.Fatal(err)
	}
	err = open.Close()
	if err != nil {
		t.Fatal(err)
	}
}
