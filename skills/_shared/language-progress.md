# Language & Step Progress

## Language Detection

Detect the user's language from their messages in the conversation. Respond in that language throughout execution — responses, questions, summaries, and step labels all adapt to the user's language.

Skill instructions (the SKILL.md file) stay in English. Only your **output** changes.

If language is unclear, default to English.

## Step Progress Format

Show step progress using emoji format at the start of each major step:

```
{emoji} {skill-name} [{current}/{total}] {Step description in user's language}
```

The echo command inside bash blocks uses the same format:

```bash
echo "{emoji} {skill-name} [{current}/{total}] {description}"
```

### Examples by Language

**English:**
```
📋 plan [1/4] Reading the template
📋 plan [2/4] Exploring the codebase
📋 plan [3/4] Creating plan file
📋 plan [4/4] Presenting results
```

**Portuguese (PT-BR):**
```
📋 plan [1/4] Lendo o template
📋 plan [2/4] Explorando o codigo
📋 plan [3/4] Criando arquivo do plano
📋 plan [4/4] Apresentando resultados
```

**Spanish (ES):**
```
📋 plan [1/4] Leyendo la plantilla
📋 plan [2/4] Explorando el codigo
📋 plan [3/4] Creando archivo del plan
📋 plan [4/4] Presentando resultados
```

## Emoji Mapping by Skill

Use the assigned emoji for your skill. If not listed, use the category default.

### Planning (📋)

| Skill | Emoji |
|-------|-------|
| plan | 📋 |
| plan-review | 📋 |
| plan-approved | 📋 |
| plan-check | 📋 |
| plan-wt | 📋 |
| resume | 📋 |
| backlog | 📋 |
| tdd-review | 📋 |
| session-recap | 📋 |
| address-recap | 📋 |
| flow | 📋 |
| auto-pr | 📋 |
| auto-pr-wt | 📋 |
| auto-merge | 📋 |
| quick | 📋 |

### Git & PR (🔀)

| Skill | Emoji |
|-------|-------|
| commit | 🔀 |
| ship | 🔀 |
| push | 🔀 |
| branch | 🔀 |
| pr | 🔀 |
| review | 🔀 |
| address-pr | 🔀 |
| finish | 🔀 |
| complete | 🔀 |
| hotfix | 🔀 |
| merge-chain | 🔀 |

### Testing & Quality (🧪)

| Skill | Emoji |
|-------|-------|
| test | 🧪 |
| run-tests | 🧪 |
| coverage | 🧪 |
| debug | 🧪 |
| audit | 🧪 |

### Project Setup (✨)

| Skill | Emoji |
|-------|-------|
| start | ✨ |
| context | ✨ |
| status | ✨ |
| verify-install | ✨ |
| sync-upstream | ✨ |

### Design & UI (🎨)

| Skill | Emoji |
|-------|-------|
| brand-generator | 🎨 |
| brand-guidelines | 🎨 |
| premium-website | 🎨 |
| frontend-design | 🎨 |
| generate-component | 🎨 |
| excalidraw-diagram | 🎨 |
| laravel-db-diagram | 🎨 |

### Content & Media (📹)

| Skill | Emoji |
|-------|-------|
| remotion-video | 📹 |
| yt-search | 📹 |
| notebooklm | 📹 |

### DevOps & Deploy (🚀)

| Skill | Emoji |
|-------|-------|
| cf-pages-deploy | 🚀 |
| cf-browser | 🚀 |
| firecrawl | 🚀 |

### Reports (📊)

| Skill | Emoji |
|-------|-------|
| report | 📊 |
| user-report | 📊 |

### Tools (🔧)

| Skill | Emoji |
|-------|-------|
| skill-creator | 🔧 |
| drop-feature | 🔧 |
| migration-audit | 🔧 |
| squash-migrations | 🔧 |

### Personal Skills

| Skill | Emoji |
|-------|-------|
| home-assistant-manager | 🏠 |
| unifi | 🌐 |
| tiktok-shop | 🛒 |
| listmonk | 📧 |
| uptime-kuma | 📈 |
| n8n | 🔗 |
| remove-watermark | 🖼️ |
| voice-cloning-skill | 🎙️ |
| obsidian-setup | 📓 |
| obsidian-migrate | 📓 |

## Language Override

Some skills have hardcoded language requirements:

- **user-report**: Always outputs in PT-BR regardless of user language (branded PDF reports for Brazilian stakeholders)
- **generate-component**: Generated UI code uses PT-BR text by default. The skill's responses/explanations follow the user's language, but the output code stays PT-BR.
