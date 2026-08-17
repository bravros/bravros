#!/bin/bash

# ============================================================================
# Production Database Sync Script
# ============================================================================
# Dumps a production database ON the production server, downloads only the
# compressed stream, and restores it locally. Credentials live in .db-sync.env
# (gitignored); local DB connection details are read from the Laravel .env.
#
# SCOPE: MySQL / MariaDB only. The dump (mysqldump), the size/count probes
# (information_schema), and the DROP/CREATE DATABASE statements are all
# MySQL-specific. The script fails fast if the app's DB_CONNECTION is not
# mysql/mariadb. pgsql/sqlite support is a future extension.
#
# Usage:
#   ./sync-db.sh              # Full sync (dump + restore)
#   ./sync-db.sh --dump-only  # Only download, don't restore
#   ./sync-db.sh --restore    # Restore latest backup without downloading
#   ./sync-db.sh --list       # List available backups
#   ./sync-db.sh --target=X   # Force restore target: dev | local | auto
#
# ── Two OPTIONAL features, both off until configured ────────────────────────
#
# 1. DUAL RESTORE TARGET. Set DEV_DB_HOST to a shared dev database box and the
#    script probes it on every run: it wins when reachable, otherwise the target
#    from .env is used. .env is then rewritten to match the winner, so the app
#    follows the database when you move between networks. Leave DEV_DB_HOST
#    unset (the default) and the script simply uses .env, as it always has.
#
# 2. REMOTE IMPORT. Set DEV_SSH_HOST and the compressed archive is shipped to
#    that box and imported THERE, over its local MySQL socket, instead of being
#    streamed into it across the network. This matters more than it sounds:
#    mysqldump emits one statement per net_buffer_length and the client waits
#    for the server's OK before sending the next, so importing across a link
#    pays a full round trip per statement. On a 3.3 GB dump over a LAN with
#    24 ms average RTT that meant 20-30 minutes and intermittent
#    `ERROR 2013 ... Lost connection`; shipping the ~300 MB archive instead cut
#    it to ~3 minutes (measured 15/08/2026).
#
# Useful environment overrides (also settable in .db-sync.env):
#   SYNC_CONFIRM=1            # Prompt to type the DB name before the DROP
#   KEEP_BACKUPS=N            # How many compressed backups to retain (default 3)
#   MYSQLDUMP_EXTRA_OPTS=...  # Extra mysqldump flags (e.g. --column-statistics=0)
#   ARTISAN_CMD="php artisan" # Override the artisan runner (e.g. "herd php artisan")
#   DEV_URL=...               # Override the dev URL printed at the end
#   REMOTE_IMPORT=0           # Force the over-the-network import
#   SYNC_ENV_WRITE=0          # Never rewrite .env, just warn
# ============================================================================

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
DIM='\033[2m'
NC='\033[0m'

print_step()    { echo -e "\n${BLUE}${BOLD}$1${NC}" >&2; }
print_status()  { echo -e "  ${DIM}➜${NC}  $1" >&2; }
print_success() { echo -e "  ${GREEN}✅${NC} $1" >&2; }
print_error()   { echo -e "  ${RED}❌${NC} $1" >&2; }
print_warning() { echo -e "  ${YELLOW}⚠️${NC}  $1" >&2; }
print_info()    { echo -e "  ${DIM}$1${NC}" >&2; }

print_banner() {
    echo "" >&2
    echo -e "${CYAN}${BOLD}" >&2
    echo "  ╔══════════════════════════════════════════════════╗" >&2
    echo "  ║              🗄️  Database Sync  🗄️              ║" >&2
    echo "  ╚══════════════════════════════════════════════════╝" >&2
    echo -e "${NC}" >&2
}

# Script directory (project root)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG_FILE="$SCRIPT_DIR/.db-sync.env"
ENV_FILE="$SCRIPT_DIR/.env"
BACKUP_DIR="$SCRIPT_DIR/database/backups"
TUNNEL_PID=""
PROGRESS_PID=""
IMPORT_PROGRESS_PID=""
PARTIAL_FILE=""

# Parse arguments
MODE="full"  # full, dump-only, restore, list
BACKUP_FILE=""
# auto | dev | local — env var is the default, --target= overrides it.
# Only consulted when a dev box is configured (DEV_DB_HOST); otherwise the single
# target from .env is used and no probing happens at all.
SYNC_TARGET="${SYNC_TARGET:-auto}"

for arg in "$@"; do
    case $arg in
        --dump-only)  MODE="dump-only" ;;
        --restore)    MODE="restore" ;;
        --restore=*)  MODE="restore"; BACKUP_FILE="${arg#*=}" ;;
        --list)       MODE="list" ;;
        --target=*)   SYNC_TARGET="${arg#*=}" ;;
        --help|-h)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --dump-only       Download production DB but don't restore locally"
            echo "  --restore         Restore the latest backup to local DB"
            echo "  --restore=FILE    Restore a specific backup file"
            echo "  --list            List available backups"
            echo "  --target=TARGET   Restore target: auto (default) | dev | local"
            echo "  --help            Show this help"
            echo ""
            echo "Restore target (only when DEV_DB_HOST is configured):"
            echo "  auto   Use the dev box if it answers, else the target from .env"
            echo "  dev    Force the dev box (fails if unreachable)"
            echo "  local  Force the target from .env"
            echo ""
            echo "  The winning target is written into .env (DB_* + REDIS_*), so the"
            echo "  app follows the database. Set SYNC_ENV_WRITE=0 to disable that."
            echo "  With DEV_DB_HOST unset there is one target and no probing."
            echo ""
            echo "Import speed:"
            echo "  With DEV_SSH_HOST set, the compressed dump is shipped to the dev"
            echo "  box and imported over its local MySQL socket instead of being"
            echo "  streamed across the network — no per-statement round trips."
            echo "  REMOTE_IMPORT=0   force the over-the-network import"
            echo "  DEV_SSH_HOST      ssh host for the dev box (unset = feature off)"
            echo "  DEV_MYSQL_DOCKER  container running MySQL (empty = native client)"
            echo ""
            echo "  If a sync is slow, check which table dominates the dump and"
            echo "  consider EXCLUDE_TABLES for anything you do not need locally:"
            echo "    SELECT table_name, ROUND((data_length+index_length)/1048576) mb"
            echo "      FROM information_schema.tables WHERE table_schema='<db>'"
            echo "      ORDER BY 2 DESC LIMIT 10;"
            echo ""
            echo "Config: .db-sync.env (copy from .db-sync.env.example)"
            exit 0
            ;;
        *)
            print_error "Unknown option: $arg"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

# ── Cleanup function ──────────────────────────────────────────────────────────

cleanup() {
    # Reap background progress monitors first — they are infinite sleep loops
    # that ignore SIGINT in this non-job-control script, so they must be killed
    # explicitly or they survive an interrupted run as orphaned processes.
    local p
    for p in "$PROGRESS_PID" "$IMPORT_PROGRESS_PID"; do
        if [ -n "$p" ] && kill -0 "$p" 2>/dev/null; then
            kill "$p" 2>/dev/null || true
            wait "$p" 2>/dev/null || true
        fi
    done
    # Remove any half-written .partial dump left by an interrupted/failed run
    # (it is never restorable and never matches the backup globs used by prune).
    if [ -n "$PARTIAL_FILE" ]; then
        rm -f "$PARTIAL_FILE" 2>/dev/null || true
    fi
    # No SSH tunnel is opened any more (the dump runs server-side), so there is
    # no long-lived forwarding process to reap here. TUNNEL_PID is retained only
    # so an interrupted run from an older version of this script still cleans up.
    if [ -n "$TUNNEL_PID" ] && kill -0 "$TUNNEL_PID" 2>/dev/null; then
        kill "$TUNNEL_PID" 2>/dev/null
        wait "$TUNNEL_PID" 2>/dev/null || true
    fi
    # Close the shared SSH master. ControlPersist keeps a background ssh alive
    # after the last channel closes, so it must be told to exit or it outlives
    # the run. Guarded because EXIT can fire before these vars are even set.
    if [ -n "${SSH_CONTROL_DIR:-}" ] && [ -d "${SSH_CONTROL_DIR}" ]; then
        if [ -n "${SSH_HOST:-}" ]; then
            ssh -O exit -o ControlPath="${SSH_CONTROL_PATH}" \
                -p "${SSH_PORT:-22}" "${SSH_USER:-}@${SSH_HOST}" >/dev/null 2>&1 || true
        fi
        rm -rf "${SSH_CONTROL_DIR}" 2>/dev/null || true
    fi
}

# Resource teardown runs once, on EXIT. The signal traps must EXIT — a trap that
# only runs cleanup() lets bash RESUME after Ctrl+C, powering the script on into
# DROP DATABASE / migrate on a half-finished dump. Calling exit fires the EXIT
# trap (so cleanup still runs exactly once) and yields the conventional codes.
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# ── Load configuration ────────────────────────────────────────────────────────

if [ ! -f "$CONFIG_FILE" ]; then
    print_error "Config file not found: .db-sync.env"
    print_status "Copy the example and fill in your credentials:"
    print_info "  cp .db-sync.env.example .db-sync.env"
    exit 1
fi

# shellcheck source=/dev/null
source "$CONFIG_FILE"

# ── Load local DB config from .env ────────────────────────────────────────────
# Only DB_DATABASE is authoritative here — the schema name is the same on every
# target, so it is the one local value the script does not choose for you.
#
# DB_HOST/PORT/USERNAME/PASSWORD are read as the CURRENT state (what the app is
# pointed at right now) purely so select_restore_target() can diff against it and
# rewrite .env when the winning target differs. Do not treat them as intent.
# The .db-sync.env file is only for remote/SSH config.

if [ ! -f "$ENV_FILE" ]; then
    print_error "Laravel .env not found: $ENV_FILE"
    print_status "Local DB credentials are read from .env (DB_HOST, DB_PORT, DB_DATABASE, DB_USERNAME, DB_PASSWORD)"
    exit 1
fi

