#!/usr/bin/env bash
# List worktrees for the current repo (or every repo in a workspace), with Herd
# link/cert status, merge status and ahead/behind counts. Read-only — never
# mutates state.

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null && pwd)"
# shellcheck source=./_lib.sh
source "$SCRIPT_DIR/_lib.sh"

FILTER=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --app=*)   FILTER="${1#*=}"; shift ;;
        --fresh)   WT_FORCE_FRESH=1; shift ;;
        -h|--help) echo "Usage: bash list.sh [--app=<repo>] [--fresh]"; exit 0 ;;
        --*)       die "Unknown flag: $1" ;;
        *)         FILTER="$1"; shift ;;
    esac
done

HERD_LINKS=""; HERD_SECURED=""
if herd_available; then
    HERD_LINKS=$("$HERD" links 2>/dev/null || true)
    HERD_SECURED=$("$HERD" secured 2>/dev/null || true)
fi

SEEN_REPO_PATHS=()

list_repo() {
    local app="$1" entries base main_ref
    resolve_repo "$app"
    find_worktree_config
    base=$(resolve_base_branch)

    [[ -d "$TARGET_REPO/.git" || -f "$TARGET_REPO/.git" ]] || return 0

    # Backstop against listing one repo twice — e.g. a child entry that is itself
    # a linked worktree and resolves back onto its primary. Dedupe by resolved
    # absolute path, not by name: a workspace root and a child can legitimately
    # share a basename and are then two different repos with one name.
    local seen
    for seen in "${SEEN_REPO_PATHS[@]:-}"; do
        [[ -n "$seen" && "$seen" == "$TARGET_REPO" ]] && return 0
    done
    SEEN_REPO_PATHS+=("$TARGET_REPO")

    # Path, not just the name: when a workspace root and one of its children
    # share a basename (monorepos/paylog holds child repo paylog), two bare
    # `paylog:` headers are indistinguishable.
    echo "${BOLD}${REPO_NAME}${RESET} ${DIM}(${TARGET_REPO/#$HOME/~})${RESET}:"

    entries=$(git -C "$TARGET_REPO" worktree list --porcelain 2>/dev/null | awk -v parent="$(worktree_parent)" '
        /^worktree / { path = $2 }
        /^branch / {
            branch = $2; sub("refs/heads/", "", branch)
            if (index(path, parent) == 1) print path "\t" branch
        }
    ')

    # Linked worktrees that this skill did NOT create — anything outside
    # <repo>-worktrees/. Claude Code's own `EnterWorktree` isolation puts them
    # under .claude/worktrees/, and hand-made ones can be anywhere. They still
    # pin a branch and still occupy disk, so a listing that silently omits them
    # answers "what worktrees do I have?" wrongly.
    local unmanaged
    unmanaged=$(git -C "$TARGET_REPO" worktree list --porcelain 2>/dev/null | awk -v parent="$(worktree_parent)" -v primary="$TARGET_REPO" '
        /^worktree / { path = $2; is_primary = (path == primary) }
        /^(branch |detached)/ {
            label = ($1 == "detached") ? "(detached)" : $2
            sub("refs/heads/", "", label)
            if (!is_primary && index(path, parent) != 1) print path "\t" label
        }
    ')

    if [[ -z "$entries" && -z "$unmanaged" ]]; then
        echo "  ${DIM}(no worktrees)${RESET}"
        echo
        return 0
    fi

    main_ref="main"
    git -C "$TARGET_REPO" show-ref --quiet "refs/remotes/origin/main" || main_ref="master"

    # Refresh the base refs once so merged= and ahead/behind are accurate.
    # Best-effort — stays usable offline, statuses just degrade to 'unknown'.
    # Only reached when there's actually something to report — a repo with no
    # worktrees returned above without paying any network I/O.
    safe_fetch "$TARGET_REPO" "$main_ref" "$base" || true

    print_unmanaged() {
        [[ -z "$unmanaged" ]] && return 0
        echo "  ${DIM}unmanaged (not created by /worktree):${RESET}"
        while IFS=$'\t' read -r upath ulabel; do
            [[ -z "$upath" ]] && continue
            printf "    %-40s %s\n" "${upath/#$WORKSPACE_DIR\//}" "$ulabel"
        done <<< "$unmanaged"
    }

    if [[ -z "$entries" ]]; then
        print_unmanaged
        echo
        return 0
    fi

    while IFS=$'\t' read -r path branch; do
        local name linked secured merged sm sh ab ahead behind ref_used
        name=$(basename "$path")
        linked="—"; secured="—"
        if herd_available; then
            echo "$HERD_LINKS"   | grep -qE "^\| +${name} +\|"       && linked="✓"
            echo "$HERD_SECURED" | grep -qE "^\| +${name}\.test +\|" && secured="✓"
        fi

        # merged= : is this branch's work already in production/staging?
        sm=$(branch_merge_status "$TARGET_REPO" "$branch" "origin/$main_ref"); sm="${sm%%$'\t'*}"
        sh=$(branch_merge_status "$TARGET_REPO" "$branch" "origin/$base");     sh="${sh%%$'\t'*}"
        if   [[ "$sm" == merged ]];    then merged="$main_ref";    ref_used="$main_ref"
        elif [[ "$sh" == merged ]];    then merged="$base";        ref_used="$base"
        elif [[ "$sh" == plan-only ]]; then merged="$base+plan⚠";  ref_used="$base"
        elif [[ "$sm" == plan-only ]]; then merged="$main_ref+plan⚠"; ref_used="$main_ref"
        else                                merged="no";           ref_used="$base"
        fi

        # Ahead/behind vs whichever ref the merge status keyed on.
        ab=$(ahead_behind "$TARGET_REPO" "$branch" "origin/$ref_used")
        ahead=$(echo "$ab" | awk '{print $1}')
        behind=$(echo "$ab" | awk '{print $2}')

        printf "  %-22s %-34s  link=%s cert=%s  merged=%-14s ↑%-4s ↓%-4s vs origin/%s\n" \
            "$name" "$branch" "$linked" "$secured" "$merged" "$ahead" "$behind" "$ref_used"
    done <<< "$entries"

    print_unmanaged
    echo
}

# No filter: list the current repo plus any child repos. A workspace root can
# itself be a git repo AND hold children, so both are covered.
if [[ -n "$FILTER" ]]; then
    list_repo "$FILTER"
    exit 0
fi

if git rev-parse --show-toplevel >/dev/null 2>&1; then
    list_repo ""
    root="$TARGET_REPO"
else
    root="$PWD"
fi

children=$(discover_child_repos "$root")
if [[ -z "$children" ]] && [[ "$root" == "$PWD" ]] && ! git rev-parse --show-toplevel >/dev/null 2>&1; then
    die "Not inside a git repo, and no git-repo children found in $PWD."
fi

while IFS= read -r child; do
    [[ -z "$child" ]] && continue
    list_repo "$child"
done <<< "$children"
