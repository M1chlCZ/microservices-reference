// Edge steering for the two-cluster deployment.
//
// The scheduled handler probes the active cluster every minute. After two
// consecutive failures it wakes the standby through a GitHub workflow and
// switches traffic once the standby answers. It fails back to the primary
// after recovery. The fetch handler routes requests to the active cluster
// and reacts to a request failure immediately.
//
// Bindings:
//   KV namespace HEALTH       active cluster, failure counter, wake flag
//   Var PRIMARY_NAME          name of the primary cluster
//   Var STANDBY_NAME          name of the standby cluster
//   Var PRIMARY_URL           public URL of the primary cluster
//   Var STANDBY_URL           public URL of the standby cluster
//   Var GITHUB_REPO           owner/repository, e.g. M1chlCZ/microservices-reference
//   Var GITHUB_WORKFLOW       workflow file, e.g. wake-standby.yml
//   Secret GITHUB_TOKEN       token with actions:write on the repository
//   Secret ALERT_WEBHOOK      optional webhook for failover messages

const HEALTH_TIMEOUT_MS = 5000;
const FAILURE_THRESHOLD = 2;

async function probe(baseUrl) {
  try {
    const response = await fetch(`${baseUrl}/healthz`, {
      signal: AbortSignal.timeout(HEALTH_TIMEOUT_MS),
    });
    return response.ok;
  } catch {
    return false;
  }
}

async function activeName(env) {
  const stored = await env.HEALTH.get("active");
  return stored === env.STANDBY_NAME ? env.STANDBY_NAME : env.PRIMARY_NAME;
}

function baseUrl(env, name) {
  return name === env.STANDBY_NAME ? env.STANDBY_URL : env.PRIMARY_URL;
}

function otherName(env, name) {
  return name === env.PRIMARY_NAME ? env.STANDBY_NAME : env.PRIMARY_NAME;
}

async function alert(env, message) {
  if (!env.ALERT_WEBHOOK) {
    return;
  }
  await fetch(env.ALERT_WEBHOOK, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ text: message }),
  });
}

async function wakeStandby(env) {
  await fetch(
    `https://api.github.com/repos/${env.GITHUB_REPO}/actions/workflows/${env.GITHUB_WORKFLOW}/dispatches`,
    {
      method: "POST",
      headers: {
        authorization: `Bearer ${env.GITHUB_TOKEN}`,
        accept: "application/vnd.github+json",
        "user-agent": "microservices-reference-edge",
      },
      body: JSON.stringify({ ref: "main" }),
    },
  );
}

async function switchTo(env, name, reason) {
  const previous = await activeName(env);
  if (previous === name) {
    return;
  }
  await env.HEALTH.put("active", name);
  await env.HEALTH.delete("consecutive_failures");
  await env.HEALTH.delete("wake_sent");
  await alert(env, `failover: ${previous} -> ${name} (${reason})`);
}

async function proxy(request, env, name) {
  const response = await fetch(baseUrl(env, name) + new URL(request.url).pathname + new URL(request.url).search, request);
  const headers = new Headers(response.headers);
  headers.set("x-served-by", name);
  return new Response(response.body, { status: response.status, headers });
}

export default {
  async fetch(request, env) {
    const active = await activeName(env);
    try {
      return await proxy(request, env, active);
    } catch {
      const other = otherName(env, active);
      if (await probe(baseUrl(env, other))) {
        await switchTo(env, other, "request failure");
        return await proxy(request, env, other);
      }
      await alert(env, `no healthy cluster for ${request.url}`);
      return new Response("service unavailable\n", { status: 503 });
    }
  },

  async scheduled(_event, env, _ctx) {
    const active = await activeName(env);
    const other = otherName(env, active);
    const activeHealthy = await probe(baseUrl(env, active));

    if (activeHealthy) {
      await env.HEALTH.delete("consecutive_failures");
      if (active === env.STANDBY_NAME && (await probe(baseUrl(env, env.PRIMARY_NAME)))) {
        await switchTo(env, env.PRIMARY_NAME, "primary recovered");
      }
      return;
    }

    const failures = Number((await env.HEALTH.get("consecutive_failures")) ?? "0") + 1;
    await env.HEALTH.put("consecutive_failures", String(failures));
    if (failures < FAILURE_THRESHOLD) {
      return;
    }

    if (await probe(baseUrl(env, other))) {
      await switchTo(env, other, `${failures} consecutive probe failures`);
      return;
    }

    if (other === env.STANDBY_NAME && !(await env.HEALTH.get("wake_sent"))) {
      await env.HEALTH.put("wake_sent", "1");
      await wakeStandby(env);
      await alert(env, `primary failed, waking the standby cluster`);
      return;
    }

    await alert(env, `both clusters unhealthy`);
  },
};