# Read a key from a dotenv-style file without sourcing it. Strips surrounding
# single/double quotes and ignores commented lines.
read_env_value() {
    local key="$1"
    local file="$2"
    local raw
    # `|| true`: a key that is simply absent makes grep exit 1, and under pipefail
    # that would kill the script before it ever printed a banner. Absent == empty.
    raw=$(grep -E "^[[:space:]]*${key}=" "$file" | grep -v '^[[:space:]]*#' | tail -1 | sed -E "s/^[[:space:]]*${key}=//" || true)
    # Strip matching surrounding quotes
    if [[ "$raw" =~ ^\"(.*)\"$ ]]; then
        raw="${BASH_REMATCH[1]}"
    elif [[ "$raw" =~ ^\'(.*)\'$ ]]; then
        raw="${BASH_REMATCH[1]}"
    fi
    printf '%s' "$raw"
}

LOCAL_DB_HOST="$(read_env_value DB_HOST "$ENV_FILE")"
LOCAL_DB_PORT="$(read_env_value DB_PORT "$ENV_FILE")"
LOCAL_DB_NAME="$(read_env_value DB_DATABASE "$ENV_FILE")"
LOCAL_DB_USER="$(read_env_value DB_USERNAME "$ENV_FILE")"
LOCAL_DB_PASS="$(read_env_value DB_PASSWORD "$ENV_FILE")"

# Engine guard — this script only understands MySQL/MariaDB. Read DB_CONNECTION
# from the app's .env and fail loudly for any other engine rather than emitting
# a broken dump. (.db-sync.env may override via LOCAL_DB_CONNECTION.)
LOCAL_DB_CONNECTION="${LOCAL_DB_CONNECTION:-$(read_env_value DB_CONNECTION "$ENV_FILE")}"
LOCAL_DB_CONNECTION="${LOCAL_DB_CONNECTION:-mysql}"
case "$LOCAL_DB_CONNECTION" in
    mysql|mariadb) ;;
    *)
        print_error "sync-db.sh currently supports MySQL/MariaDB only (DB_CONNECTION=${LOCAL_DB_CONNECTION})."
        print_info "  pgsql/sqlite are not supported — restore those with their native tooling."
        exit 1
        ;;
esac

# Charset/collation for the CREATE DATABASE. Read from the app's .env (Laravel
# exposes DB_CHARSET / DB_COLLATION) so the recreated schema matches what the app
# expects; .db-sync.env may override; sane utf8mb4 default last.
LOCAL_DB_CHARSET="${LOCAL_DB_CHARSET:-$(read_env_value DB_CHARSET "$ENV_FILE")}"
LOCAL_DB_CHARSET="${LOCAL_DB_CHARSET:-utf8mb4}"
LOCAL_DB_COLLATION="${LOCAL_DB_COLLATION:-$(read_env_value DB_COLLATION "$ENV_FILE")}"
LOCAL_DB_COLLATION="${LOCAL_DB_COLLATION:-utf8mb4_unicode_ci}"

# Validate .env-derived local config. DB_DATABASE is always required — the schema
# name is the same on every target, so it is the one value no probe can supply.
if [ -z "${LOCAL_DB_NAME:-}" ]; then
    print_error "Missing required key in .env: DB_DATABASE"
    exit 1
fi
# Host and user are only required when there is no dev box to supply them.
if [ -z "${DEV_DB_HOST:-}" ]; then
    for var in LOCAL_DB_HOST LOCAL_DB_USER; do
        if [ -z "${!var:-}" ]; then
            mapped_key="DB_HOST"; [ "$var" = "LOCAL_DB_USER" ] && mapped_key="DB_USERNAME"
            print_error "Missing required key in .env: $mapped_key"
            exit 1
        fi
    done
fi

# Validate required config in .db-sync.env (remote-only now)
REQUIRED_VARS=(SSH_USER SSH_HOST PROD_DB_NAME PROD_DB_USER PROD_DB_PASS)
for var in "${REQUIRED_VARS[@]}"; do
    if [ -z "${!var:-}" ]; then
        print_error "Missing required config: $var in .db-sync.env"
        exit 1
    fi
done

# Defaults
SSH_PORT="${SSH_PORT:-22}"
SSH_KEY="${SSH_KEY:-}"

# One TCP connection for the whole run, shared by every remote_bash/remote_query.
#
# A full sync opens ~8 separate ssh connections in seconds (reachability test,
# tooling probe, DB test, 2 version probes, size + table count, then the dump).
# Something on the path rate-limits NEW port-22 connections: the first ~3 connect,
# every later one times out, and the run dies at the dump with "Operation timed
# out" while the box stays healthy on ICMP and 443 (observed 17/08/2026).
# It is NOT the server's ufw — every rule there is ALLOW, not LIMIT — nor fail2ban
# (0 bans during an outage), so it is upstream: provider filtering or the ISP.
# Until 06/08 this never bit, because the pre-1b9ccd1fd design held ONE `-f -N -L`
# tunnel open for the whole run (2 connections); dropping it for server-side zstd
# traded connection reuse away and took the count to 8.
#
# Multiplexing makes every later call ride the first connection: one handshake,
# no rate-limit exposure, and a faster run.
#
# The path is built under /tmp, NOT $TMPDIR, and uses the %C hash rather than
# %r@%h:%p. A unix domain socket path is capped at 104 bytes on macOS, and
# $TMPDIR alone is ~50 ("/var/folders/8f/m336ky.../T/") — ssh then appends its own
# 17-char temp suffix while creating the socket and blows the limit, failing every
# call with `unix_listener: path "..." too long for Unix domain socket`. Keep it short.
# It must also contain no spaces: ssh_base_opts is word-split by its unquoted callers.
SSH_CONTROL_DIR="$(mktemp -d /tmp/sdb.XXXXXX)"
SSH_CONTROL_PATH="${SSH_CONTROL_DIR}/%C"
LOCAL_DB_PORT="${LOCAL_DB_PORT:-3306}"
APP_NAME="${APP_NAME:-$(basename "$SCRIPT_DIR")}"
KEEP_BACKUPS="${KEEP_BACKUPS:-3}"  # how many compressed backups to retain (rotation)
# Production DB as reached FROM the production server. Defaults to that server's
# own loopback, which is the usual single-box setup; point it at a managed/RDS
# endpoint when the database lives somewhere else.
PROD_DB_HOST="${PROD_DB_HOST:-127.0.0.1}"
PROD_DB_PORT="${PROD_DB_PORT:-3306}"
# Extra mysqldump flags, default OFF for portability. MariaDB and older mysqldump
# builds abort on unknown flags, so nothing engine-specific is baked in. Example
# opt-in for a MySQL-8 client dumping a 5.7 server: MYSQLDUMP_EXTRA_OPTS="--column-statistics=0"
MYSQLDUMP_EXTRA_OPTS="${MYSQLDUMP_EXTRA_OPTS:-}"
# Artisan runner. Plain `php artisan` works wherever php is on PATH; override to
# e.g. "herd php artisan" or "./vendor/bin/sail artisan" in .db-sync.env.
ARTISAN="${ARTISAN_CMD:-php artisan}"

# ── Restore targets (OPTIONAL — off unless DEV_DB_HOST is set) ────────────────
# Some setups keep the dev database on a shared box that is only reachable from
# certain networks. Set DEV_DB_HOST and the script probes it on every run: it
# wins when reachable, otherwise the FALLBACK_* target does.
#
# Which one wins is decided by PROBE, never by config: whatever .env currently
# says is treated as the PREVIOUS choice, not as the intent — that is the whole
# point, since .env is exactly what goes stale when you change location.
#
# Leave DEV_DB_HOST unset (the default) and none of this runs: there is one
# target, taken from .env, and the script never rewrites .env.
DEV_DB_HOST="${DEV_DB_HOST:-}"
DEV_DB_PORT="${DEV_DB_PORT:-3306}"
DEV_DB_USER="${DEV_DB_USER:-}"
DEV_DB_PASS="${DEV_DB_PASS-}"
DEV_REDIS_HOST="${DEV_REDIS_HOST:-$DEV_DB_HOST}"
DEV_REDIS_PORT="${DEV_REDIS_PORT:-6379}"
DEV_REDIS_PASS="${DEV_REDIS_PASS-}"

# ── Remote import (OPTIONAL — off unless DEV_SSH_HOST is set) ─────────────────
# With DEV_SSH_HOST set, the dump is SHIPPED TO that box COMPRESSED and imported
# on it, instead of being streamed into it from here.
#
# Why: mysqldump emits one statement per net_buffer_length, and the client waits
# for the server's OK before sending the next. Importing across a link therefore
# pays a full network round trip PER STATEMENT, and pushes the whole dump
# uncompressed while doing it. Measured 15/08/2026 on a 10GbE LAN that shows
# 0.46 ms at best but 24 ms average and 109 ms peaks: a 3.3 GB dump took 20-30
# minutes and died with `ERROR 2013 ... Lost connection` at a DIFFERENT line
# every run (random position ⇒ transient link, not bad data). Decompression was
# never the cost — 316 MB unpacks in ~3s.
#
# Shipping the ~300 MB archive instead moved ~11x fewer bytes and cut the import
# to ~3 minutes, because it then runs over the target's own MySQL socket: no TCP,
# no docker-proxy, no round-trip tax, and nothing left on a flaky path for the
# minutes an import takes.
#
# Falls back to the over-the-network import by itself whenever anything is
# missing (no SSH key, no zstd, renamed container), so this is safe to leave on.
DEV_SSH_HOST="${DEV_SSH_HOST:-}"          # ssh alias/host for the dev box; unset = feature off
DEV_MYSQL_DOCKER="${DEV_MYSQL_DOCKER-}"   # container running MySQL; empty = native client on PATH
DEV_TMP_DIR="${DEV_TMP_DIR:-/tmp}"
REMOTE_IMPORT="${REMOTE_IMPORT:-1}"       # 0 = always import over the network

# Fallback target — where the restore lands when there is no dev box, or it is
# unreachable. Defaults to whatever .env already says, so an unconfigured project
# behaves exactly as it did before any of this existed. The `-` (not `:-`) on the
# password expansions is load-bearing: it keeps an explicitly-empty override
# empty instead of silently substituting the default back in.
FALLBACK_DB_HOST="${FALLBACK_DB_HOST:-${LOCAL_DB_HOST:-127.0.0.1}}"
FALLBACK_DB_PORT="${FALLBACK_DB_PORT:-${LOCAL_DB_PORT:-3306}}"
FALLBACK_DB_USER="${FALLBACK_DB_USER:-${LOCAL_DB_USER:-root}}"
FALLBACK_DB_PASS="${FALLBACK_DB_PASS-$LOCAL_DB_PASS}"
FALLBACK_REDIS_HOST="${FALLBACK_REDIS_HOST:-$(read_env_value REDIS_HOST "$ENV_FILE")}"
FALLBACK_REDIS_HOST="${FALLBACK_REDIS_HOST:-127.0.0.1}"
FALLBACK_REDIS_PORT="${FALLBACK_REDIS_PORT:-6379}"
FALLBACK_REDIS_PASS="${FALLBACK_REDIS_PASS-}"

PROBE_TIMEOUT="${PROBE_TIMEOUT:-3}"      # seconds, per connection attempt
SYNC_ENV_WRITE="${SYNC_ENV_WRITE:-1}"    # 0 = never rewrite .env, just warn

# Expand tilde in SSH_KEY if set
if [ -n "$SSH_KEY" ]; then
    SSH_KEY="${SSH_KEY/#\~/$HOME}"
fi

# Create backup directory
mkdir -p "$BACKUP_DIR"

# ── Ensure .db-sync.env is gitignored ────────────────────────────────────────

ensure_gitignored() {
    local gitignore="$SCRIPT_DIR/.gitignore"
    local entry
    # Credentials AND the local compressed backups must never be committed.
    for entry in '.db-sync.env' 'database/backups/'; do
        if [ ! -f "$gitignore" ] || ! grep -qxF "$entry" "$gitignore"; then
            echo "$entry" >> "$gitignore"
            print_success "$entry added to .gitignore"
        fi
    done
}

ensure_gitignored

# ── Find a local binary ──────────────────────────────────────────────────────
# Used for mysql and redis-cli alike. On macOS, Herd's copies are preferred over
# any Homebrew build so the client versions match the stack the app itself talks
# to; on Linux the usual system paths are searched. PATH is the last resort on
# both, so an unusual install still works.

find_local_binary() {
    local binary="$1"
    local _os
    _os="$(uname -s)"

    if [ "$_os" = "Darwin" ]; then
        # Herd's bundled MySQL/Redis clients first
        local herd_paths=(
            "$HOME/Library/Application Support/Herd/config/mysql"
            "$HOME/Library/Application Support/Herd/bin"
        )

        for herd_path in "${herd_paths[@]}"; do
            if [ -d "$herd_path" ]; then
                local found
                found=$(find "$herd_path" -name "$binary" -type f 2>/dev/null | head -1 || true)
                if [ -n "$found" ] && [ -x "$found" ]; then
                    echo "$found"
                    return 0
                fi
            fi
        done

        # Homebrew MySQL paths
        local brew_paths=(
            "/opt/homebrew/opt/mysql/bin/$binary"
            "/opt/homebrew/opt/mysql-client/bin/$binary"
            "/usr/local/opt/mysql/bin/$binary"
            "/usr/local/opt/mysql-client/bin/$binary"
            "/opt/homebrew/bin/$binary"
            "/usr/local/bin/$binary"
        )

        for path in "${brew_paths[@]}"; do
            if [ -x "$path" ]; then
                echo "$path"
                return 0
            fi
        done
    else
        # Linux: system/apt paths
        for path in "/usr/bin/$binary" "/usr/local/bin/$binary" "/usr/local/mysql/bin/$binary"; do
            if [ -x "$path" ]; then
                echo "$path"
                return 0
            fi
        done
    fi

    # Fallback to PATH (all platforms)
    if command -v "$binary" &>/dev/null; then
        command -v "$binary"
        return 0
    fi

    return 1
}

# ── Archive codec helpers (zstd for new dumps, gzip for legacy ones) ──────────
# Dumps are written with zstd; historical *.sql.gz backups stay restorable, so
# every read path dispatches on the file extension rather than assuming a codec.
#
# Measured on the real afterpay dump (208 MB of SQL, 2026-08-03), zstd -3 beat
# gzip on every axis at once — there is no trade being made here:
#
#   gzip     1326 ms compress · 9.9 MB · 517 ms decompress
#   zstd -3   185 ms compress · 6.9 MB ·  71 ms decompress   (7.2x / -31% / 7.3x)
#   zstd -3 -T4  115 ms compress

ZSTD_LEVEL="${ZSTD_LEVEL:-3}"
ZSTD_THREADS="${ZSTD_THREADS:-4}"

# Both helpers below dispatch on the file extension, and BOTH are also called on
# the in-flight "${dump_file}.partial" sidecar — whose name ends in `.partial`,
# not `.zst`. Stripping that suffix first is therefore mandatory: without it the
# case fell through to the catch-all and reported a perfectly good 7 MB dump as
# "empty or corrupt" (caught in testing, 2026-08-03).
archive_codec() {
    local f="${1%.partial}"
    case "$f" in
        *.zst) printf 'zst' ;;
        *.gz)  printf 'gz' ;;
        *)     return 1 ;;
    esac
}

