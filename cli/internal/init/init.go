package projectinit

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/bravros/bravros/cli/internal/config"
	"github.com/bravros/bravros/cli/internal/hooks"
	"github.com/bravros/bravros/cli/internal/stack"
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

	// 1. Check if already initialized (new or legacy filename both count)
	cfgPath := filepath.Join(opts.Root, config.ConfigFilename)
	legacyPath := filepath.Join(opts.Root, config.LegacyConfigFilename)
	if _, err := os.Stat(cfgPath); err == nil {
		result.AlreadyInitialized = true
	} else if _, err := os.Stat(legacyPath); err == nil {
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

	// 4. Write .kaisser.yml
	if err := stack.WriteConfig(opts.Root, detectResult); err != nil {
		return nil, fmt.Errorf("failed to write %s: %w", config.ConfigFilename, err)
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
//
// For each hook file, the function classifies the existing destination using the
// hooks package state machine:
//   - Missing     → copied from template via hooks.Refresh (writes + chmod 0755)
//   - Pristine    → refreshed via hooks.Refresh (adds sentinel marker)
//   - OldCanonical → refreshed via hooks.Refresh (bumps version)
//   - Customized  → skipped (user-managed hook — no overwrite)
//   - Foreign     → skipped (no kaisser origin — no overwrite)
//
// Non-hook files (e.g. README.md) are copied via copyFile only when missing.
// After installing hooks, `git config core.hooksPath=.githooks` is set idempotently
// via hooks.EnsureHooksPath.
func installHooks(root string) (bool, error) {
	srcDir := hooksSourceDir()
	if srcDir == "" {
		return false, fmt.Errorf("could not determine hooks source directory")
	}

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return false, fmt.Errorf("hooks template directory not found: %w", err)
	}

	destDir := filepath.Join(root, ".bravros", "hooks")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return false, fmt.Errorf("failed to create .bravros/hooks: %w", err)
	}

	anyChanged := false

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		src := filepath.Join(srcDir, entry.Name())
		dst := filepath.Join(destDir, entry.Name())

		// Non-hook files (e.g. README.md): copy only if missing, never overwrite.
		if entry.Name() == "README.md" {
			if _, err := os.Stat(dst); os.IsNotExist(err) {
				if err := copyFile(src, dst); err != nil {
					return false, fmt.Errorf("failed to copy %s: %w", entry.Name(), err)
				}
				anyChanged = true
			}
			continue
		}

		// For hook files, use the hooks package state machine.
		status, err := hooks.Classify(dst, src)
		if err != nil {
			return false, fmt.Errorf("failed to classify hook %s: %w", entry.Name(), err)
		}

		switch status {
		case hooks.StatusMissing, hooks.StatusPristine, hooks.StatusOldCanonical:
			// Missing: install fresh; Pristine/OldCanonical: upgrade to current canonical.
			if err := hooks.Refresh(dst, src); err != nil {
				return false, fmt.Errorf("failed to install hook %s: %w", entry.Name(), err)
			}
			anyChanged = true

		case hooks.StatusCurrent, hooks.StatusForeign:
			// User-managed or unrecognised hook — do not overwrite.
			// Ensure it is executable if it exists.
			_ = os.Chmod(dst, 0755)
			fmt.Fprintf(os.Stderr, "skipping %s (existing hook is %s — not overwriting)\n", entry.Name(), status)
		}
	}

	// Set git config core.hooksPath idempotently.
	if err := hooks.EnsureHooksPath(root); err != nil {
		return false, fmt.Errorf("failed to set core.hooksPath: %w", err)
	}

	return anyChanged, nil
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
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
