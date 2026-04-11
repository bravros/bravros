package projectinit

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/bravros/private/internal/stack"
)

// InitOpts configures the init behavior.
type InitOpts struct {
	Root              string // project root (default ".")
	StackOverride     string // --stack flag (e.g., "laravel", "nextjs")
	SkipHooks         bool
	SkipWorkflows     bool
	SkipStagingBranch bool
}

// InitResult holds the outcome of the init operation.
type InitResult struct {
	Stack                string   `json:"stack"`
	ConfigWritten        bool     `json:"config_written"`
	HooksInstalled       bool     `json:"hooks_installed"`
	WorkflowsCreated     []string `json:"workflows_created"`
	StagingBranchCreated bool     `json:"staging_branch_created"`
	PlanningDirCreated   bool     `json:"planning_dir_created"`
	AlreadyInitialized   bool     `json:"already_initialized"`
}

// Init initializes a project with SDLC structure.
func Init(opts InitOpts) (*InitResult, error) {
	if opts.Root == "" {
		opts.Root = "."
	}

	result := &InitResult{}

	// 1. Check if already initialized
	cfgPath := filepath.Join(opts.Root, ".bravros.yml")
	if _, err := os.Stat(cfgPath); err == nil {
		result.AlreadyInitialized = true
	}

	// 2. Run stack detection
	detectResult, err := stack.Detect(opts.Root, stack.DetectOpts{Versions: true})
	if err != nil {
		return nil, fmt.Errorf("stack detection failed: %w", err)
	}

	// 3. Apply stack override if set
	if opts.StackOverride != "" {
		detectResult.Stack.Framework = opts.StackOverride
	}

	result.Stack = detectResult.Stack.Framework
	if result.Stack == "" {
		result.Stack = detectResult.Stack.Language
	}

	// 4. Write .bravros.yml
	if err := stack.WriteConfig(opts.Root, detectResult); err != nil {
		return nil, fmt.Errorf("failed to write .bravros.yml: %w", err)
	}
	result.ConfigWritten = true

	// 5. Create .planning/backlog/archive/
	planningArchive := filepath.Join(opts.Root, ".planning", "backlog", "archive")
	if err := os.MkdirAll(planningArchive, 0755); err != nil {
		return nil, fmt.Errorf("failed to create .planning structure: %w", err)
	}
	result.PlanningDirCreated = true

	// 6. Install hooks
	if !opts.SkipHooks {
		installed, err := installHooks(opts.Root)
		if err != nil {
			// Non-fatal: hooks are optional (templates may not exist)
			result.HooksInstalled = false
		} else {
			result.HooksInstalled = installed
		}
	}

	// 7. Create workflows dir
	if !opts.SkipWorkflows {
		workflowsDir := filepath.Join(opts.Root, ".github", "workflows")
		if err := os.MkdirAll(workflowsDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create .github/workflows: %w", err)
		}
		result.WorkflowsCreated = []string{".github/workflows/"}
	}

	// 8. Create staging branch if missing
	if !opts.SkipStagingBranch {
		created, err := ensureStagingBranch(opts.Root)
		if err != nil {
			// Non-fatal: git may not be available
			result.StagingBranchCreated = false
		} else {
			result.StagingBranchCreated = created
		}
	}

	return result, nil
}

// hooksSourceDir returns the path to the hooks template directory.
// Exported for testing — tests can override via HooksSourceOverride.
var HooksSourceOverride string

func hooksSourceDir() string {
	if HooksSourceOverride != "" {
		return HooksSourceOverride
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "templates", ".githooks")
}

// installHooks copies hook files from templates to .githooks/ and sets git config.
func installHooks(root string) (bool, error) {
	srcDir := hooksSourceDir()
	if srcDir == "" {
		return false, fmt.Errorf("could not determine hooks source directory")
	}

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return false, fmt.Errorf("hooks template directory not found: %w", err)
	}

	destDir := filepath.Join(root, ".githooks")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return false, fmt.Errorf("failed to create .githooks: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		src := filepath.Join(srcDir, entry.Name())
		dst := filepath.Join(destDir, entry.Name())
		if err := copyFile(src, dst); err != nil {
			return false, fmt.Errorf("failed to copy hook %s: %w", entry.Name(), err)
		}
		// Make hook executable (skip non-hook files like README)
		if entry.Name() != "README.md" {
			os.Chmod(dst, 0755)
		}
	}

	// Set git config core.hooksPath
	cmd := exec.Command("git", "config", "core.hooksPath", ".githooks")
	cmd.Dir = root
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("failed to set core.hooksPath: %w", err)
	}

	return true, nil
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// ensureStagingBranch creates a "homolog" branch if it doesn't exist.
func ensureStagingBranch(root string) (bool, error) {
	// Check if branch exists
	cmd := exec.Command("git", "branch", "--list", "homolog")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to check branches: %w", err)
	}

	// Branch already exists
	if len(out) > 0 {
		return false, nil
	}

	// Create from current HEAD
	cmd = exec.Command("git", "branch", "homolog")
	cmd.Dir = root
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("failed to create homolog branch: %w", err)
	}

	return true, nil
}