# Echo the decompress-to-stdout command for a backup file, based on extension.
decompress_cmd() {
    local codec
    codec=$(archive_codec "$1") || return 1
    case "$codec" in
        zst) printf '%s' "$ZSTD_BIN -dc" ;;
        gz)  printf '%s' "gunzip -c" ;;
    esac
}

# Verify archive integrity in-place. Both codecs embed checksums (zstd XXH64,
# gzip CRC32), so this catches truncation and bit-rot for either format.
verify_archive() {
    local codec
    codec=$(archive_codec "$1") || return 1
    case "$codec" in
        zst) "$ZSTD_BIN" -t "$1" >/dev/null 2>&1 ;;
        gz)  gzip -t "$1" 2>/dev/null ;;
    esac
}

# All backups for this app, newest first, across BOTH codecs.
#
# The globs are resolved BEFORE ls sees them, and a no-match is dropped rather
# than passed through. Handing ls a pattern that matches nothing makes it exit
# non-zero EVEN WHEN the other glob matched fine — and under `set -euo pipefail`
# that status propagates out of `F=$(list_backup_files | head -1)` and kills the
# script instantly, with no message printed and nothing written to stderr.
#
# Observed 06/08/2026: `--restore` died immediately after the pre-flight, looking
# for all the world like the connection check had hung. The real cause was that
# every legacy *.sql.gz had finally rotated away, leaving the .gz glob with
# nothing to match for the first time. Zero backups is a normal state, not an
# error — callers check for empty output and report it themselves.
list_backup_files() {
    local f
    local matches=()
    for f in "$BACKUP_DIR"/${APP_NAME}-*.sql.zst "$BACKUP_DIR"/${APP_NAME}-*.sql.gz; do
        [ -f "$f" ] && matches+=("$f")
    done
    [ "${#matches[@]}" -eq 0 ] && return 0
    ls -t "${matches[@]}"
}

# ── Remote execution helpers ─────────────────────────────────────────────────
# The dump runs ON the production server and is compressed THERE, so only the
# compressed stream crosses the network. The older design opened an SSH tunnel
# and ran mysqldump locally, which pulled the ENTIRE uncompressed result set over
# the wire — on a 4 GB database that is ~3.3 GB transferred to produce a ~300 MB
# file, and it needed a local mysqldump whose version had to match the server's.
#
# Credentials are handed to the remote shell inside the script body delivered on
# stdin (`bash -s`), never as an ssh command argument. Remote argv is literally
# "bash -s", so the password is not visible in the server's process list; it is
# materialised only in a mktemp defaults-file (chmod 600) that is shredded by an
# EXIT trap. `printf %q` makes the injection safe for values containing quotes.

ssh_base_opts() {
    printf '%s' "-o StrictHostKeyChecking=accept-new -o ConnectTimeout=10 -o ServerAliveInterval=30 -o ServerAliveCountMax=3 -o ControlMaster=auto -o ControlPath=${SSH_CONTROL_PATH} -o ControlPersist=300"
}

# Run a bash script (stdin) on production. Args: none. Script arrives via stdin.
#
# NOTE: the SSH_KEY test MUST be a full `if`, not `[ -n "$SSH_KEY" ] && key_opt=...`.
# This script runs under `set -euo pipefail`, and when SSH_KEY is empty (the normal
# case — the 1Password agent supplies the key) that `&&` chain evaluates to status 1.
# As the last stage of a pipeline it runs in a subshell, so `set -e` killed the
# subshell BEFORE ssh was ever reached: empty output, empty stderr, no diagnostic.
# That failure mode cost a debugging round on 2026-08-03 — leave this as an `if`.
remote_bash() {
    local key_opt=""
    if [ -n "${SSH_KEY:-}" ]; then
        key_opt="-i $SSH_KEY"
    fi
    # shellcheck disable=SC2046,SC2086
    ssh $(ssh_base_opts) $key_opt -p "$SSH_PORT" "${SSH_USER}@${SSH_HOST}" bash -s
}

# Emit the remote credential preamble: writes a 600 defaults-file, shreds on exit.
#
# host/port go in the same file so the production database can live somewhere
# other than the SSH box (a managed/RDS endpoint reachable from it).
remote_creds_preamble() {
    local q_user q_pass q_host q_port
    q_user=$(printf '%q' "$PROD_DB_USER")
    q_pass=$(printf '%q' "$PROD_DB_PASS")
    q_host=$(printf '%q' "$PROD_DB_HOST")
    q_port=$(printf '%q' "$PROD_DB_PORT")
    cat <<PREAMBLE
set -euo pipefail
DEFAULTS_FILE=\$(mktemp) || exit 1
chmod 600 "\$DEFAULTS_FILE"
trap 'shred -u "\$DEFAULTS_FILE" 2>/dev/null || rm -f "\$DEFAULTS_FILE"' EXIT
{ printf '[client]\n'; printf 'user=%s\n' $q_user; printf 'password=%s\n' $q_pass; \
  printf 'host=%s\n' $q_host; printf 'port=%s\n' $q_port; } > "\$DEFAULTS_FILE"
PREAMBLE
}

# Run one SQL query on production, print the raw result.
remote_query() {
    local sql="$1" q_db q_sql
    q_db=$(printf '%q' "$PROD_DB_NAME")
    q_sql=$(printf '%q' "$sql")
    { remote_creds_preamble
      echo "mysql --defaults-extra-file=\"\$DEFAULTS_FILE\" -N -e $q_sql $q_db"
    } | remote_bash 2>/dev/null
}

# ── Remote import helpers (dev box) ──────────────────────────────────────────

REMOTE_IMPORT_OK=0      # 1 once the dev-box import path is proven usable
REMOTE_MYSQL_CMD=""     # how to invoke the mysql client THERE (native, or via docker exec)

dev_ssh() {
    ssh -o BatchMode=yes \
        -o StrictHostKeyChecking=accept-new \
        -o ConnectTimeout=10 \
        -o ServerAliveInterval=15 \
        -o ServerAliveCountMax=8 \
        "$DEV_SSH_HOST" "$@"
}

# Run a bash script (delivered on stdin) on the dev box.
dev_bash() { dev_ssh bash -s; }

# Decide whether the dump can be imported ON the dev box rather than streamed
# into it.
#
# Every probe here is a real capability check, because the fallback is merely
# slower, never broken: any failure silently leaves REMOTE_IMPORT_OK=0 and the
# over-the-network pipe runs instead. That matters for a machine without the
# dev box's SSH key, or when the container has been renamed.
#
# `return 0` is deliberate on every exit path — this runs under `set -e`, and a
# function ending on a failed test returns that failure to its caller.
detect_remote_import() {
    REMOTE_IMPORT_OK=0
    REMOTE_MYSQL_CMD=""

    [ "$REMOTE_IMPORT" = "1" ] || return 0
    [ -n "$DEV_SSH_HOST" ] || return 0
    # Only meaningful when the restore is actually landing on that box.
    [ -n "${TARGET_DB_HOST:-}" ] && [ "${TARGET_DB_HOST}" = "$DEV_DB_HOST" ] || return 0

    if ! dev_ssh 'command -v zstd' >/dev/null 2>&1; then
        print_info "  (no SSH access or no zstd on ${DEV_SSH_HOST} — importing over the network)"
        return 0
    fi

    # The mysql client may live inside a container (DEV_MYSQL_DOCKER) or on the
    # box's PATH. Importing through `docker exec -i` reaches mysqld over the
    # container's UNIX socket, which is the whole point — no TCP, no docker-proxy.
    if [ -n "$DEV_MYSQL_DOCKER" ] \
       && dev_ssh "docker exec -i ${DEV_MYSQL_DOCKER} sh -c 'command -v mysql'" >/dev/null 2>&1; then
        REMOTE_MYSQL_CMD="docker exec -i ${DEV_MYSQL_DOCKER} mysql"
    elif dev_ssh 'command -v mysql' >/dev/null 2>&1; then
        REMOTE_MYSQL_CMD="mysql"
    else
        print_info "  (no mysql client on ${DEV_SSH_HOST} — importing over the network)"
        return 0
    fi

    REMOTE_IMPORT_OK=1
    return 0
}

