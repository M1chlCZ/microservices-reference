# Microservices reference backend

A small Go reference that shows how a service-oriented backend looks: three
independent services, a synchronous call with compensation, a transactional
outbox with background delivery, RFC 9457 problem responses, and
least-privilege database migrations.

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
   │  POST /orders
   ▼
 orders ──────────── POST /reservations ────────────► inventory
   │  ▲                                                   │
   │  └────────────── DELETE /reservations/{id} ──────────┘
   │                    (compensation)
   │
   ├── orders_db: orders, order_items, outbox_jobs
   │
   └── outbox relay (pgoutbox worker)
         │  POST /events
         ▼
      notifications ──── notifications_db: notifications
```

| Service | Address | Database | Responsibility |
| --- | --- | --- | --- |
| `orders` | `:8081` | `orders_db` | Order API, stock reservation, outbox relay |
| `inventory` | `:8082` | `inventory_db` | Stock positions and reservations |
| `notifications` | `:8083` | `notifications_db` | Records delivered events |

Each service owns its database and its schema. No service reads the database
of another service.

## Endpoints

| Service | Method and path | Purpose |
| --- | --- | --- |
| orders | `POST /orders` | Create an order |
| orders | `GET /orders/{id}` | Read one order |
| inventory | `GET /stock/{sku}` | Read one stock position |
| inventory | `POST /reservations` | Reserve stock for every item at once |
| inventory | `DELETE /reservations/{id}` | Release a reservation |
| notifications | `POST /events` | Record one event |
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

`make smoke` creates an order, reads it back, checks the stock conflict path
and confirms that the notification arrived through the outbox relay.

## Local development

Start PostgreSQL, then run the provisioning script, the migrations and the
services:

```sh
psql -h localhost -U postgres -f deploy/postgres/init/00-roles.sql

export ORDERS_MIGRATOR_URL=postgres://orders_migrator:orders-migrator-dev@localhost:5432/orders_db
export INVENTORY_MIGRATOR_URL=postgres://inventory_migrator:inventory-migrator-dev@localhost:5432/inventory_db
export NOTIFICATIONS_MIGRATOR_URL=postgres://notifications_migrator:notifications-migrator-dev@localhost:5432/notifications_db
go run ./cmd/migrate

export INVENTORY_DATABASE_URL=postgres://inventory_app:inventory-app-dev@localhost:5432/inventory_db
export ORDERS_DATABASE_URL=postgres://orders_app:orders-app-dev@localhost:5432/orders_db
export NOTIFICATIONS_DATABASE_URL=postgres://notifications_app:notifications-app-dev@localhost:5432/notifications_db
export INVENTORY_URL=http://localhost:8082
export NOTIFICATIONS_URL=http://localhost:8083

go run ./cmd/inventory &
go run ./cmd/notifications &
go run ./cmd/orders &
```

The passwords in `deploy/postgres/init/00-roles.sql` and `.env.example` are
development values. Use real secrets in a real deployment.

## Patterns

### Synchronous call with compensation

`orders` reserves stock in `inventory` before it stores the order. When the
order does not commit, `orders` releases the reservation again. This is a
small saga with one compensation step. A production system adds a saga log
and retries the compensation.

### Transactional outbox

The order row, its lines and the `order.created` event commit in one database
transaction. The event lives in the `outbox_jobs` table of `orders_db`. The
`orders` service runs a pgoutbox worker that claims due events, posts them to
the notifications service and completes or retries them with backoff.

### Least-privilege migrations

Every database has three roles: a `*_owner` role that owns the objects, a
`*_migrator` login that applies migrations, and a `*_app` login that serves
traffic. pgmig verifies this separation before it applies anything, so a
compromised application login cannot change the schema.

### Problem responses

Every error leaves the stack as an RFC 9457 problem document. The middleware
adds a request ID, logs access, recovers panics and limits the body size.

## Design notes

- The outbox relay runs inside the `orders` service, because the outbox table
  belongs to that service. A separate relay deployment needs its own worker
  identity and is a deployment choice, not a code change.
- The event payload is small: `orderId`, `customerRef` and `totalMinor`.
  Consumers that need more data call the orders API.
- The reference has no authentication, no API gateway and no rate limiting.
  Add them at the edge.
- The services log with `log/slog` in JSON. Every response carries
  `Cache-Control: no-store`.

## Development

```sh
make test    # unit tests with fake stores
make vet
make build
```

The tests cover the handlers and the domain rules with in-memory stores. The
PostgreSQL stores stay thin: transactions, constraints and SQL only.

## License

MIT. See [LICENSE](LICENSE).
