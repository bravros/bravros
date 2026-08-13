#!/bin/bash

# ============================================================================
# Production Database Sync Script
# ============================================================================
# Downloads a production database via a secure SSH tunnel and restores it
# locally. Credentials live in .db-sync.env (gitignored); local DB connection
# details are read from the Laravel .env.
#
# SCOPE: MySQL / MariaDB only. The dump (mysqldump), the size/count probes
# (information_schema), and the DROP/CREATE DATABASE statements are all
# MySQL-specific. The script fails fast if the Laravel app's DB_CONNECTION is
# not mysql/mariadb. pgsql/sqlite support is a future extension.
#
# Usage:
#   ./sync-db.sh              # Full sync (dump + restore)
#   ./sync-db.sh --dump-only  # Only download, don't restore
#   ./sync-db.sh --restore    # Restore latest backup without downloading
#   ./sync-db.sh --list       # List available backups
#
# Useful environment overrides (also settable in .db-sync.env):
#   SYNC_CONFIRM=1            # Prompt to type the DB name before the DROP
#   KEEP_BACKUPS=N            # How many compressed backups to retain (default 3)
#   MYSQLDUMP_EXTRA_OPTS=...  # Extra mysqldump flags (e.g. --column-statistics=0)
#   ARTISAN_CMD="php artisan" # Override the artisan runner (e.g. "herd php artisan")
#   DEV_URL=...               # Override the dev URL printed at the end
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

for arg in "$@"; do
    case $arg in
        --dump-only)  MODE="dump-only" ;;
        --restore)    MODE="restore" ;;
        --restore=*)  MODE="restore"; BACKUP_FILE="${arg#*=}" ;;
        --list)       MODE="list" ;;
        --help|-h)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --dump-only       Download production DB but don't restore locally"
            echo "  --restore         Restore the latest backup to local DB"
            echo "  --restore=FILE    Restore a specific backup file"
            echo "  --list            List available backups"
            echo "  --help            Show this help"
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
    # (it is never restorable and never matches the *.sql.gz prune glob).
    if [ -n "$PARTIAL_FILE" ]; then
        rm -f "$PARTIAL_FILE" 2>/dev/null || true
    fi
    if [ -n "$TUNNEL_PID" ] && kill -0 "$TUNNEL_PID" 2>/dev/null; then
        echo "" >&2
        print_status "🔒 Closing SSH tunnel..."
        kill "$TUNNEL_PID" 2>/dev/null
        wait "$TUNNEL_PID" 2>/dev/null || true
        print_success "Tunnel closed"
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
# Local DB credentials come from the Laravel .env (DB_HOST, DB_PORT, DB_DATABASE,
# DB_USERNAME, DB_PASSWORD). The .db-sync.env file is only for remote/SSH config.

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
    raw=$(grep -E "^[[:space:]]*${key}=" "$file" | grep -v '^[[:space:]]*#' | tail -1 | sed -E "s/^[[:space:]]*${key}=//")
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
# from the Laravel .env and fail loudly for any other engine rather than emitting
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

# Local DB charset/collation for the CREATE DATABASE. Read from the Laravel .env
# (Laravel exposes DB_CHARSET / DB_COLLATION) so the recreated schema matches the
# app's expected collation; .db-sync.env may override; sane utf8mb4 default last.
LOCAL_DB_CHARSET="${LOCAL_DB_CHARSET:-$(read_env_value DB_CHARSET "$ENV_FILE")}"
LOCAL_DB_CHARSET="${LOCAL_DB_CHARSET:-utf8mb4}"
LOCAL_DB_COLLATION="${LOCAL_DB_COLLATION:-$(read_env_value DB_COLLATION "$ENV_FILE")}"
LOCAL_DB_COLLATION="${LOCAL_DB_COLLATION:-utf8mb4_unicode_ci}"

