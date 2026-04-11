#!/usr/bin/env bash

# Bravros Installer
# AI-native SDLC. License-enforced. Zero bloat.
#
# Usage:
#   curl -fsSL https://bravros.dev/install | bash    # remote install
#   bash install.sh                                   # local install
#   bash install.sh --uninstall                       # remove everything
#   bash install.sh --dry-run                         # print steps without executing
#
# Repo: github.com/bravros/bravros

set -euo pipefail

# ============================================================================
# CONSTANTS
# ============================================================================

BRAVROS_VERSION="1.0.0"
GITHUB_REPO="bravros/bravros"
GITHUB_API="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
TS="$(date +%Y%m%d-%H%M%S)"
INSTALL_DIR="$HOME/.claude"
BIN_DIR="$INSTALL_DIR/bin"
SKILLS_DIR="$INSTALL_DIR/skills"
TEMPLATES_DIR="$INSTALL_DIR/templates"

DRY_RUN=false
UNINSTALL=false

# ============================================================================
# PHASE 1: PLATFORM DETECTION
# ============================================================================

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

case "$ARCH" in
  amd64|arm64) ;;
  *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

BINARY_NAME="bravros-${OS}-${ARCH}"

# sed_inplace — cross-platform sed -i
sed_inplace() {
  if [ "$OS" = "darwin" ]; then
    sed -i '' "$@"
  else
    sed -i "$@"
  fi
}

# Detect shell RC file
if [[ "$SHELL" == */zsh ]] && [ -f "$HOME/.zshrc" ]; then
  SHELL_RC="$HOME/.zshrc"
elif [[ "$SHELL" == */bash ]] && [ -f "$HOME/.bashrc" ]; then
  SHELL_RC="$HOME/.bashrc"
elif [ -f "$HOME/.zshrc" ]; then
  SHELL_RC="$HOME/.zshrc"
elif [ -f "$HOME/.bashrc" ]; then
  SHELL_RC="$HOME/.bashrc"
else
  SHELL_RC=""
fi

# Detect install mode
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || echo "")"
if [ -n "$SCRIPT_DIR" ] && [ -d "$SCRIPT_DIR/skills" ] && [ -d "$SCRIPT_DIR/cli" ]; then
  INSTALL_MODE="local"
else
  INSTALL_MODE="remote"
fi

# ============================================================================
# HELPERS — ANSI-colored output (no gum dependency)
# ============================================================================

_BOLD='\033[1m'
_DIM='\033[2m'
_RESET='\033[0m'
_GREEN='\033[38;5;82m'
_YELLOW='\033[38;5;220m'
_RED='\033[38;5;196m'
_CYAN='\033[38;5;45m'
_BLUE='\033[38;5;33m'
_MAGENTA='\033[38;5;141m'

ok()   { printf "${_GREEN}  ✓${_RESET} %s\n" "$*"; }
info() { printf "${_CYAN}  →${_RESET} %s\n" "$*"; }
warn() { printf "${_YELLOW}  ⚠${_RESET} %s\n" "$*"; }
err()  { printf "${_RED}  ✗${_RESET} %s\n" "$*" >&2; }
step() { printf "\n${_BOLD}${_BLUE}  ▸ %s${_RESET}\n" "$*"; }

# ============================================================================
# PHASE 2: ASCII BANNER
# ============================================================================

show_banner() {
  local c1='\033[38;5;33m'   # blue
  local c2='\033[38;5;39m'
  local c3='\033[38;5;45m'   # cyan
  local c4='\033[38;5;51m'
  local c5='\033[38;5;87m'
  local c6='\033[38;5;123m'
  local r='\033[0m'

  printf "\n"
  printf "  ${c1}██████╗ ██████╗  █████╗ ██╗   ██╗██████╗  ██████╗ ███████╗${r}\n"
  printf "  ${c2}██╔══██╗██╔══██╗██╔══██╗██║   ██║██╔══██╗██╔═══██╗██╔════╝${r}\n"
  printf "  ${c3}██████╔╝██████╔╝███████║██║   ██║██████╔╝██║   ██║███████╗${r}\n"
  printf "  ${c4}██╔══██╗██╔══██╗██╔══██║╚██╗ ██╔╝██╔══██╗██║   ██║╚════██║${r}\n"
  printf "  ${c5}██████╔╝██║  ██║██║  ██║ ╚████╔╝ ██║  ██║╚██████╔╝███████║${r}\n"
  printf "  ${c6}╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═╝  ╚═══╝  ╚═╝  ╚═╝ ╚═════╝ ╚══════╝${r}\n"
  printf "\n"
  printf "  ${_DIM}AI-native SDLC. License-enforced. Zero bloat.${_RESET}\n"
  printf "  ${_DIM}Version: ${BRAVROS_VERSION}  •  ${OS}/${ARCH}  •  ${INSTALL_MODE} mode${_RESET}\n"
  printf "\n"
}

