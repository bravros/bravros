#!/usr/bin/env bash
# Destroy a worktree — full reverse of create.sh.
#
# Order matters: Herd unlink runs FIRST so a later failure (e.g. a dirty tree
# blocking `git worktree remove`) never leaves a dangling link behind. All
# teardown steps are best-effort; errors are collected and reported at the end.

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null && pwd)"
# shellcheck source=./_lib.sh
source "$SCRIPT_DIR/_lib.sh"

usage() {
    cat <<'EOF'
Usage: bash destroy.sh <name> [--dry-run] [--force] [--yes] [--merged-into=<ref>]

<name>              worktree name (e.g. paylog109, mysiteloginfix)
--dry-run           print the teardown scope and exit
--force             skip the merge safety check
--yes               skip the interactive confirmation prompt
--merged-into=REF   require the branch to be merged into REF specifically
                    (e.g. --merged-into=main for "only if it shipped to prod").
                    Default: merged into EITHER the base branch or main.
EOF
    exit 1
}

NAME="" DRY_RUN=0 FORCE=0 YES=0 REQUIRE_REF=""

while [[ $# -gt 0 ]]; do
    case "$1" in
        --dry-run)       DRY_RUN=1; shift ;;
        --force)         FORCE=1;   shift ;;
        --yes)           YES=1;     shift ;;
        --merged-into=*) REQUIRE_REF="${1#*=}"; shift ;;
        -h|--help)       usage ;;
        --*)             die "Unknown flag: $1" ;;
        *)
            if [[ -z "$NAME" ]]; then NAME="$1"; else die "Unexpected positional: $1"; fi
            shift ;;
    esac
done

[[ -z "$NAME" ]] && usage

# ─── Locate the worktree ──────────────────────────────────────────────────────
locate_worktree_repo "$NAME"
find_worktree_config

N="${NAME#"$REPO_NAME"}"
[[ -n "$N" ]] || die "Could not parse an id out of worktree name '$NAME'."
# Same gate create.sh applies to the id it is given. The extracted value feeds
# worktree_path, worktree_domain, the Herd link name and the backtick-quoted
# DROP DATABASE below, so a typo should fail here rather than build a malformed
# path or domain and half-succeed.
validate_n "$N"

WT_PATH=$(worktree_path "$N")
DOMAIN=$(worktree_domain "$N")
PREFIX="${NAME}_"
BASE=$(resolve_base_branch)

_tpl=$(cfg_map_value db clone_name); [[ -z "$_tpl" ]] && _tpl="{repo}_wt{n}"
CLONE_DB_NAME=$(expand_tpl "$_tpl" "$NAME" "$N")

# ─── Gather scope ─────────────────────────────────────────────────────────────
WORKTREE_EXISTS=0; BRANCH=""; HERD_LINKED=0; HERD_SECURED=0
DB_EXISTS=0; REDIS_KEY_COUNT=0

[[ -d "$WT_PATH" ]] && WORKTREE_EXISTS=1

if (( WORKTREE_EXISTS )); then
    BRANCH=$(git -C "$WT_PATH" rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
elif git -C "$TARGET_REPO" show-ref --quiet "refs/heads/$NAME"; then
    BRANCH="$NAME"
fi

# Base override from plan frontmatter when the id is numeric.
plan_file=$(find_plan_file "$N" || true)
if [[ -n "$plan_file" ]]; then
    pb=$(read_frontmatter "$plan_file" base)
    [[ -n "$pb" && "$pb" != "null" ]] && BASE="$pb"
fi

herd_link_exists "$NAME"      && HERD_LINKED=1
herd_secured_exists "$DOMAIN" && HERD_SECURED=1

if [[ -f "$TARGET_REPO/.env" ]] && [[ "$(db_driver "$TARGET_REPO/.env")" =~ ^(mysql|mariadb)$ ]]; then
    mysql_db_exists "$TARGET_REPO/.env" "$CLONE_DB_NAME" 2>/dev/null && DB_EXISTS=1
    args=$(redis_args "$TARGET_REPO/.env")
    if command -v redis-cli >/dev/null 2>&1; then
        # shellcheck disable=SC2086
        REDIS_KEY_COUNT=$(redis-cli $args --no-auth-warning --scan --pattern "${PREFIX}*" 2>/dev/null | wc -l | tr -d ' ')
    fi
fi

# ─── Merge status vs BOTH base and main ───────────────────────────────────────
# Operators think in terms of "did it ship to prod?", so main is always reported
# even when the base branch is homolog.
STATUS_BASE="unknown"; PATHS_BASE=""
STATUS_MAIN="unknown"; PATHS_MAIN=""
MAIN_REF="main"
git -C "$TARGET_REPO" show-ref --quiet "refs/remotes/origin/main" || MAIN_REF="master"

if [[ -n "$BRANCH" ]]; then
    git -C "$TARGET_REPO" fetch origin "$BASE" "$MAIN_REF" --quiet 2>/dev/null || true
    ms=$(branch_merge_status "$TARGET_REPO" "$BRANCH" "origin/$BASE")
    STATUS_BASE="${ms%%$'\t'*}"; [[ "$ms" == *$'\t'* ]] && PATHS_BASE="${ms#*$'\t'}"
    if [[ "$MAIN_REF" != "$BASE" ]]; then
        ms=$(branch_merge_status "$TARGET_REPO" "$BRANCH" "origin/$MAIN_REF")
        STATUS_MAIN="${ms%%$'\t'*}"; [[ "$ms" == *$'\t'* ]] && PATHS_MAIN="${ms#*$'\t'}"
    else
        STATUS_MAIN="$STATUS_BASE"; PATHS_MAIN="$PATHS_BASE"
    fi
fi

# The effective gate. Default: merged into EITHER ref — the code is preserved in
# both cases. --merged-into=<ref> narrows it to one.
GATE_OK=0; GATE_DESC=""
if [[ -n "$REQUIRE_REF" ]]; then
    case "$REQUIRE_REF" in
        "$MAIN_REF") [[ "$STATUS_MAIN" == "merged" ]] && GATE_OK=1 ;;
        "$BASE")     [[ "$STATUS_BASE" == "merged" ]] && GATE_OK=1 ;;
        *)
            git -C "$TARGET_REPO" fetch origin "$REQUIRE_REF" --quiet 2>/dev/null || true
            ms=$(branch_merge_status "$TARGET_REPO" "$BRANCH" "origin/$REQUIRE_REF")
            [[ "${ms%%$'\t'*}" == "merged" ]] && GATE_OK=1 ;;
    esac
    GATE_DESC="merged into origin/$REQUIRE_REF (required explicitly)"