# Copy the compressed archive to the dev box. Small and quick (a ~300 MB archive
# is ~4s at a measured 74 MB/s), and retried, because a failure here costs
# seconds — whereas a link blip DURING an import costs the whole run.
dev_send_dump() {
    local src="$1" dest="$2" attempt

    for attempt in 1 2 3; do
        if scp -o BatchMode=yes -o StrictHostKeyChecking=accept-new \
               -o ConnectTimeout=10 -q "$src" "${DEV_SSH_HOST}:${dest}" 2>/dev/null; then
            return 0
        fi
        print_warning "Transfer to ${DEV_SSH_HOST} failed (attempt ${attempt}/3) — retrying..."
    done
    return 1
}

# Import the archive on the dev box, reading it from that box's own disk.
#
# The DB password never appears in any argv: it is written to a 600 defaults-file
# on the dev-box host from the script body arriving on stdin, then `docker cp`-ed
# into the container (which cannot see the host filesystem) and removed from both
# sides by an EXIT trap — the same shape as remote_creds_preamble() for prod.
dev_import_dump() {
    local remote_file="$1"
    local q_user q_pass q_db q_file
    q_user=$(printf '%q' "$LOCAL_DB_USER")
    q_pass=$(printf '%q' "$LOCAL_DB_PASS")
    q_db=$(printf '%q' "$LOCAL_DB_NAME")
    q_file=$(printf '%q' "$remote_file")

    dev_bash <<REMOTE_IMPORT_SCRIPT
set -euo pipefail

DEFAULTS_FILE=\$(mktemp) || exit 1
chmod 600 "\$DEFAULTS_FILE"
IN_CONTAINER="/tmp/.db-sync-\$\$.cnf"

cleanup_remote() {
    rm -f "\$DEFAULTS_FILE" $q_file 2>/dev/null || true
$([ -n "$DEV_MYSQL_DOCKER" ] && printf '    docker exec %s rm -f "$IN_CONTAINER" 2>/dev/null || true\n' "$DEV_MYSQL_DOCKER")
}
trap cleanup_remote EXIT

{ printf '[client]\n'; printf 'user=%s\n' $q_user; printf 'password=%s\n' $q_pass; } > "\$DEFAULTS_FILE"

$([ -n "$DEV_MYSQL_DOCKER" ] && printf 'docker cp "$DEFAULTS_FILE" %s:"$IN_CONTAINER" >/dev/null\nCNF="$IN_CONTAINER"\n' "$DEV_MYSQL_DOCKER" || printf 'CNF="$DEFAULTS_FILE"\n')

# --defaults-extra-file MUST be the first option mysql sees. --max-allowed-packet
# matches the server's 64M so the larger statements the dump now emits fit.
zstd -dc $q_file | $REMOTE_MYSQL_CMD --defaults-extra-file="\$CNF" \\
    --max-allowed-packet=64M \\
    $q_db
REMOTE_IMPORT_SCRIPT
}

# ── Target probing ───────────────────────────────────────────────────────────

# Bounded TCP reachability check.
#
# `-G` caps the CONNECT phase specifically; plain `-w` only caps the idle phase
# once connected. That distinction is the whole reason this helper exists: a
# blackholed LAN address (the dev box powered off mid-upgrade, ARP going nowhere) makes
# a bare connect sit through the full ~75s TCP SYN timeout, which would turn
# "fall back to the local stack" into a minute-plus stall on every laptop run.
#
# It is also what makes probe_redis() safe — see the note there.
probe_tcp() {
    nc -z -G "$PROBE_TIMEOUT" -w "$PROBE_TIMEOUT" "$1" "$2" >/dev/null 2>&1
}

# Full credential check: the port is open AND these credentials authenticate.
# Reachability alone is not enough — a MySQL that is up but rejects our user is a
# target we must NOT select, or the restore dies after dropping the database.
probe_mysql() {
    local host="$1" port="$2" user="$3" pass="$4"
    probe_tcp "$host" "$port" || return 1
    if [ -n "$pass" ]; then
        MYSQL_PWD="$pass" "$MYSQL_BIN" -h "$host" -P "$port" -u "$user" \
            --connect-timeout="$PROBE_TIMEOUT" -N -B -e 'SELECT 1' >/dev/null 2>&1
    else
        "$MYSQL_BIN" -h "$host" -P "$port" -u "$user" \
            --connect-timeout="$PROBE_TIMEOUT" -N -B -e 'SELECT 1' >/dev/null 2>&1
    fi
}

# Redis liveness.
#
# Some bundled redis-cli builds (Herd's, for one) have no -t/--timeout flag
# (`-t` exits "Unrecognized option"), so the client cannot bound itself — the
# probe_tcp() gate above is what keeps this call from hanging. Do not reorder it.
#
# A PONG proves the port really is Redis. NOAUTH/WRONGPASS prove that just as
# well, so they count as reachable: a password mismatch is a misconfiguration
# worth reporting, not a reason to silently demote the entire target.
probe_redis() {
    local host="$1" port="$2" pass="$3" out
    probe_tcp "$host" "$port" || return 1
    [ -z "${REDIS_CLI_BIN:-}" ] && return 0   # no client — TCP is the best evidence we have
    if [ -n "$pass" ]; then
        out=$("$REDIS_CLI_BIN" -h "$host" -p "$port" -a "$pass" --no-auth-warning ping 2>&1)
    else
        out=$("$REDIS_CLI_BIN" -h "$host" -p "$port" ping 2>&1)
    fi
    case "$out" in
        PONG*)                return 0 ;;
        *NOAUTH*|*WRONGPASS*) REDIS_AUTH_WARN="$out"; return 0 ;;
        *)                    return 1 ;;
    esac
}

# ── .env rewriting ───────────────────────────────────────────────────────────

# Set one dotenv key, preserving everything else in the file.
#
# Rewrites EVERY occurrence rather than just the first: read_env_value() takes the
# LAST match, so a stale duplicate left further down the file would silently win
# and point the app at the target we just decided against.
set_env_value() {
    local key="$1" value="$2" file="$3" tmp
    tmp=$(mktemp) || return 1
    ENV_KEY="$key" ENV_VALUE="$value" awk '
        BEGIN { k = ENVIRON["ENV_KEY"]; v = ENVIRON["ENV_VALUE"]; seen = 0 }
        $0 ~ "^[[:space:]]*" k "=" { print k "=" v; seen = 1; next }
        { print }
        END { if (!seen) print k "=" v }
    ' "$file" > "$tmp" || { rm -f "$tmp"; return 1; }
    cat "$tmp" > "$file"   # write THROUGH the original inode: keeps perms, and keeps
    rm -f "$tmp"           # any editor/file watcher pointed at the same file
}

# Point .env at the selected target so the app follows the database.
#
# Without this the fallback is worthless: a laptop off the network would restore
# into the local stack while the app kept dialling the dev box. DB_DATABASE and
# REDIS_PREFIX are deliberately NOT touched — the schema name and the key
# namespace are properties of the app, not of the host it happens to live on.
#
# Never called when no dev box is configured: with a single target there is
# nothing to choose between, and rewriting .env would be pure damage.
sync_env_to_target() {
    local spec key desired current changed=0

    # REDIS_PASSWORD is withheld when the probe never actually authenticated —
    # a NOAUTH/WRONGPASS reply proves Redis is UP but proves nothing about the
    # password we hold. Writing it anyway silently replaces a working password
    # with a wrong (often empty) one, and the breakage only shows up later, in
    # the app, as a cache/queue failure nobody connects back to a DB sync.
    if [ "${REDIS_PASS_UNVERIFIED:-0}" = "1" ]; then
        print_warning "Leaving REDIS_PASSWORD in .env alone — the probe never authenticated, so the configured value is unproven."
    fi

    for spec in \
        "DB_HOST=$TARGET_DB_HOST" \
        "DB_PORT=$TARGET_DB_PORT" \
        "DB_USERNAME=$TARGET_DB_USER" \
        "DB_PASSWORD=$TARGET_DB_PASS" \
        "REDIS_HOST=$TARGET_REDIS_HOST" \
        "REDIS_PORT=$TARGET_REDIS_PORT" \
        "REDIS_PASSWORD=$TARGET_REDIS_PASS"
    do
        key="${spec%%=*}"
        desired="${spec#*=}"
        if [ "$key" = "REDIS_PASSWORD" ] && [ "${REDIS_PASS_UNVERIFIED:-0}" = "1" ]; then
            continue
        fi
        current="$(read_env_value "$key" "$ENV_FILE")"
        [ "$current" = "$desired" ] || changed=1
    done

    if [ "$changed" -eq 0 ]; then
        print_success ".env already points at ${TARGET_NAME} — no changes needed"
        return 0
    fi

    if [ "$SYNC_ENV_WRITE" != "1" ]; then
        print_warning ".env points somewhere else, but SYNC_ENV_WRITE=0 — not rewriting it."
        print_info "  Set these by hand or the app will not follow the database:"
        print_info "    DB_HOST=$TARGET_DB_HOST   DB_PORT=$TARGET_DB_PORT"
        print_info "    DB_USERNAME=$TARGET_DB_USER   DB_PASSWORD=$TARGET_DB_PASS"
        print_info "    REDIS_HOST=$TARGET_REDIS_HOST   REDIS_PORT=$TARGET_REDIS_PORT   REDIS_PASSWORD=$TARGET_REDIS_PASS"
        return 0
    fi

    cp -p "$ENV_FILE" "${ENV_FILE}.bak"
    print_status "📝 Repointing .env at ${TARGET_NAME} (previous saved to .env.bak)"

    for spec in \
        "DB_HOST=$TARGET_DB_HOST" \
        "DB_PORT=$TARGET_DB_PORT" \
        "DB_USERNAME=$TARGET_DB_USER" \
        "DB_PASSWORD=$TARGET_DB_PASS" \
        "REDIS_HOST=$TARGET_REDIS_HOST" \
        "REDIS_PORT=$TARGET_REDIS_PORT" \
        "REDIS_PASSWORD=$TARGET_REDIS_PASS"
    do
        key="${spec%%=*}"
        desired="${spec#*=}"
        if [ "$key" = "REDIS_PASSWORD" ] && [ "${REDIS_PASS_UNVERIFIED:-0}" = "1" ]; then
            continue
        fi
        current="$(read_env_value "$key" "$ENV_FILE")"
        if [ "$current" != "$desired" ]; then
            set_env_value "$key" "$desired" "$ENV_FILE" || {
                print_error "Failed to write $key to .env — restoring the backup."
                cp -p "${ENV_FILE}.bak" "$ENV_FILE"
                exit 1
            }
            print_info "  ${key}: ${current:-<empty>} → ${desired:-<empty>}"
        fi
    done

    # A cached config would keep the OLD credentials and send the migrate step at
    # the end of restore_local() to the wrong database entirely — clear it here,
    # not later, because that migrate runs before the existing optimize:clear.
    (cd "$SCRIPT_DIR" && $ARTISAN config:clear >/dev/null 2>&1) \
        || print_warning "config:clear failed — run it by hand if the app reads stale DB credentials"

    print_success ".env now points at ${TARGET_NAME}"
}