# ============================================================================
# PARSE ARGUMENTS
# ============================================================================

for arg in "$@"; do
  case "$arg" in
    --dry-run)   DRY_RUN=true ;;
    --uninstall) UNINSTALL=true ;;
  esac
done

show_banner

if $DRY_RUN; then
  info "DRY RUN — no changes will be made"
  printf "\n"
fi

# ============================================================================
# PHASE 3: UNINSTALL
# ============================================================================

if $UNINSTALL; then
  step "Uninstalling Bravros"

  # Known Bravros skills (same list used in Phase 7 for deployment)
  BRAVROS_SKILLS=(
    _shared address-pr address-recap audit auto-merge auto-pr auto-pr-wt
    backlog branch brand-generator brand-guidelines cf-browser cf-pages-deploy
    cloudflare-auto-deploy commit complete context coverage debug drop-feature
    excalidraw-diagram firecrawl finish flow frontend-design generate-component
    home-assistant-manager hotfix laravel-db-diagram merge-chain migration-audit
    n8n notebooklm obsidian-migrate obsidian-setup plan plan-approved plan-check
    plan-review plan-wt pr push quick remove-watermark report resume review
    run-tests session-recap ship simplify skill-creator squash-migrations start
    status sync-upstream taste-skill tdd-review test unifi update-config
    update-hooks uptime-kuma user-report verify-install workflow-sync yt-search
    listmonk schedule loop
  )

  if ! $DRY_RUN; then
    # Remove binary
    rm -f "$BIN_DIR/bravros"
    ok "Removed binary"

    # Remove skills
    for s in "${BRAVROS_SKILLS[@]}"; do
      rm -rf "$SKILLS_DIR/$s" 2>/dev/null || true
    done
    ok "Removed skills"

    # Remove audit hook from settings.json
    if [ -f "$INSTALL_DIR/settings.json" ]; then
      if grep -q 'bravros audit' "$INSTALL_DIR/settings.json" 2>/dev/null; then
        # Remove the entire PreToolUse hook block containing bravros audit
        python3 -c "
import json
from pathlib import Path
p = Path.home() / '.claude' / 'settings.json'
cfg = json.loads(p.read_text())
hooks = cfg.get('hooks', {})
ptu = hooks.get('PreToolUse', [])
hooks['PreToolUse'] = [h for h in ptu if 'bravros audit' not in json.dumps(h)]
if not hooks['PreToolUse']:
    del hooks['PreToolUse']
# Remove SessionStart bravros update hook
ss = hooks.get('SessionStart', [])
hooks['SessionStart'] = [h for h in ss if 'bravros update' not in json.dumps(h)]
if not hooks['SessionStart']:
    del hooks['SessionStart']
cfg['hooks'] = hooks
# Remove statusLine if it references bravros
sl = cfg.get('statusLine', {})
if isinstance(sl, dict) and 'bravros' in sl.get('command', ''):
    del cfg['statusLine']
p.write_text(json.dumps(cfg, indent=2) + '\n')
" 2>/dev/null || true
        ok "Removed audit hook and statusLine from settings.json"
      fi
    fi

    # Remove PATH entry from shell RC
    if [ -n "$SHELL_RC" ] && [ -f "$SHELL_RC" ]; then
      if grep -q '# Bravros' "$SHELL_RC" 2>/dev/null; then
        sed_inplace '/# Bravros/d' "$SHELL_RC"
        sed_inplace '/\.claude\/bin/d' "$SHELL_RC"
        ok "Removed PATH entry from $SHELL_RC"
      fi
    fi
  else
    info "Would remove: $BIN_DIR/bravros"
    info "Would remove: ${#BRAVROS_SKILLS[@]} skills from $SKILLS_DIR"
    info "Would remove: audit hook and statusLine from settings.json"
    info "Would remove: PATH entry from $SHELL_RC"
  fi

  printf "\n"
  ok "Bravros has been uninstalled."
  exit 0
fi

# ============================================================================
# PHASE 4: BLUEPRINT MIGRATION DETECTION
# ============================================================================

