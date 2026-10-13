# Changelog

All notable changes to this project are documented in this file. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the
project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Mutual TLS on the internal gRPC connections. `make certs` (or
  `go run ./cmd/certgen`) writes a development CA, a server certificate for
  every service name and a client certificate to `deploy/certs`. The
  `GRPC_TLS_CA`, `GRPC_TLS_SERVER_CERT`, `GRPC_TLS_SERVER_KEY`,
  `GRPC_TLS_CLIENT_CERT` and `GRPC_TLS_CLIENT_KEY` variables enable TLS per
  side; a configured server requires and verifies a client certificate, and a
  configured client verifies the server. Endpoints without the variables stay
  on plaintext.
- OpenTelemetry tracing of the REST edge and the internal gRPC calls, with
  W3C trace context propagation across both boundaries. The
  `OTEL_TRACES_EXPORTER` variable selects the `stdout` or `otlp` exporter;
  tracing is disabled by default.
- The gateway example in `cmd/gateway`: `POST /reservations` forwards to the
  inventory gRPC service and continues the caller trace. The compose stack
  and the smoke test run the gateway on `:8090`.
- A package that issues the development certificates (`internal/certs`) and
  tests for mutual TLS, trace propagation and the gateway.
