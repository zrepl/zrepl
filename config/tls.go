package config

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"

	"github.com/pkg/errors"
)

type TLSConfig struct {
	CertPath     string         `yaml:"cert"`
	KeyPath      string         `yaml:"key"`
	ClientCAs    string         `yaml:"client_ca,optional"`
	ClientAuth   ClientAuthType `yaml:"client_auth_type,optional"`
	MinVersion   TLSVersion     `yaml:"min_version,optional"`
	MaxVersion   TLSVersion     `yaml:"max_version,optional"`
	CipherSuites []CipherSuite  `yaml:"cipher_suites,optional"`
}

func (c *TLSConfig) loadCertificate() (*tls.Certificate, error) {
	cert, err := ioutil.ReadFile(c.CertPath)
	if err != nil {
		return nil, errors.Wrap(err, "tls: failed to read certificate")
	}
	key, err := ioutil.ReadFile(c.KeyPath)
	if err != nil {
		return nil, errors.Wrap(err, "tls: failed to read key")
	}

	merged, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return nil, errors.Wrap(err, "tls: failed to load X509KeyPair")
	}
	return &merged, nil
}

func (c *TLSConfig) Config() (*tls.Config, error) {
	if c == nil {
		return nil, nil
	}

	if c.CertPath == "" {
		return nil, errors.New("tls: missing cert path")
	}
	if c.KeyPath == "" {
		return nil, errors.New("tls: missing key path")
	}

	_, err := c.loadCertificate()
	if err != nil {
		return nil, err
	}

	cfg := tls.Config{
		MinVersion: c.MinVersion.Value(),
		MaxVersion: c.MaxVersion.Value(),
		ClientAuth: c.ClientAuth.Value(),
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			return c.loadCertificate()
		},
		PreferServerCipherSuites: true,
	}

	if c.ClientCAs != "" {
		if cfg.ClientAuth == tls.NoClientCert {
			return nil, errors.New("tls: client CAs set but client_auth_type is not set")
		}

		pool := x509.NewCertPool()
		cas, err := ioutil.ReadFile(c.ClientCAs)
		if err != nil {
			return nil, err
		}
		pool.AppendCertsFromPEM(cas)
		cfg.ClientCAs = pool
	}

	if len(c.CipherSuites) > 0 {
		cfg.CipherSuites = make([]uint16, len(c.CipherSuites))
		for i, cs := range c.CipherSuites {
			cfg.CipherSuites[i] = cs.Value()
		}
	}

	return &cfg, nil
}

type TLSVersion uint16

var tlsVersions = map[string]TLSVersion{
	"TLS1.3": (TLSVersion)(tls.VersionTLS13),
	"TLS1.2": (TLSVersion)(tls.VersionTLS12),
	"TLS1.1": (TLSVersion)(tls.VersionTLS11),
	"TLS1.0": (TLSVersion)(tls.VersionTLS10),
}

func (tv *TLSVersion) Value() uint16 {
	return uint16(*tv)
}

func (tv *TLSVersion) UnmarshalText(data []byte) error {
	s := string(data)
	if v, ok := tlsVersions[s]; ok {
		*tv = v
		return nil
	}
	return fmt.Errorf("unknown TLS version: " + s)
}

func (tv *TLSVersion) MarshalText() (text []byte, err error) {
	for s, v := range tlsVersions {
		if *tv == v {
			return []byte(s), nil
		}
	}
	return nil, fmt.Errorf("unknown TLS version: %v", *tv)
}

type ClientAuthType tls.ClientAuthType

func (c *ClientAuthType) Value() tls.ClientAuthType {
	return (tls.ClientAuthType)(*c)
}

func (c *ClientAuthType) UnmarshalText(text []byte) error {
	switch string(text) {
	case "", "NoClientCert":
		*c = ClientAuthType(tls.NoClientCert)
	case "RequestClientCert":
		*c = ClientAuthType(tls.RequestClientCert)
	case "RequireAnyClientCert":
		*c = ClientAuthType(tls.RequireAnyClientCert)
	case "VerifyClientCertIfGiven":
		*c = ClientAuthType(tls.VerifyClientCertIfGiven)
	case "RequireAndVerifyClientCert":
		*c = ClientAuthType(tls.RequireAndVerifyClientCert)
	default:
		return errors.Errorf("invalid ClientAuthType: %s", text)
	}
	return nil
}

type CipherSuite uint16

func (c *CipherSuite) Value() uint16 {
	return uint16(*c)
}

func (c *CipherSuite) UnmarshalText(text []byte) error {
	s := string(text)
	for _, cs := range tls.CipherSuites() {
		if cs.Name == s {
			*c = (CipherSuite)(cs.ID)
			return nil
		}
	}
	return errors.Errorf("unknown cipher: %s", s)
}
