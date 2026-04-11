---
name: laravel-db-diagram
description: >
  Generate Excalidraw ER/database diagrams from Laravel migration files.
  Use this skill whenever the user wants to visualize their database schema,
  create an ER diagram, show table relationships, map out the data model,
  or says "db diagram", "database diagram", "ER diagram", "schema diagram",
  "show me the tables", "visualize the database", or "map the models".
  Also triggers when the user wants to document database structure for a README,
  onboarding guide, or technical spec. Reads migration files directly — no live
  database connection needed.
---

## Model Requirement

This skill runs well on **Sonnet**. No deep reasoning required — mechanical/scripted operations.

# Excalidraw DB Diagram Generator

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

Generate ER (Entity-Relationship) diagrams from Laravel migration files as `.excalidraw` JSON files.

This skill is a companion to `excalidraw-diagram` — it shares the same render pipeline, color palette, and Excalidraw JSON format, but is specialized for database schema visualization.

---

## When to Use This vs excalidraw-diagram

| Use This Skill | Use excalidraw-diagram |
|----------------|----------------------|
| Database/ER diagrams | Architecture diagrams |
| Table relationships | Workflow/sequence diagrams |
| Migration-based schemas | Concept/mental model diagrams |
| Data model documentation | General technical diagrams |

---

## Process

### Step 1/5: Discover Migrations

Scan the project's migration directory:

```bash
ls database/migrations/*.php
```

Read each migration file to extract:
- Table names (from `Schema::create` and `Schema::table`)
- Columns (name, type, nullable, default, unique)
- Primary keys
- Foreign keys (column, referenced table, referenced column)
- Indexes
- Pivot/junction tables (tables with two `foreignId` columns)

### Step 2/5: Build the Schema Model

From the migrations, construct a mental model:

```
tables:
  - name: users
    columns: [id, name, email, ...]
    relationships:
      - hasMany: posts (via posts.user_id)
      - hasMany: orders (via orders.user_id)

  - name: posts
    columns: [id, user_id, title, body, ...]
    relationships:
      - belongsTo: users (via user_id)
      - belongsToMany: tags (via post_tag)
```

**Relationship detection rules:**
- `foreignId('user_id')` or `foreign('user_id')->references('id')->on('users')` = belongsTo
- Pivot tables (exactly 2 foreign keys + optional timestamps) = belongsToMany
- `morphs('commentable')` = polymorphic relationship
- Infer `hasMany`/`hasOne` from the inverse of `belongsTo`

### Step 3/5: Plan the Layout

**Layout strategy for ER diagrams:**

1. **Core entities first** — tables with the most relationships go in the center
2. **Related tables nearby** — place tables with foreign keys adjacent to the table they reference
3. **Pivot tables between** — junction tables sit between the two tables they connect
4. **Leaf tables on edges** — tables with only one relationship go on the periphery

**Grid-based positioning:**
- Use a grid with ~400px column width and ~350px row height
- Core entities get larger boxes (wider to fit more columns)
- Start top-left, flow left-to-right, then top-to-bottom

### Step 4/5: Generate Excalidraw JSON

For each table, generate:

1. **Table header** — Rectangle with table name (use Primary/Neutral colors from palette)
2. **Column list** — Free-floating text below the header, one line per column
3. **Relationship arrows** — Connecting foreign keys to referenced tables

**Read these reference files before generating:**
- `../excalidraw-diagram/references/color-palette.md` — All colors
- `../excalidraw-diagram/references/element-templates.md` — JSON element templates
- `references/db-element-patterns.md` — DB-specific element patterns (table boxes, column lists, relationship arrows)

### Step 5/5: Render & Validate

Use the same render pipeline as the base excalidraw-diagram skill:

```bash
cd .claude/skills/excalidraw-diagram/references && uv run python render_excalidraw.py <path-to-file.excalidraw>
```

Then Read the resulting PNG and fix any issues. Follow the same render-view-fix loop documented in the excalidraw-diagram skill.

---

## ER Diagram Design Rules

### Table Representation

Each table is a vertical stack:
- **Header rectangle**: table name in bold, colored by role
- **Column text block**: columns listed as `column_name  type  [constraints]`
- Primary keys marked with a key indicator
- Foreign keys highlighted (they're the relationship endpoints)

### Color Coding by Role

Use colors from the shared `color-palette.md`:

| Table Role | Fill Color | Stroke Color |
|-----------|-----------|-------------|
| Core entity (users, etc.) | Primary fill | Primary stroke |
| Secondary entity | Secondary fill | Primary stroke |
| Pivot/junction table | Decision fill (yellow) | Decision stroke |
| Lookup/reference table | Tertiary fill | Primary stroke |
| Polymorphic | AI/LLM fill (purple) | AI/LLM stroke |

### Relationship Lines

| Relationship | Arrow Style | Label |
|-------------|-------------|-------|
| belongsTo (1:N) | Solid arrow → referenced table | FK column name |
| belongsToMany (N:M) | Dashed arrows from both tables → pivot | via pivot_table |
| morphs (polymorphic) | Dotted arrow | morphable |
| hasOne | Thin solid arrow | 1:1 |

### Typography

- Table name: fontSize 20, bold, title color from palette
- Column names: fontSize 14, fontFamily 3 (monospace), body color
- Type annotations: fontSize 12, body/detail color
- Constraint badges (PK, FK, UQ, NULL): fontSize 11

### Spacing

- Column line height: 22px
- Table padding: 16px horizontal, 12px vertical
- Min gap between tables: 80px
- Arrow gap from table edge: 4px

---

## Handling Large Schemas

For projects with 15+ tables:

1. **Group by domain** — Identify bounded contexts (auth, billing, content, etc.)
2. **Generate multiple diagrams** — One per domain, plus one overview showing only table names and relationships (no columns)
3. **Overview diagram** — Simplified boxes (just table names), all relationships visible
4. **Detail diagrams** — Full column lists for tables within each domain

Ask the user which approach they prefer before generating.

---

## Scope Control

The user might want to diagram:
- **Full schema** — all tables and relationships
- **Specific tables** — "show me the orders system" (orders, order_items, products, etc.)
- **Single table + neighbors** — one table and everything directly related to it

Ask if the scope isn't clear from context. Default to full schema for small projects (<10 tables) and ask for larger ones.

---

## Quality Checklist

1. **All tables from migrations included** (or explicitly scoped)
2. **All foreign keys shown as relationship arrows**
3. **Pivot tables identified and placed between related tables**
4. **Column types accurate** (match migration definitions)
5. **No orphan tables** (every table connected unless truly standalone)
6. **Readable at rendered size** (column text not too small)
7. **Balanced layout** (no huge gaps or cramped clusters)
8. **Rendered and visually validated** via the render pipeline
