#!/usr/bin/env python3
"""Reconcile the Bravros managed-global block into a user's ~/.claude/CLAUDE.md.

The managed global (SDLC/workflow rules) is deployed into a marker-delimited region so
each user's personal content OUTSIDE the markers survives every update. This script owns
the deterministic reconcile; the LLM-guided first-migration cleanup lives in the
verify-install skill prose.

Behavior (deterministic, no LLM, no secrets):
  - dest missing            -> write the managed block only.                 [created]
  - dest has BOTH markers   -> replace ONLY the region between them.         [updated / unchanged]
    (legacy kaisser-branded and annotated marker variants are recognized and
     normalized to the canonical plain markers in the same pass)
  - dest without markers    -> back up, keep existing content as personal
                               (on top), append the managed block below.     [migrated]

Exit code is always 0 on success; the outcome word is printed to stdout so callers can log it.

Usage:
  reconcile-global-claude.py --src <managed-source.md> --dest <~/.claude/CLAUDE.md> [--backup <path>]
"""
import argparse
import os
import re
import shutil
import sys

# Canonical markers — the plain form used by deploy.go, deploy_b0148_test.go and the
# verify-install briefing. Always WRITTEN in this form.
BEGIN = "# >>> bravros-managed-global >>>"
END = "# <<< bravros-managed-global <<<"

# Deployed files in the wild carry historical variants: the legacy kaisser-branded
# markers, and/or a parenthetical annotation embedded in the BEGIN line
# ("(auto-updated by install.sh / …)"). RECOGNIZE all of them when locating the
# managed region — an exact-substring match on the canonical form would miss those
# files, fall through to the migration path, and duplicate the whole block.
BEGIN_RE = re.compile(r"(?m)^# >>> (?:bravros|kaisser)-managed-global[^\n]*>>>[ \t]*$")
END_RE = re.compile(r"(?m)^# <<< (?:bravros|kaisser)-managed-global <<<[ \t]*$")

# The do-not-edit warning lives INSIDE the block, not in the marker string, so the
# marker stays byte-stable for exact matching by every consumer.
NOTE = "<!-- auto-updated by install.sh / verify-install — do not edit inside this block -->"


def build_block(src_text: str) -> str:
    return f"{BEGIN}\n{NOTE}\n{src_text.strip()}\n{END}\n"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--src", required=True, help="managed-global source markdown")
    ap.add_argument("--dest", required=True, help="target ~/.claude/CLAUDE.md")
    ap.add_argument("--backup", default="", help="if a change is written to an existing dest, copy it here first")
    ap.add_argument("--check", action="store_true", help="dry-run: print the outcome word but write nothing")
    args = ap.parse_args()

    if not os.path.isfile(args.src):
        print(f"reconcile: source not found: {args.src}", file=sys.stderr)
        return 1

    with open(args.src, "r", encoding="utf-8") as f:
        block = build_block(f.read())

    dest = os.path.expanduser(args.dest)

    # Case 1: no dest -> seed with the managed block only.
    if not os.path.isfile(dest):
        if args.check:
            print("created")
            return 0
        os.makedirs(os.path.dirname(dest) or ".", exist_ok=True)
        with open(dest, "w", encoding="utf-8") as f:
            f.write(block)
        print("created")
        return 0

    with open(dest, "r", encoding="utf-8") as f:
        current = f.read()

    def backup():
        if args.backup:
            try:
                shutil.copy2(dest, os.path.expanduser(args.backup))
            except OSError:
                pass

    begin_m = BEGIN_RE.search(current)
    end_m = END_RE.search(current, begin_m.end()) if begin_m else None

    # Case 2: a recognizable marker pair, well-ordered (END after BEGIN) -> replace only the
    # region between them, normalizing any variant markers to the canonical form. If no END
    # follows the BEGIN, or a marker is missing, fall through to the migrated path rather
    # than silently nuking everything after BEGIN — the whole point of this script is to
    # never destroy personal content on a malformed/interrupted-write file.
    if begin_m and end_m:
        pre = current[: begin_m.start()]
        post = current[end_m.end():]
        new = pre + block.rstrip("\n") + post
        # Normalise: exactly one trailing newline.
        new = new.rstrip("\n") + "\n"
        if new == current:
            print("unchanged")
            return 0
        if args.check:
            print("updated")
            return 0
        backup()
        with open(dest, "w", encoding="utf-8") as f:
            f.write(new)
        print("updated")
        return 0

    # Case 3: no (complete) marker pair -> first migration. Preserve existing content as
    # personal, append the managed block below. Never destroys anything.
    if args.check:
        print("migrated")
        return 0
    backup()
    personal = current.rstrip("\n")
    note = ("<!-- Everything ABOVE this line is your personal global config and is preserved on\n"
            "     every update. Everything inside the managed markers below is auto-updated —\n"
            "     edit it in the toolkit repo (home/CLAUDE.md), not here. -->")
    new = f"{personal}\n\n{note}\n\n{block}"
    with open(dest, "w", encoding="utf-8") as f:
        f.write(new)
    print("migrated")
    return 0


if __name__ == "__main__":
    sys.exit(main())
