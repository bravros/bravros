#!/usr/bin/env bash
# Shared helpers for the /worktree skill scripts.
# Sourced by create.sh / destroy.sh / list.sh / sync.sh — do not execute directly.
#
# Generalized from the paylog-workspace project-local skill (interview 2026-07-28).
# Nothing here is project-specific: repos are discovered at runtime, Herd is located
# rather than hardcoded, and per-project overrides come from an optional .worktree.yml.

# ─── ANSI ─────────────────────────────────────────────────────────────────────
if [[ -t 1 ]]; then
    BOLD=$'\033[1m'; RED=$'\033[31m'; GREEN=$'\033[32m'; YELLOW=$'\033[33m'
    DIM=$'\033[2m'; RESET=$'\033[0m'
else
    BOLD=""; RED=""; GREEN=""; YELLOW=""; DIM=""; RESET=""
fi

info() { echo "${DIM}→${RESET} $*"; }
ok()   { echo "${GREEN}✓${RESET} $*"; }
warn() { echo "${YELLOW}⚠${RESET}  $*" >&2; }
fail() { echo "${RED}✗${RESET} $*" >&2; }
die()  { fail "$*"; exit 1; }

require_cmd() { command -v "$1" >/dev/null 2>&1 || die "Required command '$1' is not installed."; }

# ─── Herd discovery ───────────────────────────────────────────────────────────
# Never hardcode an owner-specific path (audit rule 49). Prefer PATH, then the
# standard macOS install location. HERD stays empty when Herd is unavailable —
# every caller must treat that as "skip the Herd steps", not as an error.
HERD=""
HERD_PHP=""
discover_herd() {
    local candidate
    if command -v herd >/dev/null 2>&1; then
        HERD="$(command -v herd)"
    else
        candidate="$HOME/Library/Application Support/Herd/bin/herd"
        [[ -x "$candidate" ]] && HERD="$candidate"
    fi

    # Use Herd's PHP explicitly for artisan when present — Homebrew/system php
    # often lacks the intl extension that money/number formatting requires.
    candidate="$HOME/Library/Application Support/Herd/bin/php"
    if [[ -x "$candidate" ]]; then
        HERD_PHP="$candidate"
    elif command -v php >/dev/null 2>&1; then
        HERD_PHP="$(command -v php)"
    fi
}
discover_herd

herd_available() { [[ -n "$HERD" && -x "$HERD" ]]; }

# ─── Repo / workspace discovery ───────────────────────────────────────────────
# Resolve the target repo from the caller's cwd plus an optional app name.
#
#   cwd is inside a git repo           → that repo's toplevel is the target.
#   cwd is a workspace root (no .git,
#   but has git-repo children)         → the app arg names the child.
#
# Sets: TARGET_REPO (absolute path), REPO_NAME (basename), WORKSPACE_DIR (parent).
# Echoes nothing; callers read the globals.
TARGET_REPO="" REPO_NAME="" WORKSPACE_DIR=""

