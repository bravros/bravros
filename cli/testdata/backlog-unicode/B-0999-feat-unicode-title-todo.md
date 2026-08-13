---
id: "B-0999"
title: "Unicode × em-dash — right-arrow → and CJK 日本語テスト"
type: feat
status: new
priority: high
size: medium
project: test-unicode
tags: []
created: "2026-05-14"
plan: null
depends: null
---

# Unicode × em-dash — right-arrow → and CJK 日本語テスト

Test fixture for B-0268: backlog table renderer unicode fix.
This title contains:
- ASCII text
- Multiplication sign: ×
- Em-dash: —
- Right arrow: →
- CJK characters: 日本語テスト

The table renderer must not split any of these multi-byte runes mid-sequence.
