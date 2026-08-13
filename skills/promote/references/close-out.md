# /promote — Step 5: sync, plan closure, token consumption

Loaded by SKILL.md. Runs after the merge is verified `MERGED`.

## Sync homolog from main

```bash
git checkout homolog

# Re-derive the PRE-merge base — the only surviving witness of where main was before the promote.
PROMOTE_BASE=$(git rev-parse --verify --quiet refs/bravros/promote-base || true)

git fetch origin
if ! git merge --ff-only origin/main; then
  echo "ℹ️  Fast-forward merge failed (homolog and main diverged). Falling back to merge commit."
  git merge --no-ff -m "🔀 merge: sync homolog from main (post-promote)" origin/main
fi
git push origin homolog

# Release merge-lock now that the main push is complete (see merge-flow.md step 5).
bravros merge-lock release

# B-0269: clean up PR review cache now that this PR landed on main.
rm -f "/tmp/review-cache-${PR_NUMBER}.txt" ".planning/.review-cache-${PR_NUMBER}.txt"
```

## Plan/backlog closure — events append, no renames

Close ONLY plans whose PR is actually in the promoted range. Bare `P-NNNN` mentions are
**advisory only, never auto-closed** — mention-based closure once closed the wrong plan
(P-0185).

```bash
# ⚠️ zsh-compatible array assignment only — `mapfile` doesn't exist in zsh and would
#    silently make the loop iterate zero times.
# ⚠️ Range against $PROMOTE_BASE, NEVER a live origin/main (empty by construction
#    post-merge) and never local main (can lag → widened range → false closes).
if [ -n "$PROMOTE_BASE" ]; then
  # PR numbers ACTUALLY in the promoted range: merge-commit + squash-merge subjects.
  RANGE_PRS=($(git log --format=%s "$PROMOTE_BASE..homolog" 2>/dev/null \
    | grep -oE 'Merge pull request #[0-9]+|\(#[0-9]+\)' | grep -oE '[0-9]+' | sort -u))
  MENTIONED_PLANS=($(git log --format=%B "$PROMOTE_BASE..homolog" 2>/dev/null | grep -oE 'P-[0-9]+' | sort -u))
else
  echo "⚠️ refs/bravros/promote-base is missing — Step 1 did not run in this promote." >&2
  echo "⚠️ Skipping plan/backlog closure rather than walking an empty range (non-fatal)." >&2
  RANGE_PRS=()
  MENTIONED_PLANS=()
fi

TS=$(date -u +%FT%TZ)
CLOSED_IDS=""
# Legacy plans: `pr:` frontmatter names a PR in the range.
CANDIDATE_FILES=($(ls .planning/*-{reviewed,approved,todo}.md .planning/P-*/PLAN.md .planning/P-*.md 2>/dev/null | sort -u))
for PLAN_FILE in "${CANDIDATE_FILES[@]}"; do
  PLAN_PR=$(grep -m1 -E '^pr:' "$PLAN_FILE" | grep -oE '[0-9]+' | head -1)
  [ -z "$PLAN_PR" ] && continue
  case " ${RANGE_PRS[*]} " in *" $PLAN_PR "*) ;; *) continue ;; esac
  pid=$(basename "$PLAN_FILE" | grep -oE 'P-[0-9]+' | head -1)
  [ -z "$pid" ] && pid=$(echo "$PLAN_FILE" | grep -oE 'P-[0-9]+' | head -1)
  [ -z "$pid" ] && continue
  echo "{\"ts\":\"$TS\",\"id\":\"e_$(date -u +%s)$RANDOM\",\"kind\":\"completed\",\"subject\":\"$pid\",\"pr\":$PLAN_PR,\"by\":\"agent:promote\"}" >> .planning/events.jsonl
  CLOSED_IDS="$CLOSED_IDS $pid"
  BACKLOG_IDS=($(awk '/^backlog:/{flag=1;next} /^[a-z_]+:/{flag=0} flag' "$PLAN_FILE" | grep -oE 'B-[0-9]+' | sort -u))
  for bid in "${BACKLOG_IDS[@]}"; do
    echo "{\"ts\":\"$TS\",\"id\":\"e_$(date -u +%s)$RANDOM\",\"kind\":\"completed\",\"subject\":\"$bid\",\"by\":\"agent:promote\"}" >> .planning/events.jsonl
  done
done
# New-model plans: `pr_opened` events whose PR is in the range and that lack a `completed` event.
for prn in "${RANGE_PRS[@]}"; do
  pid=$(grep -h '"kind":"pr_opened"' .planning/events*.jsonl 2>/dev/null \
    | grep -E "\"pr\": ?\"?#?${prn}\"?[,}]" | tail -1 | grep -oE '"subject":"[^"]+"' | cut -d'"' -f4)
  [ -z "$pid" ] && continue
  case " $CLOSED_IDS " in *" $pid "*) continue ;; esac
  grep -h '"kind":"completed"' .planning/events*.jsonl 2>/dev/null | grep -q "\"subject\":\"$pid\"" && continue
  echo "{\"ts\":\"$TS\",\"id\":\"e_$(date -u +%s)$RANDOM\",\"kind\":\"completed\",\"subject\":\"$pid\",\"pr\":$prn,\"by\":\"agent:promote\"}" >> .planning/events.jsonl
  CLOSED_IDS="$CLOSED_IDS $pid"
done
# Advisory list: plans merely MENTIONED in commit bodies — print, NEVER close.
for pid in "${MENTIONED_PLANS[@]}"; do
  case " $CLOSED_IDS " in *" $pid "*) continue ;; esac
  echo "ℹ️  candidate to close (mentioned in range, no PR match): $pid — if it really shipped, append its completed event by hand"
done

git diff --quiet HEAD -- .planning/events.jsonl \
  || { bravros commit "📋 plan: close plans shipped by promote $(date +%F)" .planning/events.jsonl; git push origin homolog; }
```

## Retire the snapshot + consume the token

```bash
# The base ref's only consumer has run. Next promote overwrites it anyway.
git update-ref -d refs/bravros/promote-base 2>/dev/null || true

bravros promote revoke   # single-use consumed
```

## Announces (100% PT-BR)

Token missing (pre-flight):

```bash
# <!-- announce-template: "Aguardando token de promoção. Execute bravros promote unlock em um terminal separado. Projeto {PROJECT}." -->
bravros ha say --force "Aguardando token de promoção. Execute bravros promote unlock em um terminal separado. Projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." studio >/dev/null 2>&1 || true
```

Completion. The release-tag variant is ONLY valid on the portable `claude` repo (every main
merge there auto-tags a release); on product repos `git describe` returns whatever tag is
nearest — announcing it as a release would mislead. Snapshot tags are excluded either way:

```bash
PROMOTE_PROJECT=$(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")" || echo "")
RELEASE_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "")
case "$RELEASE_TAG" in
  pre-deploy-*|pre-reset-*|backup-*) RELEASE_TAG="" ;;   # snapshot tags — not a release
esac
# <!-- announce-template: "Versão {TAG} publicada em produção. Projeto {PROJECT}." -->
if [ "$PROMOTE_PROJECT" = "claude" ] && [ -n "$RELEASE_TAG" ]; then
  bravros ha say --force "Versão ${RELEASE_TAG} publicada em produção. Projeto ${PROMOTE_PROJECT}." studio >/dev/null 2>&1 || true
else
  bravros ha say --force "Promoção concluída. Código publicado em produção. Projeto ${PROMOTE_PROJECT}." studio >/dev/null 2>&1 || true
fi
```
