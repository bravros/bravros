#!/bin/bash

# Claude Code SDLC Installer
# Syncs skills, references, hooks, scripts, templates, and settings across machines.
# Repo: github.com/skaisser/claude

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TS="$(date +%Y%m%d-%H%M%S)"

# ============================================================================
# OS DETECTION
# ============================================================================

detect_os() {
  case "$(uname -s)" in
    Darwin*) IS_MACOS=true; IS_LINUX=false ;;
    Linux*)  IS_MACOS=false; IS_LINUX=true ;;
    *)       IS_MACOS=false; IS_LINUX=false ;;
  esac

  if $IS_MACOS; then
    PORTABLE_REPO_DEFAULT="$HOME/Sites/claude"
  else
    PORTABLE_REPO_DEFAULT="$HOME/claude"
  fi
}
detect_os

sed_inplace() {
  if $IS_MACOS; then
    sed -i '' "$@"
  else
    sed -i "$@"
  fi
}

echo "🚀 Installing Claude Code SDLC configuration..."
echo ""

# ============================================================================
# 1. CREATE DIRECTORIES
# ============================================================================

mkdir -p ~/.claude/skills
mkdir -p ~/.claude/hooks
mkdir -p ~/.claude/scripts
mkdir -p ~/.claude/templates
mkdir -p ~/.claude/cache

# Detect shell rc file (used later for aliases and env vars)
# Prefer the RC file matching the user's actual shell to avoid writing to
# a stub created by other installers (e.g., UV creates a tiny .zshrc on Linux).
if [[ "$SHELL" == */zsh ]] && [ -f ~/.zshrc ]; then
    SHELL_RC=~/.zshrc
elif [[ "$SHELL" == */bash ]] && [ -f ~/.bashrc ]; then
    SHELL_RC=~/.bashrc
elif [ -f ~/.zshrc ]; then
    SHELL_RC=~/.zshrc
elif [ -f ~/.bashrc ]; then
    SHELL_RC=~/.bashrc
else
    SHELL_RC=""
fi

# ============================================================================
# 2. CLEAN UP DEPRECATED FILES
# ============================================================================

echo "🧹 Cleaning up deprecated artifacts..."

# Remove stale .venv directories (created by agents, breaks cp -rf)
find ~/.claude/skills -name ".venv" -type d -exec rm -rf {} + 2>/dev/null

# Remove commands directory entirely (skills replace commands)
if [ -d ~/.claude/commands ]; then
    echo "   Removing ~/.claude/commands/ (replaced by skills)"
    rm -rf ~/.claude/commands
fi

# Remove agents directory entirely (skills replace agents)
if [ -d ~/.claude/agents ]; then
    echo "   Removing ~/.claude/agents/ (replaced by skills)"
    rm -rf ~/.claude/agents
fi

# Remove AGENTS.md (merged into CLAUDE.md)
if [ -f ~/.claude/AGENTS.md ]; then
    echo "   Removing ~/.claude/AGENTS.md (merged into CLAUDE.md)"
    rm -f ~/.claude/AGENTS.md
fi

# Remove deprecated skills (if any old ones exist)
DEPRECATED_SKILLS=("criar-campanha" "mcp-builder" "prepare4kaisser" "linear-init" "debug" "taste-skill")
for skill in "${DEPRECATED_SKILLS[@]}"; do
    if [ -d ~/.claude/skills/"$skill" ]; then
        echo "   Removing deprecated skill: $skill/"
        rm -rf ~/.claude/skills/"$skill"
    fi
done

# Remove old skill names (renamed in SDLC 4.0)
RENAMED_SKILLS=("flow-auto" "flow-auto-wt" "batch-flow")
for skill in "${RENAMED_SKILLS[@]}"; do
    if [ -d ~/.claude/skills/"$skill" ]; then
        echo "   Removing renamed skill: $skill/"
        rm -rf ~/.claude/skills/"$skill"
    fi
done

# Remove consolidated firecrawl variants (now in firecrawl/references/commands/)
FIRECRAWL_VARIANTS=("firecrawl-agent" "firecrawl-browser" "firecrawl-crawl" "firecrawl-download" "firecrawl-map" "firecrawl-scrape" "firecrawl-search")
for skill in "${FIRECRAWL_VARIANTS[@]}"; do
    if [ -d ~/.claude/skills/"$skill" ]; then
        echo "   Removing consolidated firecrawl variant: $skill/"
        rm -rf ~/.claude/skills/"$skill"
    fi
done

# Remove deprecated audit.py hook (replaced by claude-cli audit)
if [ -f ~/.claude/hooks/audit.py ]; then
    echo "   Removing deprecated hook: audit.py (replaced by claude-cli audit)"
    rm -f ~/.claude/hooks/audit.py
fi

# Remove blueprint CLI (replaced by claude-cli)
if [ -d ~/.blueprint ]; then
    echo "   Removing deprecated ~/.blueprint/ (replaced by claude-cli)"
    rm -rf ~/.blueprint
