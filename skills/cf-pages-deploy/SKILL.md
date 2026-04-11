---
name: cf-pages-deploy
description: >
  Deploy a static HTML/CSS/JavaScript site to Cloudflare Pages using the Wrangler CLI.
  Use this skill whenever the user wants to deploy, publish, or push a static website to
  Cloudflare Pages — including vanilla HTML/CSS/JS sites, pre-built SPAs, or any folder
  of static assets. Trigger on mentions of "deploy to Cloudflare", "Cloudflare Pages",
  "publish my site", "push to pages", "go live on Cloudflare", or any request involving
  uploading static files to Cloudflare's hosting. Also trigger when the user asks to set up
  a new Cloudflare Pages project for a static site, even if they don't say "deploy" explicitly.
---

## Model Requirement

This skill runs well on **Sonnet**. No deep reasoning required — mechanical/scripted operations.

# Deploy Static Sites to Cloudflare Pages

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

This skill deploys a folder of static HTML, CSS, and vanilla JavaScript files to Cloudflare Pages using Wrangler's direct upload feature. No build step is needed — the files are pushed as-is.

## Quick deploy (one command)

This skill bundles a `scripts/deploy.sh` script that handles everything — auth check, zone discovery, deploy, custom domain, and DNS record — in one command:

```bash
bash <skill-path>/scripts/deploy.sh <directory> <project-name> [custom-domain]
```

Examples:
```bash
# Deploy only
bash scripts/deploy.sh src/ my-site

# Deploy + custom domain
bash scripts/deploy.sh src/ maglash-lp lp.maglash.com.br
```

The script automatically finds the correct Cloudflare account for the domain, deploys to it, registers the custom domain via API, and creates the CNAME record. If the OAuth token lacks DNS permissions, it prints the exact manual step needed.

**Always prefer the script** over running individual commands. The step-by-step workflow below is for reference and troubleshooting.

## How it works

Cloudflare Pages "Direct Upload" lets you push a local folder of pre-built assets straight to their CDN. The core command is `npx wrangler pages deploy <directory> --project-name <name>`. If the project doesn't exist yet, Wrangler creates it automatically.

## Important: always use `npx wrangler`

Every wrangler command in this skill MUST be invoked as `npx wrangler`, not bare `wrangler`. This ensures the correct version is used and avoids PATH issues. This applies to all commands: `npx wrangler --version`, `npx wrangler whoami`, `npx wrangler pages deploy`, etc.

## Deployment workflow

### 1/7: Figure out what to deploy

Ask the user which folder contains their static site if not already specified. Common locations: `src/`, `dist/`, `build/`, `public/`, `out/`, or the project root.

Verify the folder exists and contains at least an `index.html`:

```bash
ls <deploy-directory>/index.html
```

If there's no `index.html` at the root of the deploy directory, warn the user — Cloudflare Pages expects one for the site's homepage.

### 2/7: Verify Wrangler is available

```bash
npx wrangler --version
```

If this fails, install it: `npm install -g wrangler`, then retry with `npx wrangler --version`.

### 3/7: Check authentication

This step is critical — always run it to confirm the user's login status and discover which accounts are available:

```bash
npx wrangler whoami
```

If the user IS authenticated, the output will show their email and a table of accounts with account IDs. Take note of the accounts — you'll need to pick the right one if there are multiple.

If the user is NOT authenticated (`npx wrangler whoami` returns an error or says "not logged in"), tell them to run `npx wrangler login` in their terminal (it opens a browser for OAuth). Wait for them to confirm before proceeding.

### 4/7: Decide on a project name

Ask the user what they'd like to call their Pages project. This becomes the subdomain: `<project-name>.pages.dev`. Project names must be lowercase, alphanumeric, and can contain hyphens.

If the user already has an existing project, they can reuse the name to create a new deployment under it.

### 5/7: Handle multiple accounts

If `npx wrangler whoami` showed multiple accounts, Wrangler's interactive prompt won't work in non-interactive environments. In that case, pass the account ID explicitly:

```bash
npx wrangler pages deploy <directory> --project-name <name> --account-id <account-id>
```

