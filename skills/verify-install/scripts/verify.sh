#!/bin/bash
# ============================================================================
# Skaisser SDLC 4.0 — Installation Verification
# Compares portable repo (~/Sites/claude) against deployed (~/.claude)
# Usage: bash verify.sh [--fix] [--json]
# ============================================================================

set -uo pipefail

# ── Ensure installed tools are visible ──────────────────────────────────────
# install.sh adds these to shell RC but the current session may not have them
export PATH="$HOME/.claude/bin:$HOME/.local/bin:$PATH"

# ── Config ──────────────────────────────────────────────────────────────────
if [ "$(uname -s)" = "Darwin" ]; then
  _DEFAULT_REPO="$HOME/Sites/claude"
else
  _DEFAULT_REPO="$HOME/claude"
fi
PORTABLE_REPO="${PORTABLE_REPO:-$_DEFAULT_REPO}"
DEPLOYED_DIR="${DEPLOYED_DIR:-$HOME/.claude}"
FIX_MODE=false
JSON_MODE=false

for arg in "$@"; do
    case "$arg" in
        --fix)  FIX_MODE=true ;;
        --json) JSON_MODE=true ;;
    esac
done

# ── Colors & Symbols ───────────────────────────────────────────────────────
if [ -t 1 ] && [ "$JSON_MODE" = false ]; then
    GREEN='\033[0;32m'
    RED='\033[0;31m'
    YELLOW='\033[0;33m'
    CYAN='\033[0;36m'
    BOLD='\033[1m'
    DIM='\033[2m'
    RESET='\033[0m'
else
    GREEN='' RED='' YELLOW='' CYAN='' BOLD='' DIM='' RESET=''
fi

PASS="✅"
FAIL="❌"
WARN="⚠️ "
INFO="ℹ️ "

# ── Counters ───────────────────────────────────────────────────────────────
TOTAL_PASS=0
TOTAL_FAIL=0
TOTAL_WARN=0
FIXES_APPLIED=0

# JSON accumulator
JSON_RESULTS="[]"

# ── Helpers ────────────────────────────────────────────────────────────────

header() {
    if [ "$JSON_MODE" = false ]; then
        echo ""
        echo -e "${BOLD}━━ $1 ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
    fi
}

pass() {
    TOTAL_PASS=$((TOTAL_PASS + 1))
    if [ "$JSON_MODE" = false ]; then
        printf "  ${GREEN}${PASS}${RESET} %-28s %s\n" "$1" "${DIM}$2${RESET}"
    fi
    json_add "$1" "pass" "$2" ""
}

fail() {
    TOTAL_FAIL=$((TOTAL_FAIL + 1))
    if [ "$JSON_MODE" = false ]; then
        printf "  ${RED}${FAIL}${RESET} %-28s ${RED}%s${RESET}\n" "$1" "$2"
        if [ -n "${3:-}" ]; then
            echo -e "     ${DIM}↳ Fix: $3${RESET}"
        fi
    fi
    json_add "$1" "fail" "$2" "${3:-}"
}

warn() {
    TOTAL_WARN=$((TOTAL_WARN + 1))
    if [ "$JSON_MODE" = false ]; then
        printf "  ${YELLOW}${WARN}${RESET}  %-28s ${YELLOW}%s${RESET}\n" "$1" "$2"
        if [ -n "${3:-}" ]; then
            echo -e "     ${DIM}↳ $3${RESET}"
        fi
    fi
    json_add "$1" "warn" "$2" "${3:-}"
}

info() {
    if [ "$JSON_MODE" = false ]; then
        printf "  ${CYAN}${INFO}${RESET}  %-28s %s\n" "$1" "$2"
    fi
}

fix_applied() {
    FIXES_APPLIED=$((FIXES_APPLIED + 1))
    if [ "$JSON_MODE" = false ]; then
        echo -e "     ${GREEN}✔ Fixed: $1${RESET}"
    fi
}

json_add() {
    # Append a result to JSON_RESULTS array (jq-free, just string building)
    local name="$1" status="$2" message="$3" fix="${4:-}"
    # Escape quotes in strings
    name="${name//\"/\\\"}"
    message="${message//\"/\\\"}"
    fix="${fix//\"/\\\"}"
    local entry="{\"name\":\"$name\",\"status\":\"$status\",\"message\":\"$message\",\"fix\":\"$fix\"}"
    if [ "$JSON_RESULTS" = "[]" ]; then
        JSON_RESULTS="[$entry"
    else
        JSON_RESULTS="$JSON_RESULTS,$entry"
    fi
}

md5_of() {
    if command -v md5sum &>/dev/null; then
        md5sum "$1" 2>/dev/null | awk '{print $1}'
    elif command -v md5 &>/dev/null; then
        md5 -q "$1" 2>/dev/null
    else
        # Fallback: use openssl
        openssl md5 "$1" 2>/dev/null | awk '{print $NF}'
    fi
}