fi

# Remove blueprint-specific skills (bp-* duplicates)
BP_SKILLS=("bp-branch" "bp-commit" "bp-context" "bp-push" "bp-ship" "bp-status" "bp-tdd-review" "bp-test")
for skill in "${BP_SKILLS[@]}"; do
    if [ -d ~/.claude/skills/"$skill" ]; then
        echo "   Removing blueprint skill: $skill/ (use non-prefixed version)"
        rm -rf ~/.claude/skills/"$skill"
    fi
done

# Remove deprecated scripts (replaced by claude-cli)
DEPRECATED_SCRIPTS=("linear.py" "plan-status.py" "plan-meta.sh" "git-context.py" "plan-sync-frontmatter.py")
for script in "${DEPRECATED_SCRIPTS[@]}"; do
    if [ -f ~/.claude/scripts/"$script" ]; then
        echo "   Removing deprecated script: $script (replaced by claude-cli)"
        rm -f ~/.claude/scripts/"$script"
    fi
done

# Remove deprecated Linear config files
for f in ~/.linear-api-key ~/.linear-config; do
    if [ -f "$f" ]; then
        echo "   Removing deprecated: $(basename $f) (Linear syncs via GitHub)"
        rm -f "$f"
    fi
done

# Remove deprecated cache dirs
if [ -d ~/.claude/cache/linear ]; then
    echo "   Removing deprecated cache: linear/"
    rm -rf ~/.claude/cache/linear
fi

# Remove root references directory (references now bundled inside each skill)
if [ -d ~/.claude/references ]; then
    echo "   Removing ~/.claude/references/ (bundled inside skills now)"
    rm -rf ~/.claude/references
fi

# ============================================================================
# 3. COPY SKILLS
# ============================================================================

echo "📦 Copying skills..."
if [ -d "$SCRIPT_DIR/skills" ]; then
    for item in "$SCRIPT_DIR/skills/"*; do
        [ -d "$item" ] && cp -rf "$item" ~/.claude/skills/
    done
else
    echo "   No skills directory found"
fi

# Remove macOS-only skills on Linux
if $IS_LINUX; then
    echo "   Skipping macOS-only skills on Linux: obsidian-setup, ha-mac-unlock"
    rm -rf ~/.claude/skills/obsidian-setup 2>/dev/null
    rm -rf ~/.claude/scripts/ha-mac-unlock 2>/dev/null
fi

# References are bundled inside each skill's references/ directory.
# No separate copy step needed — skills carry their own references.

# ============================================================================
# 4. COPY HOOKS
# ============================================================================

echo "📦 Copying hooks..."
if [ -d "$SCRIPT_DIR/hooks" ]; then
    for f in "$SCRIPT_DIR/hooks/"*.py "$SCRIPT_DIR/hooks/"*.sh; do
        [ -f "$f" ] && cp -f "$f" ~/.claude/hooks/ && chmod +x ~/.claude/hooks/"$(basename "$f")"
    done
else
    echo "   No hooks directory found"
fi

# ============================================================================
# 5. COPY SCRIPTS
# ============================================================================

echo "📦 Copying scripts..."
if [ -d "$SCRIPT_DIR/scripts" ]; then
    for f in "$SCRIPT_DIR/scripts/"*.py "$SCRIPT_DIR/scripts/"*.sh; do
        if [ -f "$f" ]; then
            cp -f "$f" ~/.claude/scripts/
            chmod +x ~/.claude/scripts/"$(basename "$f")" 2>/dev/null || true
        fi
    done
else
    echo "   No scripts directory found"
fi

# Clean up removed scripts from deployed location
rm -f ~/.claude/scripts/ha.sh ~/.claude/scripts/ha-say.sh 2>/dev/null

# ============================================================================
# 5b. BUILD GO CLI (claude-cli)
# ============================================================================

echo "🔨 Installing claude-cli..."
mkdir -p ~/.claude/bin

# Auto-detect platform and copy the right binary
_OS=$(uname -s | tr '[:upper:]' '[:lower:]')   # darwin or linux
_ARCH=$(uname -m)                                # arm64 or x86_64
case "$_ARCH" in
    x86_64)  _ARCH="amd64" ;;
    aarch64) _ARCH="arm64" ;;
esac
_BINARY="$SCRIPT_DIR/cli/claude-cli-${_OS}-${_ARCH}"

_NEED_DOWNLOAD=false
_GH_REPO="skaisser/claude"

if [ -f "$_BINARY" ]; then
    # Local binary exists (dev build) — copy it
    cp -f "$_BINARY" ~/.claude/bin/claude-cli
    chmod +x ~/.claude/bin/claude-cli
    if $IS_MACOS; then
      codesign -s - ~/.claude/bin/claude-cli 2>/dev/null || true
    fi
    echo "   ✅ Installed ~/.claude/bin/claude-cli (${_OS}/${_ARCH}) — $(~/.claude/bin/claude-cli version)"
