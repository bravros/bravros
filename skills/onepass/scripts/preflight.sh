#!/usr/bin/env bash
# Detect which 1Password CLI auth mode is active — and try to auto-signin if
# desktop integration is configured but the session is stale.
#
# Exits 0 and prints mode on stdout; specific codes otherwise:
#   0 → "desktop" or "service-account" printed to stdout
#   2 → op binary not installed (run install-op.sh to fix)
#   1 → op installed but not authenticated and auto-signin failed
#
# Usage:   mode=$(bash preflight.sh) || exit $?

set -u

if ! command -v op >/dev/null 2>&1; then
  echo "op CLI not installed. Run: bash $(dirname "$0")/install-op.sh" >&2
  exit 2
fi

# 1. Already authenticated via desktop integration?
if op whoami >/dev/null 2>&1; then
  echo "desktop"
  exit 0
fi

# 2. Service account?
if [ -n "${OP_SERVICE_ACCOUNT_TOKEN:-}" ]; then
  if op user get --me >/dev/null 2>&1; then
    echo "service-account"
    exit 0
  fi
  echo "OP_SERVICE_ACCOUNT_TOKEN is set but 'op user get --me' failed — token may be revoked or wrong account" >&2
  exit 1
fi

# 3. Auto-signin attempt (mirrors ~/Sites/claude/install.sh pattern).
#    Works when desktop integration is enabled but the session has expired —
#    triggers Touch ID on macOS, system auth on Linux/Windows. Diagnostic
#    output goes to stderr so callers that do `mode=$(preflight.sh)` still
#    capture the mode string cleanly.
echo "🔐 1Password session not active — attempting op signin (Touch ID / system auth)..." >&2
if op signin 2>/dev/null; then
  if op whoami >/dev/null 2>&1; then
    echo "desktop"
    exit 0
  fi
fi

cat >&2 <<'EOF'
Auto-signin failed. Enable one of:
  1) Desktop app: 1Password → Settings → Developer → Integrate with 1Password CLI
     Then run `op signin` once interactively in your terminal to establish the account.
  2) Service account: export OP_SERVICE_ACCOUNT_TOKEN=ops_...
EOF
exit 1