# ── Redis namespace guard ────────────────────────────────────────────────────
# Every app AND every worktree on this machine shares ONE Redis per target, so
# the only thing keeping each app and each worktree apart is
# REDIS_PREFIX. This script is where REDIS_HOST gets rewritten as you move
# between home and away, which makes it the right moment to prove the namespace
# is still unique — a blank or duplicated prefix silently merges two apps' cache,
# sessions and queues, and that failure is invisible until it is baffling.
#
# It only ever WARNS. A prefix collision is no reason to refuse a database
# restore, and picking a new prefix here would fight /worktree, which owns that
# value for worktrees.
check_redis_prefix() {
    local prefix other other_prefix other_host name collided=0

    # A project with no Redis at all gets no opinion from this function — an
    # empty prefix is only a hazard when something is actually sharing a Redis.
    grep -qE '^[[:space:]]*REDIS_(HOST|PREFIX)=' "$ENV_FILE" 2>/dev/null || return 0

    prefix="$(read_env_value REDIS_PREFIX "$ENV_FILE")"
    if [ -z "$prefix" ]; then
        print_warning "REDIS_PREFIX is empty — this app shares Redis keys with every other app on ${TARGET_REDIS_HOST}."
        print_info "  Set a unique one in .env, e.g.  REDIS_PREFIX=$(basename "$SCRIPT_DIR")_"
        return 0
    fi

    # Sibling apps sit next to this repo; worktrees live in <repo>-worktrees/.
    for other in "$SCRIPT_DIR"/../*/.env "$SCRIPT_DIR"/../"$(basename "$SCRIPT_DIR")"-worktrees/*/.env; do
        [ -f "$other" ] || continue
        [ "$other" -ef "$ENV_FILE" ] && continue
        other_prefix="$(read_env_value REDIS_PREFIX "$other")"
        other_host="$(read_env_value REDIS_HOST "$other")"
        [ "$other_prefix" = "$prefix" ] || continue
        [ "$other_host" = "$TARGET_REDIS_HOST" ] || continue
        name=$(basename "$(dirname "$other")")
        collided=1
        print_warning "REDIS_PREFIX '${prefix}' is ALSO used by '${name}' on ${TARGET_REDIS_HOST} — cache, sessions and queues will collide."
    done

    [ "$collided" -eq 0 ] && print_success "Redis namespace: ${prefix} (unique on ${TARGET_REDIS_HOST})"
    return 0
}

# ── Restore-target selection ─────────────────────────────────────────────────

# Pick where the dump gets restored: the shared dev box when it is fully up, the
# target from .env otherwise.
#
# BOTH MySQL and Redis must answer on the dev box for it to win. A half-up box —
# MySQL back after a version upgrade but Redis not yet — would otherwise be
# selected and leave the app with a working database and a dead cache/queue,
# which fails much less obviously, and much later, than simply not selecting it.
#
# With no dev box configured this collapses to "use .env", prints nothing, and
# never touches .env. That is the default for a fresh project.
select_restore_target() {
    if [ -z "$DEV_DB_HOST" ]; then
        TARGET_NAME="local (${LOCAL_DB_HOST})"
        TARGET_DB_HOST="$LOCAL_DB_HOST";   TARGET_DB_PORT="$LOCAL_DB_PORT"
        TARGET_DB_USER="$LOCAL_DB_USER";   TARGET_DB_PASS="$LOCAL_DB_PASS"
        TARGET_REDIS_HOST="$FALLBACK_REDIS_HOST"
        TARGET_REDIS_PORT="$FALLBACK_REDIS_PORT"
        TARGET_REDIS_PASS="$FALLBACK_REDIS_PASS"
        check_redis_prefix
        detect_remote_import
        return 0
    fi

    print_step "🎯 Selecting Restore Target"

    local dev_ok=0
    REDIS_AUTH_WARN=""
    REDIS_PASS_UNVERIFIED=0

    case "$SYNC_TARGET" in
        auto|dev)
            print_status "🔎 Probing dev box (${DEV_DB_HOST}) — MySQL :${DEV_DB_PORT} + Redis :${DEV_REDIS_PORT}..."
            local mysql_ok=1 redis_ok=1
            probe_mysql "$DEV_DB_HOST" "$DEV_DB_PORT" "$DEV_DB_USER" "$DEV_DB_PASS" || mysql_ok=0
            probe_redis "$DEV_REDIS_HOST" "$DEV_REDIS_PORT" "$DEV_REDIS_PASS" || redis_ok=0
            [ "$mysql_ok" -eq 1 ] && print_success "dev box MySQL OK" || print_warning "dev box MySQL unreachable"
            [ "$redis_ok" -eq 1 ] && print_success "dev box Redis OK" || print_warning "dev box Redis unreachable"
            [ -n "$REDIS_AUTH_WARN" ] && print_warning "dev box Redis answered but rejected the password: ${REDIS_AUTH_WARN}"
            [ "$mysql_ok" -eq 1 ] && [ "$redis_ok" -eq 1 ] && dev_ok=1
            ;;
        local)
            print_status "🔧 --target=local — skipping the dev-box probe"
            ;;
        *)
            print_error "Unknown target '${SYNC_TARGET}' — expected: auto, dev or local"
            exit 1
            ;;
    esac

    if [ "$SYNC_TARGET" = "dev" ] && [ "$dev_ok" -ne 1 ]; then
        print_error "--target=dev was forced, but the dev box is not fully reachable."
        print_info "  Drop the flag to fall back automatically."
        exit 1
    fi

    # A Redis that answered only with NOAUTH/WRONGPASS is proof the port is Redis,
    # not proof our password is right — so the value must not be written to .env.
    if [ -n "$REDIS_AUTH_WARN" ]; then
        REDIS_PASS_UNVERIFIED=1
    fi

    if [ "$dev_ok" -eq 1 ]; then
        TARGET_NAME="dev box (${DEV_DB_HOST})"
        TARGET_DB_HOST="$DEV_DB_HOST";       TARGET_DB_PORT="$DEV_DB_PORT"
        TARGET_DB_USER="$DEV_DB_USER";       TARGET_DB_PASS="$DEV_DB_PASS"
        TARGET_REDIS_HOST="$DEV_REDIS_HOST"; TARGET_REDIS_PORT="$DEV_REDIS_PORT"
        TARGET_REDIS_PASS="$DEV_REDIS_PASS"
    else
        TARGET_NAME="local (${FALLBACK_DB_HOST})"
        TARGET_DB_HOST="$FALLBACK_DB_HOST";        TARGET_DB_PORT="$FALLBACK_DB_PORT"
        TARGET_DB_USER="$FALLBACK_DB_USER";        TARGET_DB_PASS="$FALLBACK_DB_PASS"
        TARGET_REDIS_HOST="$FALLBACK_REDIS_HOST";  TARGET_REDIS_PORT="$FALLBACK_REDIS_PORT"
        TARGET_REDIS_PASS="$FALLBACK_REDIS_PASS"
        [ "$SYNC_TARGET" = "auto" ] && print_status "↩️  Falling back to the local stack"
    fi

    print_success "Target: ${TARGET_NAME}"

    # Everything downstream (mysql_local, drop_and_recreate_local_db, restore_local)
    # already speaks LOCAL_DB_*, so the winner is published under those names and
    # no other call site needs to know a choice was made. The schema name stays as
    # .env declared it — it is identical on both targets.
    LOCAL_DB_HOST="$TARGET_DB_HOST"
    LOCAL_DB_PORT="$TARGET_DB_PORT"
    LOCAL_DB_USER="$TARGET_DB_USER"
    LOCAL_DB_PASS="$TARGET_DB_PASS"

    sync_env_to_target

    # Runs AFTER the rewrite: the prefix must be unique on the Redis we just
    # pointed the app at, which is a different host depending on home/away.
    check_redis_prefix

    # Decided here, while the target is fresh, so restore_local() only has to read
    # the verdict. Never fatal — a failed probe just means the slower path.
    detect_remote_import
    if [ "$REMOTE_IMPORT_OK" = "1" ]; then
        print_success "Import path: on ${DEV_SSH_HOST} itself (ships the compressed dump, imports over the local socket)"
    fi
}

# ── Version report ───────────────────────────────────────────────────────────
# Printed on every run because the dev box is deliberately kept on the same
# MySQL major as production. A dump taken from a NEWER server can fail to import
# into an older one (auth plugins, utf8mb4_0900_* collations, functional indexes,
# and 8.4's removal of several 8.0 defaults) — and it fails MID-IMPORT, after the
# local database has already been dropped. Seeing both versions before the dump
# starts is what turns that into a decision instead of a surprise.

PROD_MYSQL_VERSION="n/a";  PROD_REDIS_VERSION="n/a"
LOCAL_MYSQL_VERSION="n/a"; LOCAL_REDIS_VERSION="n/a"

# NOTE on the explicit `return 0` closing this function and fetch_local_versions:
# both are invoked as `[ cond ] && fetch_…`, and the FINAL command of a `&&` list
# is NOT exempt from `set -e`. A function whose own last statement is a failing
# test — `[ -z "$V" ] && V="n/a"` returns 1 whenever V is non-empty, i.e. on the
# SUCCESS path — therefore returns 1 and kills the entire script, silently, right
# after the last thing it printed.
#
# That is exactly what happened on 06/08/2026: `./sync-db.sh` stopped dead after
# "Production database connection OK", never reaching the dump. `--restore`
# survived the same code only because with_prod=0 made the test the non-final
# (exempt) command. Keep these returns, and prefer `if` over trailing `&&` here.
fetch_prod_versions() {
    # `|| true` on every probe: under `set -euo pipefail` a bare `V=$(cmd | head -1)`
    # is NOT exempt from `set -e`, so one flaky ssh kills the run silently right after
    # its last print (17/08/2026, died just past "Production database connection OK").
    PROD_MYSQL_VERSION=$(remote_query 'SELECT VERSION()' 2>/dev/null | head -1 || true)
    if [ -z "$PROD_MYSQL_VERSION" ]; then PROD_MYSQL_VERSION="n/a"; fi

    # Three fallbacks, because production Redis usually requires auth we do not
    # hold: ask the live server, else the daemon binary, else the client binary.
    # A NOAUTH reply still proves Redis is up, so the binary version is a fair
    # answer rather than a failure.
    PROD_REDIS_VERSION=$(cat <<'REMOTE_VER' | remote_bash 2>/dev/null | tail -1 || true
v=$(redis-cli INFO server 2>/dev/null | tr -d '\r' | sed -n 's/^redis_version://p')
[ -z "$v" ] && v=$(redis-server --version 2>/dev/null | sed -n 's/.*v=\([0-9][0-9.]*\).*/\1/p')
[ -z "$v" ] && v=$(redis-cli --version 2>/dev/null | sed -n 's/.*[ ]\([0-9][0-9.]*\).*/\1/p')
echo "${v:-n/a}"
REMOTE_VER
)
    if [ -z "$PROD_REDIS_VERSION" ]; then PROD_REDIS_VERSION="n/a"; fi
    return 0
}

