#!/bin/bash
# ============================================================================
# Bravros SDLC — installation health check
# Compares the source repo against the deployed runtime (~/.bravros).
# Usage: bash verify.sh [--fix] [--json] [--auto]
# ============================================================================

set -uo pipefail

DEPLOYED_DIR="${DEPLOYED_DIR:-$HOME/.claude}"
export PATH="$DEPLOYED_DIR/bin:$HOME/.local/bin:$PATH"

# Source repo: $PORTABLE_REPO wins, then the canonical checkout locations.
if [ -n "${PORTABLE_REPO:-}" ]; then
    :
elif [ -d "$HOME/Code/monorepos/bravros/private" ]; then
    PORTABLE_REPO="$HOME/Code/monorepos/bravros/private"
elif [ -d "$HOME/Code/monorepos/bravros/bravros" ]; then
    PORTABLE_REPO="$HOME/Code/monorepos/bravros/bravros"
else
    PORTABLE_REPO="$HOME/bravros"
fi

VERIFY_SCRIPT="$DEPLOYED_DIR/skills/verify-install/scripts/verify.sh"
FIX_MODE=false
JSON_MODE=false
AUTO_MODE=false

for arg in "$@"; do
    case "$arg" in
        --fix)  FIX_MODE=true ;;
        --json) JSON_MODE=true ;;
        --auto) AUTO_MODE=true ;;
    esac
done

# --auto is the SessionStart contract: silent when healthy, machine-readable when
# not. It buffers every finding and prints nothing at all on a clean run.
QUIET=false
[ "$JSON_MODE" = true ] && QUIET=true
[ "$AUTO_MODE" = true ] && QUIET=true

if [ -t 1 ] && [ "$QUIET" = false ]; then
    GREEN='\033[0;32m'; RED='\033[0;31m'; YELLOW='\033[0;33m'
    CYAN='\033[0;36m'; BOLD='\033[1m'; DIM='\033[2m'; RESET='\033[0m'
else
    GREEN='' RED='' YELLOW='' CYAN='' BOLD='' DIM='' RESET=''
fi

TOTAL_PASS=0; TOTAL_FAIL=0; TOTAL_WARN=0; TOTAL_INTENTIONAL=0; FIXES_APPLIED=0
JSON_RESULTS="[]"
AUTO_LINES=""

# ── Intentional-divergence opt-out ──────────────────────────────────────────
# Some operators deliberately run a non-canonical setup (this one runs with the
# hooks stripped out of settings.json on purpose). A permanently-red report is
# worse than no report: it trains you to ignore the output, which is exactly
# where real drift hides. $DEPLOYED_DIR/.verify-ignore lists check LABELS to
# report as INTENTIONAL instead of failing — one per line, `#` comments allowed,
# a trailing `*` matches by prefix. Ignored checks never count as failures and
# --fix skips them. Never "repair" one; never suggest deleting the file.
VERIFY_IGNORE_FILE="${BRAVROS_VERIFY_IGNORE:-$DEPLOYED_DIR/.verify-ignore}"
VERIFY_IGNORE_LIST=()
if [ -f "$VERIFY_IGNORE_FILE" ]; then
    while IFS= read -r _vline || [ -n "$_vline" ]; do
        _vline="${_vline%%#*}"
        _vline="$(printf '%s' "$_vline" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
        [ -n "$_vline" ] && VERIFY_IGNORE_LIST+=("$_vline")
    done < "$VERIFY_IGNORE_FILE"
fi

is_ignored() {
    local label="$1" pat
    [ ${#VERIFY_IGNORE_LIST[@]} -gt 0 ] || return 1
    for pat in "${VERIFY_IGNORE_LIST[@]}"; do
        case "$pat" in
            *\*) [ "${label##"${pat%\*}"}" != "$label" ] && return 0 ;;
            *)   [ "$label" = "$pat" ] && return 0 ;;
        esac
    done
    return 1
}