elif [ -x ~/.claude/bin/claude-cli ] && ~/.claude/bin/claude-cli version &>/dev/null; then
    # Binary exists and runs — check if outdated vs latest release
    _CURRENT_VERSION=$(~/.claude/bin/claude-cli version 2>/dev/null | awk '{print $NF}')
    if command -v gh &>/dev/null; then
        _LATEST_TAG=$(gh release view --repo "$_GH_REPO" --json tagName -q '.tagName' 2>/dev/null || true)
        if [ -n "$_LATEST_TAG" ] && [ "$_CURRENT_VERSION" != "$_LATEST_TAG" ]; then
            echo "   Outdated: ${_CURRENT_VERSION} → ${_LATEST_TAG}"
            _NEED_DOWNLOAD=true
        else
            echo "   ✅ claude-cli already installed (${_CURRENT_VERSION}) — up to date"
        fi
    else
        echo "   ✅ claude-cli already installed (${_CURRENT_VERSION}) — skipping version check (no gh)"
    fi
else
    _NEED_DOWNLOAD=true
fi

if [ "$_NEED_DOWNLOAD" = true ]; then
    if command -v gh &>/dev/null; then
        _LATEST_TAG=${_LATEST_TAG:-$(gh release view --repo "$_GH_REPO" --json tagName -q '.tagName' 2>/dev/null || true)}
        if [ -n "$_LATEST_TAG" ]; then
            echo "   Downloading claude-cli ${_LATEST_TAG} from GitHub..."
            rm -f "/tmp/claude-cli-${_OS}-${_ARCH}"
            gh release download "$_LATEST_TAG" --repo "$_GH_REPO" --pattern "claude-cli-${_OS}-${_ARCH}" --dir /tmp 2>/dev/null || true
            if [ -s "/tmp/claude-cli-${_OS}-${_ARCH}" ]; then
                mv -f "/tmp/claude-cli-${_OS}-${_ARCH}" ~/.claude/bin/claude-cli
                chmod +x ~/.claude/bin/claude-cli
                if $IS_MACOS; then
                  codesign -s - ~/.claude/bin/claude-cli 2>/dev/null || true
                fi
                echo "   ✅ Downloaded ~/.claude/bin/claude-cli (${_OS}/${_ARCH}) — $(~/.claude/bin/claude-cli version)"
            else
                echo "   ⚠️  Download failed — build manually: cd cli && go build -ldflags=\"-s -w\" -o claude-cli-${_OS}-${_ARCH} ."
            fi
        else
            echo "   ⚠️  No GitHub release found"
            echo "   To build: cd cli && go build -ldflags=\"-s -w\" -o claude-cli-${_OS}-${_ARCH} ."
        fi
    else
        echo "   ⚠️  gh CLI not found and no local binary — install gh or build manually"
        echo "   To build: cd cli && go build -ldflags=\"-s -w\" -o claude-cli-${_OS}-${_ARCH} ."
    fi
fi

# Ensure ~/.claude/bin is in PATH
if [ -n "$SHELL_RC" ]; then
    if ! grep -q 'claude/bin' "$SHELL_RC" 2>/dev/null; then
        echo 'export PATH="$HOME/.claude/bin:$PATH"' >> "$SHELL_RC"
        echo "   Added ~/.claude/bin to PATH in $SHELL_RC"
    fi
fi

# ============================================================================
# 6. COPY TEMPLATES
# ============================================================================

echo "📦 Copying templates..."
if [ -d "$SCRIPT_DIR/templates" ]; then
    cp -rf "$SCRIPT_DIR/templates/." ~/.claude/templates/
    chmod +x ~/.claude/templates/.githooks/commit-msg 2>/dev/null || true
else
    echo "   No templates directory found"
fi

# ============================================================================
# 7. COPY CONFIG FILES
# ============================================================================

# Settings.json
if [ -f "$SCRIPT_DIR/config/settings.json" ]; then
    echo "📦 Copying settings..."
    if [ -f ~/.claude/settings.json ]; then
        if ! diff -q "$SCRIPT_DIR/config/settings.json" ~/.claude/settings.json > /dev/null 2>&1; then
            echo "   Backing up existing settings to settings.json.bak.${TS}"
            cp ~/.claude/settings.json ~/.claude/settings.json.bak."${TS}"
        fi
    fi
    cp -f "$SCRIPT_DIR/config/settings.json" ~/.claude/settings.json
fi

