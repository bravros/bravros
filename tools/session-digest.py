#!/usr/bin/env python3
"""Stream Claude Code session transcripts into compact per-project digests.

Usage: python3 scripts/session-digest.py <out-dir>

Reads every ~/.claude/projects/**/*.jsonl line by line, skips lines > 2MB
(the transcript-mining rule: no regexes over raw transcripts), and writes
<out-dir>/<project>.jsonl with one compact record per user prompt, assistant
text block, Skill invocation, Bash command, AskUserQuestion, Agent/Workflow
dispatch, or police-block tool result. Record fields: f (session id prefix),
ts, r (user|asst|tool|meta), k (prompt|cmd|text|skill|bash|ask|agent|workflow|
police|session), t (text, 600 chars). <out-dir>/_stats.json holds skill
invocation counts and sessions per project.

Used by the 2026-09-11 skills audit (B-0037..B-0044 came out of it); safe to
hand to parallel subagents — they read the digest, never the raw transcripts.
"""
import json, os, sys, glob, collections

ROOT = os.path.expanduser("~/.claude/projects")
OUT = sys.argv[1]
MAXLINE = 2 * 1024 * 1024
TRUNC = 600
os.makedirs(OUT, exist_ok=True)

stats = collections.Counter()
skill_counts = collections.Counter()
per_project_sessions = collections.Counter()


def text_of(content):
    if isinstance(content, str):
        return content
    parts = []
    if isinstance(content, list):
        for b in content:
            if isinstance(b, dict) and b.get("type") == "text":
                parts.append(b.get("text", ""))
    return "\n".join(parts)


def rec(out, base, ts, role, kind, text, extra=None):
    text = (text or "").strip()
    if not text:
        return
    r = {"f": base, "ts": ts, "r": role, "k": kind, "t": text[:TRUNC]}
    if len(text) > TRUNC:
        r["len"] = len(text)
    if extra:
        r.update(extra)
    out.write(json.dumps(r, ensure_ascii=False) + "\n")


for proj in sorted(os.listdir(ROOT)):
    pdir = os.path.join(ROOT, proj)
    if not os.path.isdir(pdir):
        continue
    files = sorted(glob.glob(os.path.join(pdir, "**", "*.jsonl"), recursive=True))
    if not files:
        continue
    with open(os.path.join(OUT, proj + ".jsonl"), "w") as out:
        for fp in files:
            base = os.path.basename(fp)[:8]
            per_project_sessions[proj] += 1
            first_ts = None
            with open(fp, "rb") as fh:
                for raw in fh:
                    if len(raw) > MAXLINE:
                        stats["skipped_big_lines"] += 1
                        continue
                    try:
                        d = json.loads(raw)
                    except Exception:
                        stats["bad_json"] += 1
                        continue
                    t = d.get("type")
                    ts = (d.get("timestamp") or "")[:16]
                    if first_ts is None and ts:
                        first_ts = ts
                        out.write(json.dumps({"f": base, "ts": ts, "r": "meta", "k": "session",
                                              "t": f"cwd={d.get('cwd','')} branch={d.get('gitBranch','')} v={d.get('version','')}"}) + "\n")
                    msg = d.get("message") or {}
                    content = msg.get("content")
                    if t == "user":
                        if isinstance(content, list) and all(isinstance(b, dict) and b.get("type") == "tool_result" for b in content):
                            for b in content:
                                c = b.get("content")
                                ctext = c if isinstance(c, str) else text_of(c)
                                if "Police Block" in ctext or "police unlock" in ctext or "promote unlock" in ctext or "mint" in ctext and "token" in ctext:
                                    rec(out, base, ts, "tool", "police", ctext[:700])
                                    stats["police_results"] += 1
                            continue
                        txt = text_of(content)
                        if txt.startswith("<local-command-stdout>") or txt.startswith("<command-name>"):
                            kind = "cmd"
                        elif txt.startswith("<task-notification>") or txt.startswith("<system-reminder>"):
                            continue
                        else:
                            kind = "prompt"
                        rec(out, base, ts, "user", kind, txt)
                        stats["user"] += 1
                    elif t == "assistant":
                        if isinstance(content, list):
                            for b in content:
                                if not isinstance(b, dict):
                                    continue
                                if b.get("type") == "text":
                                    rec(out, base, ts, "asst", "text", b.get("text", ""))
                                    stats["asst_text"] += 1
                                elif b.get("type") == "tool_use":
                                    name = b.get("name", "")
                                    inp = b.get("input") or {}
                                    if name == "Skill":
                                        sk = inp.get("skill", "")
                                        skill_counts[sk] += 1
                                        rec(out, base, ts, "asst", "skill", f"/{sk} {inp.get('args','') or ''}")
                                    elif name == "Bash":
                                        rec(out, base, ts, "asst", "bash", inp.get("command", "")[:300])
                                    elif name == "AskUserQuestion":
                                        qs = inp.get("questions") or []
                                        rec(out, base, ts, "asst", "ask", " | ".join(q.get("question", "") for q in qs if isinstance(q, dict)))
                                    elif name in ("Agent", "Workflow"):
                                        rec(out, base, ts, "asst", name.lower(), (inp.get("description") or inp.get("name") or "")[:200])
                        elif isinstance(content, str):
                            rec(out, base, ts, "asst", "text", content)

with open(os.path.join(OUT, "_stats.json"), "w") as f:
    json.dump({"stats": stats, "skills": skill_counts.most_common(), "sessions": per_project_sessions.most_common()}, f, indent=1, ensure_ascii=False)
print(json.dumps(stats), file=sys.stderr)