# Validate .env-derived local config
for var in LOCAL_DB_HOST LOCAL_DB_NAME LOCAL_DB_USER; do
    if [ -z "${!var:-}" ]; then
        mapped_key="DB_HOST"
        case "$var" in
            LOCAL_DB_NAME) mapped_key="DB_DATABASE" ;;
            LOCAL_DB_USER) mapped_key="DB_USERNAME" ;;
        esac
        print_error "Missing required key in .env: $mapped_key"
        exit 1
    fi
done

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
PROD_DB_HOST="${PROD_DB_HOST:-127.0.0.1}"
PROD_DB_PORT="${PROD_DB_PORT:-3306}"
LOCAL_DB_PORT="${LOCAL_DB_PORT:-3306}"
APP_NAME="${APP_NAME:-$(basename "$SCRIPT_DIR")}"
KEEP_BACKUPS="${KEEP_BACKUPS:-3}"          # how many compressed backups to retain (rotation)
# Extra mysqldump flags, default OFF for portability. MariaDB and older mysqldump
# clients abort on unknown flags, so nothing engine-specific is baked in. Example
# opt-in for a MySQL-8 client dumping a 5.7 server: MYSQLDUMP_EXTRA_OPTS="--column-statistics=0"
MYSQLDUMP_EXTRA_OPTS="${MYSQLDUMP_EXTRA_OPTS:-}"
# Artisan runner. Plain `php artisan` works on Herd (php is on PATH) and on Linux;
# override to e.g. "herd php artisan" or "./vendor/bin/sail artisan" in .db-sync.env.
ARTISAN="${ARTISAN_CMD:-php artisan}"

# Expand tilde in SSH_KEY if set
if [ -n "$SSH_KEY" ]; then
    SSH_KEY="${SSH_KEY/#\~/$HOME}"
fi

# Create backup directory
mkdir -p "$BACKUP_DIR"

# ── Ensure secrets + backups are gitignored ──────────────────────────────────

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

# ── Find local mysql binary ──────────────────────────────────────────────────