# Compare all files in a directory tree by MD5
# Returns 0 if all match, 1 if any differ
compare_dir_md5() {
    local src_dir="$1"
    local dep_dir="$2"
    local mismatches=0

    # Check all source files exist in deployed with matching MD5
    while IFS= read -r -d '' src_file; do
        local rel_path="${src_file#$src_dir/}"
        local dep_file="$dep_dir/$rel_path"

        if [ ! -f "$dep_file" ]; then
            mismatches=$((mismatches + 1))
            continue
        fi

        local src_md5 dep_md5
        src_md5=$(md5_of "$src_file")
        dep_md5=$(md5_of "$dep_file")

        if [ "$src_md5" != "$dep_md5" ]; then
            mismatches=$((mismatches + 1))
        fi
    done < <(find "$src_dir" -type f -not -name ".venv" -not -path "*/.venv/*" -not -name ".DS_Store" -print0 2>/dev/null)

    return $mismatches
}

# ── Pre-flight ─────────────────────────────────────────────────────────────

if [ "$JSON_MODE" = false ]; then
    echo ""
    echo -e "${BOLD}══════════════════════════════════════════════════════════${RESET}"
    echo -e "${BOLD}  Skaisser SDLC 4.0 — Installation Verification${RESET}"
    echo -e "${BOLD}══════════════════════════════════════════════════════════${RESET}"
    if [ "$FIX_MODE" = true ]; then
        echo -e "  ${CYAN}Mode: auto-fix enabled${RESET}"
    fi
fi

# Check portable repo exists
if [ ! -d "$PORTABLE_REPO" ]; then
    fail "Portable repo" "NOT FOUND at $PORTABLE_REPO" "git clone git@github.com:skaisser/claude.git $PORTABLE_REPO"
    if [ "$JSON_MODE" = true ]; then
        echo "${JSON_RESULTS}]"
    fi
    exit 1
fi

if [ ! -f "$PORTABLE_REPO/install.sh" ]; then
    fail "install.sh" "NOT FOUND in $PORTABLE_REPO" "Ensure the portable repo is complete"
    if [ "$JSON_MODE" = true ]; then
        echo "${JSON_RESULTS}]"
    fi
    exit 1
fi

# ============================================================================
# 1. SKILLS INTEGRITY
# ============================================================================

header "Skills Integrity"

SKILLS_MATCH=0
SKILLS_MISMATCH=0
SKILLS_MISSING=0
SKILLS_ORPHANED=0

# Check source skills are deployed correctly
for src_skill_dir in "$PORTABLE_REPO/skills/"*/; do
    [ ! -d "$src_skill_dir" ] && continue
    skill_name=$(basename "$src_skill_dir")
    dep_skill_dir="$DEPLOYED_DIR/skills/$skill_name"

    # Skip macOS-only skills on Linux
    if [ "$(uname -s)" != "Darwin" ]; then
        case "$skill_name" in
            obsidian-setup|ha-mac-unlock) continue ;;
        esac
    fi

    if [ ! -d "$dep_skill_dir" ]; then
        fail "$skill_name" "MISSING (not deployed)" "cp -rf $src_skill_dir $DEPLOYED_DIR/skills/"
        SKILLS_MISSING=$((SKILLS_MISSING + 1))
        if [ "$FIX_MODE" = true ]; then
            cp -rf "$src_skill_dir" "$DEPLOYED_DIR/skills/"
            fix_applied "Copied $skill_name to deployed"
        fi
        continue
    fi

    # Compare all files by MD5
    mismatch_files=""
    while IFS= read -r -d '' src_file; do
        rel_path="${src_file#$src_skill_dir}"
        dep_file="$dep_skill_dir/$rel_path"

        if [ ! -f "$dep_file" ]; then
            mismatch_files="$mismatch_files $rel_path(missing)"
            continue
        fi

        src_md5=$(md5_of "$src_file")
        dep_md5=$(md5_of "$dep_file")

        if [ "$src_md5" != "$dep_md5" ]; then
            mismatch_files="$mismatch_files $rel_path"
        fi
    done < <(find "$src_skill_dir" -type f -not -name ".DS_Store" -not -path "*/.venv/*" -print0 2>/dev/null)

    if [ -z "$mismatch_files" ]; then
        pass "$skill_name" "match"
        SKILLS_MATCH=$((SKILLS_MATCH + 1))
    else
        # Trim and show first 2 mismatched files
        trimmed=$(echo "$mismatch_files" | xargs | cut -d' ' -f1-2)
        fail "$skill_name" "MISMATCH ($trimmed)" "cp -rf $src_skill_dir $DEPLOYED_DIR/skills/"
        SKILLS_MISMATCH=$((SKILLS_MISMATCH + 1))
        if [ "$FIX_MODE" = true ]; then
            cp -rf "$src_skill_dir" "$DEPLOYED_DIR/skills/"
            fix_applied "Re-copied $skill_name"
        fi
    fi