if [ -d "$HOME/.blueprint" ]; then
  step "Blueprint installation detected"

  MIGRATE=true
  if [ -t 0 ]; then
    printf "  Migrate to Bravros? [Y/n] "
    read -r answer </dev/tty 2>/dev/null || answer="y"
    case "$answer" in
      [nN]*) MIGRATE=false ;;
    esac
  else
    info "Pipe mode — auto-migrating from Blueprint"
  fi

  if $MIGRATE && ! $DRY_RUN; then
    # Remove Blueprint directory
    rm -rf "$HOME/.blueprint"
    ok "Removed ~/.blueprint/"

    # Remove Blueprint skills
    BP_SKILLS=("bp-branch" "bp-commit" "bp-context" "bp-push" "bp-ship" "bp-status" "bp-tdd-review" "bp-test")
    for s in "${BP_SKILLS[@]}"; do
      rm -rf "$SKILLS_DIR/$s" 2>/dev/null || true
    done
    ok "Removed Blueprint skills"

    # Replace blueprint audit hook with bravros in settings.json
    if [ -f "$INSTALL_DIR/settings.json" ] && grep -q 'blueprint' "$INSTALL_DIR/settings.json" 2>/dev/null; then
      sed_inplace 's/blueprint audit/bravros audit/g' "$INSTALL_DIR/settings.json"
      sed_inplace 's/blueprint statusline/bravros statusline/g' "$INSTALL_DIR/settings.json"
      sed_inplace 's/blueprint selfupdate/bravros selfupdate/g' "$INSTALL_DIR/settings.json"
      ok "Migrated settings.json hooks from blueprint to bravros"
    fi

    # Remove .blueprint/bin PATH from shell RC
    if [ -n "$SHELL_RC" ] && [ -f "$SHELL_RC" ]; then
      if grep -q '\.blueprint/bin' "$SHELL_RC" 2>/dev/null; then
        sed_inplace '/\.blueprint\/bin/d' "$SHELL_RC"
        ok "Removed Blueprint PATH from $SHELL_RC"
      fi
    fi

    info "Blueprint removed — continuing Bravros install"
  elif $DRY_RUN; then
    info "Would remove ~/.blueprint/ and migrate settings"
  fi
fi

# ============================================================================
# PHASE 5: DIRECTORY SETUP
# ============================================================================

step "Setting up directories"

DIRS=("$BIN_DIR" "$SKILLS_DIR" "$TEMPLATES_DIR" "$INSTALL_DIR/hooks" "$INSTALL_DIR/scripts" "$INSTALL_DIR/cache")

if ! $DRY_RUN; then
  for d in "${DIRS[@]}"; do
    mkdir -p "$d"
  done
  ok "Created directory structure"

  # Clean up deprecated artifacts
  DEPRECATED_SKILLS=("criar-campanha" "mcp-builder" "prepare4kaisser" "linear-init")
  RENAMED_SKILLS=("flow-auto" "flow-auto-wt" "batch-flow")
  FIRECRAWL_VARIANTS=("firecrawl-agent" "firecrawl-browser" "firecrawl-crawl" "firecrawl-download" "firecrawl-map" "firecrawl-scrape" "firecrawl-search")
  BP_LEGACY=("bp-branch" "bp-commit" "bp-context" "bp-push" "bp-ship" "bp-status" "bp-tdd-review" "bp-test")

  cleaned=0
  for s in "${DEPRECATED_SKILLS[@]}" "${RENAMED_SKILLS[@]}" "${FIRECRAWL_VARIANTS[@]}" "${BP_LEGACY[@]}"; do
    if [ -d "$SKILLS_DIR/$s" ]; then
      rm -rf "$SKILLS_DIR/$s"
      cleaned=$((cleaned + 1))
    fi
  done

  # Remove deprecated directories
  for d in commands agents references; do
    if [ -d "$INSTALL_DIR/$d" ]; then
      rm -rf "$INSTALL_DIR/$d"
      cleaned=$((cleaned + 1))
    fi
  done

  # Remove deprecated files
  rm -f "$INSTALL_DIR/AGENTS.md" 2>/dev/null || true
  rm -f "$INSTALL_DIR/hooks/audit.py" 2>/dev/null || true

  # Remove stale .venv dirs in skills
  find "$SKILLS_DIR" -name ".venv" -type d -exec rm -rf {} + 2>/dev/null || true

  if [ "$cleaned" -gt 0 ]; then
    ok "Cleaned $cleaned deprecated artifacts"
  fi
else
  info "Would create: ${DIRS[*]}"
  info "Would clean deprecated artifacts"
fi

