# gRPC internal communication design

Date: 2026-10-11

## Context

The reference backend currently talks over HTTP everywhere: public REST on
every service, synchronous internal calls (orders to inventory, orders to
payments) and outbox event delivery to `/events` endpoints. Internal REST
gives no contract, no codegen and no compile-time map of the calls.

Goal: replace every internal HTTP call with one contract-first stack, keep
the public REST edge, and keep the system runnable with `docker compose`.

## Decision

Use gRPC with Protobuf (grpc-go) for all internal communication. The
`.proto` files become the code map: peers generate clients from the same
contract, and a breaking change fails at compile time.

Alternatives considered:

- Connect (Buf): the same proto-first idea with a net/http transport and a
  JSON-friendly wire format. Rejected for now: less ecosystem momentum and a
  weaker signal than the gRPC name.
- NATS JetStream: the standard broker choice for event-driven systems.
  Rejected for now: it adds infrastructure without defining synchronous
  contracts. The transactional outbox already gives durable delivery, so a
  broker stays an optional future step.

## Architecture

Every service keeps its public HTTP listener (fiber, ports 8081 to 8088) and
adds one internal gRPC listener (ports 9081 to 9088).

```
 client ── REST ──► orders / inventory / payments / ...   (public, fiber)
                       │            ▲
                       │ gRPC       │ gRPC
                       ▼            │
                 inventory :9082  payments :9086
                       ▲
                       │ gRPC EventSink.Deliver
              outbox relays ──► notifications / shipping / invoicing / orders
```

Public surfaces that stay HTTP: the REST API, the provider payment webhook
and the carrier webhook. They simulate external callers.

## Protobuf contracts

Generated `*.pb.go` files are committed next to the protos, so builds and CI
need no protoc. `make proto` regenerates them and CI fails when a
regeneration produces a diff.

```
api/events/v1/events.proto
  service EventSink { rpc Deliver(Event) returns (DeliverResponse); }
  message Event { string kind = 1; bytes data = 2; }

api/inventory/v1/inventory.proto
  service Inventory { rpc Reserve(ReserveRequest) returns (ReserveResponse); }
  message ReservationItem { string sku = 1; int32 quantity = 2; }
  message ReserveRequest { repeated ReservationItem items = 1; }
  message ReserveResponse { string reservation_id = 1; }

api/payments/v1/payments.proto
  service Payments { rpc CreateIntent(CreateIntentRequest) returns (CreateIntentResponse); }
  message CreateIntentRequest { string order_id = 1; int64 amount_minor = 2; string currency = 3; string idempotency_key = 4; }
  message CreateIntentResponse { string intent_id = 1; string provider_ref = 2; bool duplicate = 3; }
```

Business errors travel as gRPC status codes: `InvalidArgument` for 422-class
input, `NotFound` for 404-class lookups, `AlreadyExists` or `FailedPrecondition`
for conflict-class answers, `Unavailable` for transport trouble. The callers
map the codes back to their existing domain errors.

## Shared bootstrap

`internal/platform/grpc.go` provides:

- `NewGRPCServer(logger)` with the gRPC health service, reflection and a
  unary logging interceptor.
- `RunGRPC(ctx, server, addr, logger)` for graceful stop.
- `Dial(target)` returning a client connection with per-call deadlines set by
  the callers (five seconds, like today).

The local stack uses insecure credentials. TLS is a deployment concern and
stays out of the reference.

## Event delivery

The outbox relays switch from HTTP POST to the generated `EventSink` client.
A route maps an event kind to one or more gRPC targets. A delivery succeeds
only when every target answers OK. A failed target leaves the job pending, so
pgoutbox retries it with backoff. Consumers are already idempotent, so a
repeated delivery is safe.

The `/events` HTTP endpoints and the HTTP delivery code are removed.

## What is removed

- `internal/orders/client.go` (HTTP inventory client) becomes a gRPC client.
- The payments HTTP client used by checkout becomes a gRPC client.
- The HTTP `POST /events` endpoints in notifications, shipping, invoicing and
  orders.
- The HTTP delivery inside `internal/eventrelay`.

## What stays

- Public REST on fiber, including all current endpoints.
- Provider and carrier webhooks.
- The transactional outbox, migrations and problem responses.

## Testing

- New gRPC tests use `google.golang.org/grpc/test/bufconn`, so they run
  in-process with no ports.
- The existing fake-store handler tests stay untouched.
- The end-to-end check runs `docker compose up --build`, the smoke script and
  `docker compose down`.

## Pending integration work folded into this change

The five services added last (identity, catalog, payments, shipping,
invoicing) still need their wiring: migration accessors and migrate targets,
provisioning SQL, compose services, `.env.example`, README and the smoke
script. The orders checkout also gains the payment intent call, the
`payment.captured` intake and the `order.paid` event. All of this lands in
the same change.

## Risks

- grpc-go and protobuf add the heaviest dependencies in the module. Accepted
  for the contract and the portfolio signal.
- Addresses in the environment become gRPC targets (`host:port`) for internal
  traffic, which changes the compose wiring.
- Committed generated code can drift from the protos. The CI regeneration
  check keeps them honest.
