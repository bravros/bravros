---
name: cloudflare-auto-deploy
description: >
  Auto-deploy any project to Cloudflare Pages — detect stack, build, create project, deploy, set up npm script.
  Use this skill whenever the user wants to deploy a project to Cloudflare Pages with zero config.
  Triggers on: "deploy to cloudflare", "cloudflare deploy", "cf deploy", "deploy this",
  "push to pages", "go live", "set up cloudflare pages", "auto deploy", "/cloudflare-auto-deploy".
  Differs from cf-pages-deploy: this skill handles build-step projects (React, Vite, Next.js, etc.),
  auto-detects output directories, and sets up the npm deploy script. Use cf-pages-deploy for
  static HTML sites and custom domain wiring.
compatibility:
  - npm
  - npx
  - wrangler (via npx)
  - Cloudflare account
---

# Cloudflare Auto Deploy

One-command deploy for any project to Cloudflare Pages. Detects your stack, builds, creates the project, deploys, and sets up `npm run deploy` for future use.

## When to Use

- Project has a build step (Vite, Next.js static export, Create React App, etc.)
- User wants zero-config deployment to Cloudflare Pages
- User says "deploy this to cloudflare" or "/claudeflare"

For static HTML sites without a build step, or when custom domain/DNS wiring is needed, use `/cf-pages-deploy` instead.

## Process

### Step 1: Detect Stack & Output Directory

Read `package.json` to determine the build system and output directory.

Read `references/build-tools.md` for the full framework detection table (build commands and output directories per framework).

If unclear, check `vite.config.*` for `build.outDir`, or `next.config.*` for `output: 'export'`.

**Verify the output directory exists after build.** If it doesn't, the build command likely failed.

### Step 2: Verify Wrangler Auth

```bash
npx wrangler whoami 2>&1
```

If not logged in, tell the user:
> Run `! npx wrangler login` in your terminal to authenticate with Cloudflare.

### Step 3: Handle Multiple Accounts

If `whoami` shows multiple accounts, ask the user which one to use. Store the account ID for all subsequent commands.

If only one account, use it automatically.

### Step 4: Derive Project Name

Generate a project name from the directory name or `package.json` `name` field:
- Lowercase, alphanumeric + hyphens only
- Strip `@scope/` prefixes
- Example: `bravros-site`, `kaisserdev`, `my-app`

Confirm with the user: "Deploying as `<name>` — this will be available at `<name>.pages.dev`. OK?"

### Step 5: Build

```bash
npm run build
```

Verify the output directory exists and contains `index.html`:

```bash
ls <output-dir>/index.html
```

### Step 6: Create Project & Deploy

```bash
CLOUDFLARE_ACCOUNT_ID=<account-id> npx wrangler pages project create <project-name> --production-branch main 2>&1
```

If the project already exists, this will error — that's fine, skip to deploy.

```bash
CLOUDFLARE_ACCOUNT_ID=<account-id> npx wrangler pages deploy <output-dir> --project-name <project-name> --branch main --commit-dirty=true 2>&1
```

### Step 7: Set Up SPA Fallback

If the project is an SPA (React, Vue, Svelte, etc.), ensure `_redirects` exists in the public/static directory. Copy from `assets/_redirects-template` in this skill directory.

For Vite projects, this goes in `public/_redirects`. For CRA, `public/_redirects`. For Next.js static export, it's handled automatically.

If `_redirects` doesn't exist, create it and rebuild before deploying.

### Step 8: Add Deploy Script to package.json

Check if `package.json` already has a `deploy` script. If not, add one:

```json
"deploy": "npm run build && CLOUDFLARE_ACCOUNT_ID=<account-id> npx wrangler pages deploy <output-dir> --project-name <project-name> --branch main --commit-dirty=true"
```

The account ID is safe to inline — it's a public identifier, not a secret.

### Step 9: Report

Output:

```
Deployed to Cloudflare Pages:

  Production:  https://<project-name>.pages.dev
  Deploy URL:  https://<hash>.<project-name>.pages.dev

  Future deploys:  npm run deploy
  
  To add a custom domain, go to:
  https://dash.cloudflare.com/?to=/:account/pages/view/<project-name>/domains
```

## Example Session

```
User: "deploy this to cloudflare"

1. Read package.json → Vite project, output: dist/
2. npx wrangler whoami → 3 accounts
3. Ask user → "Pessoal" (aa3085...)
4. Project name: "bravros-site" (from package.json name)
5. npm run build → dist/ created, index.html present
6. Create project → bravros-site.pages.dev
7. _redirects exists in public/ → SPA fallback OK
8. Deploy dist/ → https://abc123.bravros-site.pages.dev
9. Add "deploy" script to package.json
10. Done → https://bravros-site.pages.dev
```

## GitHub Auto-Deploy (Optional Follow-Up)

After the initial deploy, suggest GitHub auto-deploy:

> For automatic deploys on every push, connect the repo in the Cloudflare dashboard:
> Settings > Builds & deployments > Connect to Git
> Build command: `npm run build` | Output: `<output-dir>` | Env: `NODE_VERSION=22`

This requires OAuth and can't be done via CLI — the user must do it in the dashboard.

## Notes

- Always use `npx wrangler`, never bare `wrangler`
- Account ID is NOT a secret — safe to commit in package.json
- The `--commit-dirty=true` flag allows deploying with uncommitted changes
- Cloudflare Pages has a 25 MiB per-file limit
- Free tier: 500 deploys/month, 1 build at a time — more than enough for most projects
- For custom domains and DNS wiring, use `/cf-pages-deploy` which has full API-based domain setup
