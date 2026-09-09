#!/usr/bin/env bash
# announce.sh — presence-, room- & host-aware wrapper around `bravros ha say` for skills + ad-hoc shells.
#
# Why: Claude Code's bash sessions don't load zsh's op_lazy hooks, so HASS_TOKEN is never
# auto-hydrated. This wrapper reads the token from 1Password on demand, decides the right
# audio surface for this machine + presence + room, then either delegates to `bravros ha say`
# (Echo/Alexa) or speaks locally via macOS `say`.
#
# Routing:
#   - Mac Studio host (hostname matches *mac-studio* or BRAVROS_TTS=1) → ALWAYS Echo via
#     `bravros ha say` (its audio is intentionally remote; it has no useful local speaker).
#   - Portable Mac (MacBook):
#       home → Echo via `bravros ha say`; if HA is unreachable, fall back to local macOS
#              `say` so the message is still heard.
#       away → local macOS `say -v ${BRAVROS_SAY_VOICE:-Luciana}` (pt-BR).
#   - BRAVROS_ANNOUNCE_DEBUG=1 prints the chosen route and exits WITHOUT speaking.
#   - Any failure (op unavailable, HA unreachable, CLI missing) is non-fatal — exits 0 so
#     calling skills are never blocked by an audio nicety.
#
# Presence detection (~/.claude/.home-network):
#   Primary signal is the DEFAULT GATEWAY MAC — a stable fingerprint of the home router that
#   needs no special permissions. SSID is deliberately NOT used: macOS 26 gates Wi-Fi network
#   names behind Location Services, so `networksetup -getairportnetwork` reports "not
#   associated" even while connected. The gateway MAC has neither that problem nor the
#   false-positive rate of a bare subnet check (10.0.x.x is a very common LAN range).
#   - ~/.claude/.home-network holds one known-home gateway MAC per line; `#` comments and
#     blank lines ignored. Seeded by `bravros ha home --learn`.
#   - File ABSENT  → legacy behavior: assume home. Keeps the Studio and fresh installs working.
#   - File PRESENT → home only when the current gateway MAC is listed.
#   - Manual overrides still win: ~/.claude/.away or BRAVROS_AWAY=1 forces away;
#     BRAVROS_ECHO=1 forces Echo.
#
# Room override (~/.claude/.echo-room):
#   ~240 call sites across skills/ hardcode the device as "studio". Rather than edit them all,
#   the room file redirects every announcement to whichever Echo the operator is currently
#   near. Managed with `bravros ha room <name>` / `bravros ha room --clear`.
#   Precedence: BRAVROS_ECHO_DEVICE env > at-desk pin > ~/.claude/.echo-room > per-call arg.
#   AT-DESK PIN: the room file is honoured only on a roaming (Wi-Fi) MacBook. On the Mac
#   Studio host, or on a MacBook with a WIRED default route, the operator is physically at
#   the studio desk — docking the laptop is what puts it on Ethernet — so the target is
#   pinned to the studio Echo regardless of what the room file says.
#
# Do Not Disturb:
#   Alexa SILENTLY DISCARDS announcements sent to a device with DND on — HA returns 200 and
#   nothing plays. That failure mode is invisible, so by default we clear the target's DND
#   switch immediately before speaking. Set BRAVROS_DND_AUTOCLEAR=0 to respect DND instead.
#
# Voice note: Luciana is the only genuine pt-BR voice installed. The newer expressive
# voices (Eddy, Reed, Rocko, Sandy, Shelley, Flo, Grandma, Grandpa) are English engines
# that mispronounce Portuguese — do not substitute them.
#
# Agent signature (who is speaking):
#   Several agents share the same Echo — Claude Code, Codex, Google Antigravity, Grok Build —
#   and several sessions of each run in parallel. Every announcement therefore ENDS with the
#   agent's name, after the origin clause ("… projeto <repo>, Claude Code."), because the
#   closing words are what the operator retains. The wrapper appends it so call sites don't
#   have to: `--agent "<spoken name>"` wins, then BRAVROS_ANNOUNCE_AGENT, then auto-detection
#   (CLAUDECODE=1 → "Claude Code"). Idempotent: a message that already ends with the chosen
#   signature — or with any known one (Codex, Google Antigravity, Grok Bot …) hand-written by
#   that agent's own rules — is left alone, so nothing ever gets double-signed.
#   The spoken form is free text: use whatever Alexa's pt-BR voice pronounces best, and for a
#   multi-persona host say which one ("Grok Bot, agente atua").
#
# Usage:
#   announce.sh "<message>" [device]            # chime + speech (default)
#   announce.sh --tts "<message>" [device]      # silent prefix, no chime
#   announce.sh --force "<message>" [device]    # bypass Mac-unlock gate (studio only)
#   announce.sh --agent "Grok Bot" "<message>"  # sign as that agent (also --agent=NAME)
#
# Tunables:
#   BRAVROS_ANNOUNCE_AGENT    agent signature appended as the last words (see above);
#                             set to "" to disable even the auto-detected one.
#   BRAVROS_ANNOUNCE_PREFIX   opener prepended to every message (default "Chefe, ");
#                             set to "" to disable. Idempotent — never double-applied.
#   BRAVROS_SAY_CHIME         sound played before local `say` (default Ping.aiff);
#                             set to "" to disable. Echo route ignores it (Alexa chimes).
#   BRAVROS_SAY_VOICE         local `say` voice (default Luciana, the only real pt-BR one)
#   BRAVROS_ECHO_DEVICE       force a target Echo, beating the room file
#   BRAVROS_DND_AUTOCLEAR     1 (default) clears target DND before speaking; 0 respects it
#   BRAVROS_HA_DEVICES        path of the room -> Echo entity-slug map (default
#                             ~/.claude/ha-devices.json) — the SAME file the binary reads,
#                             see cli/internal/ha/devices.go and templates/ha-devices.example.json
#   BRAVROS_HASS_TOKEN_OP_REF 1Password secret reference for the HA long-lived token
#                             (op://<Vault>/<Item>/<field>); falls back to the
#                             "hass_token_op_ref" key of ha-devices.json, and to no `op`
#                             hydration at all when neither is set.

