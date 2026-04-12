#!/bin/sh
# Bravros binary installer — single responsibility: verify + install the CLI.
# All skill/hook/config setup happens via `bravros setup` (invoked automatically
# by `bravros activate <license-key>`).
#
# Usage:
#   curl -fsSL https://bravros.dev/install | sh
#
# Verify this installer and the embedded public key at: https://bravros.dev/security
# (Key ID: 366384ABA1561E2A)

set -eu

# ── Minisign public key ──────────────────────────────────────────────────────
# To verify this key is genuine, check https://bravros.dev/security
MINISIGN_PUBKEY="RWQqHlahq4RjNnCasO/8yMsgtLGfdHejILKMxxpsulIs1rII6IgMO26G"

# ── Constants ────────────────────────────────────────────────────────────────
GITHUB_REPO="bravros/bravros"
BASE_URL="https://github.com/${GITHUB_REPO}/releases/latest/download"
BIN_DIR="$HOME/.claude/bin"

# ── Platform detection ───────────────────────────────────────────────────────
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
esac

case "$OS" in
  darwin|linux) ;;
  *) echo "Unsupported OS: $OS" >&2; exit 1 ;;
esac

case "$OS-$ARCH" in
  linux-arm64) echo "Unsupported: linux/arm64 — use darwin or linux/amd64" >&2; exit 1 ;;
esac

TARBALL="bravros-${OS}-${ARCH}.tar.gz"

# ── Helpers ──────────────────────────────────────────────────────────────────
info() { printf '  → %s\n' "$*"; }
ok()   { printf '  ✓ %s\n' "$*"; }
die()  { printf 'error: %s\n' "$*" >&2; exit 1; }
require() { command -v "$1" >/dev/null 2>&1 || die "Required tool not found: $1"; }

ensure_minisign() {
  command -v minisign >/dev/null 2>&1 && return
  info "Installing minisign..."
  if command -v brew >/dev/null 2>&1; then
    brew install minisign >/dev/null 2>&1 || die "brew install minisign failed"
  elif command -v apt-get >/dev/null 2>&1; then
    sudo apt-get install -y minisign >/dev/null 2>&1 || die "apt-get install minisign failed"
  else
    die "Install minisign first: https://jedisct1.github.io/minisign/"
  fi
}

add_to_path() {
  for rc in "$HOME/.zshrc" "$HOME/.bashrc"; do
    [ -f "$rc" ] && ! grep -qF '.claude/bin' "$rc" && printf '\nexport PATH="$HOME/.claude/bin:$PATH"\n' >> "$rc"
  done
}

# ── Main ─────────────────────────────────────────────────────────────────────
require curl
ensure_minisign

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

info "Downloading bravros for ${OS}/${ARCH}..."
curl -fsSL "${BASE_URL}/${TARBALL}"            -o "${TMPDIR}/${TARBALL}"
curl -fsSL "${BASE_URL}/checksums.txt"         -o "${TMPDIR}/checksums.txt"
curl -fsSL "${BASE_URL}/checksums.txt.minisig" -o "${TMPDIR}/checksums.txt.minisig"

info "Verifying minisign signature..."
minisign -Vm "${TMPDIR}/checksums.txt" \
  -P "${MINISIGN_PUBKEY}" \
  -x "${TMPDIR}/checksums.txt.minisig" \
  || die "Signature verification failed — download may be tampered!"
ok "Signature valid"

info "Verifying SHA256 checksum..."
( cd "${TMPDIR}" && grep "${TARBALL}" checksums.txt | \
  { command -v sha256sum >/dev/null 2>&1 && sha256sum -c - || shasum -a 256 -c -; } >/dev/null ) \
  || die "SHA256 mismatch — download may be corrupted!"
ok "Checksum verified"

info "Installing to ${BIN_DIR}/bravros..."
mkdir -p "${BIN_DIR}"
tar -xzf "${TMPDIR}/${TARBALL}" -C "${BIN_DIR}" bravros
chmod +x "${BIN_DIR}/bravros"
add_to_path
ok "Installed"

printf '\nBravros installed! Run:\n  export PATH="$HOME/.claude/bin:$PATH"\n  bravros activate <license-key>\n\n'