# is_locked FILE — the operator deliberately froze this path. Two mechanisms:
# chmod 400 (portable) and macOS `chflags uchg` (survives chmod). BOTH mean
# healthy-by-choice, never drift: `bravros deploy` already skips a locked
# settings.json, and this script must agree or the lock is worthless.
is_locked() {
    local f="$1"
    [ -e "$f" ] || return 1
    [ ! -w "$f" ] && return 0
    if [ "$(uname -s)" = "Darwin" ]; then
        ls -lO "$f" 2>/dev/null | awk '{print $5}' | grep -q 'uchg' && return 0
    fi
    return 1
}

# Set by fail()/warn(): 1 when the check just reported was on the ignore list.
# Read only through should_fix — `[ "$FIX_MODE" = true ]` alone is NOT enough,
# because fail()'s `return` exits fail(), not the caller's if/else, so the fix
# block would still revert the divergence the operator declared intentional.
LAST_CHECK_IGNORED=0
should_fix() {
    [ "$FIX_MODE" = true ] || return 1
    [ "$LAST_CHECK_IGNORED" = "1" ] && return 1
    # cp -f UNLINKS a destination it cannot open for writing, silently defeating
    # an operator lock. Callers that write a known path must pass it.
    if [ -n "${1:-}" ] && is_locked "$1"; then
        [ "$QUIET" = false ] && echo -e "     ${DIM}skipped — $1 is locked (operator lock)${RESET}"
        return 1
    fi
    return 0
}

json_add() {
    local name="${1//\"/\\\"}" status="$2" message="${3//\"/\\\"}" fix="${4:-}"
    fix="${fix//\"/\\\"}"
    local entry="{\"name\":\"$name\",\"status\":\"$status\",\"message\":\"$message\",\"fix\":\"$fix\"}"
    if [ "$JSON_RESULTS" = "[]" ]; then JSON_RESULTS="[$entry"; else JSON_RESULTS="$JSON_RESULTS,$entry"; fi
}

auto_line() { AUTO_LINES="${AUTO_LINES}$1"$'\n'; }

header() {
    [ "$QUIET" = false ] || return 0
    echo ""
    echo -e "${BOLD}━━ $1 ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
}

pass() {
    TOTAL_PASS=$((TOTAL_PASS + 1))
    [ "$QUIET" = false ] && printf "  ${GREEN}✅${RESET} %-28s %s\n" "$1" "${DIM}$2${RESET}"
    json_add "$1" "pass" "$2" ""
}

intentional() {
    TOTAL_INTENTIONAL=$((TOTAL_INTENTIONAL + 1))
    [ "$QUIET" = false ] && printf "  ${CYAN}ℹ️ ${RESET} %-28s ${DIM}INTENTIONAL — %s${RESET}\n" "$1" "$2"
    json_add "$1" "intentional" "$2" ""
}

fail() {
    if is_ignored "$1"; then LAST_CHECK_IGNORED=1; intentional "$1" "$2"; return; fi
    LAST_CHECK_IGNORED=0
    TOTAL_FAIL=$((TOTAL_FAIL + 1))
    if [ "$QUIET" = false ]; then
        printf "  ${RED}❌${RESET} %-28s ${RED}%s${RESET}\n" "$1" "$2"
        [ -n "${3:-}" ] && echo -e "     ${DIM}↳ Fix: $3${RESET}"
    fi
    json_add "$1" "fail" "$2" "${3:-}"
}

warn() {
    if is_ignored "$1"; then LAST_CHECK_IGNORED=1; intentional "$1" "$2"; return; fi
    LAST_CHECK_IGNORED=0
    TOTAL_WARN=$((TOTAL_WARN + 1))
    if [ "$QUIET" = false ]; then
        printf "  ${YELLOW}⚠️ ${RESET}  %-28s ${YELLOW}%s${RESET}\n" "$1" "$2"
        [ -n "${3:-}" ] && echo -e "     ${DIM}↳ $3${RESET}"
    fi
    json_add "$1" "warn" "$2" "${3:-}"
}

