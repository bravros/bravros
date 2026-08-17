#!/bin/sh
# Bravros installer — detect platform, download, verify, install, hand off.
#
# Single responsibility: put a *verified* binary on disk and then `exec bravros
# setup`. Every interactive choice lives in the Go binary (P-0015 D9: thin
# installer, rich binary), so this script must stay boring and auditable.
#
# Usage (advertised form — see the stdin note below):
#   bash -c "$(curl -fsSL https://install.bravros.dev)"
#
# Still supported (the bookmarked form):
#   curl -fsSL https://install.bravros.dev | sh
#
# Verify this installer and the embedded public key at: https://bravros.dev/security
# (Key ID: 366384ABA1561E2A)
#
# ── The stdin decision (P-0015 Phase 5) — BOTH halves, do not drop either ─────
# `curl … | sh` cannot prompt: the pipe IS stdin, so the shell has already
# consumed it and any `read` in the script (or in the binary we exec) sees EOF.
# Laravel's php.new hits the same wall and answers it by publishing the
# `bash -c "$(curl …)"` form, where command substitution leaves stdin attached
# to the terminal.
#
# We do BOTH, deliberately:
#   1. The docs-advertised one-liner becomes `bash -c "$(curl -fsSL …)"`.
#   2. This script ALSO runs itself against /dev/tty when stdin is not a TTY,
#      so the one-liner people already bookmarked keeps working AND can still
#      prompt. When /dev/tty cannot be opened — a true CI runner — we fall
#      through non-interactively and print instructions instead of failing.
#
# Mechanically, (2) is the `main() { … }; main < /dev/tty` form at the bottom of
# this file rather than a top-of-file `exec < /dev/tty`. Under `curl | sh` the
# shell is *reading this script from fd 0*; replacing fd 0 mid-script makes it
# read the remainder of the program from the terminal — i.e. it starts executing
# whatever the user types. Redirecting only the call to `main` binds stdin for
# the entire program (including the `exec bravros setup` handoff, which inherits
# it) while leaving the shell's own script-source fd alone. Same outcome, no
# second download, no self-destruct.

set -eu

# ── Minisign public key ──────────────────────────────────────────────────────
# To verify this key is genuine, check https://bravros.dev/security
MINISIGN_PUBKEY="RWQqHlahq4RjNnCasO/8yMsgtLGfdHejILKMxxpsulIs1rII6IgMO26G"

# ── Constants ────────────────────────────────────────────────────────────────
GITHUB_REPO="bravros/bravros"
BASE_URL="https://github.com/${GITHUB_REPO}/releases/latest/download"
BIN_DIR="$HOME/.claude/bin"
UNINSTALLER="${BIN_DIR}/bravros-uninstall"

# ── Argument parsing ─────────────────────────────────────────────────────────
# --force skips the installed-version check below and always reinstalls.
# Works with both invocation idioms: `bash -c "$(curl -fsSL …)" -- --force`
# and `curl -fsSL … | sh -s -- --force` — the `--` placeholder absorbs $0 so
# "$@" here is just the flags.
FORCE=false
for arg in "$@"; do
  case "$arg" in
    --force) FORCE=true ;;
  esac
done

# ── TTY gate ─────────────────────────────────────────────────────────────────
# First statement of substance, per D9. Everything decorative — colour, the
# spinner, the badges — is gated on this. A log file or a CI transcript gets
# plain lines with no escape codes and no carriage-return animation.
NO_TTY=false
if [ ! -t 1 ]; then
  NO_TTY=true
fi
if [ -n "${NO_COLOR:-}" ] || [ "${TERM:-}" = "dumb" ]; then
  NO_TTY=true
fi

if [ "$NO_TTY" = true ]; then
  C_RESET=''; C_INFO=''; C_OK=''; C_ERR=''; C_DIM=''; C_BOLD=''
else
  C_RESET="$(printf '\033[0m')"
  C_INFO="$(printf '\033[48;5;25m\033[97m')"   # white on blue
  C_OK="$(printf '\033[48;5;28m\033[97m')"     # white on green
  C_ERR="$(printf '\033[48;5;124m\033[97m')"   # white on red
  C_DIM="$(printf '\033[2m')"
  C_BOLD="$(printf '\033[1m')"
