# Architecture

This document describes the reference in two parts: the system architecture
of the services and their models, and the reliable-operation architecture that
runs them on Kubernetes with failover. The compose stack stays the quickest
way to run everything on one machine; the deployment part appends
cluster-level availability to the same services and models.

## System architecture

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

- Eight services, each with its own database and schema. No service reads the
  database of another service.
- REST on the edge with fiber and RFC 9457 problem responses. All internal
  traffic is gRPC with optional mutual TLS and W3C trace context propagation.
- A transactional outbox per producer: state changes and their events commit
  in one transaction, and pgoutbox relays deliver them with retries.
- The checkout saga: orders reserves stock and creates a payment intent
  before it commits the order; a failed later step releases the reservation.
- The gateway example forwards REST to the inventory gRPC service and owns no
  database.

### Data ownership

The databases are not shared. Each service reads and writes only its own
database, and no database connects to another database. Data crosses a
service boundary in exactly two ways:

- A gRPC call carries the fields of the operation, for example the SKUs and
  quantities of a reservation or the order ID of a payment intent.
- An outbox event carries a kind and a small JSON payload with IDs and
  totals, for example `{orderId, customerRef, totalMinor}`.

Records link by ID, not by a foreign key across databases. The order stores
its reservation ID, and the payment intent, shipment and invoice are keyed by
the order ID. Every consumer writes its own row in its own database.

| Service | Database | Tables |
| --- | --- | --- |
| orders | `orders_db` | `orders`, `order_items`, `outbox_jobs` |
| inventory | `inventory_db` | `stock`, `reservations`, `reservation_items` |
| payments | `payments_db` | `payment_intents`, `outbox_jobs` |
| shipping | `shipping_db` | `shipments`, `outbox_jobs` |
| invoicing | `invoicing_db` | `invoice_sequences`, `invoices`, `outbox_jobs` |
| notifications | `notifications_db` | `notifications` |
| identity | `identity_db` | `accounts`, `sessions` |
| catalog | `catalog_db` | `products` |

```
  inventory_db ◄── reservation id ── orders_db ── order id ──► payments_db
                                        │
                                        ├── order id ──► shipping_db
                                        ├── order id ──► invoicing_db
                                        └── order id ──► notifications_db
```

### Request flow

A checkout request, from the edge to the databases. In the Kubernetes
deployment the Caddy router forwards each path to its service; here the
request reaches `orders` directly.

```
client ──POST /orders──► orders :8081

  1. orders ──Reserve (gRPC)──► inventory :9082
     SKUs and quantities. Inventory updates the stock and writes the
     reservation in one transaction and returns the reservation ID.

  2. orders ──CreateIntent (gRPC)──► payments :9086
     The idempotency key is the order ID. Payments writes the intent and
     returns its ID.

  3. orders, one transaction ──► orders_db
     INSERT orders, order_items and the order.created outbox event.

client ◄──201 pending_payment
```

The rest of the flow is asynchronous. Every arrow is at-least-once and every
consumer is idempotent.

```
orders relay ──order.created──► notifications :9083 ──► notifications_db

provider webhook ──► payments :9086
  one transaction: the intent becomes captured, payment.captured is enqueued

orders relay ──payment.captured──► orders :8081
  one transaction: the order becomes paid, order.paid is enqueued

orders relay ──order.paid──► shipping :9087    ──► shipments    ──► shipping_db
                           ──► invoicing :9088  ──► invoices     ──► invoicing_db
                           ──► notifications :9083 ──► notifications_db

shipping relay  ──shipment.created──► notifications :9083 ──► notifications_db
invoicing relay ──invoice.issued───► notifications :9083 ──► notifications_db
```

The gateway example is the short path into the same model:

```
client ──POST /reservations──► gateway :8090 ──Reserve (gRPC)──► inventory :9082
                                                 ──► inventory_db
```

The README documents the gRPC contracts, the endpoints, the checkout flow and
the local development setup in detail. This section is the summary the
deployment part builds on.

## Reliable operation

The deployment architecture appended to the system architecture above. It
keeps the services and their models unchanged and adds the database and
compute failover on top. The code is already stateless. Every piece of state
lives in PostgreSQL. Compute failover is therefore safe, and the database
becomes the only stateful tier.

### Topology

