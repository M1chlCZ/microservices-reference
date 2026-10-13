# Microservices reference backend

A small Go reference for a service-oriented backend: eight services, REST on
the edge, gRPC between the services with optional mutual TLS, a transactional
outbox with background delivery, OpenTelemetry tracing, least-privilege
database migrations and RFC 9457 problem responses.

The stack composes three of my published open-source packages:

- [pgmig](https://github.com/m1chlcz/pgmig) applies the database migrations
  with a separate migrator identity.
- [pgoutbox](https://github.com/m1chlcz/pgoutbox) claims and delivers outbox
  events with retries and leases.
- [fiber-problem](https://github.com/m1chlcz/fiber-problem) renders every
  error as an `application/problem+json` response.

## Architecture

```
 client
   │  REST (HTTP)
   ├──────────────► gateway :8090 ──gRPC──► inventory :9082
   ▼
 orders :8081 ──gRPC──► inventory :9082
   │      ──gRPC──► payments  :9086
   │
   ├── orders_db: orders, order_items, outbox_jobs
   │
   └── outbox relay (pgoutbox worker, gRPC EventSink)
         ├── order.created ──► notifications :9083
         └── order.paid    ──► notifications :9083
                               shipping      :9087
                               invoicing     :9088

 payments  ── payment.captured ──► orders
 shipping  ── shipment.created ──► notifications
 invoicing ── invoice.issued   ──► notifications
```

| Service | REST | gRPC | Database | Responsibility |
| --- | --- | --- | --- | --- |
| orders | 8081 | 9081 | `orders_db` | Checkout, order state, outbox relay |
| inventory | 8082 | 9082 | `inventory_db` | Stock positions and reservations |
| notifications | 8083 | 9083 | `notifications_db` | Event log |
| identity | 8084 | 9084 | `identity_db` | Accounts, sessions, TOTP |
| catalog | 8085 | 9085 | `catalog_db` | Products and status |
| payments | 8086 | 9086 | `payments_db` | Payment intents, provider webhook |
| shipping | 8087 | 9087 | `shipping_db` | Shipments, carrier webhook |
| invoicing | 8088 | 9088 | `invoicing_db` | Invoice numbering and records |
| gateway | 8090 | — | — | Gateway example: REST to inventory gRPC |

Every service owns its database and its schema. No service reads the database
of another service. Public REST stays on fiber; all internal traffic is gRPC.
Every gRPC server serves the health service and reflection. The gateway
example owns no database; it only calls the inventory service.

## gRPC contracts

| Proto | Service | Caller |
| --- | --- | --- |
| `api/events/v1/events.proto` | `EventSink.Deliver` | Every outbox relay |
| `api/inventory/v1/inventory.proto` | `Inventory.Reserve`, `Inventory.Release` | orders |
| `api/payments/v1/payments.proto` | `Payments.CreateIntent` | orders |

Generated code is committed, so builds need no protoc. `make proto`
regenerates it, and CI fails when the generated code is not current. Debug a
running stack with grpcurl:

```sh
grpcurl -plaintext localhost:9082 grpc.health.v1.Health/Check
grpcurl -plaintext -d '{"items":[{"sku":"WIDGET-001","quantity":1}]}' \
  localhost:9082 inventory.v1.Inventory/Reserve
```

When mTLS is configured, replace `-plaintext` with the client certificate:

```sh
grpcurl -cacert deploy/certs/ca.crt \
  -cert deploy/certs/client.crt -key deploy/certs/client.key \
  localhost:9082 grpc.health.v1.Health/Check
```

## Checkout flow

1. `POST /orders` reserves stock in inventory (gRPC), creates a payment
   intent in payments (gRPC, idempotency key is the order ID) and stores the
   order as `pending_payment` with the `order.created` event in one
   transaction.
2. The relay delivers `order.created` to notifications.
3. The provider webhook on payments captures the intent and emits
   `payment.captured`.
4. The relay delivers `payment.captured` to orders. Orders marks the order
   `paid` and emits `order.paid` in the same transaction.
5. The relay delivers `order.paid` to shipping (creates a shipment),
   invoicing (issues a numbered invoice) and notifications.
6. `shipment.created` and `invoice.issued` reach notifications through their
   own relays.

Compensation: when the payment intent or the order insert fails, orders
releases the reservation again.

## Endpoints

| Service | Method and path | Purpose |
| --- | --- | --- |
| identity | `POST /accounts` | Create an account |
| identity | `POST /sessions` | Log in, returns a session token |
| identity | `GET /sessions/me` | Read the account of a bearer token |
| identity | `DELETE /sessions` | Log out |
| identity | `POST /accounts/totp` | Start TOTP enrollment |
| identity | `POST /accounts/totp/verify` | Confirm a TOTP code |
| catalog | `POST /products` | Create a draft product |
| catalog | `GET /products?limit=20&cursor=` | List active products |
| catalog | `GET /products/{id}` | Read one product |
| catalog | `PATCH /products/{id}` | Update name, price or status |
| orders | `POST /orders` | Create an order |
| orders | `GET /orders/{id}` | Read one order |
| inventory | `GET /stock/{sku}` | Read one stock position |
| inventory | `POST /reservations` | Reserve stock for every item at once |
| inventory | `DELETE /reservations/{id}` | Release a reservation |
| payments | `POST /payment-intents` | Create an intent (idempotency key) |
| payments | `POST /webhooks/provider` | Provider callback, captures the intent |
| payments | `GET /payment-intents/{id}` | Read one intent |
| shipping | `POST /webhooks/carrier` | Carrier callback, updates the status |
| shipping | `GET /shipments/{orderId}` | Read the shipment of an order |
| invoicing | `GET /invoices/{orderId}` | Read the invoice of an order |
| notifications | `GET /notifications?limit=20` | List recent events |
| gateway | `POST /reservations` | Reserve stock through the inventory gRPC service |
| all | `GET /healthz` | Liveness check |

Example:

```sh
curl -X POST http://localhost:8081/orders \
  -H 'Content-Type: application/json' \
  -d '{"customerRef":"demo","lines":[{"sku":"WIDGET-001","quantity":2,"unitPriceMinor":1999}]}'
```

## Quick start

```sh
docker compose up --build -d
make smoke
docker compose down
```

`make smoke` creates an order, checks the stock conflict path, captures the
payment through the provider webhook, and reads the paid order, the shipment,
the invoice and the notifications. It ends with one reservation through the
gateway example. The stack runs plaintext and untraced; see Certificates and
Tracing to switch the internal connections to mTLS and to export spans.

## Local development

Start PostgreSQL, then run the provisioning script, the migrations and the
services with the values from `.env.example`:

```sh
psql -h localhost -U postgres -f deploy/postgres/init/00-roles.sql

export $(grep -v '^#' .env.example | xargs)
go run ./cmd/migrate

go run ./cmd/inventory & go run ./cmd/notifications & go run ./cmd/orders &
go run ./cmd/identity & go run ./cmd/catalog & go run ./cmd/payments &
go run ./cmd/shipping & go run ./cmd/invoicing &

go run ./cmd/gateway &   # http://localhost:8090, calls INVENTORY_GRPC
```

The passwords in `deploy/postgres/init/00-roles.sql` and `.env.example` are
development values. Use real secrets in a real deployment.

## Certificates

The internal gRPC connections run plaintext by default. mTLS needs a
certificate authority, a server certificate and a client certificate.
Generate the development set with:

```sh
make certs
```

`make certs` runs `go run ./cmd/certgen -dir deploy/certs`, which writes:

| File | Contents |
| --- | --- |
| `ca.crt`, `ca.key` | Development certificate authority, valid for 10 years |
| `server.crt`, `server.key` | Server certificate, valid for 1 year |
| `client.crt`, `client.key` | Client certificate, valid for 1 year |

The server certificate carries `localhost`, `127.0.0.1`, `::1` and every
service name as subject alternative names, and its extended key usage allows
TLS server authentication. The client certificate allows client
authentication. Both are signed by the development CA. Pass `-hosts` to add
names, for example a LAN address.

`deploy/certs` is excluded from git and from the Docker build. The files are
development secrets: never commit or ship them.

## mTLS configuration

Every service reads the same variables:

| Variable | Used by | Purpose |
| --- | --- | --- |
| `GRPC_TLS_CA` | server and client | CA bundle that verifies the peer |
| `GRPC_TLS_SERVER_CERT`, `GRPC_TLS_SERVER_KEY` | gRPC server | Local server certificate |
| `GRPC_TLS_CLIENT_CERT`, `GRPC_TLS_CLIENT_KEY` | gRPC client | Local client certificate |

A gRPC server needs the CA bundle and the server pair; a gRPC client needs the
CA bundle and the client pair. When one variable of a side is set, all
variables of that side are required and a partial configuration stops the
service at startup. When none is set, the endpoint stays on plaintext. There
is no mode that skips verification: a TLS server requires a client certificate
signed by the CA, and a TLS client verifies the server certificate, its
extended key usage and its host name.

Local example, with PostgreSQL and the migrations from Local development in
place:

```sh
make certs
export $(grep -v '^#' .env.example | xargs)
export GRPC_TLS_CA=deploy/certs/ca.crt
export GRPC_TLS_SERVER_CERT=deploy/certs/server.crt
export GRPC_TLS_SERVER_KEY=deploy/certs/server.key
export GRPC_TLS_CLIENT_CERT=deploy/certs/client.crt
export GRPC_TLS_CLIENT_KEY=deploy/certs/client.key

go run ./cmd/inventory &   # serves mTLS on :9082
go run ./cmd/gateway &     # dials inventory with its client certificate
```

A service that both serves and dials gRPC, such as orders, uses the server
pair for its own listener and the client pair for its outgoing calls; export
all five variables for such a service. In compose, mount the certificates as a
read-only volume and set the variables per service:

```yaml
    volumes:
      - ./deploy/certs:/certs:ro
    environment:
      GRPC_TLS_CA: /certs/ca.crt
      GRPC_TLS_SERVER_CERT: /certs/server.crt
      GRPC_TLS_SERVER_KEY: /certs/server.key
      GRPC_TLS_CLIENT_CERT: /certs/client.crt
      GRPC_TLS_CLIENT_KEY: /certs/client.key
```

Set only the variables of the roles the service uses. The smoke test needs no
changes when the whole stack runs over mTLS.

## Tracing

Set `OTEL_TRACES_EXPORTER` to export spans; without it the services run
untraced and all tracing calls are no-ops.

| Variable | Values | Default |
| --- | --- | --- |
| `OTEL_TRACES_EXPORTER` | `none`, `stdout`, `otlp` | `none` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP collector address | `https://localhost:4317` |
| `OTEL_SERVICE_NAME` | Resource service name | The service name |

The instrumented boundaries:

- The REST edge starts a server span for every request, named after the method
  and the path, for example `POST /orders`.
- Every outgoing gRPC call starts a client span named after the full method,
  for example `inventory.v1.Inventory/Reserve`, and injects the W3C trace
  context into the call metadata.
- Every gRPC server starts a server span for the same method and extracts the
  caller context, so one trace crosses the HTTP and gRPC boundaries.

`stdout` prints the spans as JSON to standard output. `otlp` exports them over
gRPC to the collector from `OTEL_EXPORTER_OTLP_ENDPOINT` and reads the other
standard `OTEL_EXPORTER_OTLP_*` variables. A local collector without TLS needs
for example `OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317`.

```sh
OTEL_TRACES_EXPORTER=stdout go run ./cmd/gateway
```

## Gateway example

`cmd/gateway` is a small edge service with one endpoint. It owns no database
and no gRPC service; it forwards `POST /reservations` to the inventory service
over the internal gRPC connection, so the mTLS and tracing configuration of
the stack applies unchanged:

```
client ──REST──► gateway :8090 ──gRPC──► inventory :9082
```

```sh
curl -X POST http://localhost:8090/reservations \
  -H 'Content-Type: application/json' \
  -d '{"items":[{"sku":"WIDGET-001","quantity":1}]}'
```

The handler passes the request context to the gRPC call, so the trace of the
edge request continues in the internal service (see Tracing). Run it locally
with `make gateway`; the target dials `INVENTORY_GRPC` (`localhost:9082` by
default). The compose stack runs the gateway on `:8090` and `make smoke` calls
it after the checkout flow. The gateway is an example, not a full API gateway:
it has no routing, no rate limiting and no authentication.

## Patterns

- **Contract-first gRPC**: the `.proto` files are the code map. Peers generate
  clients from the same contract, and a breaking change fails at compile
  time. Generated code is committed and CI checks it is current.
- **Transactional outbox**: state changes and their events commit in one
  database transaction. A pgoutbox worker claims due events, delivers them
  over gRPC and retries failures with backoff. Consumers are idempotent, so
  at-least-once delivery is safe.
- **Synchronous saga with compensation**: checkout reserves stock and creates
  a payment intent before it commits the order. A failed later step releases
  the reservation.
- **Least-privilege migrations**: every database has an owner role, a
  migrator login and an app login. pgmig verifies this separation before it
  applies anything.
- **Problem responses**: every REST error leaves the stack as an RFC 9457
  problem document with a request ID.

## Design notes

- The outbox relay runs inside the service that owns the outbox table. A
  broker such as NATS or Kafka would replace the relay and keep the outbox.
- Internal gRPC runs plaintext by default. Setting the certificate variables
  switches both sides to mTLS with required client certificates, and no
  setting skips verification.
- The stack is single-node: one PostgreSQL instance holds eight databases
  with separate roles.
- The gateway example is deliberately small: one endpoint, no routing, no
  rate limiting. Add a real gateway and authentication at the edge.

## Development

```sh
make proto   # regenerate the protobuf code
make certs   # write the development CA, server and client certificates
make gateway # run the gateway example against INVENTORY_GRPC
make test    # unit and gRPC tests with fakes and ephemeral listeners
make vet
make build
```

## License

MIT. See [LICENSE](LICENSE).