# ============================================================================
# PHASE 6: BINARY DOWNLOAD & INSTALL
# ============================================================================

step "Installing bravros binary"

install_binary() {
  local target="$BIN_DIR/bravros"

  if [ "$INSTALL_MODE" = "local" ]; then
    # Local mode — try multiple known locations
    local src=""
    if [ -f "$SCRIPT_DIR/bin/bravros" ]; then
      src="$SCRIPT_DIR/bin/bravros"
    elif [ -f "$SCRIPT_DIR/cli/${BINARY_NAME}" ]; then
      src="$SCRIPT_DIR/cli/${BINARY_NAME}"
    elif [ -f "$SCRIPT_DIR/bin/${BINARY_NAME}" ]; then
      src="$SCRIPT_DIR/bin/${BINARY_NAME}"
    fi
    if [ -n "$src" ]; then
      cp -f "$src" "$target"
      chmod +x "$target"
      ok "Installed binary from local build (${OS}/${ARCH})"
    else
      warn "Local binary not found in bin/ or cli/"
      warn "Build with: cd cli && go build -ldflags=\"-s -w\" -o ../bin/bravros ."
      return 1
    fi
  else
    # Remote mode — assets are .tar.gz archives from GoReleaser
    local tarball="${BINARY_NAME}.tar.gz"
    local tmp_tar="/tmp/${tarball}"
    local tmp_dir="/tmp/bravros-extract-$$"
    rm -f "$tmp_tar"
    rm -rf "$tmp_dir"
    mkdir -p "$tmp_dir"

    if command -v gh &>/dev/null; then
      info "Downloading via gh CLI..."
      if gh release download --repo "$GITHUB_REPO" --pattern "$tarball" --dir /tmp 2>/dev/null && [ -s "$tmp_tar" ]; then
        tar -xzf "$tmp_tar" -C "$tmp_dir" 2>/dev/null
        if [ -f "$tmp_dir/bravros" ]; then
          mv -f "$tmp_dir/bravros" "$target"
          chmod +x "$target"
          rm -rf "$tmp_dir" "$tmp_tar"
          ok "Downloaded binary via gh (${OS}/${ARCH})"
          return 0
        fi
      fi
      warn "gh download failed — falling back to curl"
    fi

    # Fallback: curl from GitHub API
    info "Downloading via curl..."
    local download_url
    download_url=$(curl -fsSL "$GITHUB_API" 2>/dev/null | \
      grep -o "\"browser_download_url\": *\"[^\"]*${tarball}\"" | \
      head -1 | \
      grep -o 'https://[^"]*' || true)

    if [ -n "$download_url" ]; then
      curl -fSL --progress-bar -o "$tmp_tar" "$download_url"
      if [ -s "$tmp_tar" ]; then
        tar -xzf "$tmp_tar" -C "$tmp_dir" 2>/dev/null
        if [ -f "$tmp_dir/bravros" ]; then
          mv -f "$tmp_dir/bravros" "$target"
          chmod +x "$target"
          rm -rf "$tmp_dir" "$tmp_tar"
          ok "Downloaded binary via curl (${OS}/${ARCH})"
        else
          err "Tarball did not contain bravros binary"
          rm -rf "$tmp_dir" "$tmp_tar"
          return 1
        fi
      else
        err "Download produced empty file"
        rm -rf "$tmp_dir" "$tmp_tar"
        return 1
      fi
    else
      err "Could not find release asset: $tarball"
      warn "Ensure a release exists at https://github.com/${GITHUB_REPO}/releases"
      rm -rf "$tmp_dir"
      return 1
    fi
  fi
}

if ! $DRY_RUN; then
  install_binary || true

  # macOS ad-hoc codesign
  if [ "$OS" = "darwin" ] && [ -f "$BIN_DIR/bravros" ]; then
    codesign -s - "$BIN_DIR/bravros" 2>/dev/null || true
  fi

  # Verify binary
  if [ -x "$BIN_DIR/bravros" ]; then
    _ver=$("$BIN_DIR/bravros" version 2>/dev/null || echo "installed (version check unavailable)")
    ok "Binary: $_ver"
  else
    warn "Binary installed but not executable"
  fi

  # Add to PATH
  if [ -n "$SHELL_RC" ]; then
    if ! grep -q '\.claude/bin' "$SHELL_RC" 2>/dev/null; then
      printf '\n# Bravros\nexport PATH="$HOME/.claude/bin:$PATH"\n' >> "$SHELL_RC"
      ok "Added ~/.claude/bin to PATH in $SHELL_RC"
    fi
  fi
  export PATH="$HOME/.claude/bin:$PATH"