set +e

# --- parse flags: --agent is ours, everything else passes verbatim to `bravros ha say` ---
PASS_FLAGS=()
AGENT_FLAG=""; AGENT_FLAG_SET=0
while [ "${1:0:2}" = "--" ]; do
  case "$1" in
    --agent=*) AGENT_FLAG="${1#--agent=}"; AGENT_FLAG_SET=1; shift ;;
    --agent)   AGENT_FLAG="${2-}"; AGENT_FLAG_SET=1; shift 2 ;;
    *)         PASS_FLAGS+=("$1"); shift ;;
  esac
done
MSG="$1"
DEVICE="${2:-studio}"

# --- agent signature ---
# Resolution: --agent flag > BRAVROS_ANNOUNCE_AGENT (explicit empty disables) > auto-detect.
# Claude Code exports CLAUDECODE=1 into every Bash tool call, which is how its ~240 skill call
# sites get signed with zero edits. Codex, Antigravity and Grok have no verified marker, so
# their rules files pass --agent (or hand-write the name, which the dedupe below respects).
if [ "$AGENT_FLAG_SET" = "1" ]; then
  ANNOUNCE_AGENT="$AGENT_FLAG"
elif [ -n "${BRAVROS_ANNOUNCE_AGENT+x}" ]; then
  ANNOUNCE_AGENT="$BRAVROS_ANNOUNCE_AGENT"
elif [ "${CLAUDECODE:-}" = "1" ]; then
  ANNOUNCE_AGENT="Claude Code"
else
  ANNOUNCE_AGENT=""
