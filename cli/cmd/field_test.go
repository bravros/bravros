package cmd

import "testing"

func TestFieldExtract_TopLevelString(t *testing.T) {
	json := `{"base_branch": "main", "branch": "feat/test"}`
	got := fieldExtract(json, "base_branch")
	if got != "main" {
		t.Errorf("expected 'main', got '%s'", got)
	}
}

func TestFieldExtract_TopLevelBool(t *testing.T) {
	json := `{"has_ci": true, "file_exists": false}`
	if got := fieldExtract(json, "has_ci"); got != "true" {
		t.Errorf("expected 'true', got '%s'", got)
	}
	if got := fieldExtract(json, "file_exists"); got != "false" {
		t.Errorf("expected 'false', got '%s'", got)
	}
}

func TestFieldExtract_TopLevelNumber(t *testing.T) {
	json := `{"count": 42}`
	if got := fieldExtract(json, "count"); got != "42" {
		t.Errorf("expected '42', got '%s'", got)
	}
}

func TestFieldExtract_NestedDotPath(t *testing.T) {
	json := `{"stack": {"framework": "laravel", "language": "php"}}`
	got := fieldExtract(json, "stack.framework")
	if got != "laravel" {
		t.Errorf("expected 'laravel', got '%s'", got)
	}
}

func TestFieldExtract_DeepNested(t *testing.T) {
	json := `{"env": {"deployed": {"url": "https://example.com"}}}`
	got := fieldExtract(json, "env.deployed.url")
	if got != "https://example.com" {
		t.Errorf("expected 'https://example.com', got '%s'", got)
	}
}

func TestFieldExtract_KeyWithSlash(t *testing.T) {
	// Monorepo key like "apps/api" contains a slash — the dot separates nesting levels
	json := `{"stacks": {"apps/api": {"framework": "laravel"}}}`
	got := fieldExtract(json, "stacks.apps/api.framework")
	if got != "laravel" {
		t.Errorf("expected 'laravel', got '%s'", got)
	}
}

func TestFieldExtract_MissingField(t *testing.T) {
	json := `{"base_branch": "main"}`
	got := fieldExtract(json, "nonexistent")
	if got != "" {
		t.Errorf("expected empty string, got '%s'", got)
	}
}

func TestFieldExtract_MissingNestedField(t *testing.T) {
	json := `{"stack": {"framework": "laravel"}}`
	got := fieldExtract(json, "stack.missing")
	if got != "" {
		t.Errorf("expected empty string, got '%s'", got)
	}
}

func TestFieldExtract_EmptyJSON(t *testing.T) {
	got := fieldExtract("", "field")
	if got != "" {
		t.Errorf("expected empty string, got '%s'", got)
	}
}

func TestFieldExtract_InvalidJSON(t *testing.T) {
	got := fieldExtract("not json", "field")
	if got != "" {
		t.Errorf("expected empty string, got '%s'", got)
	}
}

func TestFieldExtract_NestedObjectReturnsJSON(t *testing.T) {
	json := `{"stack": {"framework": "laravel", "language": "php"}}`
	got := fieldExtract(json, "stack")
	// Should return the nested object as JSON
	if got == "" {
		t.Error("expected non-empty JSON for nested object")
	}
}

// --- fieldExtractFound (P-0121 follow-up #15 — strict-mode sibling) ---
//
// fieldExtractFound is the (value, found) variant used by strict callers like
// `bravros meta --field`. Skill typos previously regressed real fixes because
// the lenient fieldExtract returned "" silently with exit 0; metaCmd now exits
// non-zero on found=false.

func TestFieldExtractFound_TopLevelHit(t *testing.T) {
	val, found := fieldExtractFound(`{"branch": "homolog"}`, "branch")
	if !found || val != "homolog" {
		t.Errorf("expected (homolog,true), got (%q,%v)", val, found)
	}
}

func TestFieldExtractFound_NestedHit(t *testing.T) {
	val, found := fieldExtractFound(`{"stack": {"framework": "laravel"}}`, "stack.framework")
	if !found || val != "laravel" {
		t.Errorf("expected (laravel,true), got (%q,%v)", val, found)
	}
}

func TestFieldExtractFound_TopLevelMiss(t *testing.T) {
	_, found := fieldExtractFound(`{"branch": "homolog"}`, "totally.not.real")
	if found {
		t.Errorf("expected found=false for unknown path, got true")
	}
}

func TestFieldExtractFound_NestedMiss(t *testing.T) {
	_, found := fieldExtractFound(`{"stack": {"framework": "laravel"}}`, "stack.notakey")
	if found {
		t.Errorf("expected found=false for unknown nested key, got true")
	}
}

// TestFieldExtractFound_NullValueIsFound — null values count as present.
// Skill callers must distinguish "field missing" from "field present but null"
// (e.g. plan frontmatter `pr: null` is valid pre-PR-creation state); the
// second still means "we found it" so should NOT trigger metaCmd's strict-exit.
func TestFieldExtractFound_NullValueIsFound(t *testing.T) {
	val, found := fieldExtractFound(`{"backlog": null}`, "backlog")
	if !found {
		t.Errorf("expected found=true for explicit null, got false")
	}
	if val != "" {
		t.Errorf("expected val=\"\" for null, got %q", val)
	}
}

func TestFieldExtractFound_InvalidJSONReturnsFalse(t *testing.T) {
	_, found := fieldExtractFound("not json at all", "field")
	if found {
		t.Errorf("expected found=false for invalid JSON, got true")
	}
}

func TestFieldExtractFound_DescendIntoNonObjectReturnsFalse(t *testing.T) {
	// "branch" is a string; trying to descend into "branch.foo" is an unknown path
	_, found := fieldExtractFound(`{"branch": "homolog"}`, "branch.foo")
	if found {
		t.Errorf("expected found=false when descending into non-object, got true")
	}
}
