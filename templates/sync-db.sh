#!/bin/bash

# ============================================================================
# Production Database Sync Script
# ============================================================================
# Downloads production database via secure SSH tunnel and restores it locally.
# Credentials are stored in .db-sync.env (gitignored).
#
# Usage:
#   ./sync-db.sh              # Full sync (dump + restore)
#   ./sync-db.sh --dump-only  # Only download, don't restore
#   ./sync-db.sh --restore    # Restore latest backup without downloading
#   ./sync-db.sh --list       # List available backups
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
BACKUP_DIR="$SCRIPT_DIR/database/backups"
TUNNEL_PID=""

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
    if [ -n "$TUNNEL_PID" ] && kill -0 "$TUNNEL_PID" 2>/dev/null; then
        echo "" >&2
        print_status "🔒 Closing SSH tunnel..."
        kill "$TUNNEL_PID" 2>/dev/null
        wait "$TUNNEL_PID" 2>/dev/null || true
        print_success "Tunnel closed"
    fi
}

trap cleanup EXIT SIGINT SIGTERM

# ── Load configuration ────────────────────────────────────────────────────────

if [ ! -f "$CONFIG_FILE" ]; then
    print_error "Config file not found: .db-sync.env"
    print_status "Copy the example and fill in your credentials:"
    print_info "  cp .db-sync.env.example .db-sync.env"
    exit 1
fi

# shellcheck source=/dev/null
source "$CONFIG_FILE"

# Validate required config
REQUIRED_VARS=(SSH_USER SSH_HOST PROD_DB_NAME PROD_DB_USER PROD_DB_PASS LOCAL_DB_NAME LOCAL_DB_USER)
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
LOCAL_DB_HOST="${LOCAL_DB_HOST:-127.0.0.1}"
LOCAL_DB_PORT="${LOCAL_DB_PORT:-3306}"
LOCAL_DB_PASS="${LOCAL_DB_PASS:-}"
APP_NAME="${APP_NAME:-$(basename "$SCRIPT_DIR")}"

# Expand tilde in SSH_KEY if set
if [ -n "$SSH_KEY" ]; then
    SSH_KEY="${SSH_KEY/#\~/$HOME}"
fi

# Create backup directory
mkdir -p "$BACKUP_DIR"

# ── Ensure .db-sync.env is gitignored ────────────────────────────────────────

ensure_gitignored() {
    local gitignore="$SCRIPT_DIR/.gitignore"
    if [ ! -f "$gitignore" ] || ! grep -qxF '.db-sync.env' "$gitignore"; then
        echo '.db-sync.env' >> "$gitignore"
        print_success ".db-sync.env added to .gitignore"
    fi
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
        --column-statistics=0 \
        $exclude_opts \
        "$PROD_DB_NAME" 2>/dev/null | gzip > "$dump_file"

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
    print_status "🗑️ Dropping and recreating local database..."
    # shellcheck disable=SC2086
    "$MYSQL_BIN" -h "$LOCAL_DB_HOST" -P "$LOCAL_DB_PORT" -u "$LOCAL_DB_USER" $pass_opt \
        -e "DROP DATABASE IF EXISTS \`${LOCAL_DB_NAME}\`; CREATE DATABASE \`${LOCAL_DB_NAME}\` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;"
    print_success "Fresh database created"

    local start_time
    start_time=$(date +%s)

    print_status "📦 Importing data — hang tight, this takes a bit for large databases..."
    # shellcheck disable=SC2086
    gunzip < "$dump_file" | "$MYSQL_BIN" -h "$LOCAL_DB_HOST" -P "$LOCAL_DB_PORT" \
        -u "$LOCAL_DB_USER" $pass_opt "$LOCAL_DB_NAME"

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
    if herd php artisan migrate --force 2>/dev/null || php artisan migrate --force 2>/dev/null; then
        print_success "Migrations applied"
    else
        print_warning "Some migrations skipped (DB may already be up to date)"
    fi

    print_status "🧹 Clearing caches..."
    herd php artisan optimize:clear >/dev/null 2>&1 || php artisan optimize:clear >/dev/null 2>&1 || true
    print_success "Caches cleared"
}

# ── Main execution ───────────────────────────────────────────────────────────

case $MODE in
    "full")
        open_tunnel
        DUMP_FILE=$(dump_production)
        restore_local "$DUMP_FILE"
        # Delete downloaded backup after confirmed successful restore
        rm -f "$DUMP_FILE"
        print_success "Downloaded backup deleted (restore confirmed)"
        ;;
    "dump-only")
        open_tunnel
        dump_production
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

echo "" >&2
echo -e "  ${GREEN}${BOLD}🎉 All done! Your local database is fresh and ready.${NC}" >&2
echo -e "  ${DIM}   Open https://${APP_NAME}.test and get to work!${NC}" >&2
echo "" >&2