fi
# Known signatures other agents' rules hand-write into the message. A message already ending
# in one of these is left untouched — the author signed it, we don't countersign.
KNOWN_SIGNATURES="claude code|codex|google antigravity|antigravity|grok bot|grok build|grok"
ends_with_signature() {   # $1 = message, $2 = signature (case-insensitive, punctuation-blind)
  local m s
  m="$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | sed -E 's/[[:space:][:punct:]]+$//')"
  s="$(printf '%s' "$2" | tr '[:upper:]' '[:lower:]' | sed -E 's/[[:space:][:punct:]]+$//')"
  [ -n "$s" ] || return 1
  case "$m" in *"$s") return 0 ;; esac
  return 1
}
already_signed() {        # $1 = message → 0 when it ends with ANY known signature
  local sig; local IFS='|'
  for sig in $KNOWN_SIGNATURES; do ends_with_signature "$1" "$sig" && return 0; done
  return 1
}
if [ -n "$MSG" ] && [ -n "$ANNOUNCE_AGENT" ]; then
  if ! ends_with_signature "$MSG" "$ANNOUNCE_AGENT" && ! already_signed "$MSG"; then
    MSG="$(printf '%s' "$MSG" | sed -E 's/[[:space:]]+$//')"
    case "$MSG" in *[.!?]) ;; *) MSG="${MSG}." ;; esac
    MSG="${MSG} ${ANNOUNCE_AGENT}."
  fi
fi

# --- global mute kill-switch ---
# State lives in a plain file, NOT in the binary, so every producer agrees without needing a
# `bravros` release: `bravros ha mute`, scripts/mute-announce.sh (StreamDeck), or a bare
# `touch ~/.claude/.mute` all work identically. Contents are a Unix expiry timestamp, or
# "forever". Mute silences EVERY surface including the local `say` fallback — it exists for
# calls, where a laptop speaking aloud is worse than the Echo. Expired mutes self-heal so a
# pre-call "mute 30m" can never strand the operator in permanent silence.
MUTED=0
MUTE_UNTIL=""
MUTE_FILE="$HOME/.claude/.mute"
if [ -f "$MUTE_FILE" ]; then
  _m="$(tr -d '[:space:]' < "$MUTE_FILE" 2>/dev/null)"
  if [ -z "$_m" ] || [ "$_m" = "forever" ]; then
    MUTED=1; MUTE_UNTIL="forever"
  elif printf '%s' "$_m" | grep -qE '^[0-9]+$'; then
    if [ "$(date +%s)" -lt "$_m" ]; then
      MUTED=1; MUTE_UNTIL="$(date -r "$_m" '+%H:%M' 2>/dev/null || echo "$_m")"
    else
      rm -f "$MUTE_FILE" 2>/dev/null
    fi
  else
    MUTED=1; MUTE_UNTIL="unparseable"   # fail safe: stay silent rather than blurt
  fi
fi

# --- operator address prefix ---
# Every announcement opens by addressing the operator, so a ping is recognisable as
# directed at them from the first syllable. Applied here rather than in the ~240 call
# sites across skills/ so ad-hoc shell calls and future announces inherit it for free.
# Idempotent: a message that already opens with the prefix is left alone.
# Override with BRAVROS_ANNOUNCE_PREFIX; set it to the empty string to disable entirely
# (the `-` form, not `:-`, is deliberate so an explicit empty value is honoured).
ANNOUNCE_PREFIX="${BRAVROS_ANNOUNCE_PREFIX-Chefe, }"
if [ -n "$MSG" ] && [ -n "$ANNOUNCE_PREFIX" ]; then
  case "$MSG" in
    "$ANNOUNCE_PREFIX"*) ;;
    *) MSG="${ANNOUNCE_PREFIX}${MSG}" ;;
  esac
fi

# --- host detection ---
HOST="$(hostname -s 2>/dev/null | tr '[:upper:]' '[:lower:]')"
IS_STUDIO=0
case "$HOST" in *mac-studio*) IS_STUDIO=1 ;; esac
[ "$BRAVROS_TTS" = "1" ] && IS_STUDIO=1

