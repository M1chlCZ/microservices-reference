// Package platform holds the small building blocks that every service shares:
// configuration, database access and the HTTP server.
package platform

import (
	"fmt"
	"os"
	"strings"
)

// RequireEnv returns the value of key. It fails when the value is empty.
func RequireEnv(key string) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return "", fmt.Errorf("environment variable %s is required", key)
	}
	return value, nil
}

// EnvOr returns the value of key. It returns the fallback when the value is
// empty.
func EnvOr(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

// WorkerID returns a process identifier for an outbox worker. It combines the
// host name and the process ID, and uses name when the host name is not
// available.
func WorkerID(name string) string {
	host, err := os.Hostname()
	if err != nil {
		host = name
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}