else
  info "Would install binary: $BINARY_NAME → $BIN_DIR/bravros"
  info "Would add ~/.claude/bin to PATH in $SHELL_RC"
fi

# ============================================================================
# PHASE 7: SKILLS DEPLOYMENT
# ============================================================================

step "Installing skills"

if ! $DRY_RUN; then
  if [ "$INSTALL_MODE" = "local" ] && [ -d "$SCRIPT_DIR/skills" ]; then
    count=0
    total=$(find "$SCRIPT_DIR/skills" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')

    for item in "$SCRIPT_DIR/skills/"*/; do
      [ -d "$item" ] || continue
      name=$(basename "$item")
      count=$((count + 1))
      printf "\r  Installing skill %d/%d: %-30s" "$count" "$total" "$name"
      cp -rf "$item" "$SKILLS_DIR/"
    done
    printf "\r%80s\r" ""  # clear line

    # Remove macOS-only skills on Linux
    if [ "$OS" = "linux" ]; then
      rm -rf "$SKILLS_DIR/obsidian-setup" 2>/dev/null || true
      rm -rf "$SKILLS_DIR/ha-mac-unlock" 2>/dev/null || true
      info "Skipped macOS-only skills on Linux"
    fi

    ok "Installed ${count} skills to $SKILLS_DIR"

  elif [ "$INSTALL_MODE" = "remote" ]; then
    info "Downloading skills and templates from repo..."
    local_tmp="/tmp/bravros-repo-$$.tar.gz"
    tmpdir="/tmp/bravros-repo-$$"
    rm -f "$local_tmp"
    rm -rf "$tmpdir"
    mkdir -p "$tmpdir"

    downloaded=false

    # Download repo archive via gh (handles private repo auth)
    if command -v gh &>/dev/null; then
      if gh api repos/${GITHUB_REPO}/tarball -H "Accept: application/vnd.github+json" > "$local_tmp" 2>/dev/null && [ -s "$local_tmp" ]; then
        downloaded=true
      fi
    fi

    if $downloaded; then
      tar -xzf "$local_tmp" -C "$tmpdir" --strip-components=1 2>/dev/null

      # Install skills
      if [ -d "$tmpdir/skills" ]; then
        count=0
        total=$(find "$tmpdir/skills" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')
        for item in "$tmpdir/skills/"*/; do
          [ -d "$item" ] || continue
          name=$(basename "$item")
          count=$((count + 1))
          printf "\r  Installing skill %d/%d: %-30s" "$count" "$total" "$name"
          cp -rf "$item" "$SKILLS_DIR/"
        done
        printf "\r%80s\r" ""

        # Remove macOS-only skills on Linux
        if [ "$OS" = "linux" ]; then
          rm -rf "$SKILLS_DIR/obsidian-setup" 2>/dev/null || true
          rm -rf "$SKILLS_DIR/ha-mac-unlock" 2>/dev/null || true
        fi

        ok "Installed ${count} skills to $SKILLS_DIR"
      else
        warn "No skills directory found in repo archive"
      fi

      # Install templates
      if [ -d "$tmpdir/templates" ]; then
        cp -rf "$tmpdir/templates/." "$TEMPLATES_DIR/"
        chmod +x "$TEMPLATES_DIR/.githooks/commit-msg" 2>/dev/null || true
        chmod +x "$TEMPLATES_DIR/.githooks/pre-push" 2>/dev/null || true
        ok "Templates installed from repo archive"
      fi

      rm -rf "$tmpdir" "$local_tmp"
    else
      warn "Could not download repo archive — skills and templates not installed"
      warn "Ensure you have access to https://github.com/${GITHUB_REPO}"
      rm -rf "$tmpdir" "$local_tmp"
    fi
  else
    warn "No skills source found"
  fi
else
  info "Would install skills from $INSTALL_MODE source"
fi

# ============================================================================
# PHASE 8: TEMPLATES DEPLOYMENT
# ============================================================================

step "Installing templates"

if ! $DRY_RUN; then
  if [ "$INSTALL_MODE" = "local" ] && [ -d "$SCRIPT_DIR/templates" ]; then
    cp -rf "$SCRIPT_DIR/templates/." "$TEMPLATES_DIR/"
    chmod +x "$TEMPLATES_DIR/.githooks/commit-msg" 2>/dev/null || true
    chmod +x "$TEMPLATES_DIR/.githooks/pre-push" 2>/dev/null || true
    ok "Templates installed (git hooks, GitHub Actions, CLAUDE.md, scaffold)"
  elif [ "$INSTALL_MODE" = "remote" ]; then
    # Templates are bundled in the skills tarball or in the binary
    info "Templates: included in release tarball (already extracted)"
    chmod +x "$TEMPLATES_DIR/.githooks/commit-msg" 2>/dev/null || true
    chmod +x "$TEMPLATES_DIR/.githooks/pre-push" 2>/dev/null || true
  else
    warn "No templates source found"
  fi
else
  info "Would install templates to $TEMPLATES_DIR"
fi

# ============================================================================
# PHASE 9: SETTINGS.JSON CONFIGURATION
# ============================================================================

step "Configuring settings.json"

configure_settings() {
  local settings="$INSTALL_DIR/settings.json"

  if [ -f "$settings" ]; then
    # Backup
    cp "$settings" "${settings}.bak.${TS}"
    info "Backed up settings.json"

    # Granular merge using python3 (idempotent)
    python3 -c "
import json
from pathlib import Path

p = Path.home() / '.claude' / 'settings.json'
cfg = json.loads(p.read_text())

# Ensure env block
cfg.setdefault('env', {})
cfg['env'].setdefault('CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS', '1')

# Ensure permissions block
if 'permissions' not in cfg:
    cfg['permissions'] = {
        'allow': [
            'Bash(git add:*)',
            'Bash(git push:*)',
            'mcp__sequential-thinking__sequentialthinking',
            '/commit'
        ],
        'deny': [],
        'ask': [],
        'defaultMode': 'dontAsk'
    }

# Ensure hooks
cfg.setdefault('hooks', {})

# PreToolUse audit hook
ptu = cfg['hooks'].get('PreToolUse', [])
has_bravros_audit = any('bravros audit' in json.dumps(h) for h in ptu)
has_blueprint_audit = any('blueprint audit' in json.dumps(h) for h in ptu)

if has_blueprint_audit and not has_bravros_audit:
    # Replace blueprint with bravros
    for h in ptu:
        s = json.dumps(h)
        if 'blueprint audit' in s:
            s = s.replace('blueprint audit', 'bravros audit')
            idx = ptu.index(h)
            ptu[idx] = json.loads(s)
    cfg['hooks']['PreToolUse'] = ptu
elif not has_bravros_audit:
    ptu.append({
        'matcher': '.*',
        'hooks': [{
            'type': 'command',
            'command': '\$HOME/.claude/bin/bravros audit'
        }]
    })
    cfg['hooks']['PreToolUse'] = ptu

# SessionStart update hook
ss = cfg['hooks'].get('SessionStart', [])
has_bravros_update = any('bravros update' in json.dumps(h) for h in ss)
if not has_bravros_update:
    ss.append({
        'hooks': [{
            'type': 'command',
            'command': '\$HOME/.claude/bin/bravros update'
        }]
    })
    cfg['hooks']['SessionStart'] = ss

# StatusLine — always set to bravros (replaces claude-cli and blueprint)
sl = cfg.get('statusLine', {})
if not isinstance(sl, dict) or 'bravros' not in sl.get('command', ''):
    cfg['statusLine'] = {
        'type': 'command',
        'command': '\$HOME/.claude/bin/bravros statusline'
    }

# Replace claude-cli selfupdate with bravros update in existing SessionStart
for entry in cfg['hooks'].get('SessionStart', []):
    entry['hooks'] = [h for h in entry.get('hooks', []) if 'claude-cli selfupdate' not in h.get('command', '')]
cfg['hooks']['SessionStart'] = [e for e in cfg['hooks'].get('SessionStart', []) if e.get('hooks')]

p.write_text(json.dumps(cfg, indent=2) + '\n')
" 2>/dev/null
    ok "Merged settings.json (idempotent)"

  else
    # Create fresh settings.json
    cat > "$settings" <<'SETTINGS'
{
  "env": {
    "CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": "1"
  },
  "permissions": {
    "allow": [
      "Bash(git add:*)",
      "Bash(git push:*)",
      "mcp__sequential-thinking__sequentialthinking",
      "/commit"
    ],
    "deny": [],
    "ask": [],
    "defaultMode": "dontAsk"
  },
  "hooks": {
    "PreToolUse": [
      {
        "matcher": ".*",
        "hooks": [
          {
            "type": "command",
            "command": "$HOME/.claude/bin/bravros audit"
          }
        ]
      }
    ],
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "$HOME/.claude/bin/bravros update"
          }
        ]
      }
    ]
  },
  "statusLine": {
    "type": "command",
    "command": "$HOME/.claude/bin/bravros statusline"
  }
}
SETTINGS
    ok "Created settings.json"
  fi
}