# --- link detection (wired vs wireless) ---
# The MacBook is only ever AWAY FROM THE DESK when it's on Wi-Fi: docking it puts it on
# Ethernet (or a Thunderbolt dock), which physically means it is sitting at the studio desk
# next to the Mac Studio. So a wired link is treated as "at the desk" and pins the target to
# the studio Echo — announcing to the bedroom while docked at the desk is always wrong.
# Resolved from the DEFAULT-ROUTE interface, not merely "is any Ethernet up", so a docked-but-
# Wi-Fi-routed laptop is still correctly classified as wireless.
link_kind() {
  local iface port
  iface="$(route -n get default 2>/dev/null | awk '/interface:/ {print $2; exit}')"
  [ -n "$iface" ] || { echo "unknown"; return; }
  port="$(networksetup -listallhardwareports 2>/dev/null | awk -v d="$iface" '
    /^Hardware Port:/ { hp = substr($0, 16) }
    /^Device:/        { if ($2 == d) { print hp; exit } }')"
  case "$port" in
    *Wi-Fi*|*AirPort*) echo "wifi" ;;
    "")                echo "unknown" ;;
    *)                 echo "wired" ;;
  esac
}
LINK="$(link_kind)"

# --- room override ---
# ~240 skill call sites hardcode "studio"; the room file redirects them to wherever the
# operator actually is. Only honoured on a ROAMING MacBook — on the Mac Studio host, or on a
# wired (docked) MacBook, the operator is at the desk by definition and the target is pinned
# to the studio Echo. BRAVROS_ECHO_DEVICE is a deliberate manual act and beats all of it.
ROOM_SRC="arg"
if [ -n "$BRAVROS_ECHO_DEVICE" ]; then
  DEVICE="$BRAVROS_ECHO_DEVICE"; ROOM_SRC="env"
elif [ "$IS_STUDIO" = "1" ] || [ "$LINK" = "wired" ]; then
  DEVICE="studio"; ROOM_SRC="at-desk($LINK)"
elif [ -f "$HOME/.claude/.echo-room" ]; then
  _room="$(tr -d '[:space:]' < "$HOME/.claude/.echo-room" 2>/dev/null)"
  if [ -n "$_room" ]; then DEVICE="$_room"; ROOM_SRC="roomfile"; fi
fi

# --- gateway MAC helpers ---
# macOS `arp` prints octets WITHOUT leading zeros (4:38:83:9d:eb:c5), while most places
# that record a MAC pad them (04:38:...). Normalise both sides before comparing.
normalize_mac() {
  printf '%s' "$1" | tr 'A-Z' 'a-z' | awk -F: '{
    s = ""
    for (i = 1; i <= NF; i++) { o = $i; if (length(o) < 2) o = "0" o; s = (i == 1 ? o : s ":" o) }
    print s
  }'
}

current_gateway_mac() {
  local gw mac
  gw="$(route -n get default 2>/dev/null | awk '/gateway:/ {print $2; exit}')"
  [ -n "$gw" ] || return 1
  # Nudge the ARP cache — a cold or expired entry prints "(incomplete)".
  ping -c 1 -t 1 "$gw" >/dev/null 2>&1
  mac="$(arp -n "$gw" 2>/dev/null | grep -oE '([0-9a-fA-F]{1,2}:){5}[0-9a-fA-F]{1,2}' | head -1)"
  [ -n "$mac" ] || return 1
  normalize_mac "$mac"
}

# Returns 0 = on a known home network, 1 = not home, 2 = unknown (no fingerprint file).
detect_home_network() {
  local netfile="$HOME/.claude/.home-network" mac line
  [ -f "$netfile" ] || return 2
  mac="$(current_gateway_mac)" || return 1
  while IFS= read -r line; do
    line="${line%%#*}"
    line="$(printf '%s' "$line" | tr -d '[:space:]')"
    [ -n "$line" ] || continue
    [ "$(normalize_mac "$line")" = "$mac" ] && return 0
  done < "$netfile"
  return 1
}

# --- presence detection ---
detect_home_network; NET_STATUS=$?
case "$NET_STATUS" in
  0) AWAY=0; PRESENCE_SRC="network" ;;   # gateway MAC matched a known home router
  1) AWAY=1; PRESENCE_SRC="network" ;;   # fingerprint file exists but we're elsewhere
  *) AWAY=0; PRESENCE_SRC="default" ;;   # no fingerprint file → legacy assume-home
esac

