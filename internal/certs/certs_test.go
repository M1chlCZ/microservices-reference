package certs

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIssueCertificates(t *testing.T) {
	t.Parallel()
	authority, err := GenerateCA("test CA", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	server, err := authority.IssueServer("internal-grpc", []string{"inventory", "127.0.0.1"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	client, err := authority.IssueClient("test-client", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, certificate := range map[string]*Certificate{
		"ca":     authority,
		"server": server,
		"client": client,
	} {
		if err = certificate.WriteFiles(dir, name); err != nil {
			t.Fatal(err)
		}
	}

	verify := func(name string, keyUsage x509.ExtKeyUsage, dnsName string) {
		t.Helper()
		pair, err := tls.LoadX509KeyPair(
			filepath.Join(dir, name+".crt"),
			filepath.Join(dir, name+".key"),
		)
		if err != nil {
			t.Fatal(err)
		}
		leaf, err := x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			t.Fatal(err)
		}
		caPEM, err := os.ReadFile(filepath.Join(dir, "ca.crt"))
		if err != nil {
			t.Fatal(err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(caPEM) {
			t.Fatal("CA bundle did not parse")
		}
		if _, err = leaf.Verify(x509.VerifyOptions{
			DNSName:   dnsName,
			Roots:     roots,
			KeyUsages: []x509.ExtKeyUsage{keyUsage},
		}); err != nil {
			t.Fatalf("verify %s: %v", name, err)
		}
	}
	verify("server", x509.ExtKeyUsageServerAuth, "inventory")
	verify("server", x509.ExtKeyUsageServerAuth, "127.0.0.1")
	verify("client", x509.ExtKeyUsageClientAuth, "")
}

func TestServerCertificateRejectsClientUsage(t *testing.T) {
	t.Parallel()
	authority, err := GenerateCA("test CA", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	server, err := authority.IssueServer("internal-grpc", []string{"localhost"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if server.leaf.CheckSignatureFrom(authority.leaf) != nil {
		t.Fatal("server certificate is not signed by the authority")
	}
	if server.leaf.IsCA {
		t.Fatal("leaf certificate marked as a CA")
	}
	allowed := false
	for _, usage := range server.leaf.ExtKeyUsage {
		if usage == x509.ExtKeyUsageClientAuth {
			allowed = true
		}
	}
	if allowed {
		t.Fatal("server certificate allows client authentication")
	}
}