fetch_local_versions() {
    LOCAL_MYSQL_VERSION=$(mysql_local -N -B -e 'SELECT VERSION()' 2>/dev/null | head -1 || true)
    if [ -z "$LOCAL_MYSQL_VERSION" ]; then LOCAL_MYSQL_VERSION="n/a"; fi

    LOCAL_REDIS_VERSION="n/a"
    if [ -n "${REDIS_CLI_BIN:-}" ] && [ -n "${TARGET_REDIS_HOST:-}" ]; then
        local out
        if [ -n "$TARGET_REDIS_PASS" ]; then
            out=$("$REDIS_CLI_BIN" -h "$TARGET_REDIS_HOST" -p "$TARGET_REDIS_PORT" \
                  -a "$TARGET_REDIS_PASS" --no-auth-warning INFO server 2>/dev/null)
        else
            out=$("$REDIS_CLI_BIN" -h "$TARGET_REDIS_HOST" -p "$TARGET_REDIS_PORT" INFO server 2>/dev/null)
        fi
        out=$(printf '%s' "$out" | tr -d '\r' | sed -n 's/^redis_version://p' | head -1 || true)
        if [ -n "$out" ]; then LOCAL_REDIS_VERSION="$out"; fi
    fi
    return 0
}

# major.minor only — 8.0.36 vs 8.0.41 is not worth warning about, 8.0 vs 8.4 is.
version_series() { printf '%s' "$1" | cut -d. -f1,2; }

print_version_report() {
    local with_prod="$1"
    # TARGET_DB_HOST is set ONLY by select_restore_target(), which --dump-only
    # never runs. Gating on it keeps that mode from probing whatever stale host
    # .env still names — the very thing this feature exists to stop trusting.
    local with_local=0
    [ -n "${TARGET_DB_HOST:-}" ] && with_local=1

    [ "$with_prod" = "1" ]  && fetch_prod_versions
    [ "$with_local" = "1" ] && fetch_local_versions

    print_step "🧬 Version Report"
    printf "  ${DIM}%-30s %-14s %-14s${NC}\n" "HOST" "MYSQL" "REDIS" >&2
    if [ "$with_prod" = "1" ]; then
        printf "  %-30s %-14s %-14s\n" "production (${SSH_HOST})" "$PROD_MYSQL_VERSION" "$PROD_REDIS_VERSION" >&2
    fi
    if [ "$with_local" = "1" ]; then
        printf "  %-30s %-14s %-14s\n" "$TARGET_NAME" "$LOCAL_MYSQL_VERSION" "$LOCAL_REDIS_VERSION" >&2
    fi

    { [ "$with_prod" = "1" ] && [ "$with_local" = "1" ]; } || return 0

    local p l
    p=$(version_series "$PROD_MYSQL_VERSION"); l=$(version_series "$LOCAL_MYSQL_VERSION")
    if [ "$PROD_MYSQL_VERSION" != "n/a" ] && [ "$LOCAL_MYSQL_VERSION" != "n/a" ] && [ "$p" != "$l" ]; then
        echo "" >&2
        print_warning "MySQL series differ — production ${PROD_MYSQL_VERSION} vs ${TARGET_NAME} ${LOCAL_MYSQL_VERSION}"
        print_info "  A dump from the newer server may fail partway through the import, after the"
        print_info "  local database has already been dropped. Match the majors when you can."
    fi

    p=$(version_series "$PROD_REDIS_VERSION"); l=$(version_series "$LOCAL_REDIS_VERSION")
    if [ "$PROD_REDIS_VERSION" != "n/a" ] && [ "$LOCAL_REDIS_VERSION" != "n/a" ] && [ "$p" != "$l" ]; then
        print_warning "Redis series differ — production ${PROD_REDIS_VERSION} vs ${TARGET_NAME} ${LOCAL_REDIS_VERSION}"
    fi
}

# ── List backups ──────────────────────────────────────────────────────────────

list_backups() {
    print_step "📦 Available Backups"
    local files
    files=$(list_backup_files)
    if [ -n "$files" ]; then
        echo "" >&2
        # shellcheck disable=SC2086
        ls -lhS $files | awk '{printf "    📄 %-12s %s\n", $5, $NF}' >&2
        echo "" >&2
        LATEST=$(printf '%s\n' "$files" | head -1 || true)
        if [ -n "$LATEST" ]; then
            print_info "  Latest: $(basename "$LATEST")"
        fi
    else
        print_warning "No backups found in database/backups/"
    fi
}

if [ "$MODE" = "list" ]; then
    list_backups
    exit 0
fi

# ── Pre-flight checks ────────────────────────────────────────────────────────

print_banner
print_info "  📅 $(date '+%d/%m/%Y %H:%M')  |  Mode: ${MODE}"
echo "" >&2

print_step "🔍 Pre-flight Checks"

# Find MySQL binaries
MYSQL_BIN=$(find_local_binary "mysql") || {
    print_error "mysql binary not found — install a MySQL client (Herd/Homebrew on macOS, apt/system package on Linux)"
    exit 1
}
print_success "MySQL client found"

# mysqldump now runs on the SERVER, so a local copy is only needed as a courtesy
# check — never invoked. Missing it is not fatal.
MYSQLDUMP_BIN=$(find_local_binary "mysqldump" || true)
REDIS_CLI_BIN=$(find_local_binary "redis-cli" || true)

# zstd IS required locally: the restore path decompresses the downloaded archive.
ZSTD_BIN=$(command -v zstd 2>/dev/null || true)
if [ -z "$ZSTD_BIN" ]; then
    for candidate in /opt/homebrew/bin/zstd /usr/local/bin/zstd /usr/bin/zstd; do
        [ -x "$candidate" ] && ZSTD_BIN="$candidate" && break
    done
fi
if [ -z "$ZSTD_BIN" ]; then
    print_error "zstd not found locally — required to read the compressed dump"
    print_info "  Install it:  brew install zstd"
    exit 1
fi
print_success "zstd found ($("$ZSTD_BIN" --version 2>&1 | grep -oE 'v[0-9.]+' | head -1))"

# Choose the restore target and confirm the winner actually accepts us (only needed
# for the modes that write to a local database — a --dump-only run never touches
# one, so it must not be blocked by both targets being down).
if [ "$MODE" = "full" ] || [ "$MODE" = "restore" ]; then
    select_restore_target

    print_status "🏠 Verifying ${TARGET_NAME} accepts these credentials..."
    # probe_mysql omits MYSQL_PWD entirely when the password is empty, which is
    # what a passwordless local root needs; the password never reaches argv, where
    # `ps` would expose it to every other local user.
    if ! probe_mysql "$LOCAL_DB_HOST" "$LOCAL_DB_PORT" "$LOCAL_DB_USER" "$LOCAL_DB_PASS"; then
        print_error "Cannot connect to ${TARGET_NAME} as '${LOCAL_DB_USER}' (${LOCAL_DB_HOST}:${LOCAL_DB_PORT})"
        if [ "$LOCAL_DB_HOST" = "$FALLBACK_DB_HOST" ]; then
            print_info "  The local fallback is not answering either — start MySQL locally."
        else
            print_info "  Override the credentials in .db-sync.env (DEV_DB_USER / DEV_DB_PASS)."
        fi
        exit 1
    fi
    print_success "Local database connection OK (${LOCAL_DB_NAME}@${LOCAL_DB_HOST}:${LOCAL_DB_PORT})"
fi

# ── SSH Connection & Tunnel ──────────────────────────────────────────────────

# No SSH tunnel any more. The dump is produced and compressed ON the server, so
# nothing needs to reach the production MySQL port from this machine — which also
# means no random local port, no backgrounded `ssh -f -N`, and no PID to reap.
connect_production() {
    local key_opt=""
    local auth_method="🔑 1Password SSH agent"
    if [ -n "${SSH_KEY:-}" ]; then
        key_opt="-i $SSH_KEY"
        auth_method="🔑 Key: $SSH_KEY"
    fi

    print_step "🌐 Connecting to Production Server"
    print_status "Server: ${SSH_USER}@${SSH_HOST}"
    print_status "Auth: ${auth_method}"

    print_status "🤝 Testing SSH connection..."
    # Uses ssh_base_opts so THIS call becomes the shared ControlMaster — every
    # later remote_bash/remote_query then rides it instead of opening its own
    # connection. Keep it first, and keep it on ssh_base_opts.
    # shellcheck disable=SC2046,SC2086
    if ! ssh $(ssh_base_opts) -o BatchMode=yes \
            $key_opt \
            -p "$SSH_PORT" \
            "${SSH_USER}@${SSH_HOST}" "echo ok" &>/dev/null; then
        print_error "SSH connection failed!"
        print_info "  Check that 1Password SSH agent is running and the key is authorized"
        print_info "  If 443 answers but 22 times out, port 22 is being rate-limited —"
        print_info "  wait ~60s and retry (see the ControlMaster note near SSH_CONTROL_PATH)."
        exit 1
    fi
    print_success "SSH connection OK — server is reachable"

    # The server must have zstd + mysqldump, since both now run there.
    print_status "🧰 Checking remote tooling..."
    local missing
    # `|| true`: a probe that dies on an ssh blip must read as "nothing missing",
    # not kill the whole run under `set -e` — the very next steps re-test the
    # connection and fail loudly if the server is genuinely unreachable.
    missing=$(echo 'for b in mysqldump zstd; do command -v $b >/dev/null 2>&1 || echo $b; done' | remote_bash 2>/dev/null || true)
    if [ -n "$missing" ]; then
        print_error "Missing on production: $(echo "$missing" | tr '\n' ' ')"
        print_info "  Install with:  sudo apt-get install -y zstd"
        exit 1
    fi
    print_success "Remote mysqldump + zstd present"

    print_status "🗄️ Testing production database connection..."
    if [ "$(remote_query 'SELECT 1')" != "1" ]; then
        print_error "Cannot reach the production database as '${PROD_DB_USER}'"
        print_info "  Check DB credentials in .db-sync.env"
        exit 1
    fi
    print_success "Production database connection OK (${PROD_DB_NAME})"
}

# ── Dump production database ─────────────────────────────────────────────────