else
    [[ "$STATUS_BASE" == "merged" || "$STATUS_MAIN" == "merged" ]] && GATE_OK=1
    GATE_DESC="merged into origin/$BASE or origin/$MAIN_REF"
fi

status_label() {
    case "$1" in
        merged)    echo "${GREEN}merged ✓${RESET}" ;;
        plan-only) echo "${YELLOW}code merged, planning delta dangling${RESET}" ;;
        unmerged)  echo "${RED}NOT merged${RESET}" ;;
        *)         echo "${DIM}unknown${RESET}" ;;
    esac
}

# ─── Print scope ──────────────────────────────────────────────────────────────
echo "${BOLD}Teardown scope for ${NAME}:${RESET}"
echo
printf "  %-22s %s\n" "Repo:"      "$TARGET_REPO"
printf "  %-22s %s\n" "Worktree dir:" "$([ $WORKTREE_EXISTS == 1 ] && echo "$WT_PATH" || echo "${DIM}(not present)${RESET}")"
printf "  %-22s %s\n" "Branch:"    "$([ -n "$BRANCH" ] && echo "$BRANCH" || echo "${DIM}(unknown)${RESET}")"
if [[ -n "$BRANCH" ]]; then
    printf "    %-20s %s\n" "vs origin/$MAIN_REF" "$(status_label "$STATUS_MAIN")$([ "$STATUS_MAIN" == merged ] && echo "   (shipped to prod)")"
    # Only print the base line when it is a genuinely different ref — in a repo
    # whose base IS main the two rows would be identical.
    [[ "$BASE" != "$MAIN_REF" ]] \
        && printf "    %-20s %s\n" "vs origin/$BASE" "$(status_label "$STATUS_BASE")"
fi
printf "  %-22s %s\n" "Herd link:" "$([ $HERD_LINKED == 1 ] && echo "$NAME" || echo "${DIM}(not linked)${RESET}")"
printf "  %-22s %s\n" "Herd cert:" "$([ $HERD_SECURED == 1 ] && echo "$DOMAIN" || echo "${DIM}(not secured)${RESET}")"
printf "  %-22s %s\n" "Cloned DB:" "$([ $DB_EXISTS == 1 ] && echo "$CLONE_DB_NAME" || echo "${DIM}(no clone — parent DB untouched)${RESET}")"
printf "  %-22s %s\n" "Redis keys (prefix):" "$REDIS_KEY_COUNT key(s) under ${PREFIX}*"
echo

