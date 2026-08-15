# 1Password CLI (`op`) — gotchas + field syntax

Companion to SKILL.md. Full docs: <https://developer.1password.com/docs/cli> — this file
keeps only what the docs bury or get wrong in practice.

## Reference safety

`op://<Vault>/<Title>/<Field>` fails on non-ASCII punctuation in titles **or field
labels** — em-dashes, parens, `+ & @ /`, emoji all produce `invalid character in secret
reference`. ASCII letters, digits, spaces, `-`, `_` only. Vault/item accept name **or**
UUID — UUIDs are immutable, safer for scripts.

Special fields: `?attribute=otp` resolves the current TOTP code; `?ssh-format=openssh`
returns OpenSSH instead of PKCS#8; file fields resolve to raw contents. Get a ref
programmatically: `op item get <item> --format json --fields <label> | jq .reference`.

Variable substitution inside `.env` refs works (`DB_PASSWORD=op://$APP_ENV/db/password`),
but shell expansion happens **before** `op run` sees inline vars — `export` first.

## Field assignment syntax

- Built-in field (matches category template id): `fieldname=value` — no type needed.
- Custom field: `"label[type]=value"` or `"section.label[type]=value"`; types: `text`, `password` (concealed), `url`, `email`, `date` (YYYY-MM-DD), `otp`, `file` (value = local path).
- Delete a field on edit: `"label[delete]="`. Generate: `--generate-password=letters,digits,64`.
- `op item edit --tags` **REPLACES** existing tags, it does not append.
- Extra labels when applicable: `username` (AWS-style key id), `secret access key` (dual-secret services), `scope account/zones/repos`, `expires`.

⚠️ CLI args leak via `ps` on shared machines. High-sensitivity: `op item template get
"API Credential" > /tmp/tpl.json` → edit → `op item create --template /tmp/tpl.json` → rm.

## Service accounts — rate limits favor UUIDs

Rate limited per hour + per day; names cost extra lookups:

| Command | With names | With vault/item UUIDs |
|---------|---|---|
| `op item get` / `op read` | 3 reads | 1 read |
| `op item list` | 1 + 1 per accessible vault | 2 with `--vault <id>` |
| `op item edit`/`delete` | 5 reads + 1 write | 4 reads + 1 write |

Monitor with `op service-account ratelimit`; verify auth with `op user get --me`
(`op whoami` is for desktop mode). Service accounts cannot manage users/groups, `op
events-api`, `op connect`, or `op vault edit`; item/document commands need `--vault`.
GitHub Actions: `1password/load-secrets-action@v2` or export the token and `op run`.

`op item list` does **not** expand custom fields — auditing `rotated` dates requires
fetching each item (`op item get <id> --format=json`) in a loop.

## Rotation extras

Update `used by` if scope changed; trigger redeploys so running services pick up the new
value; log date + reason in the item Notes (`scheduled 90d rotation`, `leaked in log`, …).