Ask the user which account to use if it's unclear from context.

### 6/7: Deploy

```bash
npx wrangler pages deploy <deploy-directory> --project-name <project-name>
```

Add `--account-id <id>` if the user has multiple Cloudflare accounts (see step 5).

Wrangler will create the project automatically if it doesn't exist yet — no separate creation step is needed.

### 7/7: Confirm and share the URL

After deployment, Wrangler outputs the live URL. Share it with the user:

- **Production:** `https://<project-name>.pages.dev`
- **Preview (branch deploy):** `https://<branch>.<project-name>.pages.dev`

## Branch / preview deploys

To deploy a preview (non-production) version:

```bash
npx wrangler pages deploy <deploy-directory> --project-name <project-name> --branch <branch-name>
```

## Custom domains (automated via API)

If the user wants a custom domain (e.g., `lp.example.com`) pointed at their Pages project, you can add it automatically via the Cloudflare REST API. The domain's zone must already exist in the user's Cloudflare account.

### Step 0: Discover which account owns the domain

When the user has multiple Cloudflare accounts, you must first find which account owns the domain's zone. This determines both the account to deploy to AND the account to add the custom domain on. The Pages project must be on the SAME account as the zone.

Query the zones API with the root domain (e.g., `maglash.com.br` for `lp.maglash.com.br`):

```bash
curl -s "https://api.cloudflare.com/client/v4/zones?name=<root-domain>" \
  -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" | python3 -m json.tool
```

Look for these fields in the response:
- `result[0].id` — the **zone ID**
- `result[0].account.id` — the **account ID** that owns this zone
- `result[0].account.name` — the account name (for confirmation with the user)

Use this account ID for BOTH the `npx wrangler pages deploy --account-id` and the custom domain API calls. If the Pages project was already deployed to a different account, you'll need to redeploy to the correct one.

### Step 1: Get the API token

Wrangler stores an OAuth token locally. The tricky part is that `npx wrangler` prints a startup banner (`⛅️ wrangler X.X.X` and a line of `───`) to stdout before the token, which contaminates variable capture.

Use this pattern to strip the banner and capture just the token:

```bash
CLOUDFLARE_API_TOKEN=$(npx wrangler auth token 2>&1 | grep -v wrangler | grep -v '─' | tail -1)
```

This works because `grep -v wrangler` removes the banner line and `grep -v '─'` removes the separator. The token is the only remaining line.

You can chain this directly with curl calls using `&&`:

```bash
CLOUDFLARE_API_TOKEN=$(npx wrangler auth token 2>&1 | grep -v wrangler | grep -v '─' | tail -1) && \
curl -s "https://api.cloudflare.com/client/v4/zones?name=example.com" \
  -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" | python3 -m json.tool
```

If `npx wrangler auth token` errors out entirely (user not logged in), fall back to asking the user for an API token (they can create one at dash.cloudflare.com/profile/api-tokens with "Cloudflare Pages:Edit" and "DNS:Edit" permissions).

### Step 2: Add the custom domain to the Pages project

```bash
curl -s -X POST \
  "https://api.cloudflare.com/client/v4/accounts/<account-id>/pages/projects/<project-name>/domains" \
  -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "<custom-domain>"}' | python3 -m json.tool
```

Replace `<account-id>`, `<project-name>`, and `<custom-domain>` with the actual values. For example:

```bash
curl -s -X POST \
  "https://api.cloudflare.com/client/v4/accounts/aa3085f84de1e98a7a70b0e88d3b5d7a/pages/projects/maglash-lp/domains" \
  -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "lp.maglash.com.br"}' | python3 -m json.tool
```

A successful response will have `"success": true`.

### Step 3: Create the DNS record

The Pages custom domain API does NOT always auto-create the CNAME record. You must create it explicitly via the DNS API using the **zone ID** from Step 0:

For a **subdomain** (e.g., `lp.example.com`):

```bash
curl -s -X POST \
  "https://api.cloudflare.com/client/v4/zones/<zone-id>/dns_records" \
  -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"type":"CNAME","name":"<subdomain>","content":"<project-name>.pages.dev","proxied":true}' | python3 -m json.tool
```