print_merge_caveat() {
    if [[ "$STATUS_BASE" == "plan-only" || "$STATUS_MAIN" == "plan-only" ]]; then
        local p="${PATHS_BASE:-$PATHS_MAIN}"
        echo "${YELLOW}⚠ The CODE is merged, but planning/backlog housekeeping on '$BRANCH'"
        echo "  has not been landed on the base:${RESET}"
        echo "    ${p//,/$'\n    '}"
        echo "  Land it on $BASE from the PARENT checkout (not this worktree), then re-run destroy:"
        echo "    (cd $TARGET_REPO && git checkout $BASE && …apply the .planning change… \\"
        echo "        && bravros commit \"📋 plan: …\" <files> && git push origin $BASE)"
        echo "  Or pass --force to discard the housekeeping (the code is already safe)."
    elif [[ "$STATUS_BASE" == "unmerged" && "$STATUS_MAIN" == "unmerged" ]]; then
        if [[ "$BASE" == "$MAIN_REF" ]]; then
            echo "${YELLOW}⚠ Branch '$BRANCH' has UNMERGED code vs origin/$MAIN_REF:${RESET}"
        else
            echo "${YELLOW}⚠ Branch '$BRANCH' has UNMERGED code vs both origin/$BASE and origin/$MAIN_REF:${RESET}"
        fi
        echo "    ${PATHS_BASE//,/$'\n    '}"
        echo "  Merge it first (PR → merge), or pass --force to discard this work."
    elif [[ -n "$REQUIRE_REF" ]]; then
        echo "${YELLOW}⚠ Branch '$BRANCH' is not merged into origin/$REQUIRE_REF."
        echo "  You asked for teardown only once it landed there.${RESET}"
    else
        echo "${YELLOW}⚠ Could not determine merge status of '$BRANCH'"
        echo "  (offline or missing ref). Pass --force to destroy anyway.${RESET}"
    fi
}

if (( DRY_RUN )); then
    if (( GATE_OK )); then
        echo "${GREEN}→ safe to destroy${RESET} ($GATE_DESC)"
    else
        print_merge_caveat
        echo "  Default destroy will REFUSE. Pass --force to override."
    fi
    exit 0
fi

# ─── Safety gate ──────────────────────────────────────────────────────────────
if (( GATE_OK == 0 )) && (( FORCE == 0 )); then
    fail "Refusing destroy — see below."
    echo
    print_merge_caveat
    exit 3
fi

# ─── Confirm ──────────────────────────────────────────────────────────────────
if (( YES == 0 )); then
    read -r -p "Proceed with teardown? [y/N] " ans
    [[ "$ans" =~ ^[Yy]$ ]] || { echo "Aborted."; exit 1; }
fi

# ─── Teardown (best-effort) ───────────────────────────────────────────────────
ERRORS=()

step() {
    local label="$1"; shift
    info "${label}…"
    if "$@" >/dev/null 2>&1; then ok "$label"
    else warn "$label failed (continuing)"; ERRORS+=("$label"); fi
}

# 1. Herd unlink FIRST — no dangling link if a later step fails.
if (( HERD_LINKED )); then
    if (( WORKTREE_EXISTS )); then
        step "herd unlink $NAME" bash -c "cd '$WT_PATH' && '$HERD' unlink"
    else
        step "herd unlink $NAME" "$HERD" unlink "$NAME"
    fi
fi

# 2. Herd unsecure
(( HERD_SECURED )) && step "herd unsecure $DOMAIN" "$HERD" unsecure "$DOMAIN"

# 3. git worktree remove
if (( WORKTREE_EXISTS )); then
    if git -C "$TARGET_REPO" worktree remove "$WT_PATH" >/dev/null 2>&1; then
        ok "git worktree remove"
    else
        warn "clean remove failed — retrying with --force"
        step "git worktree remove --force" git -C "$TARGET_REPO" worktree remove --force "$WT_PATH"
    fi
fi

# 4. Drop the cloned DB — only ever the clone, never the parent's.
if (( DB_EXISTS )); then
    load_mysql_pwd "$TARGET_REPO/.env"
    args=$(mysql_args "$TARGET_REPO/.env")
    # shellcheck disable=SC2086
    step "drop database $CLONE_DB_NAME" mysql $args -e "DROP DATABASE IF EXISTS \`$CLONE_DB_NAME\`"
fi

# 5. Redis flush
if (( REDIS_KEY_COUNT > 0 )); then
    flushed=$(redis_flush_prefix "$TARGET_REPO/.env" "$PREFIX")
    ok "redis flush: $flushed key(s) removed"
fi

# 6. Delete the local branch when its CODE is already in a base — nothing is
# lost. That is `merged`, or `plan-only` once the operator forced past the
# housekeeping caveat. Use -D not -d: the gate above already verified the code
# is in origin, and `branch -d`'s extra local-HEAD check false-positives when the
# local base is behind origin (common after merging via the GitHub UI).
if [[ -n "$BRANCH" ]] && { (( GATE_OK )) || [[ "$STATUS_BASE" == "plan-only" || "$STATUS_MAIN" == "plan-only" ]]; }; then
    if git -C "$TARGET_REPO" show-ref --quiet "refs/heads/$BRANCH"; then
        step "delete local branch $BRANCH" git -C "$TARGET_REPO" branch -D "$BRANCH"
    fi
fi

echo
if (( ${#ERRORS[@]} == 0 )); then
    echo "${GREEN}${BOLD}Teardown complete.${RESET}"
else
    echo "${YELLOW}${BOLD}Teardown completed with errors:${RESET}"
    for e in "${ERRORS[@]}"; do echo "  - $e"; done
    exit 4
fi