fix_applied() {
    FIXES_APPLIED=$((FIXES_APPLIED + 1))
    [ "$QUIET" = false ] && echo -e "     ${GREEN}✔ Fixed: $1${RESET}"
    return 0
}

md5_of() {
    if command -v md5sum &>/dev/null; then md5sum "$1" 2>/dev/null | awk '{print $1}'
    elif command -v md5 &>/dev/null; then md5 -q "$1" 2>/dev/null
    else openssl md5 "$1" 2>/dev/null | awk '{print $NF}'; fi
}

# ============================================================================
# 1. BINARY
# ============================================================================
header "Bravros Binary"

CLI_BIN="$DEPLOYED_DIR/bin/bravros"
BRAVROS_BIN=""
if [ -x "$CLI_BIN" ]; then
    BRAVROS_BIN="$CLI_BIN"
elif command -v bravros &>/dev/null; then
    BRAVROS_BIN="bravros"
fi

if [ ! -f "$CLI_BIN" ]; then
    fail "bravros binary" "NOT FOUND at $CLI_BIN" "bash $PORTABLE_REPO/install.sh"
    auto_line "BINARY: missing — $CLI_BIN"
elif [ ! -x "$CLI_BIN" ]; then
    fail "bravros binary" "NOT EXECUTABLE" "chmod +x $CLI_BIN"
    auto_line "BINARY: not-executable — $CLI_BIN"
    if should_fix; then chmod +x "$CLI_BIN" && fix_applied "chmod +x $CLI_BIN"; fi
else
    CLI_VERSION=$("$CLI_BIN" version 2>/dev/null | head -1)
    if [ -n "$CLI_VERSION" ]; then
        pass "bravros version" "$CLI_VERSION"
    else
        fail "bravros version" "EXECUTION FAILED" "Binary corrupt — bash $PORTABLE_REPO/install.sh"
        auto_line "BINARY: exec-failed — $CLI_BIN"
    fi
fi

# Skills and hooks invoke `bravros` bare, so ~/.bravros/bin must be persisted in a
# shell RC — an exported PATH in the current shell does not survive.
PATH_RC_FOUND=""
for _rc in "$HOME/.zshrc" "$HOME/.zprofile" "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.profile"; do
    [ -f "$_rc" ] || continue
    if grep -E -q '(claude|bravros)/bin' "$_rc" 2>/dev/null; then PATH_RC_FOUND="$_rc"; break; fi
done
if [ -n "$PATH_RC_FOUND" ]; then
    pass "bravros PATH" "persisted in $(basename "$PATH_RC_FOUND")"
else
    fail "bravros PATH" "~/.bravros/bin not in any shell RC" "bash $PORTABLE_REPO/install.sh"
    auto_line "PATH: ~/.bravros/bin absent from every shell RC"
    if should_fix; then
        case "${SHELL:-}" in
            */zsh)  _RC="$HOME/.zshrc" ;;
            */bash) _RC="$HOME/.bashrc" ;;
            *)      _RC="$HOME/.profile" ;;
        esac
        touch "$_RC" 2>/dev/null || true
        echo 'export PATH="$HOME/.claude/bin:$PATH"' >> "$_RC" 2>/dev/null \
            && fix_applied "Added PATH export to $_RC"
    fi
fi

# ============================================================================
# 2. RUNTIME LAYOUT
# ============================================================================
header "Runtime Layout"

for dir in skills hooks scripts templates cache bin; do
    if [ -d "$DEPLOYED_DIR/$dir" ]; then
        pass "$dir/" "exists"
    else
        fail "$dir/" "MISSING" "mkdir -p $DEPLOYED_DIR/$dir"
        auto_line "LAYOUT: missing $DEPLOYED_DIR/$dir"
        if should_fix; then mkdir -p "$DEPLOYED_DIR/$dir" && fix_applied "Created $dir/"; fi
    fi
done

if [ -d "$DEPLOYED_DIR/templates" ]; then
    TEMPLATE_COUNT=$(find "$DEPLOYED_DIR/templates" -type f 2>/dev/null | wc -l | tr -d ' ')
    [ "$TEMPLATE_COUNT" -gt 0 ] && pass "templates" "$TEMPLATE_COUNT files" \
        || warn "templates" "directory empty" "bash $PORTABLE_REPO/install.sh"
