package platform

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	"google.golang.org/grpc/credentials"
)

const minTLSVersion = tls.VersionTLS12

// TLSFiles names the PEM files that secure one side of the internal gRPC
// transport: the CA bundle that verifies the peer, the local certificate and
// its private key.
type TLSFiles struct {
	CA   string
	Cert string
	Key  string
}

// ServerTLSFromEnv reads the server side of the internal gRPC TLS
// configuration. GRPC_TLS_CA, GRPC_TLS_SERVER_CERT and GRPC_TLS_SERVER_KEY
// must be set together; when none of them is set the server keeps serving
// plaintext.
func ServerTLSFromEnv() (TLSFiles, error) {
	return tlsFilesFromEnv("GRPC_TLS_SERVER_CERT", "GRPC_TLS_SERVER_KEY")
}

// ClientTLSFromEnv reads the client side of the internal gRPC TLS
// configuration. GRPC_TLS_CA, GRPC_TLS_CLIENT_CERT and GRPC_TLS_CLIENT_KEY
// must be set together; when none of them is set the client keeps dialing
// plaintext.
func ClientTLSFromEnv() (TLSFiles, error) {
	return tlsFilesFromEnv("GRPC_TLS_CLIENT_CERT", "GRPC_TLS_CLIENT_KEY")
}

func tlsFilesFromEnv(certEnv, keyEnv string) (TLSFiles, error) {
	files := TLSFiles{
		CA:   strings.TrimSpace(os.Getenv("GRPC_TLS_CA")),
		Cert: strings.TrimSpace(os.Getenv(certEnv)),
		Key:  strings.TrimSpace(os.Getenv(keyEnv)),
	}
	if files.CA == "" && files.Cert == "" && files.Key == "" {
		return TLSFiles{}, nil
	}
	if files.CA == "" || files.Cert == "" || files.Key == "" {
		return TLSFiles{}, fmt.Errorf("GRPC_TLS_CA, %s and %s must be set together", certEnv, keyEnv)
	}
	return files, nil
}

// serverCredentials returns the transport credentials of a gRPC server. The
// server requires and verifies a client certificate against the CA bundle. It
// returns nil when no certificate is configured, which keeps the server on
// plaintext.
func (files TLSFiles) serverCredentials() (credentials.TransportCredentials, error) {
	if files == (TLSFiles{}) {
		return nil, nil
	}
	certificate, pool, err := files.load()
	if err != nil {
		return nil, err
	}
	return credentials.NewTLS(&tls.Config{
		MinVersion:   minTLSVersion,
		Certificates: []tls.Certificate{certificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
	}), nil
}

// clientCredentials returns the transport credentials of a gRPC client. The
// client verifies the server certificate against the CA bundle and presents
// its own certificate. It returns nil when no certificate is configured,
// which keeps the client on plaintext.
func (files TLSFiles) clientCredentials() (credentials.TransportCredentials, error) {
	if files == (TLSFiles{}) {
		return nil, nil
	}
	certificate, pool, err := files.load()
	if err != nil {
		return nil, err
	}
	return credentials.NewTLS(&tls.Config{
		MinVersion:   minTLSVersion,
		Certificates: []tls.Certificate{certificate},
		RootCAs:      pool,
	}), nil
}

func (files TLSFiles) load() (tls.Certificate, *x509.CertPool, error) {
	certificate, err := tls.LoadX509KeyPair(files.Cert, files.Key)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("load certificate %s: %w", files.Cert, err)
	}
	caPEM, err := os.ReadFile(files.CA)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("read CA bundle: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return tls.Certificate{}, nil, fmt.Errorf("parse CA bundle %s: no certificates found", files.CA)
	}
	return certificate, pool, nil
}
