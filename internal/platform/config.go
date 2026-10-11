// Package platform holds the small building blocks that every service shares:
// configuration, database access and the HTTP server.
package platform

import (
	"fmt"
	"os"
	"strings"
	"time"
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

// EnvDuration returns the duration in key. It returns the fallback when the
// value is empty or invalid.
func EnvDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