fi

# ── Helpers ──────────────────────────────────────────────────────────────────
# ANSI badge helpers: a coloured-background label, then the message.
info() { printf '%s INFO %s %s\n' "$C_INFO" "$C_RESET" "$*"; }
ok()   { printf '%s  OK  %s %s\n' "$C_OK" "$C_RESET" "$*"; }
warn() { printf '%s WARN %s %s\n' "$C_ERR" "$C_RESET" "$*" >&2; }
die()  { printf '%s FAIL %s %s\n' "$C_ERR" "$C_RESET" "$*" >&2; exit 1; }
require() { command -v "$1" >/dev/null 2>&1 || die "Required tool not found: $1"; }

spinner_frame() {
  case $(( $1 % 10 )) in
    0) printf '⠋' ;;
    1) printf '⠙' ;;
    2) printf '⠹' ;;
    3) printf '⠸' ;;
    4) printf '⠼' ;;
    5) printf '⠴' ;;
    6) printf '⠦' ;;
    7) printf '⠧' ;;
    8) printf '⠇' ;;
    *) printf '⠏' ;;
  esac
}

# spin "<message>" <command...> — runs the command, animating while it works.
# Output is captured; it is replayed only on failure, so a happy path stays one
# line and a broken one still shows curl/minisign's own error text.
spin() {
  spin_msg="$1"
  shift
  if [ "$NO_TTY" = true ]; then
    info "$spin_msg"
    "$@"
    return $?
  fi

  spin_log="${TMPDIR_BRAVROS}/spin.log"
  "$@" >"$spin_log" 2>&1 &
  spin_pid=$!
  spin_i=0
  while kill -0 "$spin_pid" 2>/dev/null; do
    printf '\r  %s %s' "$(spinner_frame "$spin_i")" "$spin_msg"
    spin_i=$(( spin_i + 1 ))
    sleep 0.1
  done
  spin_rc=0
  wait "$spin_pid" || spin_rc=$?
  printf '\r\033[2K'
  if [ "$spin_rc" -ne 0 ] && [ -s "$spin_log" ]; then
    cat "$spin_log" >&2
  fi
  return "$spin_rc"
}

ensure_minisign() {
  command -v minisign >/dev/null 2>&1 && return 0
  info "Installing minisign (needed to verify the download)..."
  if command -v brew >/dev/null 2>&1; then
    brew install minisign >/dev/null 2>&1 || die "brew install minisign failed"
  elif command -v apt-get >/dev/null 2>&1; then
    sudo apt-get install -y minisign >/dev/null 2>&1 || die "apt-get install minisign failed"
  else
    die "Install minisign first: https://jedisct1.github.io/minisign/"
  fi
}

# Abort rather than shadow a Homebrew-managed install. Two bravros binaries on
# PATH is the worst outcome available: `brew upgrade` moves one, this installer
# moves the other, and which one runs depends on PATH order the user never sees.
detect_brew_managed() {
  [ -n "${BRAVROS_ALLOW_BREW_SHADOW:-}" ] && return 0
  command -v brew >/dev/null 2>&1 || return 0
  brew list --formula bravros >/dev/null 2>&1 || return 0
  printf '%s FAIL %s bravros is already installed via Homebrew.\n' "$C_ERR" "$C_RESET" >&2
  printf '\n  Installing into %s would shadow it — whichever comes first on\n' "$BIN_DIR" >&2
  printf '  PATH would win, silently. Pick one:\n\n' >&2
  printf '    Upgrade the Homebrew copy:   %sbrew upgrade bravros%s\n' "$C_BOLD" "$C_RESET" >&2
  printf '    Or switch to this installer: %sbrew uninstall bravros%s, then re-run this script\n\n' "$C_BOLD" "$C_RESET" >&2
  printf '  %s(override with BRAVROS_ALLOW_BREW_SHADOW=1 if you know what you are doing)%s\n' "$C_DIM" "$C_RESET" >&2
  exit 1
}

