#!/usr/bin/env bash
# Create a worktree for the current (or named) repo.
#
# Laravel repos get the full treatment: Herd link + TLS, isolated .env
# (APP_URL / REDIS_PREFIX), optional MySQL clone, storage scaffolding, smoke
# tests. Non-Laravel repos get the git worktree + runtime-dir clone and the
# PHP-specific steps are skipped with an explicit note.
#
# On any step failure after the worktree is created this script DOES NOT roll
# back — partial state is left in place with a resume hint so the operator can
# fix the underlying issue and continue manually.

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null && pwd)"
# shellcheck source=./_lib.sh
source "$SCRIPT_DIR/_lib.sh"

usage() {
    cat <<'EOF'
Usage: bash create.sh [<app>] [<id>] [--branch=<name>] [--clone-db] [--live-dump] [--shared-db]

<app>          child repo name — only needed at a multi-repo workspace root
<id>           plan id or slug — random if omitted. Accepts any of:
                 P-0109, P-109, 0109, 109   (all normalize to 109)
                 ui, spike, foo42           (slugs kept as-is)
--branch=NAME  override branch resolution
--clone-db     clone the parent DB for full schema isolation (MySQL only)
--live-dump    with --clone-db: force a live mysqldump of the current parent DB,
               ignoring any local backup (use for the freshest state)
--shared-db    explicit no-op — shared DB is already the default. Accepted so
               "create <name> --shared-db" reads the way operators say it.
--fresh        bypass the fetch TTL cache — force a live fetch of base/branch refs
EOF
    exit 1
}

# ─── Parse args ───────────────────────────────────────────────────────────────
POSITIONALS=() BRANCH_OVERRIDE="" CLONE_DB=0 LIVE_DUMP=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --branch=*)  BRANCH_OVERRIDE="${1#*=}"; shift ;;
        --clone-db)  CLONE_DB=1; shift ;;
        --live-dump) LIVE_DUMP=1; shift ;;
        --shared-db) shift ;;   # explicit affirmation of the default
        --fresh)     WT_FORCE_FRESH=1; shift ;;
        -h|--help)   usage ;;
        --*)         die "Unknown flag: $1" ;;
        *)           POSITIONALS+=("$1"); shift ;;
    esac
done