fi

# ============================================================================
# 3. SKILLS INTEGRITY (manifest-SHA)
# ============================================================================
# The digest is deploy.ComputeSkillSHA: sha256 over sorted "relpath\0filesha\n"
# records, symlinks RESOLVED, the manifest itself excluded. Resolution is what
# makes source and runtime comparable — a source skill's references/*.md are
# symlinks into skills/shared/ and deploy materializes them with `cp -RL`, so a
# clean deploy yields byte-identical digests on both sides (verified 2026-08-13).
#
# There is exactly ONE implementation of that digest and it lives in Go —
# `bravros deploy skill-sha <dir>`. Do NOT re-derive it in bash or python: a
# shell SHA cannot even build the record format (bash strings are NUL-terminated
# C strings, so `sep=$'\x00'` assigns the empty string) and every reimplementation
# drifts silently, turning this script into a false-alarm machine. No binary =
# check 1 already failed and this section is skipped.
header "Skills Integrity"

SKILLS_MATCH=0; SKILLS_DRIFT=0; SKILLS_MISSING=0; SKILLS_ORPHANED=0
MANIFEST_PATH="$DEPLOYED_DIR/skills/.deploy-manifest.json"

skill_sha() { "$BRAVROS_BIN" deploy skill-sha "$1" 2>/dev/null | tr -d '[:space:]'; }

redeploy_skill() {
    [ -n "$BRAVROS_BIN" ] || return 1
    (cd "$PORTABLE_REPO" && "$BRAVROS_BIN" deploy --force --filter "$1" >/dev/null 2>&1)
}

if [ -z "$BRAVROS_BIN" ]; then
    warn "skills integrity" "skipped — no bravros binary" "fix the binary first, then re-run"
elif [ ! -d "$PORTABLE_REPO/skills" ]; then
    # No source checkout is the NORMAL state under the embed model (P-0018):
    # skills ship inside the binary and `bravros selfupdate` owns integrity via
    # the SHA manifest. Repo-vs-deployed diffing is a dev-machine extra, so its
    # absence is informational, never a failure.
    pass "source repo" "none — embed model; integrity owned by bravros selfupdate (clone bravros/bravros only for dev diffing)"
