// Package certs issues the development certificate authority and the
// certificates that secure the internal gRPC transport. The certgen command
// writes them to disk for local runs; tests issue them in memory.
package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Certificate is an X.509 certificate and its private key.
type Certificate struct {
	leaf *x509.Certificate
	key  *ecdsa.PrivateKey
}

// GenerateCA returns a self-signed certificate authority. The authority signs
// every server and client certificate of the internal gRPC transport.
func GenerateCA(commonName string, lifetime time.Duration) (*Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate certificate authority key: %w", err)
	}
	serial, err := newSerialNumber()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               subject(commonName),
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(lifetime),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	leaf, err := createCertificate(template, template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	return &Certificate{leaf: leaf, key: key}, nil
}

// IssueServer returns a server certificate signed by the authority. Host
// entries that parse as IP addresses become IP subject alternative names;
// every other entry becomes a DNS subject alternative name.
func (ca *Certificate) IssueServer(commonName string, hosts []string, lifetime time.Duration) (*Certificate, error) {
	return ca.issue(commonName, hosts, lifetime, x509.ExtKeyUsageServerAuth)
}

// IssueClient returns a client certificate signed by the authority.
func (ca *Certificate) IssueClient(commonName string, lifetime time.Duration) (*Certificate, error) {
	return ca.issue(commonName, nil, lifetime, x509.ExtKeyUsageClientAuth)
}

// WriteFiles writes the certificate to dir/name.crt and its private key to
// dir/name.key. The directory must exist.
func (certificate *Certificate) WriteFiles(dir, name string) error {
	certificatePEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certificate.leaf.Raw,
	})
	keyDER, err := x509.MarshalECPrivateKey(certificate.key)
	if err != nil {
		return fmt.Errorf("marshal %s private key: %w", name, err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyDER,
	})
	if err = os.WriteFile(filepath.Join(dir, name+".crt"), certificatePEM, 0o644); err != nil {
		return fmt.Errorf("write %s certificate: %w", name, err)
	}
	if err = os.WriteFile(filepath.Join(dir, name+".key"), keyPEM, 0o600); err != nil {
		return fmt.Errorf("write %s private key: %w", name, err)
	}
	return nil
}

func (ca *Certificate) issue(
	commonName string,
	hosts []string,
	lifetime time.Duration,
	usage x509.ExtKeyUsage,
) (*Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate %s key: %w", commonName, err)
	}
	serial, err := newSerialNumber()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      subject(commonName),
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(lifetime),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
	}
	for _, host := range hosts {
		if ip := net.ParseIP(host); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
			continue
		}
		template.DNSNames = append(template.DNSNames, host)
	}
	leaf, err := createCertificate(template, ca.leaf, &key.PublicKey, ca.key)
	if err != nil {
		return nil, err
	}
	return &Certificate{leaf: leaf, key: key}, nil
}

func createCertificate(
	template *x509.Certificate,
	parent *x509.Certificate,
	publicKey any,
	signer *ecdsa.PrivateKey,
) (*x509.Certificate, error) {
	der, err := x509.CreateCertificate(rand.Reader, template, parent, publicKey, signer)
	if err != nil {
		return nil, fmt.Errorf("create certificate: %w", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("parse created certificate: %w", err)
	}
	return leaf, nil
}

func subject(commonName string) pkix.Name {
	return pkix.Name{
		CommonName:   commonName,
		Organization: []string{"microservices-reference development"},
	}
}

func newSerialNumber() (*big.Int, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate serial number: %w", err)
	}
	return serial, nil
}