# mcp.json
if [ -f "$SCRIPT_DIR/config/mcp.json" ]; then
    echo "📦 Copying MCP servers config..."
    if [ -f ~/.claude/mcp.json ]; then
        if ! diff -q "$SCRIPT_DIR/config/mcp.json" ~/.claude/mcp.json > /dev/null 2>&1; then
            echo "   Backing up existing mcp.json to mcp.json.bak.${TS}"
            cp ~/.claude/mcp.json ~/.claude/mcp.json.bak."${TS}"
        fi
    fi
    cp -f "$SCRIPT_DIR/config/mcp.json" ~/.claude/mcp.json
    chmod 600 ~/.claude/mcp.json 2>/dev/null || true

    # Remove macOS-only MCP servers on non-macOS systems
    if [ "$(uname)" != "Darwin" ] && command -v python3 &> /dev/null; then
        python3 - <<'PY'
import json
from pathlib import Path

cfg_path = Path.home() / '.claude' / 'mcp.json'
if not cfg_path.exists():
    raise SystemExit(0)

config = json.loads(cfg_path.read_text())
servers = config.get('mcpServers', {})
macos_only = ['herd', 'browsermcp']
removed = [s for s in macos_only if s in servers]
for s in removed:
    del servers[s]
if removed:
    cfg_path.write_text(json.dumps(config, indent=4) + "\n")
PY
        [ $? -eq 0 ] && echo "   Removed macOS-only MCP servers (herd, browsermcp) on Linux"
    fi
fi

# Statusline — Go binary is primary, bash script is fallback
if [ -f ~/.claude/bin/claude-cli ]; then
    echo "📦 Statusline: using claude-cli statusline (Go binary)"
    # Remove legacy bash statusline if Go binary is available
    rm -f ~/.claude/statusline.sh 2>/dev/null
elif [ -f "$SCRIPT_DIR/config/statusline.sh" ]; then
    echo "📦 Copying statusline (bash fallback — no claude-cli binary)..."
    cp -f "$SCRIPT_DIR/config/statusline.sh" ~/.claude/statusline.sh
    chmod +x ~/.claude/statusline.sh
    # Override settings.json statusLine to use bash fallback
    if command -v python3 &> /dev/null && [ -f ~/.claude/settings.json ]; then
        python3 -c "
import json
from pathlib import Path
p = Path.home() / '.claude' / 'settings.json'
cfg = json.loads(p.read_text())
cfg['statusLine'] = {'type': 'command', 'command': '\$HOME/.claude/statusline.sh'}
p.write_text(json.dumps(cfg, indent=2) + '\n')
" 2>/dev/null
        echo "   Updated settings.json to use bash fallback"
    fi
fi

# CLAUDE.md (global instructions)
if [ -f "$SCRIPT_DIR/CLAUDE.md" ]; then
    echo "📦 Copying global instructions..."
    if [ -f ~/.claude/CLAUDE.md ]; then
        if ! diff -q "$SCRIPT_DIR/CLAUDE.md" ~/.claude/CLAUDE.md > /dev/null 2>&1; then
            echo "   Backing up existing CLAUDE.md to CLAUDE.md.bak.${TS}"
            cp ~/.claude/CLAUDE.md ~/.claude/CLAUDE.md.bak."${TS}"
        fi
    fi
    cp -f "$SCRIPT_DIR/CLAUDE.md" ~/.claude/CLAUDE.md
fi


# ============================================================================
# 8. DETECT HERD NVM AND PATCH MCP.JSON NPX PATHS
# ============================================================================

HERD_NPX=""

echo "🔍 Detecting Node.js path..."
HERD_NODE_DIR="$HOME/Library/Application Support/Herd/config/nvm/versions/node"
if [ -d "$HERD_NODE_DIR" ]; then
    export HERD_NODE_DIR
    if command -v python3 &> /dev/null; then
        HERD_NPX=$(python3 - <<'PY'
import os
import re
from pathlib import Path

root = Path(os.environ.get('HERD_NODE_DIR', ''))
if not root.is_dir():
    raise SystemExit(0)

candidates = list(root.glob('*/bin/npx'))
def key(p: Path):
    # Parse version-like segments into a sortable tuple: v20.11.0 -> (20,11,0)
    s = p.parts[-3]  # version dir name
    nums = [int(x) for x in re.findall(r'\d+', s)]
    return (nums + [0, 0, 0])[:3]

if not candidates:
    raise SystemExit(0)

best = sorted(candidates, key=key)[-1]
print(str(best))
PY
)
    else
        # Fallback: pick the last match (best-effort)
        HERD_NPX=$(find "$HERD_NODE_DIR" -name "npx" -path "*/bin/npx" 2>/dev/null | tail -1)
    fi
    if [ -n "$HERD_NPX" ]; then
        echo "   Found Herd NVM npx: $HERD_NPX"
        if command -v python3 &> /dev/null; then
            HERD_NPX="$HERD_NPX" python3 - <<'PY'
import json
import os
from pathlib import Path

cfg_path = Path.home() / '.claude' / 'mcp.json'
if not cfg_path.exists():
    raise SystemExit(0)

herd_npx = os.environ.get('HERD_NPX', '').strip()
if not herd_npx:
    raise SystemExit(0)

