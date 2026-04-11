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