# Manual overrides beat auto-detection in both directions.
if [ -f "$HOME/.claude/.away" ] || [ "$BRAVROS_AWAY" = "1" ]; then
  AWAY=1; PRESENCE_SRC="override"
fi
if [ "$BRAVROS_ECHO" = "1" ]; then
  AWAY=0; PRESENCE_SRC="override"
fi

# --- route decision ---
# studio → echo always; macbook away → local; macbook home → echo (local fallback if HA down)
if [ "$IS_STUDIO" = "1" ]; then
  ROUTE="echo"
elif [ "$AWAY" = "1" ]; then
  ROUTE="local"
else
  ROUTE="echo"
fi

HOSTKIND=macbook; [ "$IS_STUDIO" = "1" ] && HOSTKIND=studio
PRESENCE=home;    [ "$AWAY" = "1" ] && PRESENCE=away

[ "$MUTED" = "1" ] && ROUTE="muted"

if [ "$BRAVROS_ANNOUNCE_DEBUG" = "1" ]; then
  echo "announce route=$ROUTE host=$HOSTKIND link=$LINK presence=$PRESENCE($PRESENCE_SRC) gw=$(current_gateway_mac 2>/dev/null) device=$DEVICE($ROOM_SRC) muted=$MUTED${MUTE_UNTIL:+(until $MUTE_UNTIL)} agent=${ANNOUNCE_AGENT:-none} voice=${BRAVROS_SAY_VOICE:-Luciana} chime=${BRAVROS_SAY_CHIME-/System/Library/Sounds/Ping.aiff} msg=$MSG"
  exit 0
fi

# Kill-switch wins over every route, Echo and local alike.
[ "$MUTED" = "1" ] && exit 0

say_local() {
  command -v say >/dev/null 2>&1 || return 1
  [ -n "$MSG" ] || return 0
  # Alexa's announce mode chimes natively; the local route has no equivalent, so play a
  # system sound first to match. Override with BRAVROS_SAY_CHIME (empty string disables).
  # Non-fatal throughout — a missing afplay or sound file must never block the speech.
  local chime="${BRAVROS_SAY_CHIME-/System/Library/Sounds/Ping.aiff}"
  if [ -n "$chime" ] && [ -f "$chime" ] && command -v afplay >/dev/null 2>&1; then
    afplay "$chime" >/dev/null 2>&1
  fi
  say -v "${BRAVROS_SAY_VOICE:-Luciana}" "$MSG" >/dev/null 2>&1
}

# --- local route (macbook away) ---
if [ "$ROUTE" = "local" ]; then
  say_local
  exit 0
fi

# --- local config (ha-devices.json) ---
# ONE map per machine, read by BOTH this wrapper and the binary: cli/internal/ha/devices.go
# resolves the very same path (BRAVROS_HA_DEVICES, else ~/.claude/ha-devices.json). The map
# used to be a hardcoded `case` here as well, which made "keep the two in sync" unfulfillable
# by construction once the Go side moved to a per-machine file (B-0029) — and a wrong slug
# fails SILENTLY, because Alexa drops announcements to a DND device while HA still returns 200.
# The file is deliberately not in the repo: this tree is mirrored publicly, and the room->Echo
# map is the operator's home layout. See templates/ha-devices.example.json for the shape.
HA_DEVICES_FILE="${BRAVROS_HA_DEVICES:-$HOME/.claude/ha-devices.json}"

# json_string_field <file> <key>... — prints the string at that key path, or nothing.
# Missing file, missing key, malformed JSON and a missing python3 all print nothing, so every
# caller degrades to its own fallback instead of failing. python3 is used rather than jq
# because macOS ships it and jq is not guaranteed to be installed.
json_string_field() {
  [ -f "$1" ] || return 0
  command -v python3 >/dev/null 2>&1 || return 0
  python3 - "$@" 2>/dev/null <<'PYJSON'
import json, sys

path, keys = sys.argv[1], sys.argv[2:]
try:
    with open(path, encoding="utf-8") as fh:
        cur = json.load(fh)
except Exception:
    sys.exit(0)
for key in keys:
    if not isinstance(cur, dict) or key not in cur:
        sys.exit(0)
    cur = cur[key]
if isinstance(cur, str):
    print(cur)
PYJSON
}

