package projectinit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bravros/bravros/cli/internal/config"
	"github.com/bravros/bravros/cli/internal/stack"
)

// ExpandOpts configures placeholder expansion.
type ExpandOpts struct {
	// Root is the project root used for stack detection (default ".").
	Root string
	// InputFile is the path to the template file to expand.
	InputFile string
	// OutputFile is the destination path. Empty means in-place (overwrite InputFile).
	OutputFile string
}

// ExpandResult holds the outcome of the expand operation.
type ExpandResult struct {
	// Expanded is true when at least one placeholder was replaced.
	Expanded bool
	// NoOp is true when no placeholders were found (file unchanged).
	NoOp bool
	// OutputFile is the path that was written (may equal InputFile for in-place).
	OutputFile string
	// Replacements maps each placeholder key to the value used.
	Replacements map[string]string
}

// Placeholders is the canonical list of supported template placeholders.
var Placeholders = []string{
	"{{PROJECT_NAME}}",
	"{{STACK}}",
	"{{FRAMEWORK}}",
	"{{TEST_RUNNER}}",
	"{{BASE_BRANCH}}",
}

// ExpandPlaceholders reads InputFile, substitutes template placeholders with
// values sourced from stack detection and .bravros.yml, and writes the result.
// It is idempotent: running twice produces the same output (second run is a no-op).
func ExpandPlaceholders(opts ExpandOpts) (*ExpandResult, error) {
	if opts.Root == "" {
		opts.Root = "."
	}
	if opts.InputFile == "" {
		return nil, fmt.Errorf("InputFile is required")
	}

	// Read input file.
	data, err := os.ReadFile(opts.InputFile)
	if err != nil {
		return nil, fmt.Errorf("cannot read file %s: %w", opts.InputFile, err)
	}

	// Stat to preserve permissions on in-place write.
	inputMode := os.FileMode(0644)
	if info, statErr := os.Stat(opts.InputFile); statErr == nil {
		inputMode = info.Mode().Perm()
	}

	content := string(data)

	// Fast-path: no placeholders present — idempotent no-op.
	hasPH := false
	for _, ph := range Placeholders {
		if strings.Contains(content, ph) {
			hasPH = true
			break
		}
	}
	if !hasPH {
		return &ExpandResult{NoOp: true, OutputFile: resolveOutput(opts)}, nil
	}

	// Resolve placeholder values.
	values, err := resolvePlaceholderValues(opts.Root)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve placeholder values: %w", err)
	}

	// Perform replacements.
	expanded := false
	for ph, val := range values {
		if strings.Contains(content, ph) {
			content = strings.ReplaceAll(content, ph, val)
			expanded = true
		}
	}

	// Determine output path.
	outPath := resolveOutput(opts)

	// Write output. Preserve input file permissions for in-place writes;
	// new output paths use 0644 by default.
	writeMode := inputMode
	if opts.OutputFile != "" && opts.OutputFile != opts.InputFile {
		if info, statErr := os.Stat(opts.OutputFile); statErr == nil {
			writeMode = info.Mode().Perm()
		} else {
			writeMode = 0644
		}
	}
	if err := os.WriteFile(outPath, []byte(content), writeMode); err != nil {
		return nil, fmt.Errorf("failed to write output file %s: %w", outPath, err)
	}

	return &ExpandResult{
		Expanded:     expanded,
		NoOp:         false,
		OutputFile:   outPath,
		Replacements: values,
	}, nil
}

// resolveOutput returns the effective output path.
func resolveOutput(opts ExpandOpts) string {
	if opts.OutputFile != "" {
		return opts.OutputFile
	}
	return opts.InputFile
}

// resolvePlaceholderValues builds the map of placeholder → value using
// stack detection and git context. Never shells out — calls internal APIs only.
func resolvePlaceholderValues(root string) (map[string]string, error) {
	values := map[string]string{
		"{{PROJECT_NAME}}": "",
		"{{STACK}}":        "",
		"{{FRAMEWORK}}":    "",
		"{{TEST_RUNNER}}":  "",
		"{{BASE_BRANCH}}":  "",
	}

	// Stack detection (no git, no versions needed for placeholders).
	detectResult, err := stack.Detect(root, stack.DetectOpts{SkipGit: true})
	if err == nil && detectResult != nil {
		sc := detectResult.Stack

		// PROJECT_NAME: resolves via git remote → directory basename (see projectName).
		values["{{PROJECT_NAME}}"] = projectName(root)

		// STACK: language (e.g. "go", "php", "node").
		values["{{STACK}}"] = sc.Language

		// FRAMEWORK: framework or language fallback.
		fw := sc.Framework
		if fw == "" || fw == "none" {
			fw = sc.Language
		}
		values["{{FRAMEWORK}}"] = fw

		// TEST_RUNNER: detected test runner.
		tr := sc.TestRunner
		if tr == "" || tr == "none" {
			tr = "none"
		}
		values["{{TEST_RUNNER}}"] = tr
	} else {
		// No stack detected — use safe defaults.
		values["{{PROJECT_NAME}}"] = projectName(root)
		values["{{STACK}}"] = "unknown"
		values["{{FRAMEWORK}}"] = "unknown"
		values["{{TEST_RUNNER}}"] = "none"
	}

	// BASE_BRANCH: read from git HEAD (main/master detection).
	values["{{BASE_BRANCH}}"] = detectBaseBranch(root)

	return values, nil
}

// projectName returns the project name from git remote or directory basename.
func projectName(root string) string {
	// Try to extract from .bravros.yml git.remote (e.g. git@github.com:owner/repo.git → repo)
	cfg, found := config.LoadBravrosConfig()
	if found && cfg.Git.Remote != "" {
		remote := cfg.Git.Remote
		// Strip .git suffix and take last segment.
		remote = strings.TrimSuffix(remote, ".git")
		parts := strings.FieldsFunc(remote, func(r rune) bool {
			return r == '/' || r == ':'
		})
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}

	// Fallback: directory basename.
	abs, err := absPath(root)
	if err != nil {
		return "project"
	}
	base := abs
	idx := strings.LastIndexAny(abs, "/\\")
	if idx >= 0 {
		base = abs[idx+1:]
	}
	if base == "" {
		return "project"
	}
	return base
}

// detectBaseBranch reads the Git HEAD for remotes refs/heads/main or remotes/refs/heads/master
// — pure file read, no subprocess.
func detectBaseBranch(root string) string {
	// Try refs/remotes/origin/HEAD to find default branch.
	headRef := readGitFile(root, "refs/remotes/origin/HEAD")
	if headRef != "" {
		// Content: "ref: refs/remotes/origin/main"
		headRef = strings.TrimSpace(headRef)
		if strings.HasPrefix(headRef, "ref: refs/remotes/origin/") {
			return strings.TrimPrefix(headRef, "ref: refs/remotes/origin/")
		}
	}

	// Fallback: check if refs/heads/main exists.
	if readGitFile(root, "refs/heads/main") != "" {
		return "main"
	}
	if readGitFile(root, "refs/heads/master") != "" {
		return "master"
	}

	return "main"
}

// readGitFile reads a file from the .git directory, returning trimmed content or "".
func readGitFile(root, path string) string {
	data, err := os.ReadFile(root + "/.git/" + path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// absPath resolves a possibly-relative root to an absolute path.
func absPath(root string) (string, error) {
	if root == "" || root == "." {
		return os.Getwd()
	}
	return filepath.Abs(root)
}
