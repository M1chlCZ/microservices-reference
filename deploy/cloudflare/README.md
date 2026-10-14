# Edge steering

The Worker `microservices-reference-edge` steers traffic between the primary
and the standby cluster. It is the only component with a global view of both
clusters.

## Behavior

- The scheduled handler probes `/healthz` of the active cluster every minute.
- After two consecutive failures it wakes the standby cluster through the
  `wake-standby` workflow and alerts the webhook. It switches traffic when
  the standby answers.
- It fails back to the primary after the primary recovers.
- The fetch handler routes requests to the active cluster and switches
  immediately on a request failure.
- Every response carries the header `x-served-by` with the active cluster
  name.

## Setup

1. Create the KV namespace and copy its ID into `wrangler.toml`:

   ```sh
   wrangler kv namespace create HEALTH
   ```

2. Create a fine-grained token with `actions:write` on the repository and
   store the secrets:

   ```sh
   wrangler secret put GITHUB_TOKEN
   wrangler secret put ALERT_WEBHOOK
   ```

3. Set the cluster names and URLs in `wrangler.toml`, then deploy:

   ```sh
   wrangler deploy
   ```

The Worker can also run as a monitor only. Point the public DNS of the two
clusters at the clusters directly, keep the cron trigger and skip the fetch
route. The steering state stays in KV in both cases.
