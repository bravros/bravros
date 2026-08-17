# Auth-mode setup (preflight exit 1)

`ask_question` — "Which 1Password auth mode for this machine?": `desktop` (interactive,
Touch ID / system auth), `service-account` (headless / autonomous / CI), or `both`
(desktop first, then service account). Precedence: in a shell where both are active the
**service account wins**; `unset OP_SERVICE_ACCOUNT_TOKEN` for desktop behavior. Clear
`OP_CONNECT_*` vars — Connect takes precedence over everything.

## desktop

Announce, then tell the user (separate terminal / GUI): enable **1Password app → Settings
→ Developer → Integrate with 1Password CLI**, verify with `op whoami`, then say "done" so
preflight reruns.

<!-- announce-template: "Configuração de área de trabalho do 1Password necessária. Siga as instruções no terminal separado. Projeto {PROJECT}." -->
```bash
bravros ha say --force "Configuração de área de trabalho do 1Password necessária. Siga as instruções no terminal separado. Projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." studio >/dev/null 2>&1 || true
```

## service-account

Announce, then walk the user through: create the token at <https://my.1password.com> →
**Developer Tools → Service Accounts**, granting **only** the vaults this machine needs
(least privilege). Export `OP_SERVICE_ACCOUNT_TOKEN='ops_…'` — one-shot, or persist in
`~/.zshrc`, or (best for autonomous agents) store the token *in 1Password* and fetch on
shell startup:

```bash
export OP_SERVICE_ACCOUNT_TOKEN="$(op read 'op://HomeLab/OP Service Account - Autonomous/credential' 2>/dev/null)"
```

Verify with `op user get --me`. Warn: the token bypasses biometric — treat like a root
credential; never commit, log, or paste it.

<!-- announce-template: "Configuração de conta de serviço do 1Password necessária. Siga as instruções no terminal separado. Projeto {PROJECT}." -->
```bash
bravros ha say --force "Configuração de conta de serviço do 1Password necessária. Siga as instruções no terminal separado. Projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." studio >/dev/null 2>&1 || true
```
