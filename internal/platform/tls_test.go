package platform

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/m1chlcz/microservices-reference/internal/certs"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func TestServerTLSFromEnv(t *testing.T) {
	clearTLSEnv(t)
	files, err := ServerTLSFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if files != (TLSFiles{}) {
		t.Fatalf("unset environment returned %#v", files)
	}
}

func TestServerTLSFromEnvRequiresEveryFile(t *testing.T) {
	t.Setenv("GRPC_TLS_CA", "ca.crt")
	t.Setenv("GRPC_TLS_SERVER_CERT", "")
	t.Setenv("GRPC_TLS_SERVER_KEY", "")
	if _, err := ServerTLSFromEnv(); err == nil {
		t.Fatal("partial server configuration accepted")
	}
	t.Setenv("GRPC_TLS_SERVER_CERT", "server.crt")
	t.Setenv("GRPC_TLS_SERVER_KEY", "server.key")
	files, err := ServerTLSFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if files.CA != "ca.crt" || files.Cert != "server.crt" || files.Key != "server.key" {
		t.Fatalf("files: %#v", files)
	}
}

func TestClientTLSFromEnvRequiresEveryFile(t *testing.T) {
	t.Setenv("GRPC_TLS_CA", "ca.crt")
	t.Setenv("GRPC_TLS_CLIENT_CERT", "client.crt")
	t.Setenv("GRPC_TLS_CLIENT_KEY", "")
	if _, err := ClientTLSFromEnv(); err == nil {
		t.Fatal("partial client configuration accepted")
	}
}

func TestGRPCMutualTLS(t *testing.T) {
	t.Parallel()
	authority, err := certs.GenerateCA("test CA", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	serverCertificate, err := authority.IssueServer("internal-grpc",
		[]string{"127.0.0.1", "localhost"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	clientCertificate, err := authority.IssueClient("test client", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	rogueAuthority, err := certs.GenerateCA("rogue CA", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	rogueCertificate, err := rogueAuthority.IssueClient("rogue client", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, certificate := range map[string]*certs.Certificate{
		"ca":          authority,
		"server":      serverCertificate,
		"client":      clientCertificate,
		"rogueClient": rogueCertificate,
	} {
		if err = certificate.WriteFiles(dir, name); err != nil {
			t.Fatal(err)
		}
	}
	caFile := filepath.Join(dir, "ca.crt")
	serverFiles := TLSFiles{
		CA:   caFile,
		Cert: filepath.Join(dir, "server.crt"),
		Key:  filepath.Join(dir, "server.key"),
	}
	clientFiles := TLSFiles{
		CA:   caFile,
		Cert: filepath.Join(dir, "client.crt"),
		Key:  filepath.Join(dir, "client.key"),
	}
	rogueFiles := TLSFiles{
		CA:   caFile,
		Cert: filepath.Join(dir, "rogueClient.crt"),
		Key:  filepath.Join(dir, "rogueClient.key"),
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewGRPCServer(slog.New(slog.NewTextHandler(io.Discard, nil)), serverFiles)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	address := listener.Addr().String()

	check := func(name string, connection *grpc.ClientConn) {
		t.Helper()
		response, checkErr := healthpb.NewHealthClient(connection).Check(
			context.Background(),
			&healthpb.HealthCheckRequest{},
		)
		if checkErr != nil {
			t.Fatalf("%s: %v", name, checkErr)
		}
		if response.Status != healthpb.HealthCheckResponse_SERVING {
			t.Fatalf("%s: status %v", name, response.Status)
		}
	}
	expectRejected := func(name string, connection *grpc.ClientConn) {
		t.Helper()
		if _, checkErr := healthpb.NewHealthClient(connection).Check(
			context.Background(),
			&healthpb.HealthCheckRequest{},
		); checkErr == nil {
			t.Fatalf("%s: connection accepted", name)
		}
	}

	plaintext, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = plaintext.Close() })
	expectRejected("plaintext client", plaintext)

	roots := x509.NewCertPool()
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatal(err)
	}
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("CA bundle did not parse")
	}
	withoutCertificate, err := grpc.NewClient(address,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    roots,
		})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = withoutCertificate.Close() })
	expectRejected("client without certificate", withoutCertificate)

	rogue, err := Dial(address, rogueFiles)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rogue.Close() })
	expectRejected("client of another authority", rogue)

	mutual, err := Dial(address, clientFiles)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mutual.Close() })
	check("client with certificate", mutual)
}

func clearTLSEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"GRPC_TLS_CA",
		"GRPC_TLS_SERVER_CERT",
		"GRPC_TLS_SERVER_KEY",
		"GRPC_TLS_CLIENT_CERT",
		"GRPC_TLS_CLIENT_KEY",
	} {
		t.Setenv(key, "")
	}
}