config = json.loads(cfg_path.read_text())
for _, server in (config.get('mcpServers') or {}).items():
    if server.get('command') == 'npx':
        server['command'] = herd_npx
cfg_path.write_text(json.dumps(config, indent=4) + "\n")
PY
            echo "   Updated mcp.json npx paths to Herd NVM"
        else
            echo "   ⚠️  python3 not found — mcp.json uses generic 'npx' (update manually if needed)"
        fi
    fi
elif command -v npx &> /dev/null; then
    echo "   Using system npx: $(which npx)"
else
    echo "   ⚠️  npx not found — MCP servers that use npx won't work until Node.js is installed"
fi

# Sign in to 1Password (establishes session for all subsequent op calls)
if command -v op &> /dev/null; then
    if ! op whoami &>/dev/null; then
        if $IS_MACOS; then
          echo "🔐 Signing in to 1Password (Touch ID)..."
        else
          echo "🔐 Signing in to 1Password..."
        fi
        op signin 2>/dev/null || true
    fi
fi

# Inject Context7 API key from 1Password into mcp.json
if command -v op &> /dev/null; then
    C7_KEY=$(op read "op://HomeLab/Context7 Api Key/password" 2>/dev/null || true)
    if [ -n "$C7_KEY" ] && command -v python3 &> /dev/null; then
        C7_KEY="$C7_KEY" python3 - <<'PY'
import json, os
from pathlib import Path

cfg_path = Path.home() / '.claude' / 'mcp.json'
if not cfg_path.exists():
    raise SystemExit(0)

key = os.environ.get('C7_KEY', '').strip()
if not key:
    raise SystemExit(0)

config = json.loads(cfg_path.read_text())
c7 = (config.get('mcpServers') or {}).get('context7')
if c7:
    c7.setdefault('env', {})['CONTEXT7_API_KEY'] = key
    cfg_path.write_text(json.dumps(config, indent=4) + "\n")
PY
        echo "   Injected Context7 API key from 1Password"
    fi
fi

# ============================================================================
# 9. INSTALL FIRECRAWL CLI (web scraping)
# ============================================================================

echo "📦 Checking Firecrawl CLI..."
if command -v firecrawl &> /dev/null; then
    echo "   Firecrawl already installed: $(firecrawl --version 2>/dev/null | head -1 || echo 'available')"
else
    if command -v npm &> /dev/null; then
        echo "   Installing firecrawl-cli..."
        npm install -g firecrawl-cli@1.8.0 2>/dev/null && echo "   Firecrawl CLI installed" || echo "   ⚠️  Install failed — run: npm install -g firecrawl-cli@1.8.0"
    elif [ -n "$HERD_NPX" ]; then
        NPM_BIN="$(dirname "$HERD_NPX")/npm"
        if [ -f "$NPM_BIN" ]; then
            echo "   Installing firecrawl-cli via Herd npm..."
            "$NPM_BIN" install -g firecrawl-cli@1.8.0 2>/dev/null && echo "   Firecrawl CLI installed" || echo "   ⚠️  Install failed — run: npm install -g firecrawl-cli@1.8.0"
        fi
    else
        echo "   ⚠️  npm not found — install Node.js first, then: npm install -g firecrawl-cli@1.8.0"
    fi
fi

# ============================================================================
# 10. INSTALL UV (Python package manager)
# ============================================================================

echo "📦 Checking UV (Astral)..."
if command -v uv &> /dev/null; then
    echo "   UV already installed: $(uv --version)"
else
    echo "   Installing UV..."
    curl -LsSf https://astral.sh/uv/install.sh | sh 2>/dev/null
    if [ -f "$HOME/.local/bin/uv" ]; then
        echo "   UV installed: $($HOME/.local/bin/uv --version)"
        echo "   Add to PATH: export PATH=\"\$HOME/.local/bin:\$PATH\""
    else
        echo "   ⚠️  UV install failed — hooks and scripts need UV. Install manually: https://docs.astral.sh/uv/"
    fi
fi

# ============================================================================
# 11. PYTHON DEPENDENCIES
# ============================================================================

# Python dependencies are handled inline by each script via `uv run --script`.
# No venv or pip install needed — uv caches dependencies automatically.
# Scripts using this pattern: quick_validate.py (audit.py + sdlc.py removed — pure Go via claude-cli)
echo "📦 Python dependencies: managed inline by uv (no pip install needed)"

# ============================================================================
# 11a. ENSURE PIPX IS AVAILABLE
# ============================================================================

echo "📦 Checking pipx..."
if command -v pipx &> /dev/null; then
    echo "   pipx already installed"