if ! $DRY_RUN; then
  configure_settings
else
  info "Would configure settings.json with audit hook and statusLine"
fi

# ============================================================================
# PHASE 10: MCP SERVER REGISTRATION
# ============================================================================

step "Registering MCP servers"

register_mcp() {
  local mcp_file="$INSTALL_DIR/mcp.json"

  # Check if already registered
  if [ -f "$mcp_file" ]; then
    has_context7=$(grep -c '"context7"' "$mcp_file" 2>/dev/null || echo "0")
    has_seqthink=$(grep -c '"sequential-thinking"' "$mcp_file" 2>/dev/null || echo "0")
    if [ "$has_context7" -gt 0 ] && [ "$has_seqthink" -gt 0 ]; then
      ok "MCP servers already registered (Context7 + Sequential Thinking)"
      return 0
    fi
  fi

  # Backup if exists
  if [ -f "$mcp_file" ]; then
    cp "$mcp_file" "${mcp_file}.bak.${TS}"
  fi

  # Try claude mcp add-json first
  local used_claude=false
  if command -v claude &>/dev/null; then
    if ! grep -q '"context7"' "$mcp_file" 2>/dev/null; then
      claude mcp add-json context7 '{"command":"npx","args":["-y","@upstash/context7-mcp"]}' 2>/dev/null && used_claude=true || true
    fi
    if ! grep -q '"sequential-thinking"' "$mcp_file" 2>/dev/null; then
      claude mcp add-json sequential-thinking '{"command":"npx","args":["-y","@modelcontextprotocol/server-sequential-thinking"]}' 2>/dev/null || true
    fi
  fi

  # Fallback: write mcp.json directly
  if [ ! -f "$mcp_file" ] || { ! grep -q '"context7"' "$mcp_file" 2>/dev/null && ! grep -q '"sequential-thinking"' "$mcp_file" 2>/dev/null; }; then
    if [ -f "$mcp_file" ] && python3 -c "import json" 2>/dev/null; then
      # Merge into existing
      python3 -c "
import json
from pathlib import Path

p = Path.home() / '.claude' / 'mcp.json'
if p.exists():
    cfg = json.loads(p.read_text())
else:
    cfg = {}

servers = cfg.setdefault('mcpServers', {})

if 'context7' not in servers:
    servers['context7'] = {
        'command': 'npx',
        'args': ['-y', '@upstash/context7-mcp']
    }

if 'sequential-thinking' not in servers:
    servers['sequential-thinking'] = {
        'command': 'npx',
        'args': ['-y', '@modelcontextprotocol/server-sequential-thinking']
    }

p.write_text(json.dumps(cfg, indent=4) + '\n')
" 2>/dev/null
    else
      # Create fresh
      cat > "$mcp_file" <<'MCP'
{
    "mcpServers": {
        "context7": {
            "command": "npx",
            "args": [
                "-y",
                "@upstash/context7-mcp"
            ]
        },
        "sequential-thinking": {
            "command": "npx",
            "args": [
                "-y",
                "@modelcontextprotocol/server-sequential-thinking"
            ]
        }
    }
}
MCP
    fi
  fi

  chmod 600 "$mcp_file" 2>/dev/null || true
  ok "MCP servers registered (Context7 + Sequential Thinking)"
}