# --- echo route: hydrate HASS_TOKEN then delegate to bravros ha say ---
# The 1Password reference is configuration, never a constant: a hardcoded vault/item is one
# operator's, and this file ships to everyone. BRAVROS_HASS_TOKEN_OP_REF wins, then the
# "hass_token_op_ref" key of ha-devices.json; with neither set `op` is not called at all and
# an already-exported HASS_TOKEN (interactive shells hydrate one) still works. The reference
# is passed to `op read` and never printed — the token must not reach a transcript or a log.
if [ -z "$HASS_TOKEN" ] && command -v op >/dev/null 2>&1; then
  HASS_TOKEN_OP_REF="${BRAVROS_HASS_TOKEN_OP_REF:-$(json_string_field "$HA_DEVICES_FILE" hass_token_op_ref)}"
  if [ -n "$HASS_TOKEN_OP_REF" ]; then
    HASS_TOKEN="$(op read "$HASS_TOKEN_OP_REF" 2>/dev/null)"
    export HASS_TOKEN
  fi
fi

# --- clear Do Not Disturb on the target ---
# Alexa drops announcements to a DND device and HA still returns 200, so a DND'd target is
# indistinguishable from success. Clear it first rather than lose the message silently.
# Slug resolution is `devices.<name>` from HA_DEVICES_FILE, with the name as its own slug when
# the file or the key is missing — byte-for-byte the behaviour of ha.DeviceSlug (devices.go),
# because it is now literally the same map rather than a second copy of it.
device_slug() {
  local slug
  slug="$(json_string_field "$HA_DEVICES_FILE" devices "$1")"
  if [ -n "$slug" ]; then printf '%s\n' "$slug"; else printf '%s\n' "$1"; fi
}

clear_dnd() {
  [ "${BRAVROS_DND_AUTOCLEAR:-1}" = "1" ] || return 0
  [ -n "$HASS_TOKEN" ] || return 0
  command -v curl >/dev/null 2>&1 || return 0
  local srv slug
  srv="${HASS_SERVER:-http://homeassistant.local:8123}"
  slug="$(device_slug "$DEVICE")"
  curl -sf -m 4 -o /dev/null -X POST \
    -H "Authorization: Bearer $HASS_TOKEN" -H "Content-Type: application/json" \
    -d "{\"entity_id\":\"switch.${slug}_do_not_disturb_switch\"}" \
    "$srv/api/services/switch/turn_off" 2>/dev/null
  return 0
}

# MacBook-at-home safety net: if creds/CLI/reachability fail, fall back to local say so the
# message is still heard. The Studio never falls back (intentionally Echo-only).
ha_ok=1
[ -z "$HASS_TOKEN" ] && ha_ok=0
command -v bravros >/dev/null 2>&1 || ha_ok=0
# HASS_SERVER is an OPTIONAL caller-exported reachability pre-check: when set
# (e.g. an interactive shell with op_lazy loaded), we probe HA before dispatch.
# When unset (e.g. Claude's bash, which doesn't export it), this curl is skipped
# and we rely on `bravros ha say` returning non-zero + the say_local fallback
# below to handle an unreachable HA. So a missing HASS_SERVER is safe, not a bug.
if [ "$ha_ok" = "1" ] && [ -n "$HASS_SERVER" ]; then
  curl -sf -m 3 -o /dev/null -H "Authorization: Bearer $HASS_TOKEN" "$HASS_SERVER/api/" || ha_ok=0
fi

if [ "$ha_ok" = "1" ]; then
  clear_dnd
  # stdout redirected too: the CLI prints "Sent to <device>: …" and every caller is a
  # skill or hook whose terminal that line would pollute. Failures still route to say_local.
  bravros ha say "${PASS_FLAGS[@]}" "$MSG" "$DEVICE" >/dev/null 2>&1 || { [ "$IS_STUDIO" = "0" ] && say_local; }
else
  [ "$IS_STUDIO" = "0" ] && say_local
fi
exit 0