# List immediate subdirectories of "$1" that are git repos (one per line).
discover_child_repos() {
    local root="$1" d
    for d in "$root"/*/; do
        [[ -d "$d/.git" || -f "$d/.git" ]] || continue
        basename "$d"
    done
}

# resolve_repo <app-or-empty>
resolve_repo() {
    local app="${1:-}" toplevel children count

    # Case 1: we are inside a git worktree/repo already.
    if toplevel=$(git rev-parse --show-toplevel 2>/dev/null) && [[ -n "$toplevel" ]]; then
        # Inside a LINKED worktree the target must still be the primary repo, so
        # that `<repo>-worktrees/` resolves consistently no matter where we run.
        toplevel=$(primary_worktree_of "$toplevel")
        if [[ -n "$app" && "$(basename "$toplevel")" != "$app" ]]; then
            # An app was named but we're inside a different repo. A workspace
            # root can itself be a git repo AND hold child repos (paylog-
            # workspace does), so check children before siblings.
            local child="$toplevel/$app" sibling
            sibling="$(dirname "$toplevel")/$app"
            if [[ -d "$child/.git" || -f "$child/.git" ]]; then
                toplevel="$child"
            elif [[ -d "$sibling/.git" || -f "$sibling/.git" ]]; then
                toplevel="$sibling"
            else
                fail "Named app '$app' but cwd is repo '$(basename "$toplevel")' and no child or sibling '$app' exists."
                local available
                available=$(discover_child_repos "$toplevel")
                [[ -n "$available" ]] && { echo "    Available children:" >&2; echo "$available" | sed 's/^/      /' >&2; }
                exit 1
            fi
        fi
        TARGET_REPO="$toplevel"
        REPO_NAME=$(basename "$TARGET_REPO")
        WORKSPACE_DIR=$(dirname "$TARGET_REPO")
        return 0
    fi

    # Case 2: workspace root holding child repos.
    children=$(discover_child_repos "$PWD")
    count=$(echo "$children" | grep -c . || true)
    if (( count == 0 )); then
        die "Not inside a git repo, and no git-repo children found in $PWD."
    fi

    if [[ -z "$app" ]]; then
        if (( count == 1 )); then
            app="$children"
        else
            fail "$count repos found here — name one:"
            echo "$children" | sed 's/^/      /' >&2
            exit 1
        fi
    fi

    echo "$children" | grep -qx "$app" \
        || { fail "Unknown app '$app'. Available:"; echo "$children" | sed 's/^/      /' >&2; exit 1; }

    TARGET_REPO="$PWD/$app"
    REPO_NAME="$app"
    WORKSPACE_DIR="$PWD"
}

# locate_worktree_repo <worktree-name>
# A worktree is named <repo><id>, so find the repo whose basename prefixes it and
# re-resolve onto that repo. Search order: the current repo, then its children,
# then its siblings — a workspace root can be a git repo AND hold child repos.
# Leaves TARGET_REPO / REPO_NAME / WORKSPACE_DIR pointing at the owning repo.
locate_worktree_repo() {
    local name="$1" candidate dir

    resolve_repo ""
    [[ "$name" == "$REPO_NAME"* ]] && return 0

    local root="$TARGET_REPO"
    for dir in "$root"/*/ "$(dirname "$root")"/*/; do
        [[ -d "$dir/.git" || -f "$dir/.git" ]] || continue
        candidate=$(basename "$dir")
        if [[ "$name" == "$candidate"* ]]; then
            resolve_repo "$candidate"
            return 0
        fi
    done

    fail "Could not match worktree '$name' to a repo."
    echo "    Searched under: $root and $(dirname "$root")" >&2
    exit 1
}

# Echo the primary (non-linked) worktree path for a repo. `git worktree list`
# always lists the primary first.
primary_worktree_of() {
    local anywhere="$1" first
    first=$(git -C "$anywhere" worktree list --porcelain 2>/dev/null | awk '/^worktree /{print $2; exit}')
    [[ -n "$first" ]] && echo "$first" || echo "$anywhere"
}

# ─── Naming conventions ───────────────────────────────────────────────────────
worktree_parent()  { echo "$WORKSPACE_DIR/$REPO_NAME-worktrees"; }
worktree_name()    { echo "$REPO_NAME$1"; }
worktree_path()    { echo "$WORKSPACE_DIR/$REPO_NAME-worktrees/$REPO_NAME$1"; }
worktree_domain()  { echo "$REPO_NAME$1.test"; }
worktree_url()     { echo "https://$REPO_NAME$1.test"; }

# Worktree id must be alphanumeric only — keeps URLs clean and avoids Herd link
# name issues.
validate_n() {
    [[ "$1" =~ ^[a-zA-Z0-9]+$ ]] || die "Invalid worktree id '$1'. Alphanumeric only (no dashes, dots, etc)."
}

# Normalize a worktree id. Accepts P-0243, P-243, 0243, 243 → "243"; slugs are
# left untouched. Leading zeros are stripped via 10# so printf never reads them
# as octal (0243 octal = 163 decimal, which would silently break plan lookups).
normalize_id() {
    local id="$1"
    id="${id#[Pp]-}"
    if [[ "$id" =~ ^[0-9]+$ ]]; then
        id=$((10#$id))
    fi
    echo "$id"
}

# Generate a random unused 3-4 digit id for the current repo.
generate_unused_n() {
    local n
    for _ in $(seq 1 25); do
        n=$(( RANDOM % 9000 + 1000 ))
        if [[ ! -e "$(worktree_path "$n")" ]] && ! herd_link_exists "$(worktree_name "$n")"; then
            echo "$n"
            return 0
        fi
    done
    die "Could not find an unused random id after 25 tries. Pass one explicitly."
}

herd_link_exists() {
    herd_available || return 1
    "$HERD" links 2>/dev/null | grep -qE "^\| +$1 +\|"
}

herd_secured_exists() {
    herd_available || return 1
    "$HERD" secured 2>/dev/null | grep -qE "^\| +$1 +\|"
}

# ─── .worktree.yml config ─────────────────────────────────────────────────────
# Dependency-free parser for the small fixed schema below. Looked up first next
# to the repo, then at the workspace root, so a multi-child workspace can share
# one config.
#
#   base: homolog
#   env_isolate:
#     REDIS_PREFIX: "{name}_"
#     APP_URL: "https://{name}.test"
#   runtime_dirs: [vendor, node_modules, public/build, bootstrap/cache]
#   restore_after_link: [.agents, .claude, boost.json, AGENTS.md, CLAUDE.md]
#   mcp_site_path_rewrite: true
#   db:
#     clone_name: "{repo}_wt{n}"
#     dump_glob: "database/backups/*.sql.gz"
#
# Placeholders: {name} = <repo><id>, {repo} = repo basename, {n} = the id.
WT_CONFIG_FILE=""
find_worktree_config() {
    local candidate
    for candidate in "$TARGET_REPO/.worktree.yml" "$WORKSPACE_DIR/.worktree.yml"; do
        if [[ -f "$candidate" ]]; then
            WT_CONFIG_FILE="$candidate"
            return 0
        fi
    done
    return 0
}

# cfg_scalar <key> — top-level scalar, empty when absent.
cfg_scalar() {
    [[ -n "$WT_CONFIG_FILE" ]] || return 0
    awk -v k="$1" '
        /^[[:space:]]/ { next }                       # skip nested lines
        $0 ~ "^"k":" {
            sub(/^[^:]*:[[:space:]]*/, "")
            gsub(/^["'"'"']|["'"'"']$/, "")
            sub(/[[:space:]]*#.*$/, "")
            print; exit
        }
    ' "$WT_CONFIG_FILE"
}

