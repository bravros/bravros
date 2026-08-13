#!/usr/bin/env bash
# Bring a worktree back in sync with its base (or with main/production).
#
# Rebase is the default because it keeps the worktree branch linear on top of
# the base. It is gated on a clean tree — a rebase over uncommitted work is the
# fastest way to lose it. Nothing is ever pushed: a rebase rewrites history, so
# the script prints the exact force-push-with-lease command and stops.

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null && pwd)"
# shellcheck source=./_lib.sh
source "$SCRIPT_DIR/_lib.sh"

usage() {
    cat <<'EOF'
Usage: bash sync.sh <name> [--onto=<ref>] [--merge] [--dry-run]

<name>        worktree name (e.g. paylog109, mysiteloginfix)
--onto=REF    branch to sync onto — main, homolog, … (default: the repo's base)
--merge       merge instead of rebase (keeps history, no force-push needed)
--dry-run     report what would happen and exit
EOF
    exit 1
}

NAME="" ONTO="" USE_MERGE=0 DRY_RUN=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --onto=*)  ONTO="${1#*=}"; shift ;;
        --merge)   USE_MERGE=1; shift ;;
        --dry-run) DRY_RUN=1; shift ;;
        -h|--help) usage ;;
        --*)       die "Unknown flag: $1" ;;
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
validate_n "$N"
WT_PATH=$(worktree_path "$N")
[[ -d "$WT_PATH" ]] || die "Worktree not found: $WT_PATH"

[[ -z "$ONTO" ]] && ONTO=$(resolve_base_branch)
BRANCH=$(git -C "$WT_PATH" rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
[[ -n "$BRANCH" && "$BRANCH" != "HEAD" ]] || die "Worktree $NAME is in a detached HEAD state — resolve that first."

# ─── Pre-flight ───────────────────────────────────────────────────────────────
info "Fetching origin/${ONTO}…"
git -C "$WT_PATH" fetch origin "$ONTO" --quiet || die "Could not fetch origin/$ONTO."
git -C "$WT_PATH" rev-parse --verify --quiet "origin/$ONTO" >/dev/null \
    || die "origin/$ONTO does not exist."

ab=$(ahead_behind "$TARGET_REPO" "$BRANCH" "origin/$ONTO")
AHEAD=$(echo "$ab" | awk '{print $1}')
BEHIND=$(echo "$ab" | awk '{print $2}')

echo
printf "  %-12s %s\n" "Worktree:" "$WT_PATH"
printf "  %-12s %s\n" "Branch:"   "$BRANCH"
printf "  %-12s %s\n" "Onto:"     "origin/$ONTO"
printf "  %-12s %s\n" "Position:" "↑$AHEAD ↓$BEHIND"
printf "  %-12s %s\n" "Strategy:" "$( ((USE_MERGE)) && echo merge || echo rebase )"
echo

if [[ "$BEHIND" == "0" ]]; then
    ok "Already up to date with origin/$ONTO — nothing to do."
    exit 0
fi

DIRTY=$(git -C "$WT_PATH" status --porcelain)
if [[ -n "$DIRTY" ]]; then
    fail "Worktree has uncommitted changes — refusing to $( ((USE_MERGE)) && echo merge || echo rebase )."
    echo "$DIRTY" | sed 's/^/      /' >&2
    echo
    echo "  Commit or stash inside the worktree first:"
    echo "    cd \"$WT_PATH\" && git stash"
    exit 3
fi

if (( DRY_RUN )); then
    echo "${DIM}--dry-run — stopping here.${RESET}"
    echo "  Would run: git -C \"$WT_PATH\" $( ((USE_MERGE)) && echo "merge origin/$ONTO" || echo "rebase origin/$ONTO" )"
    exit 0
fi

# ─── Sync ─────────────────────────────────────────────────────────────────────
if (( USE_MERGE )); then
    info "Merging origin/$ONTO into ${BRANCH}…"
    if git -C "$WT_PATH" merge "origin/$ONTO"; then
        ok "Merged cleanly."
        echo
        echo "Next: ${BOLD}git -C \"$WT_PATH\" push${RESET}"
    else
        fail "Merge hit conflicts — resolve them inside the worktree:"
        echo "    cd \"$WT_PATH\" && git status"
        echo "    …resolve…, then: git merge --continue"
        exit 4
    fi
else
    info "Rebasing $BRANCH onto origin/${ONTO}…"
    if git -C "$WT_PATH" rebase "origin/$ONTO"; then
        ok "Rebased cleanly."
        echo
        echo "${YELLOW}History was rewritten — the remote branch needs a force push.${RESET}"
        echo "Run it yourself once you're happy with the result:"
        echo
        echo "    ${BOLD}git -C \"$WT_PATH\" push --force-with-lease${RESET}"
        echo
    else
        fail "Rebase hit conflicts — resolve them inside the worktree:"
        echo "    cd \"$WT_PATH\" && git status"
        echo "    …resolve…, then: git rebase --continue"
        echo "    or back out entirely:  git -C \"$WT_PATH\" rebase --abort"
        echo
        echo "  (Re-run with --merge if you'd rather not rewrite history.)"
        exit 4
    fi
fi