else
    # The manifest is what `bravros deploy` consults to skip unchanged skills.
    # Its absence forces a full redeploy — worth surfacing, not a blocker here.
    if [ -f "$MANIFEST_PATH" ]; then
        pass "deploy manifest" "present"
    else
        warn "deploy manifest" "absent — next deploy will be a full re-copy" "bravros deploy"
        auto_line "MANIFEST: absent — run bravros deploy"
    fi

    for src_skill_dir in "$PORTABLE_REPO/skills/"*/; do
        [ -d "$src_skill_dir" ] || continue
        skill_name=$(basename "$src_skill_dir")
        # skills/shared/ is repo-only source material — deploy materializes copies
        # into each consumer, it must never land as a skill of its own.
        case "$skill_name" in shared|_shared) continue ;; esac
        dep_skill_dir="$DEPLOYED_DIR/skills/$skill_name"

        if [ ! -d "$dep_skill_dir" ]; then
            fail "$skill_name" "MISSING (not deployed)" "bravros deploy --force --filter $skill_name"
            auto_line "SKILL_MISSING: $skill_name — bravros deploy --force --filter $skill_name"
            SKILLS_MISSING=$((SKILLS_MISSING + 1))
            should_fix && redeploy_skill "$skill_name" && fix_applied "Deployed $skill_name"
            continue
        fi

        src_sha=$(skill_sha "${src_skill_dir%/}")
        # Deploy REWRITES host paths in content (~/.bravros/skills becomes the
        # host config dir's skills/ — cli/internal/deploy/hostpaths.go), so
        # hashing the deployed tree can NEVER
        # match the source for any skill that mentions those paths — a permanent
        # phantom DRIFT that --fix redeploys forever without converging. The deploy
        # manifest records the SOURCE sha at deploy time, so source-vs-manifest is
        # the true drift signal; the deployed-tree hash is only a fallback when no
        # manifest entry exists.
        dep_sha=""
        if [ -f "$MANIFEST_PATH" ]; then
            dep_sha=$(python3 -c "
import json,sys
try:
    m=json.load(open('$MANIFEST_PATH'))
    print(m.get('skills',{}).get('$skill_name',''))
except Exception:
    pass" 2>/dev/null | tr -d '[:space:]')
        fi
        [ -n "$dep_sha" ] || dep_sha=$(skill_sha "$dep_skill_dir")
        if [ -z "$src_sha" ] || [ -z "$dep_sha" ]; then
            warn "$skill_name" "SHA unavailable (broken symlink?)" "bravros deploy skill-sha ${src_skill_dir%/}"
            continue
        fi

        if [ "$src_sha" = "$dep_sha" ]; then
            pass "$skill_name" "match (${src_sha:0:12}…)"
            SKILLS_MATCH=$((SKILLS_MATCH + 1))
        else
            fail "$skill_name" "DRIFT (source ${src_sha:0:12}… runtime ${dep_sha:0:12}…)" \
                "bravros deploy --force --filter $skill_name"
            auto_line "SKILL_DRIFT: $skill_name — bravros deploy --force --filter $skill_name"
            SKILLS_DRIFT=$((SKILLS_DRIFT + 1))
            should_fix && redeploy_skill "$skill_name" && fix_applied "Re-deployed $skill_name"
        fi
    done

    # Orphans: deployed but retired from source. `bravros deploy` prunes these on
    # its own; they matter here because a stale SKILL.md still fires its triggers.
    for dep_skill_dir in "$DEPLOYED_DIR/skills/"*/; do
        [ -d "$dep_skill_dir" ] || continue
        skill_name=$(basename "$dep_skill_dir")
        case "$skill_name" in
            shared|_shared)
                fail "$skill_name" "MUST NOT BE DEPLOYED (repo-only shared material)" "rm -rf $dep_skill_dir"
                auto_line "SHARED_LEAK: $dep_skill_dir"
                SKILLS_ORPHANED=$((SKILLS_ORPHANED + 1))
                should_fix && rm -rf "$dep_skill_dir" && fix_applied "Removed deployed shared material"
                continue ;;
        esac
        if [ ! -d "$PORTABLE_REPO/skills/$skill_name" ]; then
            warn "$skill_name" "ORPHANED (retired from source)" "run --fix to prune"
            auto_line "SKILL_ORPHAN: $skill_name"
            SKILLS_ORPHANED=$((SKILLS_ORPHANED + 1))
            should_fix && rm -rf "$dep_skill_dir" && fix_applied "Pruned orphan: $skill_name"
        fi
    done

    [ "$QUIET" = false ] && echo -e "  ${DIM}Skills: $SKILLS_MATCH match, $SKILLS_DRIFT drift, $SKILLS_MISSING missing, $SKILLS_ORPHANED orphaned${RESET}"
fi

# ============================================================================
# 4. CONFIG & HOOKS
# ============================================================================
header "Config & Hooks"

