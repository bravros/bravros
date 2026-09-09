#!/usr/bin/env bash
# mute-announce.sh — global kill-switch for every Bravros audio announcement.
#
# Silences BOTH surfaces (Alexa/Echo and the local macOS `say` fallback). Muting both is
# deliberate: this exists for calls and meetings, where a laptop speaking aloud is picked up
# by the microphone and is worse than the Echo would have been.
#
# State is a plain file — ~/.claude/.mute — deliberately NOT a flag inside the binary, so
# every producer agrees without needing a `bravros` release: this script, `bravros ha mute`,
# and a bare `touch ~/.claude/.mute` are all equivalent. Contents: a Unix expiry timestamp,
# or the literal "forever".
#
# Built for a StreamDeck button. `toggle` is the one-button verb; it prints the resulting
# state on a single line so it can drive a button title.
#
# Usage:
#   mute-announce.sh                 # same as `status`
#   mute-announce.sh toggle          # flip (StreamDeck button)
#   mute-announce.sh on [duration]   # mute; optional 30s / 45m / 2h, default indefinite
#   mute-announce.sh off             # unmute
#   mute-announce.sh status          # print current state
#
# Exit status is 0 for every normal operation so a StreamDeck button never shows an error
# badge; only genuine misuse (unknown verb, unparseable duration) exits non-zero.

set -uo pipefail

MUTE_FILE="$HOME/.claude/.mute"

# Parse 30s / 45m / 2h / 90 (bare = minutes) into seconds. Echoes nothing on failure.
parse_duration() {
  local d="$1" n u
  n="${d%[smhSMH]}"
  u="${d#"$n"}"
  case "$n" in ''|*[!0-9]*) return 1 ;; esac
  case "$u" in
    s|S) echo $(( n )) ;;
    h|H) echo $(( n * 3600 )) ;;
    m|M|'') echo $(( n * 60 )) ;;
    *) return 1 ;;
  esac
}

is_muted() {
  [ -f "$MUTE_FILE" ] || return 1
  local raw
  raw="$(tr -d '[:space:]' < "$MUTE_FILE" 2>/dev/null)"
  if [ -z "$raw" ] || [ "$raw" = "forever" ]; then
    return 0
  fi
  if printf '%s' "$raw" | grep -qE '^[0-9]+$'; then
    # Expired mutes self-heal, so a pre-call "on 30m" can't strand you in silence.
    if [ "$(date +%s)" -lt "$raw" ]; then return 0; fi
    rm -f "$MUTE_FILE" 2>/dev/null
    return 1
  fi
  return 0   # unparseable → fail safe, stay muted
}

print_status() {
  if is_muted; then
    local raw until=""
    raw="$(tr -d '[:space:]' < "$MUTE_FILE" 2>/dev/null)"
    if printf '%s' "$raw" | grep -qE '^[0-9]+$'; then
      until="$(date -r "$raw" '+%H:%M' 2>/dev/null)"
      echo "🔇 muted until ${until:-$raw}"
    else
      echo "🔇 muted"
    fi
  else
    echo "🔊 announcements on"
  fi
}

mute_on() {
  mkdir -p "$(dirname "$MUTE_FILE")"
  if [ -n "${1:-}" ]; then
    local secs
    if ! secs="$(parse_duration "$1")"; then
      echo "❌ bad duration: $1 (use 30s, 45m, 2h)" >&2
      exit 2
    fi
    echo $(( $(date +%s) + secs )) > "$MUTE_FILE"
  else
    echo "forever" > "$MUTE_FILE"
  fi
  print_status
}

mute_off() {
  rm -f "$MUTE_FILE" 2>/dev/null
  print_status
}

case "${1:-status}" in
  toggle)        if is_muted; then mute_off; else mute_on "${2:-}"; fi ;;
  on|mute)       mute_on "${2:-}" ;;
  off|unmute)    mute_off ;;
  status|'')     print_status ;;
  -h|--help|help)
    sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'
    ;;
  *)
    echo "❌ unknown verb: $1 (use toggle|on|off|status)" >&2
    exit 2
    ;;
esac
exit 0