dump_production() {
    local timestamp
    timestamp=$(date "+%Y%m%d-%H%M%S")
    local dump_file="$BACKUP_DIR/${APP_NAME}-${timestamp}.sql.zst"
    # Dump to a .partial sidecar and publish to the real name only after the
    # archive is verified complete — a truncated dump must never be restorable.
    local tmp_file="${dump_file}.partial"
    local dump_err="$BACKUP_DIR/.last-dump.err"
    PARTIAL_FILE="$tmp_file"   # tracked at script scope so cleanup() can remove it on interrupt/failure

    print_step "📥 Downloading Production Database"

    # Build exclude options (quoted for safe injection into the remote script)
    local exclude_opts=""
    if [ -n "${EXCLUDE_TABLES:-}" ]; then
        IFS=',' read -ra TABLES <<< "$EXCLUDE_TABLES"
        for table in "${TABLES[@]}"; do
            exclude_opts="$exclude_opts $(printf '%q' "--ignore-table=${PROD_DB_NAME}.${table}")"
        done
        print_status "🚫 Excluding tables: ${EXCLUDE_TABLES}"
    fi

    local db_size table_count
    db_size=$(remote_query "SELECT ROUND(SUM(data_length + index_length) / 1024 / 1024, 1) FROM information_schema.tables WHERE table_schema = '${PROD_DB_NAME}';" || echo "?")
    table_count=$(remote_query "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = '${PROD_DB_NAME}';" || echo "?")
    [ -z "$db_size" ] && db_size="?"
    [ -z "$table_count" ] && table_count="?"

    print_info "  📊 Database: ${PROD_DB_NAME} (${table_count} tables, ~${db_size} MB)"
    print_info "  🗜️  Compressing on the server — only the compressed stream crosses the network"
    echo "" >&2
    print_status "☕ Dumping database — this takes a few minutes for ~${db_size} MB, grab a coffee..."

    local start_time
    start_time=$(date +%s)

    # Start background progress monitor
    (
        while true; do
            sleep 10
            if [ -f "$tmp_file" ]; then
                local_size=$(du -h "$tmp_file" 2>/dev/null | cut -f1)
                elapsed_now=$(( $(date +%s) - start_time ))
                if [ "$elapsed_now" -ge 60 ]; then
                    progress_time="$((elapsed_now / 60))m $((elapsed_now % 60))s"
                else
                    progress_time="${elapsed_now}s"
                fi
                printf "  ${DIM}   ⏳ %s compressed (%s elapsed)${NC}\r\n" "$local_size" "$progress_time" >&2
            fi
        done
    ) &
    PROGRESS_PID=$!

    # The dump + compression both execute on the server; ssh's stdout carries the
    # already-compressed stream straight into the local .partial file.
    #
    # `nice`/`ionice` keep this off the critical path of a live production box.
    # --no-tablespaces avoids needing the PROCESS privilege (MySQL 8.0.21+),
    # --hex-blob protects binary columns, --events preserves scheduled events
    # (previously dropped silently), and pinning utf8mb4 stops charset mangling.
    # --column-statistics is deliberately NOT passed: it is a client-side flag
    # that older server-side mysqldump builds reject outright.
    #
    # --net-buffer-length is what decides how much data each INSERT carries, and
    # the default is only ~1 MB. Since the importing client waits for the server's
    # OK after every statement, that default turns every megabyte of dump into
    # another blocking round trip — on a 3.3 GB dump, ~3,300 of them. 16 MB is the
    # documented ceiling for the flag and sits safely under the usual 64 MB
    # max_allowed_packet, so it cuts the round trips ~16x. --no-autocommit wraps
    # each table in one transaction instead of committing per statement, which is
    # the other half of the InnoDB import cost.
    #
    # MYSQLDUMP_OPTS is a FULL override of this list. The defaults are MySQL 8
    # flags; a MariaDB server's mysqldump aborts on --set-gtid-purged and
    # --no-tablespaces, so point MYSQLDUMP_OPTS at a trimmed list there.
    # MYSQLDUMP_EXTRA_OPTS is additive and stays empty by default.
    local dump_opts="${MYSQLDUMP_OPTS:---single-transaction --quick --no-tablespaces --routines --triggers --events --hex-blob --set-gtid-purged=OFF --default-character-set=utf8mb4 --max-allowed-packet=64M --net-buffer-length=16M --no-autocommit}"

    {
        remote_creds_preamble
        cat <<REMOTE_DUMP
nice -n 10 ionice -c2 -n7 mysqldump --defaults-extra-file="\$DEFAULTS_FILE" \\
    ${dump_opts} \\
    ${MYSQLDUMP_EXTRA_OPTS} \\
    ${exclude_opts} \\
    $(printf '%q' "$PROD_DB_NAME") \\
  | zstd -${ZSTD_LEVEL} -T${ZSTD_THREADS} -q
REMOTE_DUMP
    } | remote_bash 2>"$dump_err" > "$tmp_file"

    # Stop progress monitor
    kill "$PROGRESS_PID" 2>/dev/null
    wait "$PROGRESS_PID" 2>/dev/null || true
    PROGRESS_PID=""

    # Verify the archive is a complete zstd frame before publishing it under the
    # canonical backup name. (set -e + pipefail already abort above if mysqldump
    # itself failed, so a surviving .partial is never promoted to a real backup.)
    if [ ! -s "$tmp_file" ] || ! verify_archive "$tmp_file"; then
        print_error "Production dump is empty or corrupt — backup NOT written."
        [ -s "$dump_err" ] && tail -n 5 "$dump_err" >&2
        rm -f "$tmp_file"
        exit 1
    fi

    # Codec integrity alone does not prove the SQL is complete — a dump aborted
    # mid-stream still compresses into a perfectly valid frame. mysqldump's
    # trailer is the only evidence the dump actually ran to the end.
    if ! "$ZSTD_BIN" -dc "$tmp_file" | tail -c 4096 | grep -q "Dump completed"; then
        print_error "Dump is missing its 'Dump completed' trailer — truncated. NOT written."
        [ -s "$dump_err" ] && tail -n 5 "$dump_err" >&2
        rm -f "$tmp_file"
        exit 1
    fi
    mv -f "$tmp_file" "$dump_file"   # atomic publish — only complete dumps get the real name
    PARTIAL_FILE=""                  # published — nothing for cleanup() to remove

    local end_time elapsed
    end_time=$(date +%s)
    elapsed=$((end_time - start_time))

    local file_size
    file_size=$(du -h "$dump_file" | cut -f1)

    # Format elapsed time nicely
    local time_str
    if [ "$elapsed" -ge 60 ]; then
        local mins=$((elapsed / 60))
        local secs=$((elapsed % 60))
        time_str="${mins}m ${secs}s"
    else
        time_str="${elapsed}s"
    fi

    echo "" >&2
    print_success "Dump complete! ⏱️  ${time_str}"
    print_success "Saved: database/backups/$(basename "$dump_file") (${file_size} compressed)"

    # Return ONLY the file path on stdout (all other output goes to stderr)
    echo "$dump_file"
}

# ── Lock-safe DROP + CREATE of the local database ────────────────────────────
# MySQL's default lock_wait_timeout is 31536000s (one YEAR), so a DROP DATABASE
# that is blocked by a schema metadata lock waits essentially forever, printing
# nothing. Any single idle connection to the target DB — the dev site, a
# Horizon worker, an old `tinker`, a GUI client — is enough to hold that lock.
#
# Observed 27/07/2026: the sync stalled at "Dropping and recreating local
# database..." for ~37 minutes, blocked by a leftover idle connection. It looked
# identical to a slow import, and the DB was left half-populated.
#
# Fix: bound the wait so the DROP FAILS FAST instead of hanging, then clear the
# blockers and retry — idle connections first, all connections second. Only
# connections attached to the DB we are about to drop are ever killed, and that
# DB is disposable by definition, so nothing recoverable is lost.

DROP_LOCK_TIMEOUT="${DROP_LOCK_TIMEOUT:-15}"

# Wrapper for local mysql calls. MYSQL_PWD keeps the password out of the process
# list (and silences the "password on the command line is insecure" warning).
mysql_local() {
    if [ -n "$LOCAL_DB_PASS" ]; then
        MYSQL_PWD="$LOCAL_DB_PASS" "$MYSQL_BIN" \
            -h "$LOCAL_DB_HOST" -P "$LOCAL_DB_PORT" -u "$LOCAL_DB_USER" \
            --connect-timeout=10 "$@"
    else
        "$MYSQL_BIN" \
            -h "$LOCAL_DB_HOST" -P "$LOCAL_DB_PORT" -u "$LOCAL_DB_USER" \
            --connect-timeout=10 "$@"
    fi
}

show_local_db_connections() {
    mysql_local -e "
        SELECT id, user, host, command, time, state
        FROM information_schema.processlist
        WHERE db = '$1' AND id <> CONNECTION_ID()
        ORDER BY time DESC;" 2>/dev/null || true
}