# Shell detection → the profile files that shell actually reads.
# Use `if`, not an `[ ] && ... && ...` chain. Under `set -e` that chain returns 1
# on a machine with no ~/.zshrc and no ~/.bashrc — a genuinely fresh box — which
# killed the installer silently right after extracting the binary: no PATH entry,
# no settings registration, no "Installed" line, and exit 0 so nothing looked wrong.
profile_candidates() {
  case "$(basename "${SHELL:-sh}")" in
    zsh)  printf '%s\n' "${ZDOTDIR:-$HOME}/.zshrc" ;;
    bash) printf '%s\n' "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.profile" ;;
    fish) printf '%s\n' "$HOME/.config/fish/config.fish" ;;
    ksh)  printf '%s\n' "$HOME/.kshrc" "$HOME/.profile" ;;
    *)    printf '%s\n' "$HOME/.profile" ;;
  esac
}

# The `$HOME` and `$PATH` in these strings are literal on purpose — the line is
# written into a profile for the *user's* shell to expand at login, not expanded
# here. Hard-coding the resolved path would break the moment $HOME differs.
# shellcheck disable=SC2016
path_line_for() {
  case "$1" in
    *fish*) printf 'set -gx PATH "$HOME/.claude/bin" $PATH\n' ;;
    *)      printf 'export PATH="$HOME/.claude/bin:$PATH"\n' ;;
  esac
}

# Always ends by printing the exact line to add by hand. Automatic profile
# editing fails in more ways than it succeeds — chezmoi-managed dotfiles,
# read-only homes, a shell we did not guess — and a user who is told the literal
# line is never stuck.
add_to_path() {
  case ":${PATH}:" in
    *":${BIN_DIR}:"*) ok "${BIN_DIR} is already on PATH"; return 0 ;;
  esac

  # The line is resolved into a variable BEFORE the append. Calling
  # path_line_for inside the redirected group would read `$rc` in the same
  # pipeline that writes to it (SC2094) — harmless here, but the variable form
  # is both clearer and lint-clean.
  written=''
  for rc in $(profile_candidates); do
    if [ -f "$rc" ]; then
      if ! grep -qF '.claude/bin' "$rc"; then
        line="$(path_line_for "$rc")"
        printf '\n# Added by the Bravros installer\n%s\n' "$line" >> "$rc"
      fi
      written="$rc"
      break
    fi
  done

  # No profile file existed at all — create the first candidate rather than
  # leaving a fresh box with a binary it cannot find.
  if [ -z "$written" ]; then
    rc="$(profile_candidates | head -n 1)"
    line="$(path_line_for "$rc")"
    if mkdir -p "$(dirname "$rc")" 2>/dev/null &&
       printf '\n# Added by the Bravros installer\n%s\n' "$line" >> "$rc" 2>/dev/null; then
      written="$rc"
    fi
  fi

  if [ -n "$written" ]; then
    ok "PATH updated in ${written}"
  else
    warn "Could not update any shell profile automatically."
  fi

  printf '\n  %sAdd this line to your shell profile if the command is not found:%s\n' "$C_DIM" "$C_RESET"
  printf '    %s%s%s\n\n' "$C_BOLD" "$(path_line_for "${written:-sh}" | tr -d '\n')" "$C_RESET"
}

# A generated uninstaller sits next to the binary. It removes only what this
# script created; skills, templates and settings belong to `bravros setup` and
# are listed for the user rather than deleted behind their back.
write_uninstaller() {
  cat > "$UNINSTALLER" <<'UNINSTALL_EOF'
#!/bin/sh
# Generated by the Bravros installer. Removes the binary this installer placed.
set -eu
BIN_DIR="$HOME/.claude/bin"
printf 'Removing %s/bravros...\n' "$BIN_DIR"
rm -f "$BIN_DIR/bravros"
printf '\nRemoved the bravros binary.\n\nNot removed (yours to keep or delete):\n'
printf '  ~/.claude/skills      installed skills\n'
printf '  ~/.claude/templates   installed templates\n'
printf '  ~/.claude/settings.json  (the Bravros hook entries are marked as managed)\n'
printf '\nAlso remove the PATH line the installer added to your shell profile:\n'
# Literal on purpose: this is the line to DELETE from a profile, quoted verbatim
# so the user can match it by eye. Expanding it here would print a resolved path
# that does not appear in any file.
# shellcheck disable=SC2016
printf '  export PATH="$HOME/.claude/bin:$PATH"\n\n'
rm -f "$BIN_DIR/bravros-uninstall"
UNINSTALL_EOF
  chmod +x "$UNINSTALLER"
}