if ! $DRY_RUN; then
  register_mcp
else
  info "Would register MCP servers: Context7, Sequential Thinking"
fi

# ============================================================================
# PHASE 11.5: POST-INSTALL VERIFICATION
# ============================================================================

if ! $DRY_RUN; then
  step "Verifying installation"

  _verify_ok=true

  # Check binary
  if [ -x "$BIN_DIR/bravros" ]; then
    ok "Binary: $BIN_DIR/bravros"
  else
    warn "Binary missing or not executable"
    _verify_ok=false
  fi

  # Check settings.json is valid JSON
  if [ -f "$INSTALL_DIR/settings.json" ]; then
    if python3 -c "import json; json.load(open('$INSTALL_DIR/settings.json'))" 2>/dev/null; then
      ok "settings.json: valid JSON"
    else
      warn "settings.json: invalid JSON — may need manual fix"
      _verify_ok=false
    fi
  else
    warn "settings.json: missing"
    _verify_ok=false
  fi

  # Check MCP config
  if [ -f "$INSTALL_DIR/mcp.json" ]; then
    if python3 -c "import json; json.load(open('$INSTALL_DIR/mcp.json'))" 2>/dev/null; then
      ok "mcp.json: valid JSON"
    else
      warn "mcp.json: invalid JSON — may need manual fix"
      _verify_ok=false
    fi
  fi

  # Check key hooks are wired
  if grep -q 'bravros audit' "$INSTALL_DIR/settings.json" 2>/dev/null; then
    ok "Audit hook: wired"
  else
    warn "Audit hook: missing from settings.json"
    _verify_ok=false
  fi

  if grep -q 'bravros update' "$INSTALL_DIR/settings.json" 2>/dev/null; then
    ok "Auto-update hook: wired"
  else
    warn "Auto-update hook: missing from settings.json"
    _verify_ok=false
  fi

  if grep -q 'bravros statusline' "$INSTALL_DIR/settings.json" 2>/dev/null; then
    ok "Status line: wired"
  else
    warn "Status line: missing from settings.json"
    _verify_ok=false
  fi

  # Check PATH
  if echo "$PATH" | tr ':' '\n' | grep -q '.claude/bin'; then
    ok "PATH: ~/.claude/bin is in PATH"
  else
    warn "PATH: ~/.claude/bin not in PATH — run: source ~/${_shell_name:-".zshrc"}"
  fi

  if $_verify_ok; then
    ok "All checks passed"
  fi
