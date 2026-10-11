# Microservices reference backend

A small Go reference for a service-oriented backend: eight services, REST on
the edge, gRPC between the services, a transactional outbox with background
delivery, least-privilege database migrations and RFC 9457 problem responses.

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

Every service owns its database and its schema. No service reads the database
of another service. Public REST stays on fiber; all internal traffic is gRPC.
Every gRPC server serves the health service and reflection.

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
the invoice and the notifications.

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
```

The passwords in `deploy/postgres/init/00-roles.sql` and `.env.example` are
development values. Use real secrets in a real deployment.

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
- Internal gRPC uses insecure credentials on the local network. TLS and mTLS
  are the deployment step.
- The stack is single-node: one PostgreSQL instance holds eight databases
  with separate roles.
- There is no API gateway and no rate limiting. Add them at the edge.

## Development

```sh
make proto   # regenerate the protobuf code
make test    # unit and gRPC tests with fakes and ephemeral listeners
make vet
make build
```

## License

MIT. See [LICENSE](LICENSE).
