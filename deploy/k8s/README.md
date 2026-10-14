# Kubernetes deployment

Runs the reference on two clusters with a shared database and failover
steering. [ARCHITECTURE.md](../../ARCHITECTURE.md) describes the topology, the
failure scenarios and the limits.

## Prerequisites

- Two clusters, each with a way to publish the router (LoadBalancer or an
  equivalent).
- The CloudNativePG operator in the database cluster. See
  [postgres/cloudnative-pg.yaml](postgres/cloudnative-pg.yaml).
- `kubectl` with built-in kustomize.
- Three secrets: `microservices-secrets`, `grpc-certs` and
  `backup-credentials`.

## Layout

| Path | Contents |
| --- | --- |
| `base/` | Deployments, services, router, tunnel and network policies. |
| `components/grpc-tls/` | Adds the mTLS files to every service. |
| `components/hpa/` | CPU autoscaling for every service. |
| `migrate/` | The schema migration job. |
| `overlays/primary/` | base + migrate + grpc-tls + hpa. |
| `overlays/standby/` | base + grpc-tls, every deployment at zero replicas. |
| `postgres/` | The CloudNativePG cluster in the namespace `microservices-data`. |

## Apply

```sh
make certs
kubectl apply -f deploy/k8s/postgres/cloudnative-pg.yaml
kubectl -n microservices create secret generic microservices-secrets \
  --from-literal=orders-database-url=... \
  --from-literal=cloudflare-tunnel-token=...
kubectl -n microservices create secret generic grpc-certs \
  --from-file=ca.crt=deploy/certs/ca.crt \
  --from-file=server.crt=deploy/certs/server.crt \
  --from-file=server.key=deploy/certs/server.key \
  --from-file=client.crt=deploy/certs/client.crt \
  --from-file=client.key=deploy/certs/client.key
kubectl kustomize deploy/k8s/overlays/primary | kubectl apply -f -
kubectl kustomize deploy/k8s/overlays/standby | kubectl apply -f -
```

[secret.example.yaml](base/secret.example.yaml) lists every required key. The
database URLs point at the read-write service of the CNPG cluster:
`postgres-rw.microservices-data.svc:5432`.

The migrate job runs with the primary overlay. After a schema change, delete
the finished job and apply the overlay again:

```sh
kubectl -n microservices delete job migrate --ignore-not-found
kubectl kustomize deploy/k8s/overlays/primary | kubectl apply -f -
```

## Images

The workflow `images.yml` builds one image per service on every `v*` tag:

```
ghcr.io/m1chlcz/microservices-reference-<service>:<version>
```

The manifests pin version `0.3.0`. Pin digests in production.

## Standby and failover

The standby overlay sets every deployment to zero replicas. Wake it with:

```sh
kubectl -n microservices scale deployment --all --replicas=2
```

The `wake-standby` workflow runs the same command on the standby cluster
through the `STANDBY_KUBE_CONFIG_B64` secret. The Cloudflare Worker in
[../cloudflare](../cloudflare) calls that workflow when the primary fails.

## The gateway example

The gateway example listens on `POST /reservations`, the same path as the
inventory service. The router does not expose it. Reach it with:

```sh
kubectl -n microservices port-forward svc/gateway 8090:8090
```

## Validation

```sh
kubectl kustomize deploy/k8s/overlays/primary >/dev/null
kubectl kustomize deploy/k8s/overlays/standby >/dev/null
```