# $1 = database, $2 = "idle" (only Sleep) or "all". Echoes the number killed.
# Killing own-user threads needs no extra privilege, so this works unprivileged.
kill_local_db_connections() {
    local db="$1" scope="$2" id killed=0 filter=""
    [ "$scope" = "idle" ] && filter="AND command = 'Sleep'"
    while IFS= read -r id; do
        [ -z "$id" ] && continue
        mysql_local -e "KILL ${id};" >/dev/null 2>&1 || true
        killed=$((killed + 1))
    done < <(mysql_local -N -B -e "
        SELECT id FROM information_schema.processlist
        WHERE db = '${db}' AND id <> CONNECTION_ID() ${filter};" 2>/dev/null)
    printf '%s' "$killed"
}

drop_and_recreate_local_db() {
    local db="$LOCAL_DB_NAME"
    local sql="SET SESSION lock_wait_timeout = ${DROP_LOCK_TIMEOUT};
               DROP DATABASE IF EXISTS \`${db}\`;
               CREATE DATABASE \`${db}\` CHARACTER SET ${LOCAL_DB_CHARSET} COLLATE ${LOCAL_DB_COLLATION};"
    local attempt killed

    for attempt in 1 2 3; do
        if mysql_local -e "$sql" 2>/dev/null; then
            if [ "$attempt" -gt 1 ]; then
                print_success "Fresh database created, charset ${LOCAL_DB_CHARSET} (after clearing lock holders)"
            else
                print_success "Fresh database created (charset ${LOCAL_DB_CHARSET}, collation ${LOCAL_DB_COLLATION})"
            fi
            return 0
        fi

        if [ "$attempt" -eq 1 ]; then
            print_warning "DROP DATABASE is blocked — waited ${DROP_LOCK_TIMEOUT}s for a schema metadata lock."
            print_info "  Connections attached to '${db}':"
            show_local_db_connections "$db"
            print_status "🔌 Killing IDLE connections to '${db}'..."
            killed=$(kill_local_db_connections "$db" idle)
        else
            print_warning "Still blocked — killing ALL remaining connections to '${db}'."
            killed=$(kill_local_db_connections "$db" all)
        fi
        print_info "  Killed ${killed} connection(s) — retrying DROP."
    done

    print_error "Could not DROP '${db}' — something keeps re-acquiring a lock on it."
    print_info "  Stop the local app + workers, then re-run the restore:"
    print_info "    pkill -f 'artisan horizon'"
    print_info "    ./sync-db.sh --restore=$(basename "${1:-<backup.sql.zst>}")"
    return 1
}

# ── Import dispatch ──────────────────────────────────────────────────────────
# Two ways to get the SQL into the target, picked by detect_remote_import():
#
#   1. ON the dev box — ship the compressed archive there, decompress and import
#      over its local MySQL socket. Only the compressed archive crosses the
#      network, and the round trips become socket-local.
#   2. Over the network — the default path, and the right one for a loopback
#      target, a machine without the dev box's SSH key, or REMOTE_IMPORT=0.
#
# Returns non-zero on failure so the caller can drop and retry cleanly.
run_import() {
    local dump_file="$1" decomp remote_file size_h
    decomp=$(decompress_cmd "$dump_file") || return 1

    if [ "$REMOTE_IMPORT_OK" = "1" ]; then
        size_h=$(du -h "$dump_file" | cut -f1)
        remote_file="${DEV_TMP_DIR}/$(basename "$dump_file")"
        print_info "  🚚 Shipping ${size_h} to ${DEV_SSH_HOST} (compressed — not the uncompressed SQL)..."
        dev_send_dump "$dump_file" "$remote_file" || return 1
        print_info "  🔌 Importing on ${DEV_SSH_HOST} over its local MySQL socket..."
        dev_import_dump "$remote_file" || return 1
        return 0
    fi

    # --max-allowed-packet must accommodate the larger statements the dump now
    # emits (--net-buffer-length=16M); the client default is only 16 MB.
    $decomp "$dump_file" | MYSQL_PWD="$LOCAL_DB_PASS" "$MYSQL_BIN" \
        -h "$LOCAL_DB_HOST" -P "$LOCAL_DB_PORT" -u "$LOCAL_DB_USER" \
        --max-allowed-packet=64M "$LOCAL_DB_NAME" || return 1
    return 0
}

# ── Restore to local database ────────────────────────────────────────────────

restore_local() {
    local dump_file="$1"

    if [ ! -f "$dump_file" ]; then
        print_error "Backup file not found: $dump_file"
        exit 1
    fi

    # Validate the archive BEFORE the irreversible DROP. A 0-byte, corrupt, or
    # truncated dump must never be allowed to wipe and half-fill the local DB.
    if [ ! -s "$dump_file" ]; then
        print_error "Backup is empty: $(basename "$dump_file")"
        exit 1
    fi
    # Dispatch on extension so pre-existing *.sql.gz backups stay restorable
    # alongside the new *.sql.zst ones.
    local decomp
    if ! decomp=$(decompress_cmd "$dump_file"); then
        print_error "Unrecognised backup format (expected .sql.zst or .sql.gz): $(basename "$dump_file")"
        exit 1
    fi
    # ONE decompression pass covers both gates. The codec verifies its own
    # checksums while streaming (zstd XXH64 / gzip CRC32) and, under `pipefail`,
    # reports corruption as a failed pipeline — so the separate `zstd -t` that
    # used to run first was a second full pass proving the same thing. The
    # trailer check is the part that adds information: mysqldump's
    # "-- Dump completed" line is the only evidence the dump ran to the end,
    # since a stream cut short still compresses into a valid frame.
    #
    # An older revision warned that this "takes about a minute". That was wrong:
    # a 316 MB archive decompresses in ~3s (measured 15/08/2026). Decompression
    # was never the slow part; the import was. Do not reintroduce a wait warning.
    local _size_h
    _size_h=$(du -h "$dump_file" | cut -f1)
    print_status "🔎 Verifying archive integrity (${_size_h} compressed)..."
    if ! $decomp "$dump_file" 2>/dev/null | tail -c 4096 | grep -q "Dump completed"; then
        print_error "Backup is corrupt or truncated — refusing to drop local DB: $(basename "$dump_file")"
        print_info "  Either the archive failed its checksum, or the dump has no"
        print_info "  'Dump completed' trailer (cut short mid-stream)."
        exit 1
    fi
    print_success "Archive verified — complete and restorable"

    local file_size
    file_size=$(du -h "$dump_file" | cut -f1)

    print_step "📤 Restoring to Local Database"
    print_info "  📄 Source: $(basename "$dump_file") (${file_size})"
    print_info "  🏠 Target: ${LOCAL_DB_NAME}@${LOCAL_DB_HOST}:${LOCAL_DB_PORT}"

    echo "" >&2
    # The local dev DB is disposable — every sync drops and reimports it, and the
    # dump above already passed its integrity gate (non-empty, gzip -t, "Dump
    # completed" trailer), so a corrupt archive can never reach this point. The
    # drop therefore runs UNATTENDED by default: kick off the sync and walk away.
    # Set SYNC_CONFIRM=1 to restore the old type-the-database-name confirmation.
    if [ "${SYNC_CONFIRM:-0}" = "1" ] && [ -t 0 ]; then
        print_warning "About to DROP and overwrite local DB '${LOCAL_DB_NAME}' on ${LOCAL_DB_HOST}:${LOCAL_DB_PORT} — this is irreversible."
        printf "  Type the database name to confirm: " >&2
        read -r _confirm
        if [ "$_confirm" != "$LOCAL_DB_NAME" ]; then
            print_error "Confirmation mismatch — aborted."
            exit 1
        fi
    else
        print_warning "Overwriting local dev DB '${LOCAL_DB_NAME}' on ${LOCAL_DB_HOST}:${LOCAL_DB_PORT} (auto-confirmed — set SYNC_CONFIRM=1 to be prompted)."
    fi
    # DROP and import are retried as ONE unit. A half-finished import leaves the
    # schema partly populated, so the only safe retry is to start over — which is
    # cheap here (the archive is already local and verified) and is exactly what
    # turns a transient link blip into a delay instead of a failed run.
    local start_time end_time elapsed attempt import_ok=0
    for attempt in 1 2; do
        print_status "🗑️ Dropping and recreating local database..."
        # Lock-safe: fails fast on a metadata lock and clears the holders instead
        # of hanging forever. See drop_and_recreate_local_db() above.
        drop_and_recreate_local_db "$dump_file" || exit 1

        start_time=$(date +%s)
        print_status "📦 Importing data — hang tight, this takes a bit for large databases..."

        # Progress monitor. Elapsed time alone cannot distinguish "working" from
        # "wedged", so it also reports how many tables exist in the target: the
        # dump creates them in order, making the count real progress.
        (
            while true; do
                sleep 10
                elapsed_now=$(( $(date +%s) - start_time ))
                if [ "$elapsed_now" -ge 60 ]; then
                    progress_time="$((elapsed_now / 60))m $((elapsed_now % 60))s"
                else
                    progress_time="${elapsed_now}s"
                fi
                tables=$(mysql_local -N -B -e "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = '${LOCAL_DB_NAME}';" 2>/dev/null | head -1 || true)
                printf "  ${DIM}   ⏳ importing... %s tables (%s elapsed)${NC}\r\n" "${tables:-?}" "$progress_time" >&2
            done
        ) &
        IMPORT_PROGRESS_PID=$!

        if run_import "$dump_file"; then
            import_ok=1
        fi

        # Stop progress monitor
        kill "$IMPORT_PROGRESS_PID" 2>/dev/null
        wait "$IMPORT_PROGRESS_PID" 2>/dev/null || true
        IMPORT_PROGRESS_PID=""

        [ "$import_ok" = "1" ] && break
        if [ "$attempt" -eq 1 ]; then
            echo "" >&2
            print_warning "Import failed part-way — retrying once from a clean database."
            print_info "  A dropped connection mid-import (ERROR 2013) is almost always the link,"
            print_info "  not the data: the archive already passed its integrity gate above."
        fi
    done

    if [ "$import_ok" != "1" ]; then
        echo "" >&2
        print_error "Import failed twice — local database is incomplete."
        print_info "  Re-run just the restore (no re-download):"
        print_info "    ./sync-db.sh --restore=$(basename "$dump_file")"
        print_info "  Force the slower over-the-network path with:  REMOTE_IMPORT=0 ./sync-db.sh --restore"
        exit 1
    fi

    end_time=$(date +%s)
    elapsed=$((end_time - start_time))

    # Format elapsed time nicely
    local time_str
    if [ "$elapsed" -ge 60 ]; then
        local mins=$((elapsed / 60))
        local secs=$((elapsed % 60))
        time_str="${mins}m ${secs}s"
    else
        time_str="${elapsed}s"
    fi

    print_success "Import complete! ⏱️  ${time_str}"

    # Run migrations to apply any pending local changes
    print_step "🔄 Post-Restore Tasks"
    print_status "🛤️ Running pending migrations..."
    cd "$SCRIPT_DIR"
    if $ARTISAN migrate --force; then
        print_success "Migrations applied"
    else
        print_error "Migrations FAILED — local schema may be inconsistent. Review the output above."
        exit 1
    fi

    print_status "🧹 Clearing caches..."
    $ARTISAN optimize:clear >/dev/null 2>&1 || true
    print_success "Caches cleared"
}

# ── Backup retention ─────────────────────────────────────────────────────────
# Keep only the $KEEP_BACKUPS most-recent compressed dumps; delete older ones.
# bash 3.2-safe (macOS default): no mapfile, uses a process-substitution loop.

prune_backups() {
    local keep="${KEEP_BACKUPS:-3}"
    local count=0
    local f
    while IFS= read -r f; do
        count=$((count + 1))
        if [ "$count" -gt "$keep" ]; then
            rm -f "$f"
            print_info "  🗑️  Pruned old backup: $(basename "$f")"
        fi
    done < <(list_backup_files)
    if [ "$count" -gt 0 ]; then
        local kept=$count
        [ "$count" -gt "$keep" ] && kept=$keep
        print_success "Backup retention: keeping ${kept} of ${count} (KEEP_BACKUPS=${keep})"
    fi
}

# ── Main execution ───────────────────────────────────────────────────────────

case $MODE in
    "full")
        connect_production
        # Both sides are known and reachable by now, so the comparison is printed
        # BEFORE the dump — the point is to catch a version gap while backing out
        # is still free, not to explain a failed import afterwards.
        print_version_report 1
        DUMP_FILE=$(dump_production)
        restore_local "$DUMP_FILE"
        # Retain the compressed dump as a local backup (rotation keeps the last N).
        print_success "Backup retained: database/backups/$(basename "$DUMP_FILE")"
        prune_backups
        ;;
    "dump-only")
        connect_production
        # Nothing local is written in this mode, so no target was selected and
        # the report prints production only (see the gate in print_version_report).
        print_version_report 1
        dump_production
        prune_backups
        ;;
    "restore")
        if [ -n "$BACKUP_FILE" ]; then
            # Specific file
            if [[ "$BACKUP_FILE" != /* ]]; then
                BACKUP_FILE="$BACKUP_DIR/$BACKUP_FILE"
            fi
        else
            # Latest backup, whichever codec it uses
            BACKUP_FILE=$(list_backup_files | head -1 || true)
            if [ -z "$BACKUP_FILE" ]; then
                print_error "No backups found in database/backups/"
                print_info "  Run './sync-db.sh' first to download from production"
                exit 1
            fi
        fi
        # Offline mode — no SSH is opened, so production versions are unknown.
        print_version_report 0
        restore_local "$BACKUP_FILE"
        ;;
esac

# Derive the dev URL from config rather than assuming a Herd .test hostname:
# DEV_URL override > the app's APP_URL > https://<app>.test as a last resort.
DEV_URL_RESOLVED="${DEV_URL:-$(read_env_value APP_URL "$ENV_FILE")}"
DEV_URL_RESOLVED="${DEV_URL_RESOLVED:-https://${APP_NAME}.test}"

echo "" >&2
echo -e "  ${GREEN}${BOLD}🎉 All done! Your local database is fresh and ready.${NC}" >&2
echo -e "  ${DIM}   Open ${DEV_URL_RESOLVED} and get to work!${NC}" >&2
echo "" >&2