# cfg_list <key> — inline `[a, b]` or block `- a` list, one item per line.
cfg_list() {
    [[ -n "$WT_CONFIG_FILE" ]] || return 0
    awk -v k="$1" '
        $0 ~ "^"k":" {
            line = $0
            sub(/^[^:]*:[[:space:]]*/, "", line)
            if (line ~ /^\[/) {                        # inline form
                gsub(/^\[|\]$/, "", line)
                n = split(line, parts, ",")
                for (i = 1; i <= n; i++) {
                    gsub(/^[[:space:]]+|[[:space:]]+$/, "", parts[i])
                    gsub(/^["'"'"']|["'"'"']$/, "", parts[i])
                    if (parts[i] != "") print parts[i]
                }
                exit
            }
            block = 1; next                            # block form follows
        }
        block && /^[[:space:]]+-[[:space:]]*/ {
            item = $0
            sub(/^[[:space:]]+-[[:space:]]*/, "", item)
            gsub(/^["'"'"']|["'"'"']$/, "", item)
            print item; next
        }
        block && /^[^[:space:]]/ { exit }
    ' "$WT_CONFIG_FILE"
}

# cfg_map_keys <parent> — keys of a one-level nested map.
cfg_map_keys() {
    [[ -n "$WT_CONFIG_FILE" ]] || return 0
    awk -v p="$1" '
        $0 ~ "^"p":" { inmap = 1; next }
        inmap && /^[[:space:]]+[A-Za-z_][A-Za-z0-9_]*:/ {
            key = $0
            sub(/^[[:space:]]+/, "", key)
            sub(/:.*$/, "", key)
            print key; next
        }
        inmap && /^[^[:space:]]/ { exit }
    ' "$WT_CONFIG_FILE"
}

# cfg_map_value <parent> <key>
cfg_map_value() {
    [[ -n "$WT_CONFIG_FILE" ]] || return 0
    awk -v p="$1" -v k="$2" '
        $0 ~ "^"p":" { inmap = 1; next }
        inmap && /^[[:space:]]+/ {
            line = $0
            sub(/^[[:space:]]+/, "", line)
            if (line ~ "^"k":") {
                sub(/^[^:]*:[[:space:]]*/, "", line)
                gsub(/^["'"'"']|["'"'"']$/, "", line)
                sub(/[[:space:]]*#.*$/, "", line)
                print line; exit
            }
            next
        }
        inmap && /^[^[:space:]]/ { exit }
    ' "$WT_CONFIG_FILE"
}

# Expand {name} / {repo} / {n} placeholders.
expand_tpl() {
    local tpl="$1" name="$2" n="$3"
    tpl="${tpl//\{name\}/$name}"
    tpl="${tpl//\{repo\}/$REPO_NAME}"
    tpl="${tpl//\{n\}/$n}"
    echo "$tpl"
}

# ─── Framework detection ──────────────────────────────────────────────────────
# Laravel unlocks the Herd / .env / Redis / DB half of the skill. Detected from
# .bravros.yml (read-only) first, then from the filesystem.
is_laravel() {
    local kcfg="$TARGET_REPO/.bravros.yml"
    if [[ -f "$kcfg" ]] && grep -qE '^[[:space:]]+framework:[[:space:]]*laravel' "$kcfg"; then
        return 0
    fi
    [[ -f "$TARGET_REPO/artisan" ]]
}

# Default runtime dirs to APFS-clone, by framework. Overridable via
# runtime_dirs in .worktree.yml.
default_runtime_dirs() {
    local dirs=()
    [[ -d "$TARGET_REPO/vendor"          ]] && dirs+=(vendor)
    [[ -d "$TARGET_REPO/node_modules"    ]] && dirs+=(node_modules)
    [[ -d "$TARGET_REPO/public/build"    ]] && dirs+=(public/build)
    [[ -d "$TARGET_REPO/bootstrap/cache" ]] && dirs+=(bootstrap/cache)
    printf '%s\n' "${dirs[@]:-}"
}

# ─── Base branch ──────────────────────────────────────────────────────────────
# Precedence: .worktree.yml base → .bravros.yml staging_branch → homolog if it
# exists → main → the repo's current HEAD.
resolve_base_branch() {
    local b
    b=$(cfg_scalar base)
    [[ -n "$b" ]] && { echo "$b"; return 0; }

    local kcfg="$TARGET_REPO/.bravros.yml"
    if [[ -f "$kcfg" ]]; then
        b=$(awk '/^staging_branch:/ { sub(/^[^:]*:[[:space:]]*/, ""); gsub(/["'"'"']/, ""); print; exit }' "$kcfg")
        [[ -n "$b" ]] && { echo "$b"; return 0; }
    fi

    for b in homolog main master; do
        if git -C "$TARGET_REPO" show-ref --quiet "refs/heads/$b" \
           || git -C "$TARGET_REPO" show-ref --quiet "refs/remotes/origin/$b"; then
            echo "$b"; return 0
        fi
    done

    git -C "$TARGET_REPO" rev-parse --abbrev-ref HEAD 2>/dev/null
}

# ─── .env helpers ─────────────────────────────────────────────────────────────
# Echo a single dotenv value (handles unquoted, single-, double-quoted).
read_env_var() {
    local file="$1" key="$2" line val
    [[ -f "$file" ]] || return 0
    line=$(grep -E "^${key}=" "$file" | head -1) || true
    [[ -z "$line" ]] && return 0
    val="${line#*=}"
    if [[ "$val" =~ ^\"(.*)\"$ ]]; then val="${BASH_REMATCH[1]}"
    elif [[ "$val" =~ ^\'(.*)\'$ ]]; then val="${BASH_REMATCH[1]}"
    fi
    echo "$val"
}

# Replace or insert key=value in a dotenv file. REDIS_PREFIX is inserted right
# after REDIS_PORT for grouping; other new keys are appended.
set_env_var() {
    local file="$1" key="$2" value="$3"
    [[ -f "$file" ]] || die "set_env_var: $file does not exist"

    if grep -qE "^${key}=" "$file"; then
        sed -i.bak "s|^${key}=.*|${key}=${value}|" "$file"
        rm -f "${file}.bak"
    elif [[ "$key" == "REDIS_PREFIX" ]] && grep -qE "^REDIS_PORT=" "$file"; then
        awk -v k="$key" -v v="$value" '
            /^REDIS_PORT=/ { print; print k "=" v; next }
            { print }
        ' "$file" > "${file}.tmp" && mv "${file}.tmp" "$file"
    else
        echo "${key}=${value}" >> "$file"
    fi
}

# ─── Plan-frontmatter helpers ─────────────────────────────────────────────────
# Find the plan file (or folder-plan entry file) for a numeric id. Echoes path
# or empty. Handles both `.planning/P-NNNN-*.md` and `.planning/P-NNNN-*/`.
find_plan_file() {
    local n="$1" pattern match dir
    [[ "$n" =~ ^[0-9]+$ ]] || return 0
    pattern=$(printf "P-%04d-*" "$n")

    match=$(find "$TARGET_REPO/.planning" -maxdepth 1 -name "${pattern}.md" -type f -print 2>/dev/null | head -1) || true
    [[ -n "$match" ]] && { echo "$match"; return 0; }

    dir=$(find "$TARGET_REPO/.planning" -maxdepth 1 -name "$pattern" -type d -print 2>/dev/null | head -1) || true
    if [[ -n "$dir" ]]; then
        for candidate in PLAN.md TASKLIST.md; do
            [[ -f "$dir/$candidate" ]] && { echo "$dir/$candidate"; return 0; }
        done
        find "$dir" -maxdepth 1 -name '*.md' -print 2>/dev/null | head -1
    fi
}

# Read a YAML frontmatter scalar. Strips quotes.
read_frontmatter() {
    local file="$1" key="$2"
    [[ -f "$file" ]] || return 0
    awk -v k="$key" '
        /^---$/ { fm = !fm; next }
        fm && $1 == k":" {
            sub(/^[^:]+:[ \t]*/, "")
            gsub(/^["'"'"']|["'"'"']$/, "")
            print
            exit
        }
    ' "$file"
}

# ─── MySQL helpers ────────────────────────────────────────────────────────────
# Echo space-separated -h/-u args. The password is NOT handled here: callers
# MUST `load_mysql_pwd "$envfile"` in their OWN shell first. Exporting inside a
# command substitution loses the export with the subshell.
mysql_args() {
    local envfile="$1" host user
    host=$(read_env_var "$envfile" DB_HOST)
    user=$(read_env_var "$envfile" DB_USERNAME)
    [[ -n "$host" && -n "$user" ]] || die "Missing DB_HOST/DB_USERNAME in $envfile"
    echo "-h $host -u $user"
}

load_mysql_pwd() {
    export MYSQL_PWD="$(read_env_var "$1" DB_PASSWORD)"
}

# Echo the DB_CONNECTION for the repo (mysql, sqlite, pgsql, …).
db_driver() {
    local d
    d=$(read_env_var "$1" DB_CONNECTION)
    [[ -z "$d" ]] && d="mysql"
    echo "$d"
}

mysql_db_exists() {
    local envfile="$1" dbname="$2" args
    load_mysql_pwd "$envfile"
    args=$(mysql_args "$envfile")
    # shellcheck disable=SC2086
    mysql $args -BNe "SHOW DATABASES LIKE '$dbname'" 2>/dev/null | grep -qE "^${dbname}$"
}

# Validate a gzipped mysqldump before it seeds a DB, so a 0-byte, corrupt or
# truncated dump never silently half-fills a cloned worktree DB.
dump_is_valid() {
    local f="$1"
    if [[ ! -s "$f" ]]; then
        warn "dump is empty: $(basename "$f")"; return 1
    fi
    if ! gzip -t "$f" 2>/dev/null; then
        warn "dump is a corrupt/truncated gzip: $(basename "$f")"; return 1
    fi
    # mysqldump appends a "-- Dump completed" trailer; its absence means the dump
    # was cut short mid-stream even though the gzip wrapper is intact.
    if ! gunzip -c "$f" | tail -c 4096 | grep -q "Dump completed"; then
        warn "dump looks truncated (no 'Dump completed' trailer): $(basename "$f")"; return 1
    fi
    return 0
}

# ─── Redis helpers ────────────────────────────────────────────────────────────
redis_args() {
    local envfile="$1" host port pass args
    host=$(read_env_var "$envfile" REDIS_HOST); [[ -z "$host" ]] && host="127.0.0.1"
    port=$(read_env_var "$envfile" REDIS_PORT); [[ -z "$port" ]] && port="6379"
    pass=$(read_env_var "$envfile" REDIS_PASSWORD)
    args="-h $host -p $port"
    [[ -n "$pass" && "$pass" != "null" ]] && args="$args -a $pass"
    echo "$args"
}

redis_flush_prefix() {
    local envfile="$1" prefix="$2" args keys count
    command -v redis-cli >/dev/null 2>&1 || { echo "0"; return 0; }
    args=$(redis_args "$envfile")
    # shellcheck disable=SC2086
    keys=$(redis-cli $args --no-auth-warning --scan --pattern "${prefix}*" 2>/dev/null) || true
    if [[ -n "$keys" ]]; then
        count=$(echo "$keys" | wc -l | tr -d ' ')
        # shellcheck disable=SC2086
        echo "$keys" | xargs redis-cli $args --no-auth-warning unlink >/dev/null 2>&1 || true
        echo "$count"
    else
        echo "0"
    fi
}

# ─── Branch merge status (content-aware) ──────────────────────────────────────
# Classify a branch against a base ref WITHOUT relying on literal ancestry alone.
# Squash-merged / merge-commit PRs, and stray planning-only commits on top of an
# already-merged branch, both break `merge-base --is-ancestor` — that check
# reported genuinely-shipped worktrees as "NOT merged" and forced needless
# --force teardowns. Instead diff the branch against its merge-base with the
# target:
#   merged    — literal ancestor OR content-identical (all work already in base)
#   plan-only — remaining delta touches ONLY .planning/ or backlog/ paths
#   unmerged  — remaining delta includes real (non-planning) paths
#   unknown   — branch/base could not be resolved (offline, missing ref)
# Callers must `git fetch` the base ref first (kept out here for speed/reuse).
# Echoes "<status>" or "<status>\t<comma-separated-paths>".
branch_merge_status() {
    local repo="$1" branch="$2" baseref="$3" mb paths nonplan
    git -C "$repo" rev-parse --verify --quiet "$baseref" >/dev/null 2>&1 || { echo "unknown"; return 0; }
    git -C "$repo" rev-parse --verify --quiet "$branch"  >/dev/null 2>&1 || { echo "unknown"; return 0; }
    if git -C "$repo" merge-base --is-ancestor "$branch" "$baseref" 2>/dev/null; then
        echo "merged"; return 0
    fi
    mb=$(git -C "$repo" merge-base "$branch" "$baseref" 2>/dev/null) || { echo "unknown"; return 0; }
    if git -C "$repo" diff --quiet "$mb..$branch" 2>/dev/null; then
        echo "merged"; return 0   # content-identical despite non-ancestry (squash/merge-commit)
    fi
    paths=$(git -C "$repo" diff --name-only "$mb..$branch" 2>/dev/null)
    nonplan=$(echo "$paths" | grep -vE '^(\.planning/|backlog/)' || true)
    if [[ -z "$nonplan" ]]; then
        printf 'plan-only\t%s\n' "$(echo "$paths" | paste -sd, -)"
    else
        printf 'unmerged\t%s\n' "$(echo "$nonplan" | paste -sd, -)"
    fi
}

# Ahead/behind counts for a branch vs a ref. Echoes "<ahead> <behind>", or
# "? ?" when either ref is unresolvable.
ahead_behind() {
    local repo="$1" branch="$2" ref="$3" out
    git -C "$repo" rev-parse --verify --quiet "$ref"    >/dev/null 2>&1 || { echo "? ?"; return 0; }
    git -C "$repo" rev-parse --verify --quiet "$branch" >/dev/null 2>&1 || { echo "? ?"; return 0; }
    out=$(git -C "$repo" rev-list --left-right --count "$branch...$ref" 2>/dev/null) || { echo "? ?"; return 0; }
    echo "$out" | awk '{print $1, $2}'
}
