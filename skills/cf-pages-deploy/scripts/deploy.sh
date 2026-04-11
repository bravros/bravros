#!/bin/bash
# Deploy a static site to Cloudflare Pages with optional custom domain
# Usage: ./deploy.sh <directory> <project-name> [custom-domain]
# Example: ./deploy.sh src/ maglash-lp lp.maglash.com.br

set -e

DIR="${1:?Usage: ./deploy.sh <directory> <project-name> [custom-domain]}"
PROJECT="${2:?Usage: ./deploy.sh <directory> <project-name> [custom-domain]}"
CUSTOM_DOMAIN="${3:-}"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

info()  { echo -e "${GREEN}✓${NC} $1"; }
warn()  { echo -e "${YELLOW}⚠${NC} $1"; }
error() { echo -e "${RED}✗${NC} $1"; exit 1; }

# 1. Validate directory
[ -f "$DIR/index.html" ] || error "No index.html found in $DIR"
info "Found index.html in $DIR"

# 2. Check wrangler
npx wrangler --version > /dev/null 2>&1 || error "Wrangler not found. Run: npm install -g wrangler"
info "Wrangler $(npx wrangler --version 2>&1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')"

# 3. Check auth
npx wrangler whoami > /dev/null 2>&1 || error "Not authenticated. Run: npx wrangler login"
info "Authenticated with Cloudflare"

# 4. Get API token (strip wrangler banner)
TOKEN=$(npx wrangler auth token 2>&1 | grep -v '⛅' | grep -v '─' | grep -v '^$' | tail -1)
[ -n "$TOKEN" ] || error "Could not retrieve API token"
info "Got API token"

# 5. If custom domain provided, find the zone and its account
if [ -n "$CUSTOM_DOMAIN" ]; then
  # Extract root domain (last two parts, or last three for .com.br style)
  ROOT_DOMAIN=$(echo "$CUSTOM_DOMAIN" | rev | cut -d. -f1-3 | rev)

  echo -e "\n${YELLOW}→${NC} Looking up zone for $ROOT_DOMAIN..."

  ZONE_RESPONSE=$(curl -s "https://api.cloudflare.com/client/v4/zones?name=$ROOT_DOMAIN" \
    -H "Authorization: Bearer $TOKEN")

  ZONE_ID=$(echo "$ZONE_RESPONSE" | python3 -c "import sys,json; r=json.load(sys.stdin); print(r['result'][0]['id'] if r['result'] else '')" 2>/dev/null)
  ACCOUNT_ID=$(echo "$ZONE_RESPONSE" | python3 -c "import sys,json; r=json.load(sys.stdin); print(r['result'][0]['account']['id'] if r['result'] else '')" 2>/dev/null)
  ACCOUNT_NAME=$(echo "$ZONE_RESPONSE" | python3 -c "import sys,json; r=json.load(sys.stdin); print(r['result'][0]['account']['name'] if r['result'] else '')" 2>/dev/null)

  [ -n "$ZONE_ID" ] || error "Zone not found for $ROOT_DOMAIN. Is the domain on your Cloudflare account?"
  info "Zone: $ROOT_DOMAIN (ID: $ZONE_ID)"
  info "Account: $ACCOUNT_NAME (ID: $ACCOUNT_ID)"
fi

# 6. Deploy
echo -e "\n${YELLOW}→${NC} Deploying $DIR to $PROJECT..."
if [ -n "$ACCOUNT_ID" ]; then
  npx wrangler pages deploy "$DIR" --project-name "$PROJECT" --account-id "$ACCOUNT_ID"
else
  npx wrangler pages deploy "$DIR" --project-name "$PROJECT"
fi
info "Deployed to https://$PROJECT.pages.dev"

# 7. Add custom domain if provided
if [ -n "$CUSTOM_DOMAIN" ]; then
  echo -e "\n${YELLOW}→${NC} Adding custom domain $CUSTOM_DOMAIN..."

  # Add domain to Pages project
  DOMAIN_RESPONSE=$(curl -s -X POST \
    "https://api.cloudflare.com/client/v4/accounts/$ACCOUNT_ID/pages/projects/$PROJECT/domains" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d "{\"name\": \"$CUSTOM_DOMAIN\"}")

  DOMAIN_SUCCESS=$(echo "$DOMAIN_RESPONSE" | python3 -c "import sys,json; print(json.load(sys.stdin).get('success', False))" 2>/dev/null)

  if [ "$DOMAIN_SUCCESS" = "True" ]; then
    info "Custom domain registered: $CUSTOM_DOMAIN"
  else
    # Might already exist, check
    DOMAIN_ERROR=$(echo "$DOMAIN_RESPONSE" | python3 -c "import sys,json; e=json.load(sys.stdin).get('errors',[]); print(e[0]['message'] if e else 'Unknown error')" 2>/dev/null)
    warn "Domain registration: $DOMAIN_ERROR (may already exist — continuing)"
  fi

  # Create CNAME record
  SUBDOMAIN=$(echo "$CUSTOM_DOMAIN" | sed "s/\.$ROOT_DOMAIN$//")

  DNS_RESPONSE=$(curl -s -X POST \
    "https://api.cloudflare.com/client/v4/zones/$ZONE_ID/dns_records" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d "{\"type\":\"CNAME\",\"name\":\"$SUBDOMAIN\",\"content\":\"$PROJECT.pages.dev\",\"proxied\":true}")

  DNS_SUCCESS=$(echo "$DNS_RESPONSE" | python3 -c "import sys,json; print(json.load(sys.stdin).get('success', False))" 2>/dev/null)

  if [ "$DNS_SUCCESS" = "True" ]; then
    info "CNAME record created: $SUBDOMAIN → $PROJECT.pages.dev"
  else
    DNS_ERROR=$(echo "$DNS_RESPONSE" | python3 -c "import sys,json; e=json.load(sys.stdin).get('errors',[]); print(e[0]['message'] if e else 'Unknown error')" 2>/dev/null)
    if echo "$DNS_ERROR" | grep -qi "already exists"; then
      warn "CNAME record already exists — skipping"
    elif echo "$DNS_ERROR" | grep -qi "auth"; then
      warn "OAuth token lacks DNS permissions. Add CNAME manually:"
      echo "    Dashboard → DNS Records → Add Record"
      echo "    Type: CNAME | Name: $SUBDOMAIN | Content: $PROJECT.pages.dev | Proxy: ON"
    else
      warn "DNS record: $DNS_ERROR"
      echo "    Add CNAME manually: $SUBDOMAIN → $PROJECT.pages.dev (proxied)"
    fi
  fi

  echo ""
  info "Production URL: https://$PROJECT.pages.dev"
  info "Custom domain:  https://$CUSTOM_DOMAIN (may take 1-2 min for SSL)"
else
  echo ""
  info "Production URL: https://$PROJECT.pages.dev"
fi