else
    echo "   pipx not found — installing..."
    if [[ "$OSTYPE" == "linux"* ]] && command -v apt &> /dev/null; then
        sudo apt install -y pipx 2>/dev/null && pipx ensurepath 2>/dev/null && echo "   pipx installed via apt" || echo "   ⚠️  pipx install failed — run: sudo apt install pipx"
    elif command -v brew &> /dev/null; then
        brew install pipx 2>/dev/null && pipx ensurepath 2>/dev/null && echo "   pipx installed via brew" || echo "   ⚠️  pipx install failed — run: brew install pipx"
    else
        echo "   ⚠️  Cannot auto-install pipx — install manually: https://pipx.pypa.io/stable/installation/"
    fi
    # Refresh PATH so pipx is available for subsequent installs
    export PATH="$HOME/.local/bin:$PATH"
fi

# ============================================================================
# 11b. INSTALL HASS-CLI (Home Assistant)
# ============================================================================

echo "📦 Checking hass-cli..."
if command -v hass-cli &> /dev/null; then
    echo "   hass-cli already installed: $(hass-cli --version 2>/dev/null | head -1 || echo 'available')"
else
    if command -v pipx &> /dev/null; then
        echo "   Installing hass-cli via pipx..."
        pipx install homeassistant-cli 2>/dev/null && echo "   hass-cli installed" || echo "   ⚠️  Install failed — run: pipx install homeassistant-cli"
    else
        echo "   ⚠️  pipx not found — install pipx first, then run: pipx install homeassistant-cli"
    fi
fi

# ============================================================================
# 11c. INSTALL NOTEBOOKLM-PY (Google NotebookLM)
# ============================================================================

echo "📦 Checking notebooklm-py..."
if command -v notebooklm &> /dev/null; then
    echo "   notebooklm already installed: $(notebooklm --version 2>/dev/null | head -1 || echo 'available')"
    # Skip 'notebooklm skill install' — repo SKILL.md is the source of truth
    # (the CLI's bundled version may be older than the repo's)
    echo "   notebooklm skill: using repo version (source of truth)"
else
    if command -v pipx &> /dev/null; then
        echo "   Installing notebooklm-py via pipx..."
        pipx install notebooklm-py 2>/dev/null && echo "   notebooklm-py installed" || echo "   ⚠️  Install failed — run: pipx install notebooklm-py"
        # Install Playwright chromium browser for notebooklm login
        if command -v notebooklm &> /dev/null; then
            echo "   Installing Playwright chromium for notebooklm login..."
            pipx inject notebooklm-py playwright 2>/dev/null || true
            pipx runpip notebooklm-py install playwright 2>/dev/null || true
            # Install chromium browser using the venv's playwright
            NOTEBOOKLM_VENV="$HOME/.local/share/pipx/venvs/notebooklm-py"
            if [ -f "$NOTEBOOKLM_VENV/bin/playwright" ]; then
                "$NOTEBOOKLM_VENV/bin/playwright" install chromium 2>/dev/null && echo "   Playwright chromium installed" || echo "   ⚠️  Playwright chromium install failed — run: playwright install chromium"
            elif command -v playwright &> /dev/null; then
                playwright install chromium 2>/dev/null && echo "   Playwright chromium installed" || true
            fi
            # Skip 'notebooklm skill install' on fresh install too — repo SKILL.md already copied above
            echo "   notebooklm skill: using repo version (source of truth)"
        fi
    else
        echo "   ⚠️  pipx not found — install pipx first, then run: pipx install notebooklm-py"
    fi
fi

# Inject Home Assistant token from 1Password into shell rc
if command -v op &> /dev/null; then
    HA_TOKEN=$(op item get "HomeAssistant Long Live Token" --vault "HomeLab" --fields label=password --reveal 2>/dev/null || true)
    if [ -n "$HA_TOKEN" ] && [ -n "$SHELL_RC" ]; then
        if ! grep -q 'HASS_SERVER' "$SHELL_RC" 2>/dev/null; then
            cat >> "$SHELL_RC" <<HAEOF

# -----------------------------------------------
# Home Assistant API Configuration
# -----------------------------------------------
export HASS_SERVER="http://homeassistant.local:8123"
export HASS_TOKEN="$HA_TOKEN"
HAEOF
            echo "   Home Assistant: HASS_SERVER, HASS_TOKEN added to $SHELL_RC"
        else
            echo "   Home Assistant: env vars already in $SHELL_RC"
        fi
    elif [ -z "$HA_TOKEN" ]; then
        echo "   ⚠️  Could not read HA token from 1Password (HomeLab/HomeAssistant Long Live Token)"
    fi
fi

# ============================================================================
# 12. SETUP CHANNELS
# ============================================================================

echo "📦 Setting up channels..."
mkdir -p ~/.claude/channels/telegram/approved

if [ -f "$SCRIPT_DIR/channels/telegram/access.json" ]; then
    cp -f "$SCRIPT_DIR/channels/telegram/access.json" ~/.claude/channels/telegram/access.json
    echo "   Telegram channel: access.json copied"
fi