find_mysql_binary() {
    local binary="$1"
    local _os
    _os="$(uname -s)"

    if [ "$_os" = "Darwin" ]; then
        # macOS: check Herd paths first, then Homebrew
        local herd_paths=(
            "$HOME/Library/Application Support/Herd/config/mysql"
            "$HOME/Library/Application Support/Herd/bin"
        )

        for herd_path in "${herd_paths[@]}"; do
            if [ -d "$herd_path" ]; then
                local found
                found=$(find "$herd_path" -name "$binary" -type f 2>/dev/null | head -1)
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
        # Linux: check system/apt paths
        for path in /usr/bin/$binary /usr/local/bin/$binary /usr/local/mysql/bin/$binary; do
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

# ── List backups ──────────────────────────────────────────────────────────────

list_backups() {
    print_step "📦 Available Backups"
    if ls "$BACKUP_DIR"/${APP_NAME}-*.sql.gz 1>/dev/null 2>&1; then
        echo "" >&2
        ls -lhS "$BACKUP_DIR"/${APP_NAME}-*.sql.gz | awk '{printf "    📄 %-12s %s\n", $5, $NF}' >&2
        echo "" >&2
        LATEST=$(ls -t "$BACKUP_DIR"/${APP_NAME}-*.sql.gz 2>/dev/null | head -1)
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
MYSQL_BIN=$(find_mysql_binary "mysql") || {
    print_error "mysql binary not found — install MySQL (via Herd/Homebrew on macOS, or apt/system package on Linux)"
    exit 1
}
print_success "MySQL client found"

MYSQLDUMP_BIN=$(find_mysql_binary "mysqldump") || {
    print_error "mysqldump binary not found — install MySQL (via Herd/Homebrew on macOS, or apt/system package on Linux)"
    exit 1
}
print_success "mysqldump found"

# Test local database (only needed for full or restore modes)
if [ "$MODE" = "full" ] || [ "$MODE" = "restore" ]; then
    print_status "🏠 Testing local database connection..."
    local_pass_opt=""
    if [ -n "$LOCAL_DB_PASS" ]; then
        local_pass_opt="-p${LOCAL_DB_PASS}"
    fi

    # shellcheck disable=SC2086
    if ! "$MYSQL_BIN" -h "$LOCAL_DB_HOST" -P "$LOCAL_DB_PORT" -u "$LOCAL_DB_USER" $local_pass_opt -e "SELECT 1" &>/dev/null; then
        print_error "Cannot connect to local MySQL (${LOCAL_DB_HOST}:${LOCAL_DB_PORT})"
        print_info "  Make sure MySQL is running (Herd on macOS, or system service on Linux)"
        exit 1
    fi
    print_success "Local database connection OK (${LOCAL_DB_NAME}@${LOCAL_DB_HOST}:${LOCAL_DB_PORT})"
fi

# ── SSH Connection & Tunnel ──────────────────────────────────────────────────

open_tunnel() {
    # Build SSH key option (empty = use SSH agent, e.g. 1Password)
    local key_opt=""
    local auth_method="🔑 1Password SSH agent"
    if [ -n "${SSH_KEY:-}" ]; then
        key_opt="-i $SSH_KEY"
        auth_method="🔑 Key: $SSH_KEY"
    fi

    print_step "🌐 Connecting to Production Server"
    print_status "Server: ${SSH_USER}@${SSH_HOST}"
    print_status "Auth: ${auth_method}"

    # Test SSH connection first
    print_status "🤝 Testing SSH connection..."

    # shellcheck disable=SC2086
    if ! ssh -o StrictHostKeyChecking=accept-new \
            -o ConnectTimeout=10 \
            -o BatchMode=yes \
            $key_opt \
            -p "$SSH_PORT" \
            "${SSH_USER}@${SSH_HOST}" "echo ok" &>/dev/null; then
        print_error "SSH connection failed!"
        print_info "  Check that 1Password SSH agent is running and the key is authorized"
        exit 1
    fi
    print_success "SSH connection OK — server is reachable"

    # Open tunnel
    # Pick a random local port for the tunnel to avoid conflicts
    TUNNEL_LOCAL_PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("",0)); print(s.getsockname()[1]); s.close()')

    print_status "🔗 Opening secure tunnel (localhost:${TUNNEL_LOCAL_PORT} → production:${PROD_DB_PORT})..."

    # shellcheck disable=SC2086
    ssh -f -N -o StrictHostKeyChecking=accept-new \
        -o ConnectTimeout=10 \
        -o ServerAliveInterval=30 \
        -o ServerAliveCountMax=3 \
        $key_opt \
        -p "$SSH_PORT" \
        -L "${TUNNEL_LOCAL_PORT}:${PROD_DB_HOST}:${PROD_DB_PORT}" \
        "${SSH_USER}@${SSH_HOST}"

    TUNNEL_PID=$(lsof -ti "tcp:${TUNNEL_LOCAL_PORT}" -sTCP:LISTEN 2>/dev/null | head -1)

    if [ -z "$TUNNEL_PID" ]; then
        print_error "Failed to establish SSH tunnel"
        exit 1
    fi
    print_success "SSH tunnel established (PID: $TUNNEL_PID)"

    # Test the database connection through the tunnel
    print_status "🗄️ Testing production database connection..."
    if ! "$MYSQL_BIN" -h 127.0.0.1 -P "$TUNNEL_LOCAL_PORT" -u "$PROD_DB_USER" -p"$PROD_DB_PASS" -e "SELECT 1" "$PROD_DB_NAME" &>/dev/null; then
        print_error "Tunnel is up but can't reach production database"
        print_info "  Check DB credentials in .db-sync.env"
        exit 1
    fi
    print_success "Production database connection OK (${PROD_DB_NAME})"
}

# ── Dump production database ─────────────────────────────────────────────────

dump_production() {
    local timestamp
    timestamp=$(date "+%Y%m%d-%H%M%S")
    local dump_file="$BACKUP_DIR/${APP_NAME}-${timestamp}.sql.gz"
    # Dump to a .partial sidecar and publish to the real name only after the
    # archive is verified complete — a truncated dump must never be restorable.
    local tmp_file="${dump_file}.partial"
    local dump_err="$BACKUP_DIR/.last-dump.err"
    PARTIAL_FILE="$tmp_file"   # tracked at script scope so cleanup() can remove it on interrupt/failure

    print_step "📥 Downloading Production Database"

    # Build exclude options
    local exclude_opts=""
    if [ -n "${EXCLUDE_TABLES:-}" ]; then
        IFS=',' read -ra TABLES <<< "$EXCLUDE_TABLES"
        for table in "${TABLES[@]}"; do
            exclude_opts="$exclude_opts --ignore-table=${PROD_DB_NAME}.${table}"
        done
        print_status "🚫 Excluding tables: ${EXCLUDE_TABLES}"
    fi

    # Get database size estimate
    local db_size
    db_size=$("$MYSQL_BIN" -h 127.0.0.1 -P "$TUNNEL_LOCAL_PORT" \
        -u "$PROD_DB_USER" -p"$PROD_DB_PASS" \
        -N -e "SELECT ROUND(SUM(data_length + index_length) / 1024 / 1024, 1) FROM information_schema.tables WHERE table_schema = '${PROD_DB_NAME}';" 2>/dev/null || echo "?")

    # Get table count
    local table_count
    table_count=$("$MYSQL_BIN" -h 127.0.0.1 -P "$TUNNEL_LOCAL_PORT" \
        -u "$PROD_DB_USER" -p"$PROD_DB_PASS" \
        -N -e "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = '${PROD_DB_NAME}';" 2>/dev/null || echo "?")

    print_info "  📊 Database: ${PROD_DB_NAME} (${table_count} tables, ~${db_size} MB)"
    echo "" >&2
    print_status "☕ Dumping database — this takes a few minutes for ~${db_size} MB, grab a coffee..."

    local start_time
    start_time=$(date +%s)

    # Start background progress monitor — prints the growing compressed size every
    # 10s so a long dump doesn't look hung. Reaped by cleanup() on interrupt.
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

    # shellcheck disable=SC2086
    "$MYSQLDUMP_BIN" \
        -h 127.0.0.1 \
        -P "$TUNNEL_LOCAL_PORT" \
        -u "$PROD_DB_USER" \
        -p"$PROD_DB_PASS" \
        --single-transaction \
        --quick \
        --routines \
        --triggers \
        --set-gtid-purged=OFF \
        $MYSQLDUMP_EXTRA_OPTS \
        $exclude_opts \
        "$PROD_DB_NAME" 2>"$dump_err" | gzip > "$tmp_file"

    # Stop progress monitor
    kill "$PROGRESS_PID" 2>/dev/null || true
    wait "$PROGRESS_PID" 2>/dev/null || true
    PROGRESS_PID=""

    # Verify the archive is a complete gzip before publishing it under the
    # canonical backup name. (set -e + pipefail already abort above if mysqldump
    # itself failed, so a surviving .partial is never promoted to a real backup.)
    if [ ! -s "$tmp_file" ] || ! gzip -t "$tmp_file" 2>/dev/null; then
        print_error "Production dump is empty or corrupt — backup NOT written."
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
    if ! gzip -t "$dump_file" 2>/dev/null; then
        print_error "Backup is a corrupt/truncated gzip — refusing to drop local DB: $(basename "$dump_file")"
        exit 1
    fi
    # mysqldump appends a "-- Dump completed" trailer; its absence means the dump
    # was cut short mid-stream even though the gzip wrapper itself is intact.
    if ! gunzip -c "$dump_file" | tail -c 4096 | grep -q "Dump completed"; then
        print_error "Backup looks truncated (no 'Dump completed' trailer) — refusing to drop local DB: $(basename "$dump_file")"
        exit 1
    fi

    local file_size
    file_size=$(du -h "$dump_file" | cut -f1)

    print_step "📤 Restoring to Local Database"
    print_info "  📄 Source: $(basename "$dump_file") (${file_size})"
    print_info "  🏠 Target: ${LOCAL_DB_NAME}@${LOCAL_DB_HOST}:${LOCAL_DB_PORT}"

    # Build mysql password option
    local pass_opt=""
    if [ -n "$LOCAL_DB_PASS" ]; then
        pass_opt="-p${LOCAL_DB_PASS}"
    fi

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
    print_status "🗑️ Dropping and recreating local database..."
    # shellcheck disable=SC2086
    "$MYSQL_BIN" -h "$LOCAL_DB_HOST" -P "$LOCAL_DB_PORT" -u "$LOCAL_DB_USER" $pass_opt \
        -e "DROP DATABASE IF EXISTS \`${LOCAL_DB_NAME}\`; CREATE DATABASE \`${LOCAL_DB_NAME}\` CHARACTER SET ${LOCAL_DB_CHARSET} COLLATE ${LOCAL_DB_COLLATION};"
    print_success "Fresh database created (charset ${LOCAL_DB_CHARSET}, collation ${LOCAL_DB_COLLATION})"

    local start_time
    start_time=$(date +%s)

    print_status "📦 Importing data — hang tight, this takes a bit for large databases..."

    # Start background progress monitor for the import
    (
        while true; do
            sleep 10
            elapsed_now=$(( $(date +%s) - start_time ))
            if [ "$elapsed_now" -ge 60 ]; then
                progress_time="$((elapsed_now / 60))m $((elapsed_now % 60))s"
            else
                progress_time="${elapsed_now}s"
            fi
            printf "  ${DIM}   ⏳ importing... (%s elapsed)${NC}\r\n" "$progress_time" >&2
        done
    ) &
    IMPORT_PROGRESS_PID=$!

    # shellcheck disable=SC2086
    gunzip < "$dump_file" | "$MYSQL_BIN" -h "$LOCAL_DB_HOST" -P "$LOCAL_DB_PORT" \
        -u "$LOCAL_DB_USER" $pass_opt "$LOCAL_DB_NAME"

    # Stop progress monitor
    kill "$IMPORT_PROGRESS_PID" 2>/dev/null || true
    wait "$IMPORT_PROGRESS_PID" 2>/dev/null || true
    IMPORT_PROGRESS_PID=""

    local end_time elapsed
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
    done < <(ls -t "$BACKUP_DIR"/${APP_NAME}-*.sql.gz 2>/dev/null)
    if [ "$count" -gt 0 ]; then
        local kept=$count
        [ "$count" -gt "$keep" ] && kept=$keep
        print_success "Backup retention: keeping ${kept} of ${count} (KEEP_BACKUPS=${keep})"
    fi
}

# ── Main execution ───────────────────────────────────────────────────────────

case $MODE in
    "full")
        open_tunnel
        DUMP_FILE=$(dump_production)
        restore_local "$DUMP_FILE"
        # Retain the compressed dump as a local backup (rotation keeps the last N),
        # so --restore can reuse the last sync offline.
        print_success "Backup retained: database/backups/$(basename "$DUMP_FILE")"
        prune_backups
        ;;
    "dump-only")
        open_tunnel
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
            # Latest backup
            BACKUP_FILE=$(ls -t "$BACKUP_DIR"/${APP_NAME}-*.sql.gz 2>/dev/null | head -1)
            if [ -z "$BACKUP_FILE" ]; then
                print_error "No backups found in database/backups/"
                print_info "  Run './sync-db.sh' first to download from production"
                exit 1
            fi
        fi
        restore_local "$BACKUP_FILE"
        ;;
esac

# Derive the dev URL from config rather than assuming a Herd .test hostname:
# DEV_URL override > Laravel APP_URL > Herd-style https://<app>.test fallback.
DEV_URL_RESOLVED="${DEV_URL:-$(read_env_value APP_URL "$ENV_FILE")}"
DEV_URL_RESOLVED="${DEV_URL_RESOLVED:-https://${APP_NAME}.test}"

echo "" >&2
echo -e "  ${GREEN}${BOLD}🎉 All done! Your local database is fresh and ready.${NC}" >&2
echo -e "  ${DIM}   Open ${DEV_URL_RESOLVED} and get to work!${NC}" >&2
echo "" >&2