fi

# ============================================================================
# PHASE 12: POST-INSTALL SUMMARY & ACTIVATION PROMPT
# ============================================================================

step "Installation complete"

if ! $DRY_RUN; then
  _skill_count=$(find "$SKILLS_DIR" -maxdepth 2 -name 'SKILL.md' 2>/dev/null | wc -l | tr -d ' ')
  _bin_version=$("$BIN_DIR/bravros" version 2>/dev/null || echo "installed (restart shell)")
else
  _skill_count="(dry run)"
  _bin_version="(dry run)"
fi

_shell_name=$(basename "${SHELL_RC:-unknown}")

printf "\n"
printf "  ${_BOLD}${_CYAN}┌──────────────────────────────────────────────┐${_RESET}\n"
printf "  ${_BOLD}${_CYAN}│${_RESET}  ${_BOLD}Bravros${_RESET} installed successfully            ${_BOLD}${_CYAN}│${_RESET}\n"
printf "  ${_BOLD}${_CYAN}├──────────────────────────────────────────────┤${_RESET}\n"
printf "  ${_BOLD}${_CYAN}│${_RESET}  Version:  %-33s ${_BOLD}${_CYAN}│${_RESET}\n" "$_bin_version"
printf "  ${_BOLD}${_CYAN}│${_RESET}  Skills:   %-33s ${_BOLD}${_CYAN}│${_RESET}\n" "$_skill_count"
printf "  ${_BOLD}${_CYAN}│${_RESET}  Platform: %-33s ${_BOLD}${_CYAN}│${_RESET}\n" "${OS}/${ARCH}"
printf "  ${_BOLD}${_CYAN}│${_RESET}  Mode:     %-33s ${_BOLD}${_CYAN}│${_RESET}\n" "$INSTALL_MODE"
printf "  ${_BOLD}${_CYAN}└──────────────────────────────────────────────┘${_RESET}\n"

printf "\n"
printf "  ${_BOLD}Next steps:${_RESET}\n"
if [ -n "$SHELL_RC" ]; then
  printf "  ${_DIM}1.${_RESET} source ~/%s\n" "$_shell_name"
fi
printf "  ${_DIM}2.${_RESET} bravros version\n"
printf "  ${_DIM}3.${_RESET} ${_BOLD}${_MAGENTA}bravros activate${_RESET} — license and unlock features\n"
printf "  ${_DIM}4.${_RESET} cd ~/your-project && /start\n"

printf "\n"
printf "  ${_BOLD}${_GREEN}▶ Run \`bravros activate\` to get started${_RESET}\n"

# Star on GitHub (TTY only)
if [ -t 0 ] && ! $DRY_RUN; then
  printf "\n"
  printf "  Star Bravros on GitHub? [Y/n] "
  read -r star_answer </dev/tty 2>/dev/null || star_answer="n"
  case "$star_answer" in
    [nN]*) ;;
    *)
      if [ "$OS" = "darwin" ]; then
        open "https://github.com/bravros/bravros" 2>/dev/null || true
      else
        xdg-open "https://github.com/bravros/bravros" 2>/dev/null || true
      fi
      ;;
  esac
fi

printf "\n"
printf "  Happy shipping! 🚀\n"
printf "\n"
