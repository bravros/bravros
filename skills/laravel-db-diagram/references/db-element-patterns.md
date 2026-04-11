# DB Element Patterns

Excalidraw JSON patterns for database/ER diagram elements. Use alongside the shared `color-palette.md` and `element-templates.md` from the `excalidraw-diagram` skill.

---

## Table Box Pattern

A table is a header rectangle + column text block. The header contains the table name, and the columns are free-floating text positioned below it.

### Header Rectangle

```json
{
  "type": "rectangle",
  "id": "table_users_header",
  "x": 100, "y": 100,
  "width": 280, "height": 40,
  "strokeColor": "#1e3a5f",
  "backgroundColor": "#3b82f6",
  "fillStyle": "solid",
  "strokeWidth": 2,
  "strokeStyle": "solid",
  "roughness": 0,
  "opacity": 100,
  "angle": 0,
  "seed": 100001,
  "version": 1,
  "versionNonce": 100002,
  "isDeleted": false,
  "groupIds": [],
  "boundElements": [{"id": "table_users_title", "type": "text"}],
  "link": null,
  "locked": false,
  "roundness": {"type": 3}
}
```

### Header Text (inside rectangle)

```json
{
  "type": "text",
  "id": "table_users_title",
  "x": 110, "y": 107,
  "width": 260, "height": 25,
  "text": "users",
  "originalText": "users",
  "fontSize": 20,
  "fontFamily": 3,
  "textAlign": "center",
  "verticalAlign": "middle",
  "strokeColor": "#ffffff",
  "backgroundColor": "transparent",
  "fillStyle": "solid",
  "strokeWidth": 1,
  "strokeStyle": "solid",
  "roughness": 0,
  "opacity": 100,
  "angle": 0,
  "seed": 100003,
  "version": 1,
  "versionNonce": 100004,
  "isDeleted": false,
  "groupIds": [],
  "boundElements": null,
  "link": null,
  "locked": false,
  "containerId": "table_users_header",
  "lineHeight": 1.25
}
```

### Column Body Rectangle (below header, lighter fill)

```json
{
  "type": "rectangle",
  "id": "table_users_body",
  "x": 100, "y": 140,
  "width": 280, "height": 154,
  "strokeColor": "#1e3a5f",
  "backgroundColor": "#dbeafe",
  "fillStyle": "solid",
  "strokeWidth": 2,
  "strokeStyle": "solid",
  "roughness": 0,
  "opacity": 100,
  "angle": 0,
  "seed": 100005,
  "version": 1,
  "versionNonce": 100006,
  "isDeleted": false,
  "groupIds": [],
  "boundElements": [{"id": "table_users_columns", "type": "text"}],
  "link": null,
  "locked": false,
  "roundness": {"type": 3}
}
```

### Column List Text (inside body rectangle)

Format each column as: `icon column_name  type  [constraints]`

Use these indicators:
- `🔑` or `PK` for primary key
- `FK` for foreign key
- `?` for nullable
- `UQ` for unique

```json
{
  "type": "text",
  "id": "table_users_columns",
  "x": 110, "y": 148,
  "width": 260, "height": 138,
  "text": "PK id          bigint\n   name        string\n   email       string    UQ\n   password    string\n   remember    string    ?\n   timestamps",
  "originalText": "PK id          bigint\n   name        string\n   email       string    UQ\n   password    string\n   remember    string    ?\n   timestamps",
  "fontSize": 14,
  "fontFamily": 3,
  "textAlign": "left",
  "verticalAlign": "top",
  "strokeColor": "#374151",
  "backgroundColor": "transparent",
  "fillStyle": "solid",
  "strokeWidth": 1,
  "strokeStyle": "solid",
  "roughness": 0,
  "opacity": 100,
  "angle": 0,
  "seed": 100007,
  "version": 1,
  "versionNonce": 100008,
  "isDeleted": false,
  "groupIds": [],
  "boundElements": null,
  "link": null,
  "locked": false,
  "containerId": "table_users_body",
  "lineHeight": 1.25
}
```

---

## Sizing Calculations

**Column height formula:**
- Each column line = 22px (fontSize 14 * lineHeight 1.25 ≈ 17.5, rounded up + padding)
- Body rectangle height = (column_count * 22) + 24 (top/bottom padding)
- Total table height = 40 (header) + body height

**Width sizing:**
- Minimum: 240px
- For tables with long column names or types: measure the longest line and add 40px padding
- Typical: 280px works for most Laravel tables

---

## Relationship Arrow Patterns

### belongsTo (Foreign Key → Referenced Table)

Arrow from the FK column's table to the referenced table. Solid line, arrow at end.

```json
{
  "type": "arrow",
  "id": "fk_posts_user_id",
  "x": 380, "y": 200,
  "width": 120, "height": 0,
  "strokeColor": "#1e3a5f",
  "backgroundColor": "transparent",
  "fillStyle": "solid",
  "strokeWidth": 2,
  "strokeStyle": "solid",
  "roughness": 0,
  "opacity": 100,
  "angle": 0,
  "seed": 200001,
  "version": 1,
  "versionNonce": 200002,
  "isDeleted": false,
  "groupIds": [],
  "boundElements": null,
  "link": null,
  "locked": false,
  "points": [[0, 0], [120, 0]],
  "startBinding": {"elementId": "table_posts_body", "focus": 0, "gap": 4},
  "endBinding": {"elementId": "table_users_body", "focus": 0, "gap": 4},
  "startArrowhead": null,
  "endArrowhead": "arrow"
}
```

### belongsToMany (via Pivot Table)

Two arrows: one from each main table to the pivot table. Dashed style.

```json
{
  "strokeStyle": "dashed",
  "startBinding": {"elementId": "table_posts_body", "focus": 0, "gap": 4},
  "endBinding": {"elementId": "table_post_tag_body", "focus": 0, "gap": 4}
}
```

### Polymorphic (morphs)

Dotted arrow from the morphable table to the parent.

```json
{
  "strokeStyle": "dotted",
  "strokeColor": "#6d28d9"
}
```

---

## Relationship Labels

Place a small text element near the midpoint of each relationship arrow:

```json
{
  "type": "text",
  "id": "label_fk_posts_user_id",
  "x": 410, "y": 185,
  "width": 60, "height": 16,
  "text": "user_id",
  "originalText": "user_id",
  "fontSize": 12,
  "fontFamily": 3,
  "textAlign": "center",
  "verticalAlign": "top",
  "strokeColor": "#64748b",
  "backgroundColor": "transparent",
  "fillStyle": "solid",
  "strokeWidth": 1,
  "strokeStyle": "solid",
  "roughness": 0,
  "opacity": 100,
  "angle": 0,
  "seed": 200003,
  "version": 1,
  "versionNonce": 200004,
  "isDeleted": false,
  "groupIds": [],
  "boundElements": null,
  "link": null,
  "locked": false,
  "containerId": null,
  "lineHeight": 1.25
}
```

---

## Pivot Table Pattern

Pivot/junction tables use a distinct color (Decision/yellow) and are smaller since they typically have minimal columns.

Header: `backgroundColor: "#fef3c7"`, `strokeColor: "#b45309"`
Body: `backgroundColor: "#fffbeb"`, `strokeColor: "#b45309"`
Width: 200px (narrower than regular tables)
