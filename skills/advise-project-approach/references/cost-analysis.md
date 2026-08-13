# Operating-cost analysis

Go deep on cost when the user mentions budget, hosting, SaaS, cloud, database, auth, file storage, AI APIs, "free tier", "cheap", "self-host", or "scale" — or when a managed-service choice is central to the recommendation.

Homepage marketing and "free to start" are never proof that a stack is cheap to operate.

## Cost buckets to check

- base subscription or minimum plan requirement
- per-project, per-organization, per-seat, per-environment charges
- compute/runtime hours, serverless invocations, background jobs, queues, cron
- database size, read/write volume, backups, replicas, point-in-time recovery, connection pooling
- object storage, bandwidth, image/video transforms, CDN, **egress**
- auth users / MAU, MFA, SSO, organizations and teams, custom domains
- API requests, AI token usage, embeddings and vector storage, rate limits, overage pricing
- logs, metrics, tracing, alerts, retention, observability add-ons
- support tiers, compliance and audit-log features gated to enterprise plans
- migration/exit cost, data portability, lock-in, local-dev parity, self-hosting fallback

The last two buckets — egress and enterprise-gated compliance — are where "cheap" stacks usually break.

## Three scenarios, not one number

- **Prototype cost** — what stays free or near-free while usage is tiny.
- **Launch cost** — what changes once real users, stored data, background jobs, and custom domains appear.
- **Growth cost** — which line items scale fastest and which create lock-in.

Use scenario language instead of fake precision. Verified prices get a source and observed date; unverified ones get no numbers at all — name the pricing dimensions that could overturn the stack choice instead.

Distinguish development cost, launch cost, and steady-state operating cost. Never call a service "free", "cheap", "included", or "generous" without naming the limit that ends it.

If pricing pages were unreachable, say pricing was not verified and list the cost categories the user must check before committing.