done

# Check for orphaned skills (deployed but not in source)
for dep_skill_dir in "$DEPLOYED_DIR/skills/"*/; do
    [ ! -d "$dep_skill_dir" ] && continue
    skill_name=$(basename "$dep_skill_dir")
    src_skill_dir="$PORTABLE_REPO/skills/$skill_name"

    if [ ! -d "$src_skill_dir" ]; then
        warn "$skill_name" "ORPHANED (not in source)" "rm -rf $dep_skill_dir"
        SKILLS_ORPHANED=$((SKILLS_ORPHANED + 1))
        if [ "$FIX_MODE" = true ]; then
            rm -rf "$dep_skill_dir"
            fix_applied "Removed orphaned $skill_name"
        fi
    fi
done

if [ "$JSON_MODE" = false ]; then
    echo -e "  ${DIM}──────────────────────────────────────────────────────${RESET}"
    echo -e "  ${DIM}Skills: $SKILLS_MATCH match, $SKILLS_MISMATCH mismatch, $SKILLS_MISSING missing, $SKILLS_ORPHANED orphaned${RESET}"
fi

# ============================================================================
# 2. CONFIG FILES
# ============================================================================

header "Config Files"

# CLAUDE.md (byte-for-byte comparison)
if [ -f "$PORTABLE_REPO/CLAUDE.md" ] && [ -f "$DEPLOYED_DIR/CLAUDE.md" ]; then
    src_md5=$(md5_of "$PORTABLE_REPO/CLAUDE.md")
    dep_md5=$(md5_of "$DEPLOYED_DIR/CLAUDE.md")
    if [ "$src_md5" = "$dep_md5" ]; then
        pass "CLAUDE.md" "match ($src_md5)"
    else
        fail "CLAUDE.md" "MISMATCH" "cp -f $PORTABLE_REPO/CLAUDE.md $DEPLOYED_DIR/CLAUDE.md"
        if [ "$FIX_MODE" = true ]; then
            cp -f "$PORTABLE_REPO/CLAUDE.md" "$DEPLOYED_DIR/CLAUDE.md"
            fix_applied "Re-copied CLAUDE.md"
        fi
    fi
elif [ ! -f "$DEPLOYED_DIR/CLAUDE.md" ]; then
    fail "CLAUDE.md" "MISSING" "cp -f $PORTABLE_REPO/CLAUDE.md $DEPLOYED_DIR/CLAUDE.md"
    if [ "$FIX_MODE" = true ]; then
        cp -f "$PORTABLE_REPO/CLAUDE.md" "$DEPLOYED_DIR/CLAUDE.md"
        fix_applied "Copied CLAUDE.md"
    fi
fi

# settings.json (byte-for-byte — no post-copy patching)
if [ -f "$PORTABLE_REPO/config/settings.json" ] && [ -f "$DEPLOYED_DIR/settings.json" ]; then
    src_md5=$(md5_of "$PORTABLE_REPO/config/settings.json")
    dep_md5=$(md5_of "$DEPLOYED_DIR/settings.json")
    if [ "$src_md5" = "$dep_md5" ]; then
        pass "settings.json" "match ($src_md5)"
    else
        fail "settings.json" "MISMATCH" "cp -f $PORTABLE_REPO/config/settings.json $DEPLOYED_DIR/settings.json"
        if [ "$FIX_MODE" = true ]; then
            cp -f "$PORTABLE_REPO/config/settings.json" "$DEPLOYED_DIR/settings.json"
            fix_applied "Re-copied settings.json"
        fi
    fi
elif [ ! -f "$DEPLOYED_DIR/settings.json" ]; then
    fail "settings.json" "MISSING" "cp -f $PORTABLE_REPO/config/settings.json $DEPLOYED_DIR/settings.json"
fi