# resolve_latest_tag — same technique as cli/internal/fetch.ResolveLatestTag:
# follow the redirect from .../releases/latest and read the tag out of the
# resolved URL, instead of calling api.github.com (keeps this script off the
# unauthenticated 60/hr API rate limit). Prints the tag (e.g. "v3.51.0") on
# success; prints nothing and returns non-zero on any failure — callers must
# treat that as "skip the check, fall back to always-download".
resolve_latest_tag() {
  resolved_url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
    "https://github.com/${GITHUB_REPO}/releases/latest" 2>/dev/null)" || return 1
  resolved_tag="${resolved_url##*/}"
  [ -n "$resolved_tag" ] && [ "$resolved_tag" != "latest" ] || return 1
  printf '%s\n' "$resolved_tag"
}

# detect_installed_version — checks PATH first, then the install destination.
# `bravros version` prints "bravros vX.Y.Z"; this prints just "vX.Y.Z" on
# success. Prints nothing and returns non-zero when no usable binary exists.
detect_installed_version() {
  candidate=""
  if command -v bravros >/dev/null 2>&1; then
    candidate="$(command -v bravros)"
  elif [ -x "${BIN_DIR}/bravros" ]; then
    candidate="${BIN_DIR}/bravros"
  fi
  [ -n "$candidate" ] || return 1
  installed_version="$("$candidate" version 2>/dev/null | awk '{print $2}')"
  [ -n "$installed_version" ] || return 1
  printf '%s\n' "$installed_version"
}

