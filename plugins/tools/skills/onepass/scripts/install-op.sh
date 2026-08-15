#!/usr/bin/env bash
# Install the 1Password CLI (`op`) on macOS or Linux.
# Mirrors the auto-install pattern from the repo-root install.sh (pipx block):
# detect platform → use the native package manager → fall back to a manual-install
# hint if neither is available. Idempotent — no-op if `op` is already on PATH.
#
# Usage:   bash install-op.sh          # auto
#          bash install-op.sh --check  # exit 0 if installed, 1 if not
#
# References:
#   macOS:  https://developer.1password.com/docs/cli/get-started#install
#   Linux:  https://developer.1password.com/docs/cli/get-started#install  (apt/dnf repos)

set -u

if command -v op >/dev/null 2>&1; then
  [ "${1:-}" = "--check" ] && exit 0
  echo "✅ op already installed: $(op --version)"
  exit 0
fi

if [ "${1:-}" = "--check" ]; then
  exit 1
fi

_OS=$(uname -s | tr '[:upper:]' '[:lower:]')

install_macos() {
  if ! command -v brew >/dev/null 2>&1; then
    cat <<'EOF'
⚠️  Homebrew not found. Install Homebrew first:
    /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
Then re-run this installer, or run manually:
    brew install --cask 1password-cli
EOF
    return 1
  fi
  echo "📦 Installing 1Password CLI via Homebrew..."
  brew install --cask 1password-cli
}

install_linux_apt() {
  # Official 1Password apt repo (signed). Requires sudo.
  echo "📦 Installing 1Password CLI via apt (adds signed 1Password repo)..."

  local keyring="/usr/share/keyrings/1password-archive-keyring.gpg"
  local list="/etc/apt/sources.list.d/1password.list"
  local arch
  case "$(uname -m)" in
    x86_64|amd64)  arch="amd64"  ;;
    aarch64|arm64) arch="arm64" ;;
    *) echo "⚠️  Unsupported arch $(uname -m) — install manually from https://developer.1password.com/docs/cli/get-started" >&2; return 1 ;;
  esac

  curl -fsSL https://downloads.1password.com/linux/keys/1password.asc \
    | sudo gpg --dearmor --yes --output "$keyring"

  echo "deb [arch=${arch} signed-by=${keyring}] https://downloads.1password.com/linux/debian/${arch} stable main" \
    | sudo tee "$list" >/dev/null

  # Debsig policy (required by 1Password debs)
  sudo mkdir -p /etc/debsig/policies/AC2D62742012EA22/ /usr/share/debsig/keyrings/AC2D62742012EA22
  curl -fsSL https://downloads.1password.com/linux/debian/debsig/1password.pol \
    | sudo tee /etc/debsig/policies/AC2D62742012EA22/1password.pol >/dev/null
  curl -fsSL https://downloads.1password.com/linux/keys/1password.asc \
    | sudo gpg --dearmor --yes --output /usr/share/debsig/keyrings/AC2D62742012EA22/debsig.gpg

  sudo apt update
  sudo apt install -y 1password-cli
}

install_linux_dnf() {
  echo "📦 Installing 1Password CLI via dnf (adds 1Password repo)..."
  sudo rpm --import https://downloads.1password.com/linux/keys/1password.asc
  sudo sh -c 'echo -e "[1password]\nname=1Password Stable Channel\nbaseurl=https://downloads.1password.com/linux/rpm/stable/\$basearch\nenabled=1\ngpgcheck=1\nrepo_gpgcheck=1\ngpgkey=https://downloads.1password.com/linux/keys/1password.asc" > /etc/yum.repos.d/1password.repo'
  sudo dnf install -y 1password-cli
}

install_linux() {
  if command -v apt >/dev/null 2>&1; then
    install_linux_apt
  elif command -v dnf >/dev/null 2>&1; then
    install_linux_dnf
  else
    cat >&2 <<'EOF'
⚠️  No supported package manager (apt/dnf) found on this Linux system.
    Install manually: https://developer.1password.com/docs/cli/get-started#install
EOF
    return 1
  fi
}

case "$_OS" in
  darwin) install_macos ;;
  linux)  install_linux ;;
  *)
    echo "⚠️  Unsupported OS: $_OS — install manually from https://developer.1password.com/docs/cli/get-started" >&2
    exit 1
    ;;
esac

if command -v op >/dev/null 2>&1; then
  echo "✅ op installed: $(op --version)"
  cat <<'EOF'

Next step — enable auth on this machine:
  • Desktop app (recommended for interactive use):
      1Password → Settings → Developer → Integrate with 1Password CLI
  • Service account (headless / CI / autonomous runs):
      export OP_SERVICE_ACCOUNT_TOKEN=ops_...
EOF
  exit 0
else
  echo "❌ op install appears to have failed — check output above" >&2
  exit 1
fi