# mcp.json (structural check — it gets patched with Herd paths and 1Password secrets)
if [ -f "$DEPLOYED_DIR/mcp.json" ]; then
    if command -v python3 &>/dev/null; then
        MCP_CHECK=$(python3 -c "
import json, sys
try:
    cfg = json.loads(open('$DEPLOYED_DIR/mcp.json').read())
    servers = cfg.get('mcpServers', {})
    expected = ['sequential-thinking', 'context7', 'chrome-devtools']
    missing = [s for s in expected if s not in servers]
    if missing:
        print(f'MISSING_SERVERS:{\",\".join(missing)}')
    else:
        print(f'OK:{len(servers)} servers')
except Exception as e:
    print(f'INVALID_JSON:{e}')
" 2>/dev/null)
        if [[ "$MCP_CHECK" == OK:* ]]; then
            pass "mcp.json" "valid (${MCP_CHECK#OK:})"
        elif [[ "$MCP_CHECK" == MISSING_SERVERS:* ]]; then
            fail "mcp.json" "missing servers: ${MCP_CHECK#MISSING_SERVERS:}" "Re-run install.sh to restore mcp.json"
        else
            fail "mcp.json" "invalid JSON" "cp -f $PORTABLE_REPO/config/mcp.json $DEPLOYED_DIR/mcp.json && re-run install.sh"
        fi
    else
        # No python3 — just check file exists
        pass "mcp.json" "exists (python3 needed for deep check)"
    fi
else
    fail "mcp.json" "MISSING" "cp -f $PORTABLE_REPO/config/mcp.json $DEPLOYED_DIR/mcp.json"
fi

# ============================================================================
# 3. SETTINGS VALIDATION
# ============================================================================

header "Settings Validation"

if [ -f "$DEPLOYED_DIR/settings.json" ] && command -v python3 &>/dev/null; then
    SETTINGS_CHECKS=$(python3 -c "
import json, sys

cfg = json.loads(open('$DEPLOYED_DIR/settings.json').read())
results = []

# Check hooks
hooks = cfg.get('hooks', {})
pre = hooks.get('PreToolUse', [])
pre_commands = []
for group in pre:
    for h in group.get('hooks', []):
        pre_commands.append(h.get('command', ''))

if any('bravros audit' in c for c in pre_commands):
    results.append(('pass', 'PreToolUse audit hook', 'configured'))
else:
    results.append(('fail', 'PreToolUse audit hook', 'MISSING', 'Re-copy settings.json from portable repo'))

session = hooks.get('SessionStart', [])
session_commands = []
for group in session:
    for h in group.get('hooks', []):
        session_commands.append(h.get('command', ''))

if any('telegram-patch.sh' in c for c in session_commands):
    results.append(('pass', 'SessionStart telegram hook', 'configured'))
else:
    results.append(('fail', 'SessionStart telegram hook', 'MISSING', 'Re-copy settings.json'))

if any('bravros update' in c or 'selfupdate' in c for c in session_commands):
    results.append(('pass', 'SessionStart selfupdate', 'configured'))
else:
    results.append(('fail', 'SessionStart selfupdate', 'MISSING', 'Re-copy settings.json'))

# Statusline
sl = cfg.get('statusLine', {})
if 'bravros statusline' in sl.get('command', ''):
    results.append(('pass', 'statusLine', 'bravros statusline'))
elif 'statusline.sh' in sl.get('command', ''):
    results.append(('warn', 'statusLine', 'bash fallback (no Go binary)', 'Build bravros for Go-based statusline'))
else:
    results.append(('fail', 'statusLine', 'MISCONFIGURED', 'Re-copy settings.json'))

# Plugins
plugins = cfg.get('enabledPlugins', {})
for p in ['ralph-loop@claude-plugins-official', 'telegram@claude-plugins-official']:
    name = p.split('@')[0]
    if plugins.get(p):
        results.append(('pass', f'Plugin: {name}', 'enabled'))
    else:
        results.append(('fail', f'Plugin: {name}', 'NOT ENABLED', f'Add \"{p}\": true to settings.json enabledPlugins'))

# Permissions
perms = cfg.get('permissions', {})
if perms.get('defaultMode') == 'dontAsk':
    results.append(('pass', 'permissions.defaultMode', 'dontAsk'))
else:
    results.append(('warn', 'permissions.defaultMode', f'{perms.get(\"defaultMode\", \"unset\")}', 'Expected dontAsk'))

# Env
env = cfg.get('env', {})
if env.get('CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS') == '1':
    results.append(('pass', 'Agent Teams env', 'enabled'))
else:
    results.append(('fail', 'Agent Teams env', 'NOT SET', 'Add CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1 to settings.json env'))

# Voice
if cfg.get('voiceEnabled'):
    results.append(('pass', 'voiceEnabled', 'true'))
else:
    results.append(('warn', 'voiceEnabled', 'disabled', 'Set voiceEnabled: true if needed'))

for r in results:
    if r[0] == 'pass':
        print(f'PASS|{r[1]}|{r[2]}')
    elif r[0] == 'fail':
        print(f'FAIL|{r[1]}|{r[2]}|{r[3]}')
    elif r[0] == 'warn':
        print(f'WARN|{r[1]}|{r[2]}|{r[3] if len(r) > 3 else \"\"}')
" 2>/dev/null)

    while IFS='|' read -r status name message fix; do
        case "$status" in
            PASS) pass "$name" "$message" ;;
            FAIL) fail "$name" "$message" "$fix" ;;
            WARN) warn "$name" "$message" "$fix" ;;
        esac
    done <<< "$SETTINGS_CHECKS"