# ── Main ─────────────────────────────────────────────────────────────────────
main() {
  # Platform detection
  OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
  ARCH="$(uname -m)"
  case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
  esac

  case "$OS" in
    darwin|linux) ;;
    mingw*|msys*|cygwin*)
      die "Native Windows: use PowerShell instead — irm https://install.bravros.dev/install.ps1 | iex" ;;
    *) die "Unsupported OS: $OS" ;;
  esac

  # linux/arm64 used to be refused here. It is published now — the GoReleaser
  # `ignore:` block that blocked it was removed in the same change (P-0015
  # Phase 5). Unblocking one without the other is what the dossier warns about.
  case "$OS-$ARCH" in
    darwin-amd64|darwin-arm64|linux-amd64|linux-arm64) ;;
    *) die "Unsupported platform: ${OS}/${ARCH}" ;;
  esac

  TARBALL="bravros-${OS}-${ARCH}.tar.gz"

  require curl
  require tar

  # Version check (skipped entirely by --force). Any failure to resolve the
  # latest tag or the locally installed version falls through to the
  # always-download behavior below — an installer must never brick on this.
  if [ "$FORCE" != true ]; then
    LATEST_TAG="$(resolve_latest_tag)" || LATEST_TAG=""
    if [ -n "$LATEST_TAG" ]; then
      CURRENT_VERSION="$(detect_installed_version)" || CURRENT_VERSION=""
      if [ -n "$CURRENT_VERSION" ]; then
        if [ "$CURRENT_VERSION" = "$LATEST_TAG" ]; then
          # No download — but re-running the one-liner stays the repair path:
          # still hand off to `bravros setup`, which is idempotent and reports
          # ALREADY CORRECT / CHANGED / SKIPPED per component, so a broken
          # settings.json or component selection gets fixed instead of a
          # silent no-op.
          ok "already current (${CURRENT_VERSION}) — no download needed"
          # The current binary may live at BIN_DIR (installer-owned) or only on
          # PATH (e.g. brew); hand off to whichever actually exists.
          SETUP_BIN="${BIN_DIR}/bravros"
          [ -x "$SETUP_BIN" ] || SETUP_BIN="$(command -v bravros || true)"
          if [ -n "$SETUP_BIN" ] && [ -t 0 ] && [ "$NO_TTY" = false ]; then
            exec "$SETUP_BIN" setup
          fi
          [ -n "$SETUP_BIN" ] && printf '  Binary already current; run %s setup to verify or repair components.\n' "$SETUP_BIN"
          exit 0
        fi
        info "updating ${CURRENT_VERSION} → ${LATEST_TAG}"
      fi
    fi
  fi

  detect_brew_managed
  ensure_minisign

  TMPDIR_BRAVROS="$(mktemp -d)"
  trap 'rm -rf "$TMPDIR_BRAVROS"' EXIT INT TERM

  printf '\n  %sBravros%s %sinstaller%s\n\n' "$C_BOLD" "$C_RESET" "$C_DIM" "$C_RESET"

  spin "Downloading bravros for ${OS}/${ARCH}..." \
    sh -c "curl -fsSL '${BASE_URL}/${TARBALL}' -o '${TMPDIR_BRAVROS}/${TARBALL}' &&
           curl -fsSL '${BASE_URL}/checksums.txt' -o '${TMPDIR_BRAVROS}/checksums.txt' &&
           curl -fsSL '${BASE_URL}/checksums.txt.minisig' -o '${TMPDIR_BRAVROS}/checksums.txt.minisig'" \
    || die "Download failed — check your network, or https://github.com/${GITHUB_REPO}/releases"
  ok "Downloaded ${TARBALL}"

  # The trust chain, unchanged: minisign verifies checksums.txt against the
  # pinned pubkey above, then sha256 ties the archive to that signed manifest.
  # Every download stays verified — php.new verifies nothing; we do not copy that.
  spin "Verifying minisign signature..." \
    minisign -Vm "${TMPDIR_BRAVROS}/checksums.txt" \
      -P "${MINISIGN_PUBKEY}" \
      -x "${TMPDIR_BRAVROS}/checksums.txt.minisig" \
    || die "Signature verification failed — download may be tampered!"
  ok "Signature valid"

  spin "Verifying SHA256 checksum..." \
    sh -c "cd '${TMPDIR_BRAVROS}' && grep '${TARBALL}' checksums.txt |
           { command -v sha256sum >/dev/null 2>&1 && sha256sum -c - || shasum -a 256 -c -; }" \
    || die "SHA256 mismatch — download may be corrupted!"
  ok "Checksum verified"

  info "Installing to ${BIN_DIR}/bravros..."
  mkdir -p "${BIN_DIR}"
  tar -xzf "${TMPDIR_BRAVROS}/${TARBALL}" -C "${BIN_DIR}" bravros
  chmod +x "${BIN_DIR}/bravros"
  write_uninstaller
  ok "Installed $("${BIN_DIR}/bravros" version 2>/dev/null || echo bravros)"

  add_to_path

  # Hand off. `bravros setup` owns every question and every write into the
  # user's home (skills, templates, the managed settings.json merge) — this
  # script deliberately knows nothing about them.
  if [ -t 0 ] && [ "$NO_TTY" = false ]; then
    exec "${BIN_DIR}/bravros" setup
  fi

  printf '\n%sBravros installed.%s No terminal attached, so the setup wizard was skipped.\n\n' "$C_BOLD" "$C_RESET"
  printf '  Run it when you are at a prompt:\n\n'
  printf '    %s setup\n\n' "${BIN_DIR}/bravros"
  printf '  Or non-interactively:\n\n'
  printf '    %s setup --all --yes\n\n' "${BIN_DIR}/bravros"
  printf '  Then, inside a git repository:\n\n'
  printf '    cd /path/to/your-project\n    bravros init\n\n'
  printf '  To uninstall: %s\n\n' "$UNINSTALLER"
}

# See "The stdin decision" at the top of this file. Binding /dev/tty here — and
# only here — is what lets the piped one-liner still reach an interactive
# wizard; a CI runner with no controllable terminal falls through to the
# non-interactive branch inside main instead of failing.
if [ ! -t 0 ] && [ -r /dev/tty ] && [ -c /dev/tty ]; then
  main < /dev/tty
else
  main
fi