# Managed global CLAUDE.md — the deployed file is NOT a copy of the repo's
# CLAUDE.md (that one is a project doc). It carries the operator's personal
# content plus a marker-delimited block sourced from home/CLAUDE.md. Reconcile
# ONLY that block; never `cp -f` the whole file or personal edits are gone.
_reconcile="$PORTABLE_REPO/scripts/reconcile-global-claude.py"
_gsrc="$PORTABLE_REPO/home/CLAUDE.md"
[ -f "$_gsrc" ] || _gsrc="$DEPLOYED_DIR/templates/global-CLAUDE.md"
if [ -f "$_reconcile" ] && [ -f "$_gsrc" ] && command -v python3 >/dev/null 2>&1; then
    _rc=$(python3 "$_reconcile" --src "$_gsrc" --dest "$DEPLOYED_DIR/CLAUDE.md" --check 2>/dev/null)
    if [ "$_rc" = "unchanged" ]; then
        pass "CLAUDE.md managed block" "in sync"
    else
        fail "CLAUDE.md managed block" "DRIFT ($_rc)" "python3 $_reconcile --src $_gsrc --dest $DEPLOYED_DIR/CLAUDE.md"
        auto_line "CLAUDE_MD: managed block drift ($_rc)"
        if should_fix "$DEPLOYED_DIR/CLAUDE.md"; then
            python3 "$_reconcile" --src "$_gsrc" --dest "$DEPLOYED_DIR/CLAUDE.md" \
                --backup "$DEPLOYED_DIR/CLAUDE.md.bak.$(date +%Y%m%d-%H%M%S)" >/dev/null 2>&1 \
                && fix_applied "Reconciled managed block (personal content preserved)"
        fi
    fi
else
    pass "CLAUDE.md managed block" "skipped (reconcile script unavailable)"
fi

# settings.json — presence + parseability only. It is NOT byte-compared against
# config/settings.json: the operator legitimately edits it (hooks stripped,
# per-machine plugins), and a locked file is a deliberate choice, not drift.
SETTINGS="$DEPLOYED_DIR/settings.json"
if [ ! -f "$SETTINGS" ]; then
    fail "settings.json" "MISSING" "cp $PORTABLE_REPO/config/settings.json $SETTINGS"
    auto_line "SETTINGS: missing"
elif command -v jq &>/dev/null && ! jq empty "$SETTINGS" >/dev/null 2>&1; then
    fail "settings.json" "INVALID JSON" "restore from $PORTABLE_REPO/config/settings.json"
    auto_line "SETTINGS: invalid JSON"
elif is_locked "$SETTINGS"; then
    pass "settings.json" "valid — locked by operator (healthy)"
else
    pass "settings.json" "valid"
fi

# commit-msg hook — the format gate every repo's .git/hooks points at. Identity
# is the canonical marker on line 2, not a byte hash: downstream repos may carry
# an operator customization, which `bravros hooks update --force` overwrites only
# on request.
HOOK_TEMPLATE="$DEPLOYED_DIR/templates/.githooks/commit-msg"
HOOK_SRC="$PORTABLE_REPO/templates/.githooks/commit-msg"
if [ ! -f "$HOOK_TEMPLATE" ]; then
    fail "commit-msg template" "MISSING" "bash $PORTABLE_REPO/install.sh"
    auto_line "HOOK_TEMPLATE: missing"
elif ! grep -q 'bravros-managed-commit-msg-hook' "$HOOK_TEMPLATE" 2>/dev/null; then
    fail "commit-msg template" "canonical marker absent" "bravros hooks update --force"
    auto_line "HOOK_TEMPLATE: marker absent — bravros hooks update --force"
elif [ -f "$HOOK_SRC" ] && [ "$(md5_of "$HOOK_SRC")" != "$(md5_of "$HOOK_TEMPLATE")" ]; then
    warn "commit-msg template" "differs from source" "bravros hooks update --force"
    auto_line "HOOK_TEMPLATE: drift — bravros hooks update --force"
    if should_fix "$HOOK_TEMPLATE"; then
        cp -f "$HOOK_SRC" "$HOOK_TEMPLATE" && chmod +x "$HOOK_TEMPLATE" \
            && fix_applied "Refreshed commit-msg template"
    fi
else
    pass "commit-msg template" "canonical"
fi