```
                         Cloudflare edge
  client ──► DNS and TLS ──► Worker (health in KV, alerts, wake call)
                                │
                route to the healthy cluster
             ┌──────────────────┴───────────────────┐
             ▼                                      ▼
      primary cluster                        standby cluster
  cloudflared ──► router (Caddy)         cloudflared ──► router (Caddy)
                      │                                      │
             services, 2+ replicas                 services, 0 replicas
                      │                                      │
                      └──────────► PostgreSQL (3 instances) ──┘
                                   synchronous replication
                                   automatic failover
```

One database cluster serves both compute clusters. The database runs in its
own failure domain, for example a managed service or three zones with
CloudNativePG.

### Design rules

- **Stateless services.** All state is in PostgreSQL. Any replica serves any
  request. A lost pod is a lost pod, not a lost request.
- **The database is the single stateful tier.** It runs with three instances,
  synchronous replication and automatic failover.
- **Two clusters, one image set.** The primary and the standby run the same
  immutable images. Only the replica counts differ.
- **The edge decides.** A Cloudflare Worker probes both clusters every
  minute, keeps the result in KV and sends traffic to the healthy cluster.
- **The standby sleeps.** The standby runs zero replicas until the edge
  wakes it. The wake workflow scales the deployments up.
- **Health is honest.** Every service answers `/healthz` and the gRPC health
  check. Readiness gates traffic. Liveness restarts a stuck pod.
- **Changes flow through Git.** Images build on version tags. Manifests
  reference exact versions. No manual edits on a server.

### Failure scenarios

| Failure | What happens | Recovery time |
| --- | --- | --- |
| One pod dies | Kubernetes restarts it. Two or more replicas keep serving. | seconds |
| One node dies | Pods reschedule to other nodes. Anti-affinity spreads replicas. | seconds to a minute |
| One zone dies | The other zone serves. The database keeps quorum. | seconds |
| The primary cluster dies | The Worker marks it unhealthy, wakes the standby, routes traffic there. | seconds (warm) to three minutes (cold) |
| The database primary dies | CloudNativePG promotes a replica. Connections retry. | seconds |
| The Cloudflare edge fails | Workers run in every region. DNS and TLS stay. | none |

### Mechanisms per layer

**Pods.** Readiness probes gate traffic. Liveness probes restart a stuck
process. The PodDisruptionBudget keeps at least one replica during node
drains. The HorizontalPodAutoscaler adds replicas under load. Topology
spread keeps replicas on different nodes.

**Rollouts.** The rolling update keeps `maxUnavailable` at zero. A bad
release moves no traffic until the new pod is ready. The termination grace
period is 30 seconds, and the services stop accepting new work on SIGTERM.

**Events.** The transactional outbox retries failed deliveries with backoff.
Consumers are idempotent, so a repeated delivery is safe. A restarted
service resumes from the outbox.

**Network.** NetworkPolicies allow only the router to reach the services and
only the services to reach each other and the database. Mutual TLS is
available on every internal gRPC call through the `grpc-tls` component.

**Edge.** The Worker holds the cluster health in KV. It sends traffic to the
primary while the primary is healthy. When the primary fails, it calls the
wake workflow, waits for the standby health check, and routes traffic there.
When the primary recovers, traffic returns on the next health cycle.

### Runbooks

**Failover (automatic).** The Worker detects the failure within one minute
and starts the wake workflow. Watch the alerts. No manual step is required.

**Failover (manual).** Run the `wake-standby` workflow with the desired
replica count. Confirm the standby health check, then stop the primary
cluster if it still flaps.

**Failback.** Confirm the primary health check for several minutes. The
Worker returns traffic on its own. Scale the standby back to zero after the
traffic drains.

**Drill.** Every quarter, scale the primary to zero in a maintenance window.
Measure the time from the first failed health check to the first served
request. Update the recovery time in this document.

### Limits and trade-offs

- One database cluster is one failure domain. A region loss needs logical
  replication or a distributed database. This reference keeps a single
  region with three zones.
- A cold standby saves money and costs recovery time. A warm standby
  (one replica per service) removes the wake delay.
- Failback is automatic. Gate it behind a manual step if a flapping primary
  must not take traffic back.
- The Worker KV holds only health state. It is not a database.
- The wake workflow needs a scoped kubeconfig secret. Store it as a
  repository secret and rotate it.