else
    if [ ! -f "$DEPLOYED_DIR/settings.json" ]; then
        fail "settings.json" "FILE MISSING" "Run install.sh"
    else
        warn "Settings validation" "python3 not found" "Install python3 for deep validation"
    fi
fi

# ============================================================================
# 4. CLAUDE-CLI BINARY
# ============================================================================

header "bravros Binary"

CLI_BIN="$DEPLOYED_DIR/bin/bravros"

if [ -f "$CLI_BIN" ]; then
    if [ -x "$CLI_BIN" ]; then
        pass "bravros exists" "executable"
    else
        fail "bravros permissions" "NOT EXECUTABLE" "chmod +x $CLI_BIN"
        if [ "$FIX_MODE" = true ]; then
            chmod +x "$CLI_BIN"
            fix_applied "Made bravros executable"
        fi
    fi

    # Compare MD5 with source binary
    _OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    _ARCH=$(uname -m)
    case "$_ARCH" in
        x86_64)  _ARCH="amd64" ;;
        aarch64) _ARCH="arm64" ;;
    esac
    SRC_BIN="$PORTABLE_REPO/cli/bravros-${_OS}-${_ARCH}"

    if [ -f "$SRC_BIN" ]; then
        src_md5=$(md5_of "$SRC_BIN")
        dep_md5=$(md5_of "$CLI_BIN")
        if [ "$src_md5" = "$dep_md5" ]; then
            pass "bravros MD5" "match (${src_md5:0:12}...)"
        else
            fail "bravros MD5" "MISMATCH (src:${src_md5:0:8} dep:${dep_md5:0:8})" "cp -f $SRC_BIN $CLI_BIN && chmod +x $CLI_BIN"
            if [ "$FIX_MODE" = true ]; then
                cp -f "$SRC_BIN" "$CLI_BIN"
                chmod +x "$CLI_BIN"
                fix_applied "Re-copied bravros binary"
            fi
        fi
    else
        pass "bravros source" "binaries built by release Action (not shipped in repo)"
    fi

    # Version check
    CLI_VERSION=$("$CLI_BIN" version 2>/dev/null || echo "FAILED")
    if [ "$CLI_VERSION" != "FAILED" ]; then
        pass "bravros version" "$CLI_VERSION"
    else
        fail "bravros version" "EXECUTION FAILED" "Binary may be corrupt — rebuild or re-download"
    fi
else
    fail "bravros" "NOT FOUND at $CLI_BIN" "Run install.sh or build manually"
fi

# ============================================================================
# 5. 1PASSWORD INJECTIONS
# ============================================================================

header "1Password Injections"

# Check op CLI availability
if command -v op &>/dev/null; then
    pass "1Password CLI (op)" "available"

    # Sign in if not authenticated (triggers Touch ID)
    if ! op whoami &>/dev/null 2>&1; then
        op signin 2>/dev/null || true
    fi

    if op whoami &>/dev/null 2>&1; then
        pass "1Password auth" "authenticated"
    else
        warn "1Password auth" "not signed in" "Run: eval \$(op signin)"
    fi
else
    warn "1Password CLI (op)" "not installed" "Install: https://developer.1password.com/docs/cli/get-started/"
fi