# Inject Telegram Bot Token from 1Password
if command -v op &> /dev/null; then
    TG_TOKEN=$(op read "op://HomeLab/Telegram Bot Token/password" 2>/dev/null || true)
    if [ -n "$TG_TOKEN" ]; then
        echo "TELEGRAM_BOT_TOKEN=$TG_TOKEN" > ~/.claude/channels/telegram/.env
        chmod 600 ~/.claude/channels/telegram/.env
        echo "   Telegram channel: bot token injected from 1Password"
    else
        echo "   ⚠️  Could not read Telegram Bot Token from 1Password"
    fi
else
    echo "   ⚠️  1Password CLI not found — create ~/.claude/channels/telegram/.env manually"
fi

# Setup telegram relay proxy
if [ -d "$SCRIPT_DIR/channels/telegram/relay" ]; then
    mkdir -p ~/.claude/telegram
    cp -f "$SCRIPT_DIR/channels/telegram/relay/"*.ts "$SCRIPT_DIR/channels/telegram/relay/package.json" "$SCRIPT_DIR/channels/telegram/relay/bun.lock" ~/.claude/telegram/ 2>/dev/null
    echo "   Telegram relay: proxy files copied"
fi

# Copy telegram-patch hook
if [ -f "$SCRIPT_DIR/hooks/telegram-patch.sh" ]; then
    cp -f "$SCRIPT_DIR/hooks/telegram-patch.sh" ~/.claude/hooks/
    chmod +x ~/.claude/hooks/telegram-patch.sh
    echo "   Telegram hook: telegram-patch.sh copied"
fi

# Add clauddt alias to the appropriate shell rc file
if $IS_MACOS; then
    TELEGRAM_DIR="$HOME/Sites/claude-telegram"
else
    TELEGRAM_DIR="$HOME/claude-telegram"
fi

if [ -n "$SHELL_RC" ]; then
    if ! grep -q 'alias clauddt=' "$SHELL_RC" 2>/dev/null; then
        echo "" >> "$SHELL_RC"
        echo '# Claude Code with Telegram channel' >> "$SHELL_RC"
        echo "alias clauddt=\"cd $TELEGRAM_DIR && claude --dangerously-skip-permissions --channels plugin:telegram@claude-plugins-official\"" >> "$SHELL_RC"
        echo "   Added 'clauddt' alias to $SHELL_RC"
    else
        echo "   'clauddt' alias already in $SHELL_RC"
    fi
else
    echo "   ⚠️  No .zshrc or .bashrc found — add 'clauddt' alias manually"
fi

# ============================================================================
# 13. INSTALL PLUGINS
# ============================================================================

echo "📦 Installing plugins..."
REQUIRED_PLUGINS=("ralph-loop" "telegram")
for plugin in "${REQUIRED_PLUGINS[@]}"; do
    if command -v claude &> /dev/null; then
        if [ -f ~/.claude/plugins/installed_plugins.json ] && grep -q "\"$plugin@" ~/.claude/plugins/installed_plugins.json 2>/dev/null; then
            echo "   $plugin: already installed"
        else
            echo "   Installing $plugin plugin..."
            claude plugins install "$plugin" 2>/dev/null && echo "   $plugin: installed" || echo "   $plugin: install failed (run 'claude plugins install $plugin' manually)"
        fi
    else
        echo "   Skipped (claude CLI not found). Run 'claude plugins install $plugin' after installing Claude Code."
    fi
done

# Fix plugin script permissions (hooks/scripts may lack +x after install)
if [ -d ~/.claude/plugins ]; then
    if $IS_LINUX; then
      fixed=$(find ~/.claude/plugins -name "*.sh" ! -perm /111 -exec chmod +x {} + 2>/dev/null && echo "done")
    else
      fixed=$(find ~/.claude/plugins -name "*.sh" ! -perm +111 -exec chmod +x {} + 2>/dev/null && echo "done")
    fi
    echo "   Fixed plugin script permissions"
fi

# ============================================================================
# 14. VERIFICATION AND SUMMARY
# ============================================================================

echo ""
echo "✅ Installation complete!"
echo ""
echo "Installed:"
echo "  OS:           $(uname -s) ($(uname -m))"
echo "  Skills:       $(find ~/.claude/skills -name 'SKILL.md' 2>/dev/null | wc -l | tr -d ' ') skills (references bundled inside)"
echo "  CLI:          $(~/.claude/bin/claude-cli version 2>/dev/null || echo 'Not installed')"
echo "  Hooks:        $(ls ~/.claude/hooks/*.py ~/.claude/hooks/*.sh 2>/dev/null | wc -l | tr -d ' ') files"
echo "  Scripts:      $(ls ~/.claude/scripts/*.py ~/.claude/scripts/*.sh 2>/dev/null | wc -l | tr -d ' ') files (claude-cli + helpers)"
echo "  Templates:    $([ -d ~/.claude/templates ] && echo 'Yes' || echo 'No')"
echo "  Statusline:   $(~/.claude/bin/claude-cli statusline --help >/dev/null 2>&1 && echo 'Go binary' || ([ -f ~/.claude/statusline.sh ] && echo 'Bash fallback' || echo 'No'))"
echo "  Settings:     $([ -f ~/.claude/settings.json ] && echo 'Yes' || echo 'No')"
echo "  MCP Servers:  $([ -f ~/.claude/mcp.json ] && echo 'Yes' || echo 'No')"
echo "  CLAUDE.md:    $([ -f ~/.claude/CLAUDE.md ] && echo 'Yes' || echo 'No')"
echo "  UV:           $(command -v uv &>/dev/null && uv --version || echo 'Not installed')"
echo "  Linear:       Via GitHub integration (no API key needed)"
echo "  Plugins:      ralph-loop, telegram"
echo "  Channels:     $([ -f ~/.claude/channels/telegram/.env ] && echo 'telegram ✅' || echo 'telegram ⚠️ (no .env)')"

