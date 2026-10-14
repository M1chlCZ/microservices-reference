# Changelog

All notable changes to this project are documented in this file. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the
project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.3.0] - 2026-10-14

### Added

- Kubernetes manifests in `deploy/k8s`: a kustomize base with deployments,
  services, a Caddy path router and a Cloudflare Tunnel, the `grpc-tls` and
  `hpa` components, a migration job, and `primary` and `standby` overlays
  that run the same stack at two levels.
- The CloudNativePG cluster in `deploy/k8s/postgres`: three PostgreSQL
  instances with synchronous replication and automatic failover.
- Network policies: ingress denied by default, in-namespace traffic, DNS,
  the database port and outbound HTTPS and QUIC only.
- The edge steering Worker in `deploy/cloudflare`: it probes the active
  cluster every minute, wakes the standby through a GitHub workflow after two
  consecutive failures, switches traffic, alerts a webhook and fails back
  after recovery.
- `images.yml` builds one image per service on every version tag;
  `wake-standby.yml` scales the standby cluster through the
  `STANDBY_KUBE_CONFIG_B64` secret.
- [ARCHITECTURE.md](ARCHITECTURE.md): the topology, the failure scenarios,
  the runbooks and the limits of the deployment.

## [0.2.0] - 2026-10-14

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