# Context7 API Key in mcp.json
if [ -f "$DEPLOYED_DIR/mcp.json" ] && command -v python3 &>/dev/null; then
    C7_STATUS=$(python3 -c "
import json
cfg = json.loads(open('$DEPLOYED_DIR/mcp.json').read())
c7 = cfg.get('mcpServers', {}).get('context7', {})
key = c7.get('env', {}).get('CONTEXT7_API_KEY', '')
if not key or key == '__OP_INJECT__':
    print('MISSING')
else:
    print(f'OK:{key[:8]}...')
" 2>/dev/null)

    if [[ "$C7_STATUS" == OK:* ]]; then
        pass "Context7 API Key" "injected (${C7_STATUS#OK:})"
    else
        fail "Context7 API Key" "NOT INJECTED (still placeholder)" 'op read "op://HomeLab/Context7 Api Key/password" → inject into mcp.json'
        if [ "$FIX_MODE" = true ] && command -v op &>/dev/null; then
            C7_KEY=$(op read "op://HomeLab/Context7 Api Key/password" 2>/dev/null || true)
            if [ -n "$C7_KEY" ]; then
                C7_KEY="$C7_KEY" python3 -c "
import json, os
from pathlib import Path
p = Path('$DEPLOYED_DIR/mcp.json')
cfg = json.loads(p.read_text())
cfg['mcpServers']['context7'].setdefault('env', {})['CONTEXT7_API_KEY'] = os.environ['C7_KEY']
p.write_text(json.dumps(cfg, indent=4) + '\n')
" 2>/dev/null
                fix_applied "Injected Context7 API Key"
            fi
        fi
    fi
fi

# Telegram Bot Token
TG_ENV="$DEPLOYED_DIR/channels/telegram/.env"
if [ -f "$TG_ENV" ]; then
    if grep -q 'TELEGRAM_BOT_TOKEN=.\+' "$TG_ENV" 2>/dev/null; then
        TG_PREVIEW=$(grep 'TELEGRAM_BOT_TOKEN=' "$TG_ENV" | head -1 | cut -d'=' -f2 | cut -c1-8)
        pass "Telegram Bot Token" "injected (${TG_PREVIEW}...)"
    else
        fail "Telegram Bot Token" "EMPTY or MISSING value" 'op read "op://HomeLab/Telegram Bot Token/password" → write to .env'
        if [ "$FIX_MODE" = true ] && command -v op &>/dev/null; then
            TG_TOKEN=$(op read "op://HomeLab/Telegram Bot Token/password" 2>/dev/null || true)
            if [ -n "$TG_TOKEN" ]; then
                echo "TELEGRAM_BOT_TOKEN=$TG_TOKEN" > "$TG_ENV"
                chmod 600 "$TG_ENV"
                fix_applied "Injected Telegram Bot Token"
            fi
        fi
    fi
else
    fail "Telegram Bot Token" "FILE MISSING ($TG_ENV)" "mkdir -p $(dirname $TG_ENV) && run install.sh"
    if [ "$FIX_MODE" = true ] && command -v op &>/dev/null; then
        mkdir -p "$(dirname "$TG_ENV")"
        TG_TOKEN=$(op read "op://HomeLab/Telegram Bot Token/password" 2>/dev/null || true)
        if [ -n "$TG_TOKEN" ]; then
            echo "TELEGRAM_BOT_TOKEN=$TG_TOKEN" > "$TG_ENV"
            chmod 600 "$TG_ENV"
            fix_applied "Created .env with Telegram Bot Token"
        fi
    fi
fi

# Home Assistant (HASS_SERVER + HASS_TOKEN in shell RC)
# Prefer the RC file matching the user's actual shell (same logic as install.sh)
SHELL_RC=""
if [[ "$SHELL" == */zsh ]] && [ -f "$HOME/.zshrc" ]; then
    SHELL_RC="$HOME/.zshrc"
elif [[ "$SHELL" == */bash ]] && [ -f "$HOME/.bashrc" ]; then
    SHELL_RC="$HOME/.bashrc"
elif [ -f "$HOME/.zshrc" ]; then
    SHELL_RC="$HOME/.zshrc"
elif [ -f "$HOME/.bashrc" ]; then
    SHELL_RC="$HOME/.bashrc"
fi

if [ -n "$SHELL_RC" ]; then
    if grep -q 'HASS_SERVER' "$SHELL_RC" 2>/dev/null; then
        # Check it has a non-empty value
        HA_VAL=$(grep 'HASS_TOKEN=' "$SHELL_RC" 2>/dev/null | tail -1 | cut -d'"' -f2)
        if [ -n "$HA_VAL" ] && [ "$HA_VAL" != '""' ]; then
            pass "Home Assistant (HASS)" "configured in $SHELL_RC"
        else
            fail "Home Assistant (HASS)" "HASS_TOKEN is empty" 'op item get "HomeAssistant Long Live Token" → add to shell RC'
        fi
    else
        fail "Home Assistant (HASS)" "NOT in $SHELL_RC" "Run install.sh with op authenticated to inject HA credentials"
        if [ "$FIX_MODE" = true ] && command -v op &>/dev/null; then
            HA_TOKEN=$(op item get "HomeAssistant Long Live Token" --vault "HomeLab" --fields label=password --reveal 2>/dev/null || true)
            if [ -n "$HA_TOKEN" ]; then
                cat >> "$SHELL_RC" <<HAEOF

# -----------------------------------------------
# Home Assistant API Configuration
# -----------------------------------------------
export HASS_SERVER="http://homeassistant.local:8123"
export HASS_TOKEN="$HA_TOKEN"
HAEOF
                fix_applied "Injected Home Assistant credentials into $SHELL_RC"
            fi
        fi
    fi
else
    warn "Shell RC" "No .zshrc or .bashrc found" "Create one and re-run install.sh"
fi

# ============================================================================
# 6. DIRECTORY STRUCTURE & HOOKS
# ============================================================================

header "Directory Structure & Hooks"

REQUIRED_DIRS=(
    "skills"
    "hooks"
    "scripts"
    "templates"
    "cache"
    "bin"
    "channels/telegram"
    "channels/telegram/approved"
)

for dir in "${REQUIRED_DIRS[@]}"; do
    if [ -d "$DEPLOYED_DIR/$dir" ]; then
        pass "$dir/" "exists"
    else
        fail "$dir/" "MISSING" "mkdir -p $DEPLOYED_DIR/$dir"
        if [ "$FIX_MODE" = true ]; then
            mkdir -p "$DEPLOYED_DIR/$dir"
            fix_applied "Created $dir/"
        fi
    fi
done

# Hook executability
if [ -f "$DEPLOYED_DIR/hooks/telegram-patch.sh" ]; then
    if [ -x "$DEPLOYED_DIR/hooks/telegram-patch.sh" ]; then
        pass "telegram-patch.sh" "executable"
    else
        fail "telegram-patch.sh" "NOT EXECUTABLE" "chmod +x $DEPLOYED_DIR/hooks/telegram-patch.sh"
        if [ "$FIX_MODE" = true ]; then
            chmod +x "$DEPLOYED_DIR/hooks/telegram-patch.sh"
            fix_applied "Fixed telegram-patch.sh permissions"
        fi
    fi
else
    fail "telegram-patch.sh" "MISSING" "cp hooks/telegram-patch.sh from portable repo"
fi

# PATH check
if [ -n "$SHELL_RC" ]; then
    if grep -q 'claude/bin' "$SHELL_RC" 2>/dev/null; then
        pass "PATH (~/.claude/bin)" "in $SHELL_RC"
    else
        fail "PATH (~/.claude/bin)" "NOT in $SHELL_RC" 'echo '\''export PATH="\$HOME/.claude/bin:\$PATH"'\'' >> '"$SHELL_RC"
        if [ "$FIX_MODE" = true ]; then
            echo 'export PATH="$HOME/.claude/bin:$PATH"' >> "$SHELL_RC"
            fix_applied "Added ~/.claude/bin to PATH in $SHELL_RC"
        fi
    fi
fi

# Hooks comparison (source vs deployed)
if [ -d "$PORTABLE_REPO/hooks" ]; then
    for src_hook in "$PORTABLE_REPO/hooks/"*.sh "$PORTABLE_REPO/hooks/"*.py; do
        [ ! -f "$src_hook" ] && continue
        hook_name=$(basename "$src_hook")
        dep_hook="$DEPLOYED_DIR/hooks/$hook_name"

        if [ -f "$dep_hook" ]; then
            src_md5=$(md5_of "$src_hook")
            dep_md5=$(md5_of "$dep_hook")
            if [ "$src_md5" = "$dep_md5" ]; then
                pass "Hook: $hook_name" "match"
            else
                fail "Hook: $hook_name" "MISMATCH" "cp -f $src_hook $dep_hook && chmod +x $dep_hook"
                if [ "$FIX_MODE" = true ]; then
                    cp -f "$src_hook" "$dep_hook"
                    chmod +x "$dep_hook"
                    fix_applied "Re-copied $hook_name"
                fi
            fi
        fi
    done
fi

# Scripts comparison
if [ -d "$PORTABLE_REPO/scripts" ]; then
    for src_script in "$PORTABLE_REPO/scripts/"*.sh "$PORTABLE_REPO/scripts/"*.py; do
        [ ! -f "$src_script" ] && continue
        script_name=$(basename "$src_script")
        dep_script="$DEPLOYED_DIR/scripts/$script_name"

        if [ -f "$dep_script" ]; then
            src_md5=$(md5_of "$src_script")
            dep_md5=$(md5_of "$dep_script")
            if [ "$src_md5" = "$dep_md5" ]; then
                pass "Script: $script_name" "match"
            else
                fail "Script: $script_name" "MISMATCH" "cp -f $src_script $dep_script"
                if [ "$FIX_MODE" = true ]; then
                    cp -f "$src_script" "$dep_script"
                    chmod +x "$dep_script" 2>/dev/null || true
                    fix_applied "Re-copied $script_name"
                fi
            fi
        fi
    done
fi

# Templates comparison (just check the directory exists and has content)
if [ -d "$DEPLOYED_DIR/templates" ]; then
    TEMPLATE_COUNT=$(find "$DEPLOYED_DIR/templates" -type f 2>/dev/null | wc -l | tr -d ' ')
    if [ "$TEMPLATE_COUNT" -gt 0 ]; then
        pass "Templates" "$TEMPLATE_COUNT files deployed"
    else
        warn "Templates" "directory empty" "Re-run install.sh"
    fi
else
    fail "Templates" "MISSING" "Run install.sh"
fi

# ============================================================================
# 7. EXTERNAL TOOLS
# ============================================================================

header "External Tools"

# Tool availability checks (individual to avoid associative array issues with set -u)
check_tool() {
    local tool="$1" install_hint="$2"
    if command -v "$tool" &>/dev/null; then
        local version
        version=$("$tool" --version 2>/dev/null | head -1 || echo "available")
        pass "$tool" "$version"
    else
        fail "$tool" "NOT INSTALLED" "$install_hint"
    fi
}

check_tool "uv" "curl -LsSf https://astral.sh/uv/install.sh | sh"
check_tool "firecrawl" "npm install -g firecrawl-cli@1.8.0"
check_tool "pipx" "brew install pipx (macOS) or apt install pipx (Linux)"
check_tool "hass-cli" "pipx install homeassistant-cli"
check_tool "notebooklm" "pipx install notebooklm-py"

# Plugins check
if [ -f "$DEPLOYED_DIR/plugins/installed_plugins.json" ]; then
    for plugin in ralph-loop telegram; do
        if grep -q "\"$plugin@" "$DEPLOYED_DIR/plugins/installed_plugins.json" 2>/dev/null; then
            pass "Plugin: $plugin" "installed"
        else
            fail "Plugin: $plugin" "NOT INSTALLED" "claude plugins install $plugin"
        fi
    done
else
    warn "Plugins file" "installed_plugins.json not found" "Run: claude plugins install ralph-loop && claude plugins install telegram"
fi

# Claude Code CLI itself
if command -v claude &>/dev/null; then
    pass "Claude Code CLI" "available"
else
    warn "Claude Code CLI" "not in PATH" "Install Claude Code: https://docs.claude.com"
fi

# Telegram relay files
if [ -d "$DEPLOYED_DIR/telegram" ]; then
    TG_FILES=$(ls "$DEPLOYED_DIR/telegram/"*.ts 2>/dev/null | wc -l | tr -d ' ')
    if [ "$TG_FILES" -gt 0 ]; then
        pass "Telegram relay" "$TG_FILES TypeScript files"
    else
        warn "Telegram relay" "directory empty" "Re-run install.sh"
    fi
else
    warn "Telegram relay" "~/.claude/telegram/ missing" "Re-run install.sh"
fi

# Telegram access.json
if [ -f "$DEPLOYED_DIR/channels/telegram/access.json" ]; then
    pass "Telegram access.json" "exists"
else
    fail "Telegram access.json" "MISSING" "cp channels/telegram/access.json from portable repo"
fi

# claudd alias
if [ -n "$SHELL_RC" ] && grep -q 'alias claudd=' "$SHELL_RC" 2>/dev/null; then
    pass "claudd alias" "in $SHELL_RC"
else
    warn "claudd alias" "not found" "Add to shell RC: alias claudd=\"cd $PORTABLE_REPO-telegram && claude ...\""
fi

# ============================================================================
# SUMMARY
# ============================================================================

if [ "$JSON_MODE" = true ]; then
    echo "${JSON_RESULTS}]"
    exit 0
fi

echo ""
echo -e "${BOLD}━━ Summary ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
TOTAL=$((TOTAL_PASS + TOTAL_FAIL + TOTAL_WARN))

if [ "$TOTAL_FAIL" -eq 0 ] && [ "$TOTAL_WARN" -eq 0 ]; then
    echo -e "  ${GREEN}${BOLD}All $TOTAL checks passed!${RESET} ${GREEN}Your installation is healthy.${RESET}"
elif [ "$TOTAL_FAIL" -eq 0 ]; then
    echo -e "  ${GREEN}$TOTAL_PASS passed${RESET}, ${YELLOW}$TOTAL_WARN warnings${RESET}"
    echo -e "  ${DIM}Warnings are non-critical but worth reviewing.${RESET}"
else
    echo -e "  ${GREEN}$TOTAL_PASS passed${RESET}, ${RED}$TOTAL_FAIL failed${RESET}, ${YELLOW}$TOTAL_WARN warnings${RESET}"
    if [ "$FIX_MODE" = true ]; then
        echo -e "  ${GREEN}$FIXES_APPLIED fixes applied.${RESET}"
        if [ "$TOTAL_FAIL" -gt "$FIXES_APPLIED" ]; then
            echo -e "  ${DIM}Some issues require manual intervention (see above).${RESET}"
        fi
    else
        echo -e "  ${DIM}Run with ${BOLD}--fix${RESET}${DIM} to auto-fix issues:${RESET}"
        echo -e "  ${CYAN}bash ~/.claude/skills/verify-install/scripts/verify.sh --fix${RESET}"
    fi
fi

if [ "$FIX_MODE" = true ] && [ "$FIXES_APPLIED" -gt 0 ]; then
    echo ""
    echo -e "  ${CYAN}Re-run verification to confirm all fixes:${RESET}"
    echo -e "  ${DIM}bash ~/.claude/skills/verify-install/scripts/verify.sh${RESET}"
fi

echo ""

# Exit code: 0 if no failures, 1 if any failures
[ "$TOTAL_FAIL" -eq 0 ]