For example, to point `lp.maglash.com.br` to `maglash-lp.pages.dev`:

```bash
curl -s -X POST \
  "https://api.cloudflare.com/client/v4/zones/2492a394bb318b61ea0dd1aeea9fd330/dns_records" \
  -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"type":"CNAME","name":"lp","content":"maglash-lp.pages.dev","proxied":true}' | python3 -m json.tool
```

The `"name"` field is just the subdomain part (e.g., `lp`), not the full hostname. Setting `"proxied":true` enables Cloudflare's proxy (orange cloud), which handles SSL automatically.

For an **apex domain** (e.g., `example.com`), use `"name":"@"` instead.

### Step 4: Verify the domain

After adding the DNS record, check the custom domain status — it should flip from `pending` to `active` within 1-2 minutes:

```bash
curl -s \
  "https://api.cloudflare.com/client/v4/accounts/<account-id>/pages/projects/<project-name>/domains" \
  -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" | python3 -m json.tool
```

Once `"status": "active"`, the site is live at the custom domain with HTTPS.

### Important notes

- The domain's zone (e.g., `maglash.com.br`) must already be on the same Cloudflare account as the Pages project
- The CNAME DNS record must be created explicitly — the Pages API alone is not enough
- Always use `"proxied": true` so Cloudflare handles SSL and CDN
- SSL certificates are provisioned automatically once the DNS record is in place

## Common issues

- **"No index.html found"** — Cloudflare Pages expects `index.html` at the root of the deploy directory.
- **Large files** — Pages has a 25 MiB per-file limit. Warn the user if any file exceeds this.
- **Interactive prompts hang** — When running in non-interactive environments, always pass `--project-name` and `--account-id` explicitly to avoid prompts.
- **Multiple accounts** — Always pass `--account-id` when the user has more than one Cloudflare account.
- **Preview vs production deploy** — Wrangler always outputs a commit-specific URL like `https://abc123.project.pages.dev`. This is normal. The production URL is `https://<project-name>.pages.dev`. To ensure the deploy goes to production, use `--branch <production-branch-name>` matching the branch set during project creation. If the user didn't set one, `main` or `production` are common defaults.
- **OAuth token lacks DNS permissions** — The token from `npx wrangler auth token` typically only has Workers/Pages scopes (workers:write, workers_scripts:write, etc.) and does NOT include DNS write permissions. This means the token works for the Pages domain API (Step 2) but will fail for DNS record creation (Step 3) with a `10000: Authentication error`. When this happens, the user must either create the DNS record manually in the Cloudflare dashboard, or create a separate API token at dash.cloudflare.com/profile/api-tokens with "Zone: DNS: Edit" permission.

## Example

A typical deploy session:

```
User: "Deploy my site in src/ to Cloudflare Pages as my-cool-site, point it to lp.example.com"

 1. Verify src/index.html exists                          ✓
 2. npx wrangler --version                                ✓  v4.72.0
 3. npx wrangler whoami                                   ✓  logged in as user@example.com
    Accounts: Pessoal (aa3085...), WebKPG (2db7a1...)
 4. Get API token: npx wrangler auth token                ✓
 5. Find zone: GET /zones?name=example.com                → account_id: 2db7a1... (WebKPG)
 6. Deploy to the SAME account that owns the zone:
    npx wrangler pages deploy src/ --project-name my-cool-site --account-id 2db7a1... --branch main
 7. Share production URL: https://my-cool-site.pages.dev  ✓
    (Wrangler shows a commit URL like https://abc123.my-cool-site.pages.dev — that's normal)
 8. Add custom domain via API: POST .../domains {"name":"lp.example.com"}  ✓
 9. Create CNAME via DNS API: POST .../dns_records
    → If auth error (token lacks DNS scope), tell user to add CNAME in dashboard:
      Type: CNAME | Name: lp | Content: my-cool-site.pages.dev | Proxied: on
10. Verify domain status: GET .../domains → "active"      ✓
11. Share custom domain URL: https://lp.example.com       ✓
```
