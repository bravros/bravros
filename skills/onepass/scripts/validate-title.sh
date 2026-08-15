#!/usr/bin/env bash
# Validate a proposed 1Password item title for op:// reference safety.
# Exits 0 if safe; exits 1 with a suggested ASCII rewrite on stderr.
# Usage:  bash validate-title.sh "Cloudflare API — Bravros (Deploy)"

set -u
title="${1:-}"
if [ -z "$title" ]; then
  echo "usage: validate-title.sh <title>" >&2
  exit 2
fi

# Allowed: ASCII letters, digits, space, regular hyphen, underscore.
if printf '%s' "$title" | LC_ALL=C grep -qE '^[A-Za-z0-9 _-]+$'; then
  exit 0
fi

# Suggest a rewrite: drop emoji/non-ASCII, replace em/en-dashes with -, strip ( ) + & @ /
# Order matters: replace known-bad punctuation (including em/en dashes, which are
# multi-byte UTF-8) BEFORE stripping remaining non-ASCII — otherwise the dashes
# vanish and the "<Service> - <Project>" separator is lost.
suggested=$(printf '%s' "$title" \
  | sed -E 's/[—–]/-/g; s/[()+&@/]+/ /g' \
  | LC_ALL=C tr -d '\200-\377' \
  | sed -E 's/  +/ /g; s/^ +//; s/ +$//; s/ +- +/ - /g')

cat >&2 <<EOF
Invalid title for op:// references: "$title"
Contains one or more unsafe characters (— ( ) + & @ / emoji / non-ASCII).
Suggested rewrite: "$suggested"
EOF
exit 1