echo ""
echo "🎯 Skill Workflow:"
echo ""
echo "  Quick tasks:   /quick <task>"
echo "  Backlog:       /backlog add <idea> → /backlog promote N → /plan"
echo "  Full workflow: /backlog → /plan → /plan-review → /plan-approved → /plan-check → /pr → /review → /address-pr → /finish → /complete"
echo ""
echo "  Core Skills:"
echo "    /backlog        Capture feature ideas (pre-planning backlog)"
echo "    /quick          Small fixes, 1-3 files, no plan overhead"
echo "    /plan           Create branch + plan (default, no worktree)"
echo "    /plan-wt        Create worktree + branch + plan (isolated)"
echo "    /plan-review    Pre-flight: assign models, decide execution strategy"
echo "    /plan-approved  Execute plan phase-by-phase with teams"
echo "    /plan-check     Audit plan vs actual implementation"
echo "    /pr             Create PR with comprehensive description"
echo "    /review         Trigger @claude review on GitHub PR"
echo "    /address-pr     Fetch PR review feedback and implement fixes"
echo "    /finish         Merge PR to base branch"
echo "    /complete       Cleanup worktree + branches"
echo "    /ship           Commit + push in one go"
echo "    /commit         Commit with emoji format"
echo "    /test           Create Pest tests"
echo "    /run-tests      Run targeted tests"
echo "    /coverage       Analyze test coverage"
echo "    /context        Generate CLAUDE.md files for project directories"
echo "    /start          Init new project (GitHub Actions + hooks)"
echo "    /sync           Sync ~/.claude changes to portable repo"
echo "    /branch         Create feature branch"
echo ""
echo "  Design & Research:"
echo "    /excalidraw-diagram  Generate Excalidraw diagrams (architecture, workflows)"
echo "    /laravel-db-diagram  ER diagrams from Laravel migrations"
echo "    /firecrawl           Web scraping router (scrape/search/crawl/map/browser)"
echo "    /yt-search           Search YouTube with structured results (yt-dlp, no API key)"
echo ""
echo ""
echo "  Content Generation:"
echo "    /notebooklm          Google NotebookLM: notebooks, podcasts, videos, reports, quizzes"
echo ""
echo "  Automation:"
echo "    /n8n                 Create, manage, and execute n8n workflows (hybrid mode)"
echo ""
echo "  Network & Home:"
echo "    /unifi               Manage UniFi devices, clients, DHCP, firmware (local API + 1Password)"
echo "    claude-cli ha say    TTS to Alexa Echo devices via Home Assistant (studio/sala/suite/banheiro/gourmet/todos)"
echo ""
echo "  Deployment:"
echo "    /cf-pages-deploy     Deploy static sites to Cloudflare Pages (+ custom domains)"
echo ""

# ============================================================================
# CHECK 1PASSWORD CLI
# ============================================================================

if command -v op &> /dev/null; then
    if $IS_MACOS; then
      echo "🔐 1Password CLI: available (secrets fetched at runtime via Touch ID)"
    else
      echo "🔐 1Password CLI: available (secrets fetched at runtime)"
    fi
    echo "   Skills using 1Password: /unifi (Unifi Claude Api), /n8n (n8n api key), /firecrawl (FireCrawl Api), Context7 MCP (Context7 Api Key), Telegram (Telegram Bot Token), Home Assistant (HA Long Live Token)"
else
    echo "⚠️  1Password CLI (op) not found — skills that use secrets will run in offline mode"
    echo "   Install: https://developer.1password.com/docs/cli/get-started/"
fi

echo ""
echo "⚠️  Notes:"
echo "  - Herd MCP path is macOS-specific — edit ~/.claude/mcp.json if needed"
echo "  - Run 'claude plugins install ralph-loop' if plugin install was skipped"
echo "  - Skills replace the old commands system — all commands are now skills"
if $IS_LINUX; then
    echo "  - Skipped macOS-only items: obsidian-setup skill, ha-mac-unlock scripts"
fi
echo ""
echo "Happy coding! 🎯"
