// Command certgen writes the development certificates of the internal gRPC
// transport: a certificate authority, one server certificate for every
// service name and one client certificate.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/m1chlcz/microservices-reference/internal/certs"
)

const (
	caLifetime   = 10 * 365 * 24 * time.Hour
	leafLifetime = 365 * 24 * time.Hour
)

var defaultHosts = []string{
	"localhost",
	"127.0.0.1",
	"::1",
	"orders",
	"inventory",
	"notifications",
	"identity",
	"catalog",
	"payments",
	"shipping",
	"invoicing",
	"gateway",
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "certgen:", err)
		os.Exit(1)
	}
}

func run() error {
	dir := flag.String("dir", filepath.Join("deploy", "certs"), "directory for the PEM files")
	hosts := flag.String("hosts", strings.Join(defaultHosts, ","),
		"comma separated server names and IP addresses")
	flag.Parse()

	authority, err := certs.GenerateCA("microservices-reference development CA", caLifetime)
	if err != nil {
		return err
	}
	server, err := authority.IssueServer("internal-grpc", splitHosts(*hosts), leafLifetime)
	if err != nil {
		return err
	}
	client, err := authority.IssueClient("internal-grpc-client", leafLifetime)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(*dir, 0o755); err != nil {
		return fmt.Errorf("create certificate directory: %w", err)
	}
	if err = authority.WriteFiles(*dir, "ca"); err != nil {
		return err
	}
	if err = server.WriteFiles(*dir, "server"); err != nil {
		return err
	}
	if err = client.WriteFiles(*dir, "client"); err != nil {
		return err
	}
	printUsage(*dir)
	return nil
}

func splitHosts(value string) []string {
	parts := strings.Split(value, ",")
	hosts := make([]string, 0, len(parts))
	for _, part := range parts {
		if host := strings.TrimSpace(part); host != "" {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func printUsage(dir string) {
	ca := filepath.Join(dir, "ca.crt")
	fmt.Printf("certificates written to %s\n\n", dir)
	fmt.Printf("grpc server:\n  export GRPC_TLS_CA=%s\n  export GRPC_TLS_SERVER_CERT=%s\n  export GRPC_TLS_SERVER_KEY=%s\n\n",
		ca, filepath.Join(dir, "server.crt"), filepath.Join(dir, "server.key"))
	fmt.Printf("grpc client:\n  export GRPC_TLS_CA=%s\n  export GRPC_TLS_CLIENT_CERT=%s\n  export GRPC_TLS_CLIENT_KEY=%s\n",
		ca, filepath.Join(dir, "client.crt"), filepath.Join(dir, "client.key"))
}