# Decide which positional is the app and which is the id. Inside a git repo the
# app is implicit unless the operator named it explicitly.
APP="" N=""
if git rev-parse --show-toplevel >/dev/null 2>&1; then
    _top=$(git rev-parse --show-toplevel)
    _primary=$(primary_worktree_of "$_top")
    _here=$(basename "$_primary")
    _parent=$(dirname "$_primary")
    if [[ ${#POSITIONALS[@]} -gt 0 ]]; then
        _first="${POSITIONALS[0]}"
        # A workspace root can itself be a git repo AND hold child repos
        # (paylog-workspace does), so accept a CHILD as well as a sibling —
        # matching what resolve_repo() already handles downstream.
        if [[ "$_first" == "$_here" ]] \
           || [[ -d "$_primary/$_first/.git" || -f "$_primary/$_first/.git" ]] \
           || [[ -d "$_parent/$_first/.git"  || -f "$_parent/$_first/.git" ]]; then
            APP="$_first"
            N="${POSITIONALS[1]:-}"
        else
            N="$_first"
        fi
    fi
else
    APP="${POSITIONALS[0]:-}"
    N="${POSITIONALS[1]:-}"
fi

resolve_repo "$APP"
find_worktree_config

# One snapshot of `herd links` covers both generate_unused_n()'s retry loop
# (up to 25 herd_link_exists() checks) and the preflight check below — a
# create.sh run issues exactly ONE `herd links` invocation.
cache_herd_links
[[ -z "$N" ]] && N=$(generate_unused_n)
N=$(normalize_id "$N")
validate_n "$N"

NAME=$(worktree_name "$N")
WT_PATH=$(worktree_path "$N")
WT_PARENT=$(worktree_parent)
URL=$(worktree_url "$N")
PREFIX="${NAME}_"
BASE=$(resolve_base_branch)

LARAVEL=0; is_laravel && LARAVEL=1
HERD_OK=0; herd_available && HERD_OK=1

CLONE_DB_NAME=""
if (( CLONE_DB )); then
    _tpl=$(cfg_map_value db clone_name)
    [[ -z "$_tpl" ]] && _tpl="{repo}_wt{n}"
    CLONE_DB_NAME=$(expand_tpl "$_tpl" "$NAME" "$N")
fi

# ─── Pre-flight ───────────────────────────────────────────────────────────────
info "Pre-flight checks…"
require_cmd git
# composer/npm are NOT required: runtime dirs are APFS-cloned from the parent,
# not installed. Only needed if a parent runtime dir is missing (we warn + skip).

[[ -d "$TARGET_REPO/.git" || -f "$TARGET_REPO/.git" ]] \
    || die "$TARGET_REPO is not a git repo."

if (( CLONE_DB )); then
    (( LARAVEL )) || die "--clone-db needs a Laravel repo (no .env/DB config detected)."
    driver=$(db_driver "$TARGET_REPO/.env")
    [[ "$driver" == "mysql" || "$driver" == "mariadb" ]] \
        || die "--clone-db supports MySQL only (this repo uses DB_CONNECTION=$driver). Re-run without --clone-db to share the parent DB."
    require_cmd mysql
    require_cmd mysqldump
fi

mkdir -p "$WT_PARENT"
[[ ! -e "$WT_PATH" ]] || die "Worktree path already exists: $WT_PATH"

if herd_link_exists "$NAME"; then
    die "Herd link '$NAME' already exists. Choose a different id or destroy the existing worktree first."
fi

if git -C "$TARGET_REPO" worktree list --porcelain | grep -qE "^worktree .*/${NAME}\$"; then
    die "git worktree '${NAME}' already registered in $TARGET_REPO."
fi

# The new branch is created from origin/$BASE (see `worktree add` below), so the parent's
# checked-out branch does NOT determine the worktree's code. The only thing the parent supplies is
# the runtime-dir clone (vendor/node_modules/...), which is valid as long as the lockfiles match.
# Blocking on the parent's branch used to force the operator to interrupt whatever they were doing
# in the parent checkout — including an in-flight feature session. Warn instead, and let the
# lockfile check below decide whether the runtime clone is actually safe.
current_branch=$(git -C "$TARGET_REPO" rev-parse --abbrev-ref HEAD)
PARENT_OFF_BASE=0
if [[ "$current_branch" != "$BASE" ]]; then
    PARENT_OFF_BASE=1
    warn "parent is on '$current_branch', not $BASE — branching from origin/$BASE; parent checkout left untouched."
fi

[[ -z "$(git -C "$TARGET_REPO" status --porcelain)" ]] \
    || die "$TARGET_REPO has uncommitted changes. Commit or stash them first."

ok "Pre-flight passed ($REPO_NAME, base=$BASE, laravel=$( ((LARAVEL)) && echo yes || echo no ), herd=$( ((HERD_OK)) && echo yes || echo no ))"
[[ -n "$WT_CONFIG_FILE" ]] && info "config: $WT_CONFIG_FILE"

# ─── Resolve branch ───────────────────────────────────────────────────────────
info "Resolving branch…"

BRANCH=""
if [[ -n "$BRANCH_OVERRIDE" ]]; then
    BRANCH="$BRANCH_OVERRIDE"
    info "Using --branch override: $BRANCH"
else
    PLAN_FILE=$(find_plan_file "$N" || true)
    if [[ -n "$PLAN_FILE" ]]; then
        plan_branch=$(read_frontmatter "$PLAN_FILE" branch)
        plan_base=$(read_frontmatter "$PLAN_FILE" base)
        if [[ -n "$plan_branch" && "$plan_branch" != "null" ]]; then
            BRANCH="$plan_branch"
            [[ -n "$plan_base" && "$plan_base" != "null" ]] && BASE="$plan_base"
            info "Plan $(basename "$PLAN_FILE") → branch=$BRANCH base=$BASE"
        fi
    fi
    if [[ -z "$BRANCH" ]]; then
        BRANCH="$NAME"
        info "No plan branch found → new branch $BRANCH off origin/$BASE"
    fi
fi

# BASE is the ref that actually matters — the worktree is created from
# origin/$BASE, so it is always fetched on its own. Never combined with
# BRANCH: git fails an ENTIRE multi-ref fetch if any one refspec is
# unresolvable, and the common case (no plan branch found) sets BRANCH="$NAME"
# — a brand-new id-derived name that by definition does not exist on origin
# yet. Combining them would fail BASE's refresh on every ordinary create.
safe_fetch "$TARGET_REPO" "$BASE" || true

# BRANCH gets its own single-ref fetch whenever it differs from BASE — NOT gated
# on a local ref existing. A plan-derived or --branch= override branch is exactly
# the case where the branch was pushed from another worktree/clone and has no
# local ref here yet; skipping the fetch would leave refs/remotes/origin/$BRANCH
# missing, so the `worktree add` below falls through to `-b "$BRANCH" ...
# origin/$BASE` and silently creates a NEW diverged branch of the same name,
# discarding the work already on origin. Being a single-ref call, it is immune to
# the "one bad ref poisons the batch" failure: a branch that genuinely does not
# exist on origin just warns and returns 1, and branching off BASE is correct then.
if [[ "$BRANCH" != "$BASE" ]]; then
    safe_fetch "$TARGET_REPO" "$BRANCH" || true
fi

# ─── Create worktree ──────────────────────────────────────────────────────────
info "Creating worktree at $WT_PATH (branch=$BRANCH)…"
if git -C "$TARGET_REPO" show-ref --quiet "refs/heads/$BRANCH"; then
    git -C "$TARGET_REPO" worktree add "$WT_PATH" "$BRANCH"
elif git -C "$TARGET_REPO" show-ref --quiet "refs/remotes/origin/$BRANCH"; then
    git -C "$TARGET_REPO" worktree add --track -b "$BRANCH" "$WT_PATH" "origin/$BRANCH"
else
    git -C "$TARGET_REPO" worktree add -b "$BRANCH" "$WT_PATH" "origin/$BASE"
fi
ok "Worktree created"

# ─── Copy + mutate .env ───────────────────────────────────────────────────────
if (( LARAVEL )) && [[ -f "$TARGET_REPO/.env" ]]; then
    info "Configuring .env…"
    cp "$TARGET_REPO/.env" "$WT_PATH/.env"

    # Config-driven isolation when .worktree.yml declares env_isolate; otherwise
    # the framework defaults (APP_URL + REDIS_PREFIX).
    isolate_keys=$(cfg_map_keys env_isolate)
    if [[ -n "$isolate_keys" ]]; then
        while IFS= read -r key; do
            [[ -z "$key" ]] && continue
            raw=$(cfg_map_value env_isolate "$key")
            set_env_var "$WT_PATH/.env" "$key" "$(expand_tpl "$raw" "$NAME" "$N")"
        done <<< "$isolate_keys"
        ok ".env isolated: $(echo "$isolate_keys" | paste -sd, -)"
    else
        set_env_var "$WT_PATH/.env" APP_URL "$URL"
        set_env_var "$WT_PATH/.env" REDIS_PREFIX "$PREFIX"
        ok ".env: APP_URL=$URL  REDIS_PREFIX=$PREFIX"
    fi

    (( CLONE_DB )) && set_env_var "$WT_PATH/.env" DB_DATABASE "$CLONE_DB_NAME"
elif (( LARAVEL )); then
    warn "no parent .env found — skipping env isolation"
else
    info "non-Laravel repo — skipping .env / Herd / DB steps"
fi

# ─── Clone DB (optional, MySQL only) ──────────────────────────────────────────
if (( CLONE_DB )); then
    parent_db=$(read_env_var "$TARGET_REPO/.env" DB_DATABASE)
    load_mysql_pwd "$TARGET_REPO/.env"
    args=$(mysql_args "$TARGET_REPO/.env")
    # shellcheck disable=SC2086
    mysql $args -e "CREATE DATABASE IF NOT EXISTS \`$CLONE_DB_NAME\` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"

    # Prefer the newest local dump: gunzip|mysql of a compressed snapshot is far
    # faster than re-dumping a large DB live over the network. --live-dump forces
    # the live path when the current state matters.
    latest_dump=""
    if (( LIVE_DUMP == 0 )); then
        dump_glob=$(cfg_map_value db dump_glob)
        [[ -z "$dump_glob" ]] && dump_glob="database/backups/*.sql.gz"
        # shellcheck disable=SC2086
        latest_dump=$(ls -t $TARGET_REPO/$dump_glob 2>/dev/null | head -1 || true)
        # Validate before seeding — a corrupt/truncated dump must never silently
        # half-fill the clone. On failure fall through to the live-dump path.
        if [[ -n "$latest_dump" ]] && ! dump_is_valid "$latest_dump"; then
            warn "Latest local dump is unusable — ignoring it and live-dumping the current DB instead."
            latest_dump=""
        fi
    fi

    if [[ -n "$latest_dump" ]]; then
        info "Seeding clone from latest local dump: $(basename "$latest_dump") (faster than a live dump)…"
        # shellcheck disable=SC2086
        gunzip < "$latest_dump" | mysql $args "$CLONE_DB_NAME"
        ok "DB cloned from dump: $(basename "$latest_dump") → $CLONE_DB_NAME"
    else
        (( LIVE_DUMP )) \
            && info "Live-dumping $parent_db (--live-dump — may take several minutes)…" \
            || info "No local dump found — live-dumping $parent_db (may take several minutes)…"
        # shellcheck disable=SC2086
        mysqldump $args --single-transaction --quick --routines --triggers --no-tablespaces "$parent_db" \
            | mysql $args "$CLONE_DB_NAME"
        ok "DB cloned (live): $parent_db → $CLONE_DB_NAME"
    fi
fi

# ─── Storage scaffolding (Laravel) ────────────────────────────────────────────
# Each worktree gets its own storage/ tree — never a symlink. Symlinking
# storage/app/public to the parent blows away the tracked .gitignore inside it
# and leaves every worktree permanently dirty.
if (( LARAVEL )); then
    info "Setting up storage/…"
    mkdir -p "$WT_PATH/storage/framework/cache/data" \
             "$WT_PATH/storage/framework/sessions" \
             "$WT_PATH/storage/framework/views" \
             "$WT_PATH/storage/framework/testing" \
             "$WT_PATH/storage/logs" 2>/dev/null || true
    chmod -R 775 "$WT_PATH/storage" 2>/dev/null || true
    ok "storage/ ready (fresh tree, no symlink)"
fi

# ─── MCP SITE_PATH rewrite ────────────────────────────────────────────────────
# A repo whose .mcp.json pins a Herd SITE_PATH must point at THIS worktree, or a
# Claude session started inside it reads the parent's Herd data. Auto-detected;
# set mcp_site_path_rewrite: false in .worktree.yml to opt out.
mcp_opt=$(cfg_scalar mcp_site_path_rewrite)
if [[ "$mcp_opt" != "false" ]] && [[ -f "$WT_PATH/.mcp.json" ]] && grep -q "SITE_PATH" "$WT_PATH/.mcp.json" 2>/dev/null; then
    info "Rewriting .mcp.json SITE_PATH…"
    if command -v jq >/dev/null 2>&1; then
        jq --arg path "$WT_PATH" '(.mcpServers[]?.env?.SITE_PATH) |= (if . then $path else . end)' \
            "$WT_PATH/.mcp.json" > "$WT_PATH/.mcp.json.tmp" \
            && mv "$WT_PATH/.mcp.json.tmp" "$WT_PATH/.mcp.json"
        ok ".mcp.json SITE_PATH → $WT_PATH"
    else
        warn "jq not installed — .mcp.json SITE_PATH still points at the parent."
    fi
fi

# ─── Clone runtime dirs (APFS copy-on-write — instant, dedup'd) ───────────────
# The worktree branches off the parent at the same commit, so gitignored runtime
# dirs are byte-identical. `cp -cR` uses APFS clonefile(): near-instant, no extra
# disk, no Packagist/npm round-trip. Falls back to a plain copy on non-APFS.
resume_hint() {
    cat >&2 <<EOF

Worktree is partially set up at $WT_PATH. Resume manually with:

  cd "$WT_PATH"
  for d in $(echo "$RUNTIME_DIRS" | paste -sd' ' -); do cp -cR "$TARGET_REPO/\$d" "\$d"; done
  rm -f bootstrap/cache/*.php
$( ((HERD_OK)) && echo "  \"$HERD\" link \"$NAME\" --secure" )
EOF
}

RUNTIME_DIRS=$(cfg_list runtime_dirs)
[[ -z "$RUNTIME_DIRS" ]] && RUNTIME_DIRS=$(default_runtime_dirs)

# A parent sitting on another branch may have different dependencies installed. Cloning those into
# a worktree based on origin/$BASE would ship the wrong vendor/ or node_modules/. Check the
# manifests BEFORE copying, not after.
if ((PARENT_OFF_BASE)) && [[ -n "$RUNTIME_DIRS" ]]; then
    _drifted=""
    for _m in composer.lock composer.json package-lock.json package.json; do
        git -C "$TARGET_REPO" cat-file -e "origin/$BASE:$_m" 2>/dev/null || continue
        git -C "$TARGET_REPO" diff --quiet HEAD "origin/$BASE" -- "$_m" 2>/dev/null || _drifted="$_drifted $_m"
    done
    if [[ -n "$_drifted" ]]; then
        warn "parent manifests differ from origin/$BASE:$_drifted"
        warn "skipping the runtime-dir clone — run \`composer install\` / \`npm ci\` inside the worktree."
        RUNTIME_DIRS=""
    else
        ok "parent manifests match origin/$BASE — runtime clone is safe"
    fi
fi

if [[ -n "$RUNTIME_DIRS" ]]; then
    info "Cloning runtime dirs from parent (APFS copy-on-write)…"
    while IFS= read -r d; do
        [[ -z "$d" ]] && continue
        if [[ ! -e "$TARGET_REPO/$d" ]]; then
            warn "skip $d (absent in parent — install manually if needed)"
            continue
        fi
        mkdir -p "$(dirname "$WT_PATH/$d")"
        if cp -cR "$TARGET_REPO/$d" "$WT_PATH/$d" 2>/dev/null; then
            ok "cloned $d"
        else
            warn "APFS clone unavailable for $d — falling back to plain copy"
            if ! cp -R "$TARGET_REPO/$d" "$WT_PATH/$d"; then
                fail "copy of $d failed."
                resume_hint
                exit 2
            fi
            ok "copied $d"
        fi
    done <<< "$RUNTIME_DIRS"

    # The cloned bootstrap/cache holds the parent's compiled config/routes with
    # the parent's APP_URL + REDIS_PREFIX baked in. Drop them so the worktree
    # reads its own .env fresh on first boot.
    if [[ -d "$WT_PATH/bootstrap/cache" ]]; then
        rm -f "$WT_PATH"/bootstrap/cache/*.php 2>/dev/null || true
        ok "cleared stale compiled caches"
    fi
else
    info "no runtime dirs to clone"
fi

# ─── Herd link + secure ───────────────────────────────────────────────────────
if (( LARAVEL )) && (( HERD_OK )); then
    info "Linking to Herd as $NAME with TLS…"
    ( cd "$WT_PATH" && "$HERD" link "$NAME" --secure ) || die "herd link failed"
    ok "Herd link + cert: $URL"

    if [[ -n "$HERD_PHP" ]]; then
        ( cd "$WT_PATH" && "$HERD_PHP" artisan storage:link --force >/dev/null 2>&1 ) \
            || warn "php artisan storage:link skipped (non-critical)"
    fi

    # `herd link` makes Laravel Boost regenerate .agents/, .claude/, boost.json
    # AND rewrite AGENTS.md / CLAUDE.md (it re-orders the detected-package list
    # and strips hand-written sections), leaving a fresh worktree dirty before
    # any real work. Restore the tracked versions and drop the untracked ones so
    # the branch starts byte-clean.
    RESTORE_PATHS=$(cfg_list restore_after_link)
    [[ -z "$RESTORE_PATHS" ]] && RESTORE_PATHS=$'.agents\n.claude\nboost.json\nAGENTS.md\nCLAUDE.md'
    info "Restoring files the Herd link may have regenerated…"
    (
        cd "$WT_PATH"
        # One path at a time: `git checkout --` aborts the WHOLE invocation if
        # any single pathspec is untracked, silently skipping the rest.
        while IFS= read -r p; do
            [[ -z "$p" ]] && continue
            git checkout -- "$p" 2>/dev/null || true
        done <<< "$RESTORE_PATHS"
        git clean -fdq .agents .claude 2>/dev/null || true
    )
    ok "post-link churn reverted"
elif (( LARAVEL )); then
    warn "Herd not found — skipping link/TLS. The worktree has no .test URL."
fi

# ─── Drift check ──────────────────────────────────────────────────────────────
info "Drift check (manifests / lockfiles)…"
drift=0
for f in composer.json package.json composer.lock package-lock.json pnpm-lock.yaml yarn.lock go.mod go.sum; do
    [[ -f "$WT_PATH/$f" ]] || continue
    if ! git -C "$TARGET_REPO" show "HEAD:$f" 2>/dev/null | diff -q - "$WT_PATH/$f" >/dev/null 2>&1; then
        warn "DRIFT: $f differs from $TARGET_REPO HEAD — inspect before committing"
        drift=1
    fi
done
(( drift == 0 )) && ok "No drift in lockfiles or manifests"

# ─── Verify (smoke tests) ─────────────────────────────────────────────────────
# Non-fatal: a failed check warns loudly but leaves the worktree in place.
info "Verifying setup…"
verify_fail=0

# 1) Branch descends from origin/$BASE (in track with the base branch).
if git -C "$WT_PATH" merge-base --is-ancestor "origin/$BASE" HEAD 2>/dev/null; then
    ok "in track with $BASE (HEAD descends from origin/$BASE)"
else
    fail "branch is NOT based on origin/$BASE — check the base"
    verify_fail=1
fi

# 2) Redis prefix differs from the parent. Apps sharing one Redis would
#    otherwise cross-contaminate keys, queues and sessions.
if (( LARAVEL )) && [[ -f "$WT_PATH/.env" ]]; then
    parent_prefix=$(read_env_var "$TARGET_REPO/.env" REDIS_PREFIX)
    wt_prefix=$(read_env_var "$WT_PATH/.env" REDIS_PREFIX)
    if [[ -n "$wt_prefix" && "$wt_prefix" != "$parent_prefix" ]]; then
        ok "redis prefix isolated ($wt_prefix ≠ parent '$parent_prefix')"
    else
        fail "REDIS_PREFIX '$wt_prefix' collides with parent — would share Redis keys"
        verify_fail=1
    fi
fi

# 3) Worktree tree is clean (no stray regenerated files left behind).
if [[ -z "$(git -C "$WT_PATH" status --porcelain)" ]]; then
    ok "worktree tree clean"
else
    warn "worktree has unexpected changes:"
    git -C "$WT_PATH" status --porcelain | sed 's/^/      /' >&2
    verify_fail=1
fi

# 4) Herd serves the secured URL and the app boots.
if (( LARAVEL )) && (( HERD_OK )) && command -v curl >/dev/null 2>&1; then
    [[ -n "$HERD_PHP" ]] && ( cd "$WT_PATH" && "$HERD_PHP" artisan config:clear >/dev/null 2>&1 ) || true
    http=$(curl -sk -o /dev/null -w '%{http_code}' -L "$URL" --max-time 30 2>/dev/null) || http="000"
    if [[ "$http" =~ ^(200|302)$ ]]; then
        ok "herd serves $URL (HTTP $http — app boots & reads its own .env)"
    else
        warn "herd HTTP check returned $http (TLS cert may still be warming up — retry: curl -sk $URL)"
        verify_fail=1
    fi
fi

if (( verify_fail == 0 )); then
    ok "All verification checks passed"
else
    warn "Some verification checks need attention (see above) — worktree left in place"
fi

# ─── Summary ──────────────────────────────────────────────────────────────────
echo
echo "${GREEN}${BOLD}Worktree ready.${RESET}"
echo
printf "  %-10s %s\n" "Path:"   "$WT_PATH"
(( LARAVEL )) && (( HERD_OK )) && printf "  %-10s %s\n" "URL:" "$URL"
printf "  %-10s %s\n" "Branch:" "$BRANCH (off $BASE)"
if (( LARAVEL )) && [[ -f "$WT_PATH/.env" ]]; then
    printf "  %-10s %s\n" "Redis:" "prefix=$PREFIX"
    parent_db=$(read_env_var "$TARGET_REPO/.env" DB_DATABASE)
    if (( CLONE_DB )); then
        printf "  %-10s %s\n" "DB:" "$CLONE_DB_NAME (cloned)"
    else
        printf "  %-10s %s\n" "DB:" "$parent_db (${YELLOW}shared with parent${RESET})"
    fi
fi

if (( LARAVEL )) && (( CLONE_DB == 0 )) && [[ -f "$WT_PATH/.env" ]]; then
    echo
    echo "${YELLOW}${BOLD}⚠ DB is shared with parent.${RESET}"
    echo "  Do NOT run \`php artisan migrate\` inside this worktree — it mutates the parent schema."
    echo "  Re-create with --clone-db if you need migration isolation."
fi

echo
echo "Next: ${BOLD}cd \"$WT_PATH\" && claude${RESET}"
echo