# Claude Code hooks + shared scripts, source vs deployed.
for _pair in "hooks:*.sh" "hooks:*.py" "scripts:*.sh" "scripts:*.py"; do
    _sub="${_pair%%:*}"; _glob="${_pair##*:}"
    [ -d "$PORTABLE_REPO/$_sub" ] || continue
    for src_f in "$PORTABLE_REPO/$_sub/"$_glob; do
        [ -f "$src_f" ] || continue
        f_name=$(basename "$src_f")
        dep_f="$DEPLOYED_DIR/$_sub/$f_name"
        if [ ! -f "$dep_f" ]; then
            fail "$_sub/$f_name" "MISSING" "cp -f $src_f $dep_f"
            auto_line "FILE_MISSING: $dep_f"
            if should_fix "$dep_f"; then
                cp -f "$src_f" "$dep_f" && chmod +x "$dep_f" 2>/dev/null
                fix_applied "Copied $_sub/$f_name"
            fi
        elif [ "$(md5_of "$src_f")" != "$(md5_of "$dep_f")" ]; then
            fail "$_sub/$f_name" "MISMATCH" "cp -f $src_f $dep_f"
            auto_line "FILE_DRIFT: $dep_f"
            if should_fix "$dep_f"; then
                cp -f "$src_f" "$dep_f" && chmod +x "$dep_f" 2>/dev/null
                fix_applied "Re-copied $_sub/$f_name"
            fi
        else
            pass "$_sub/$f_name" "match"
        fi
    done
done

# MCP servers — every key in config/mcp.json is bravros-managed and must be
# registered in the live runtime state (~/.bravros.json).
_CLAUDE_JSON="$HOME/.claude.json"
_MCP_CONFIG="$PORTABLE_REPO/config/mcp.json"
if command -v jq &>/dev/null && [ -f "$_CLAUDE_JSON" ] && [ -f "$_MCP_CONFIG" ]; then
    _MISSING_MCP=""
    _EXPECTED=$(jq -r '.mcpServers | keys[]' "$_MCP_CONFIG" 2>/dev/null | sort)
    _REGISTERED=$(jq -r '(.mcpServers // {}) | keys[]' "$_CLAUDE_JSON" 2>/dev/null | sort)
    for _srv in $_EXPECTED; do
        echo "$_REGISTERED" | grep -qx "$_srv" || _MISSING_MCP="${_MISSING_MCP:+$_MISSING_MCP, }$_srv"
    done
    if [ -z "$_MISSING_MCP" ]; then
        pass "mcp servers" "$(echo "$_EXPECTED" | grep -c .) registered"
    else
        fail "mcp servers" "missing: $_MISSING_MCP" "bravros mcp register --from $_MCP_CONFIG"
        auto_line "MCP_MISSING: $_MISSING_MCP"
    fi
fi

# ============================================================================
# 5. TOOLCHAIN (delegated — never duplicated here)
# ============================================================================
# `bravros doctor --quick` owns gh/jq/curl/git + ~/.bravros presence, secret-free.
# Re-implementing those checks here is how the two drift apart.
header "Toolchain"

if [ -n "$BRAVROS_BIN" ]; then
    DOCTOR_JSON=$("$BRAVROS_BIN" doctor --quick --json 2>/dev/null)
    if [ -z "$DOCTOR_JSON" ]; then
        pass "bravros doctor --quick" "healthy (silent)"
    elif command -v jq &>/dev/null; then
        D_STATUS=$(echo "$DOCTOR_JSON" | jq -r '.status // "unknown"' 2>/dev/null)
        auto_line "DOCTOR_STATUS: $D_STATUS"
        while IFS='|' read -r c_name c_status c_msg c_fix; do
            [ -n "$c_name" ] || continue
            case "$c_status" in
                ok|OK|healthy) pass "doctor: $c_name" "ok" ;;
                *) warn "doctor: $c_name" "$c_status${c_msg:+ — $c_msg}" "$c_fix"
                   auto_line "DOCTOR_CHECK: $c_name — $c_status${c_msg:+ — $c_msg}${c_fix:+ — fix: $c_fix}" ;;
            esac
        done < <(echo "$DOCTOR_JSON" | jq -r '.checks[]? | [.name, .status, (.message // ""), (.fix_hint // "")] | join("|")' 2>/dev/null)
    else
        warn "bravros doctor --quick" "output not parseable without jq" "brew install jq"
    fi
fi

