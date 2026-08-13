package projectinit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandPlaceholders(t *testing.T) {
	tests := []struct {
		name        string
		content     string   // file content to write
		wantNoOp    bool     // expect NoOp=true (no placeholders)
		wantErr     bool     // expect an error
		wantContain []string // substrings that must appear in output
		wantAbsent  []string // placeholder tokens that must NOT appear in output
	}{
		{
			name: "all_placeholders_expanded",
			content: `# {{PROJECT_NAME}}
Stack: {{STACK}}
Framework: {{FRAMEWORK}}
Test runner: {{TEST_RUNNER}}
Base branch: {{BASE_BRANCH}}
`,
			wantNoOp: false,
			// Placeholders must be gone from output.
			wantAbsent: []string{
				"{{PROJECT_NAME}}",
				"{{STACK}}",
				"{{FRAMEWORK}}",
				"{{TEST_RUNNER}}",
				"{{BASE_BRANCH}}",
			},
		},
		{
			name:     "empty_file",
			content:  "",
			wantNoOp: true, // no placeholders → silent no-op
		},
		{
			name:     "no_placeholders",
			content:  "# My Project\n\nStack: go\nTest runner: go test\n",
			wantNoOp: true, // no placeholders → silent no-op
		},
		{
			name:        "partial_placeholders",
			content:     "Branch: {{BASE_BRANCH}}\nStack: go\n",
			wantNoOp:    false,
			wantAbsent:  []string{"{{BASE_BRANCH}}"},
			wantContain: []string{"Branch: "},
		},
		{
			name:     "already_expanded_is_noop",
			content:  "# my-project\nStack: go\nBase branch: main\n",
			wantNoOp: true, // already expanded — second pass is silent no-op
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create temp dir with a minimal go.mod so stack detection finds Go.
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module testproject\n\ngo 1.21\n"), 0644)

			// Write the input file.
			inputPath := filepath.Join(dir, "template.md")
			if err := os.WriteFile(inputPath, []byte(tc.content), 0644); err != nil {
				t.Fatalf("failed to write input file: %v", err)
			}

			opts := ExpandOpts{
				Root:      dir,
				InputFile: inputPath,
			}

			result, err := ExpandPlaceholders(opts)

			if tc.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.wantNoOp {
				if !result.NoOp {
					t.Errorf("expected NoOp=true, got NoOp=false")
				}
				// File must be unchanged.
				gotBytes, _ := os.ReadFile(inputPath)
				if string(gotBytes) != tc.content {
					t.Errorf("expected file unchanged on no-op, got:\n%s", string(gotBytes))
				}
				return
			}

			if result.NoOp {
				t.Error("expected NoOp=false, got NoOp=true")
			}

			// Read output file.
			outBytes, err := os.ReadFile(result.OutputFile)
			if err != nil {
				t.Fatalf("cannot read output file: %v", err)
			}
			got := string(outBytes)

			// Verify absent tokens.
			for _, ph := range tc.wantAbsent {
				if strings.Contains(got, ph) {
					t.Errorf("placeholder %q still present in output:\n%s", ph, got)
				}
			}

			// Verify required substrings.
			for _, sub := range tc.wantContain {
				if !strings.Contains(got, sub) {
					t.Errorf("expected %q in output, got:\n%s", sub, got)
				}
			}
		})
	}
}

func TestExpandPlaceholdersMissingFile(t *testing.T) {
	dir := t.TempDir()
	opts := ExpandOpts{
		Root:      dir,
		InputFile: filepath.Join(dir, "nonexistent.md"),
	}

	_, err := ExpandPlaceholders(opts)
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "cannot read file") {
		t.Errorf("expected 'cannot read file' in error, got: %v", err)
	}
}

func TestExpandPlaceholdersOutputFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module myapp\n\ngo 1.21\n"), 0644)

	content := "Project: {{PROJECT_NAME}}\nBase: {{BASE_BRANCH}}\n"
	inputPath := filepath.Join(dir, "template.md")
	outputPath := filepath.Join(dir, "output.md")

	os.WriteFile(inputPath, []byte(content), 0644)

	opts := ExpandOpts{
		Root:       dir,
		InputFile:  inputPath,
		OutputFile: outputPath,
	}

	result, err := ExpandPlaceholders(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OutputFile != outputPath {
		t.Errorf("expected OutputFile=%s, got=%s", outputPath, result.OutputFile)
	}

	// Input file must be unchanged.
	inputBytes, _ := os.ReadFile(inputPath)
	if string(inputBytes) != content {
		t.Error("input file was modified when --output was set")
	}

	// Output file must have placeholders replaced.
	outBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("output file not written: %v", err)
	}
	got := string(outBytes)
	if strings.Contains(got, "{{PROJECT_NAME}}") {
		t.Error("{{PROJECT_NAME}} not expanded in output file")
	}
	if strings.Contains(got, "{{BASE_BRANCH}}") {
		t.Error("{{BASE_BRANCH}} not expanded in output file")
	}
}

func TestExpandPlaceholdersIdempotent(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module myapp\n\ngo 1.21\n"), 0644)

	content := "Stack: {{STACK}}\n"
	inputPath := filepath.Join(dir, "template.md")
	os.WriteFile(inputPath, []byte(content), 0644)

	opts := ExpandOpts{Root: dir, InputFile: inputPath}

	// First run — should expand.
	result1, err := ExpandPlaceholders(opts)
	if err != nil {
		t.Fatalf("first run error: %v", err)
	}
	if result1.NoOp {
		t.Error("first run: expected NoOp=false")
	}

	// Second run — no placeholders remain → no-op.
	result2, err := ExpandPlaceholders(opts)
	if err != nil {
		t.Fatalf("second run error: %v", err)
	}
	if !result2.NoOp {
		t.Error("second run: expected NoOp=true (idempotent)")
	}
}

func TestExpandPlaceholdersPreservesPermissions(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module myapp\n\ngo 1.21\n"), 0644)

	// Input file with non-default permissions (0600 — owner-only read/write).
	inputPath := filepath.Join(dir, "template.md")
	content := "Project: {{PROJECT_NAME}}\n"
	if err := os.WriteFile(inputPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write input: %v", err)
	}

	opts := ExpandOpts{Root: dir, InputFile: inputPath}
	if _, err := ExpandPlaceholders(opts); err != nil {
		t.Fatalf("ExpandPlaceholders error: %v", err)
	}

	info, err := os.Stat(inputPath)
	if err != nil {
		t.Fatalf("stat input after expand: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Errorf("expected permissions preserved as 0600, got %o", got)
	}
}
