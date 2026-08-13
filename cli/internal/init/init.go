package projectinit

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bravros/bravros/cli/internal/config"
	"github.com/bravros/bravros/cli/internal/hooks"
	"github.com/bravros/bravros/cli/internal/stack"
)

// hookTemplates carries the canonical git hooks shipped inside the binary.
// They are embedded on purpose: `bravros init` must work on a machine that has
// nothing but the binary, and must never read or write global state such as
// ~/.claude/ to find its own templates.
//
//go:embed templates
var hookTemplates embed.FS

// hookTemplateDir is the path of the embedded template directory inside hookTemplates.
const hookTemplateDir = "templates"

// InitOpts configures the init behavior.
type InitOpts struct {
	Root          string // project root (default ".")
	StackOverride string // --stack flag (e.g., "laravel", "nextjs")
	SkipHooks     bool
}

// InitResult holds the outcome of the init operation.
type InitResult struct {
	Stack              string   `json:"stack"`
	ConfigPath         string   `json:"config_path"`
	ConfigWritten      bool     `json:"config_written"`
	HooksInstalled     bool     `json:"hooks_installed"`
	Hooks              []string `json:"hooks,omitempty"`
	HooksPath          string   `json:"hooks_path,omitempty"`
	PlanningDirCreated bool     `json:"planning_dir_created"`
	AlreadyInitialized bool     `json:"already_initialized"`
}

