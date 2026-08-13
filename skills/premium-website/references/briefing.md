# premium-website — anti-slop design system router

Overrides default LLM design biases (Inter font, purple gradients, centered 3-card
layouts, neon glows, fake "John Doe" data) with a curated system. **The rules live in the
packs — do not improvise from memory; read the matching pack before writing UI code.**
Packs combine cleanly (e.g. taste + output).

| Pack | File | When to read |
|---|---|---|
| **taste** (default) | `references/taste.md` | New React/Next.js builds — dials (VARIANCE 8 / MOTION 6 / DENSITY 4), creative arsenal, bento paradigm |
| **redesign** | `references/redesign-skill.md` | Existing project needing a design audit/upgrade (100+ checks) |
| **soft** | `references/soft-skill.md` | Expensive agency-tier look — Apple/Linear aesthetic |
| **output** | `references/output-skill.md` | AI being lazy — placeholders, half-finished code |
| **minimalist** | `references/minimalist-skill.md` | Clean editorial — monochrome, crisp borders, Notion/Linear |
| **brutalist** | `references/brutalist-skill.md` | Raw mechanical — Swiss print + CRT terminal (beta) |
| **stitch** | `references/stitch-skill.md` | Google Stitch semantic rules + DESIGN.md export |

## Dev server

NEVER launch the dev server via run_in_background — it times out. The operator prefers
running dev servers in their own terminal: announce, then AskUserQuestion (run `npx vite --host` vs "I'll start it myself").

<!-- announce-template: "Prévia do servidor pronta. Aguardando instrução para iniciar. Projeto {PROJECT}." -->
```bash
bash ~/.claude/scripts/announce.sh "Prévia do servidor pronta. Aguardando instrução para iniciar. Projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." studio >/dev/null 2>&1 || true
```

## Imagery — human-in-the-loop, never an API call

No Lorem-Picsum/stock slop for hero/mood/section imagery. You write the prompt; the
operator generates it in the Gemini app (Nano Banana Pro) and hands back the file.

- **Prompt richness dominates quality**: under ~400 chars, or missing a concrete lens + lighting setup + color grade, produces slop — rewrite before handing over. Good example: the Versatile contact-gate prompt in `~/Sites/siteversatile/image-prompt.md`.
- Write `image-prompt.md` in the project root, one block per image, each with ALL of: Intent (slot + position) · Composition · Lens/Camera (body + lens + aperture) · Lighting (direction, Kelvin, practicals) · Color grade · Mood (one, committed) · Brand details inline (hex, wordmark in QUOTES) · Avoidance list · Aspect. Ask for max resolution.
- Approved files land at `./generated/approved/<slot>.jpg` + sidecar `<slot>.prompt.txt` (exact prompt used) so each image stays traceable. **Never ship a draft image in the final build.**
- **Brand fidelity has a ceiling:** 4-5 iterations on one image = stop; put the remaining brand detail in the prompt text rather than hoping the model infers it.