# Per-skill dependency manifest — surfaced, never auto-installed (an install is
# the operator's call, and --auto must stay side-effect free).
if [ -n "$BRAVROS_BIN" ] && command -v jq &>/dev/null; then
    # Output is a top-level array; the install command is per-OS
    # (install_cmd_macos / install_cmd_linux), never a bare install_cmd.
    _INSTALL_KEY="install_cmd_linux"
    [ "$(uname -s)" = "Darwin" ] && _INSTALL_KEY="install_cmd_macos"
    DEPS_JSON=$("$BRAVROS_BIN" skills deps --format json 2>/dev/null)
    if [ -n "$DEPS_JSON" ]; then
        _dep_missing=0
        while IFS='|' read -r d_name d_check d_install; do
            [ -n "$d_name" ] && [ -n "$d_check" ] || continue
            if ! eval "$d_check" >/dev/null 2>&1; then
                warn "dep: $d_name" "missing" "$d_install"
                auto_line "MISSING_SKILL_DEP: $d_name — $d_install"
                _dep_missing=$((_dep_missing + 1))
            fi
        done < <(echo "$DEPS_JSON" | jq -r --arg ik "$_INSTALL_KEY" \
            '.[]? | [(.name // ""), (.check_cmd // ""), (.[$ik] // "")] | join("|")' 2>/dev/null)
        [ "$_dep_missing" -eq 0 ] && pass "skill deps" "all present"
    fi
fi

# ============================================================================
# SUMMARY
# ============================================================================
if [ "$JSON_MODE" = true ]; then
    echo "${JSON_RESULTS}]"
    [ "$TOTAL_FAIL" -eq 0 ]
    exit $?
fi

# --auto: silence is the healthy signal. Print findings only.
if [ "$AUTO_MODE" = true ]; then
    if [ "$TOTAL_FAIL" -gt 0 ] || [ "$TOTAL_WARN" -gt 0 ]; then
        printf '%s' "$AUTO_LINES"
        echo "SUMMARY: $TOTAL_PASS pass, $TOTAL_FAIL fail, $TOTAL_WARN warn, $TOTAL_INTENTIONAL intentional"
    fi
    [ "$TOTAL_FAIL" -eq 0 ]
    exit $?
fi

echo ""
echo -e "${BOLD}━━ Summary ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
TOTAL=$((TOTAL_PASS + TOTAL_FAIL + TOTAL_WARN))
INTENTIONAL_NOTE=""
[ "$TOTAL_INTENTIONAL" -gt 0 ] && INTENTIONAL_NOTE="  ${CYAN}$TOTAL_INTENTIONAL intentional${RESET} ${DIM}(per $VERIFY_IGNORE_FILE)${RESET}"

if [ "$TOTAL_FAIL" -eq 0 ] && [ "$TOTAL_WARN" -eq 0 ]; then
    echo -e "  ${GREEN}${BOLD}All $TOTAL checks passed.${RESET}${INTENTIONAL_NOTE}"
elif [ "$TOTAL_FAIL" -eq 0 ]; then
    echo -e "  ${GREEN}$TOTAL_PASS passed${RESET}, ${YELLOW}$TOTAL_WARN warnings${RESET}${INTENTIONAL_NOTE}"
    [ "$SKILLS_ORPHANED" -gt 0 ] && echo -e "  ${YELLOW}$SKILLS_ORPHANED orphan skill(s) — run --fix to prune.${RESET}"
else
    echo -e "  ${GREEN}$TOTAL_PASS passed${RESET}, ${RED}$TOTAL_FAIL failed${RESET}, ${YELLOW}$TOTAL_WARN warnings${RESET}${INTENTIONAL_NOTE}"
    if [ "$FIX_MODE" = true ]; then
        echo -e "  ${GREEN}$FIXES_APPLIED fixes applied.${RESET}"
    else
        echo -e "  ${CYAN}bash $VERIFY_SCRIPT --fix${RESET}"
    fi
fi
echo ""

[ "$TOTAL_FAIL" -eq 0 ]