// Init initializes a project with the Bravros SDLC structure.
//
// Everything it writes lives inside the repository working tree: .bravros/config.json,
// .bravros/hooks/, .planning/, and the local `core.hooksPath` git config. It never
// touches ~/.claude/, ~/.bravros/, or any other global location — that is what the
// separate `install` and `deploy` verbs are for.
func Init(opts InitOpts) (*InitResult, error) {
	if opts.Root == "" {
		opts.Root = "."
	}

	// Preflight: a git repository is required, and this must fail BEFORE anything is
	// written. Hooks and core.hooksPath are git-only, so without a repo the caller
	// would get .bravros/config.json and .planning/ but no enforcement — a project
	// that looks initialized while the commit-format and main-push guarantees
	// silently do not hold. Half-initialized is worse than not initialized.
	if err := requireGitRepo(opts.Root); err != nil {
		return nil, err
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

	// 4. Write .bravros/config.json.
	//
	// WriteConfigAlways, not WriteConfig: the plain writer skips the file when
	// detection found nothing (an empty repo), and `bravros init` promises a
	// config.json unconditionally.
	if err := stack.WriteConfigAlways(opts.Root, detectResult); err != nil {
		return nil, fmt.Errorf("failed to write %s: %w", config.ConfigFilename, err)
	}
	result.ConfigWritten = true
	result.ConfigPath = config.ConfigFilename

	// 5. Create .planning/backlog/archive/
	planningArchive := filepath.Join(opts.Root, ".planning", "backlog", "archive")
	if err := os.MkdirAll(planningArchive, 0755); err != nil {
		return nil, fmt.Errorf("failed to create .planning structure: %w", err)
	}
	result.PlanningDirCreated = true

	// 6. Install hooks
	if !opts.SkipHooks {
		installed, names, err := installHooks(opts.Root)
		if err != nil {
			return nil, fmt.Errorf("failed to install hooks: %w", err)
		}
		result.HooksInstalled = installed
		result.Hooks = names
		result.HooksPath = filepath.Join(".bravros", "hooks")
	}

	return result, nil
}

// HooksSourceOverride points installHooks at a directory of hook templates on
// disk instead of the embedded ones. Test-only seam.
var HooksSourceOverride string

// hooksSource returns a directory holding the canonical hook templates plus a
// cleanup function. Unless HooksSourceOverride is set, the embedded templates are
// materialized into a temporary directory so the hooks package (which works on
// file paths) can classify and refresh against them.
func hooksSource() (string, func(), error) {
	if HooksSourceOverride != "" {
		return HooksSourceOverride, func() {}, nil
	}

	dir, err := os.MkdirTemp("", "bravros-hook-templates-")
	if err != nil {
		return "", func() {}, fmt.Errorf("could not stage hook templates: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }

	entries, err := fs.ReadDir(hookTemplates, hookTemplateDir)
	if err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("embedded hook templates unreadable: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := hookTemplates.ReadFile(hookTemplateDir + "/" + entry.Name())
		if err != nil {
			cleanup()
			return "", func() {}, fmt.Errorf("embedded hook %s unreadable: %w", entry.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(dir, entry.Name()), data, 0755); err != nil {
			cleanup()
			return "", func() {}, fmt.Errorf("could not stage hook %s: %w", entry.Name(), err)
		}
	}

	return dir, cleanup, nil
}

// installHooks copies the canonical hooks into <root>/.bravros/hooks/ and points
// git at them via core.hooksPath.
//
// For each hook file, the destination is classified with the hooks package state
// machine:
//   - Missing      → copied from template via hooks.Refresh (writes + chmod 0755)
//   - Pristine     → refreshed via hooks.Refresh (adds sentinel marker)
//   - OldCanonical → refreshed via hooks.Refresh (bumps version)
//   - Current      → left alone
//   - Foreign      → skipped (user-managed hook — no overwrite)
//
// Non-hook files (e.g. README.md) are copied only when missing. It returns whether
// anything changed plus the names of the hooks now present in .bravros/hooks/.
func installHooks(root string) (bool, []string, error) {
	srcDir, cleanup, err := hooksSource()
	if err != nil {
		return false, nil, err
	}
	defer cleanup()

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return false, nil, fmt.Errorf("hooks template directory not found: %w", err)
	}

	destDir := filepath.Join(root, ".bravros", "hooks")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return false, nil, fmt.Errorf("failed to create .bravros/hooks: %w", err)
	}

	anyChanged := false
	var installed []string

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		src := filepath.Join(srcDir, entry.Name())
		dst := filepath.Join(destDir, entry.Name())

		// Non-hook files (e.g. README.md): copy only if missing, never overwrite.
		if filepath.Ext(entry.Name()) != "" {
			if _, err := os.Stat(dst); os.IsNotExist(err) {
				if err := copyFile(src, dst); err != nil {
					return false, nil, fmt.Errorf("failed to copy %s: %w", entry.Name(), err)
				}
				anyChanged = true
			}
			continue
		}

		// For hook files, use the hooks package state machine.
		status, err := hooks.Classify(dst, src)
		if err != nil {
			return false, nil, fmt.Errorf("failed to classify hook %s: %w", entry.Name(), err)
		}

		switch status {
		case hooks.StatusMissing, hooks.StatusPristine, hooks.StatusOldCanonical:
			// Missing: install fresh; Pristine/OldCanonical: upgrade to current canonical.
			if err := hooks.Refresh(dst, src); err != nil {
				return false, nil, fmt.Errorf("failed to install hook %s: %w", entry.Name(), err)
			}
			anyChanged = true

		case hooks.StatusCurrent, hooks.StatusForeign:
			// User-managed or unrecognised hook — do not overwrite.
			// Ensure it is executable if it exists.
			_ = os.Chmod(dst, 0755)
			fmt.Fprintf(os.Stderr, "skipping %s (existing hook is %s — not overwriting)\n", entry.Name(), status)
		}

		installed = append(installed, entry.Name())
	}

	sort.Strings(installed)

	// Set git config core.hooksPath idempotently.
	if err := hooks.EnsureHooksPath(root); err != nil {
		return false, nil, fmt.Errorf("failed to set core.hooksPath: %w", err)
	}

	return anyChanged, installed, nil
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

// requireGitRepo reports a clear, actionable error when root is not inside a git
// work tree. Without it the failure surfaced as a raw wrapped git error —
// "failed to set core.hooksPath: ... exit status 128: fatal: not in a git directory"
// — which is the first thing a new user saw after `curl | sh` told them to run
// `bravros init`.
func requireGitRepo(root string) error {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = root
	out, err := cmd.Output()
	if err == nil && strings.TrimSpace(string(out)) == "true" {
		return nil
	}

	abs, absErr := filepath.Abs(root)
	if absErr != nil {
		abs = root
	}
	return fmt.Errorf(`%s is not a git repository.

bravros init installs git hooks (commit-msg, pre-push) and sets core.hooksPath,
so it needs a repository. Either:

  cd <your-project>   # if you meant to run it somewhere else
  git init            # if this really is a new project

then run bravros init again`, abs)
}
