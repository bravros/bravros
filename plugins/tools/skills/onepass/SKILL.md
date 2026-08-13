---
name: onepass
description: Store, read, inject, and rotate secrets via the 1Password CLI (op). Enforces op:// references so secrets never touch env files. Use when the user mentions 1Password, op:// secrets, credential rotation, or safe secret injection into code.
---

# onepass — secrets live in 1Password, code holds only `op://` references

> Read [briefing.md](file:///Users/skaisser/Sites/bravros/skills/onepass/references/briefing.md) on demand for detailed context and instructions.

## Step 0 — Preflight (every invocation)

Run `bash <skill-dir>/scripts/preflight.sh`: exit `0` (desktop or service-account mode) → proceed; exit `2` → offer `scripts/install-op.sh`, rerun; exit `1` → auth failed: announce & follow `references/auth-setup.md`. Never run `op item create`/`edit` before preflight returns 0.

```bash
bash ~/.bravros/scripts/announce.sh "Autenticação do 1Password necessária. Aguardando escolha do modo de acesso. Projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." studio >/dev/null 2>&1 || true
```

## Naming & metadata — enforce before create

- **Title:** `<Service> - <Project> <Purpose>`, ASCII only (`A-Z a-z 0-9 _ -`). Validate via `scripts/validate-title.sh`.
- **Category:** `API Credential` default. **Vault:** `HomeLab` default. **Tags:** minimum 2 (service + project).
- **Required fields:** `credential` (concealed) · `token type` · `permissions` · `used by` · `env var` · `owner` · `rotated` (date).
- After create, verify `op read "op://HomeLab/<Title>/credential" | head -c 20`.

## Workflows

- **Read/inject:** `TOKEN="$(op read 'op://…')" cmd` or `op run --env-file=.env -- <cmd>`.
- **Wire a project:** Replace plaintext secrets in `.env` with `op://` refs.
- **Rotation:** Edit `credential` and `rotated` date on existing item ID; propagate to sinks (`gh secret set` / `vercel env add`); revoke old token at provider.

## Refuse / warn

Plaintext secret in committed file · duplicate items · hard delete without `--archive` · rotating without revoking · echoing raw tokens in chat.

Reference docs: [op-cli-reference.md](file:///Users/skaisser/Sites/bravros/skills/onepass/references/op-cli-reference.md), [auth-setup.md](file:///Users/skaisser/Sites/bravros/skills/onepass/references/auth-setup.md), [briefing.md](file:///Users/skaisser/Sites/bravros/skills/onepass/references/briefing.md).
