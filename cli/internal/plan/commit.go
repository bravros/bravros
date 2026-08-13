package plan

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/bravros/bravros/cli/internal/git"
)

// CommitOptions controls optional commit behaviour.
type CommitOptions struct {
	// VerifyClean, when true, runs `git status --porcelain` AFTER the commit
	// and returns an error (including the raw porcelain output) if any
	// untracked or modified files remain. Opt-in — bare Commit() callers are
	// unaffected. See cli-pending/commit-verify-clean.md for the motivation.
	VerifyClean bool

	// ScopeStaged, when true, skips both `git add -u` and the `.planning/`
	// sweep entirely — only the pre-staged index is committed. Positional
	// extraFiles args are also treated as no-ops when this flag is set.
	// Use this flag (--scope-staged / --no-add) in parallel-session contexts
	// where each agent manages its own index and a broad sweep would capture
	// files created by sibling sessions (B-0077).
	ScopeStaged bool
}

// Commit stages plan files and optional extra files, runs pint on PHP, and commits.
// It is equivalent to CommitWithOptions(message, extraFiles, CommitOptions{}).
func Commit(message string, extraFiles []string) error {
	return CommitWithOptions(message, extraFiles, CommitOptions{})
}

// CommitWithOptions is like Commit but accepts a CommitOptions struct to
// control optional behaviour (e.g. --verify-clean post-commit gate).
func CommitWithOptions(message string, extraFiles []string, opts CommitOptions) error {
	if message == "" {
		return fmt.Errorf("commit message required")
	}

	if !opts.ScopeStaged {
		// --scope-staged not set: perform staging based on whether explicit files were given.
		if len(extraFiles) > 0 {
			// Stage specified files (caller-supplied list — trust it, no extra sweep).
			// Do NOT sweep .planning/ here — that would capture untracked plan/backlog
			// files from parallel sessions (B-0077).
			for _, f := range extraFiles {
				cmd := exec.Command("git", "add", f)
				if err := cmd.Run(); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not stage %s\n", f)
				}
			}
		} else {
			// Default path: no positional args, no flags — stage all tracked modified
			// files and sweep .planning/ for any new plan/backlog entries.
			exec.Command("git", "add", "-u").Run()
			exec.Command("git", "add", ".planning/").Run() // ignore error if no .planning dir
		}
	}
	// When ScopeStaged is true: skip all staging — commit only the pre-staged index.
	// This is the safe mode for parallel-session contexts (B-0077).

	// Find staged PHP files for pint. Three deliberate choices here (P-0183 G4 / recap F3):
	//   -z            → NUL-separated output, so paths with spaces survive splitting;
	//   -c core.quotepath=false → git's default C-quotes non-ASCII paths (a ⚡-glyph
	//                   Blade path came out as the literal `"resources/views/\342\232\241…"`,
	//                   double quotes included — an argv element pint cannot open);
	//                   -z already disables quoting, the config is belt-and-braces;
	//   :(exclude)*.blade.php → the `*.php` pathspec glob matches `.blade.php`, and pint
	//                   must never see a Blade template (this bit on every machine,
	//                   emoji or not).
	out, err := exec.Command("git", "-c", "core.quotepath=false", "diff", "--cached", "--name-only", "-z", "--", "*.php", ":(exclude)*.blade.php").Output()
	if err == nil && len(out) > 0 {
		var phpFiles []string
		for _, f := range strings.Split(string(out), "\x00") {
			if f != "" {
				phpFiles = append(phpFiles, f)
			}
		}
		// When only Blade files were staged, phpFiles is empty — do not invoke pint.
		if len(phpFiles) > 0 {
			// Check if pint exists
			if _, err := os.Stat("vendor/bin/pint"); err == nil {
				// Run pint on staged PHP files
				args := append([]string{}, phpFiles...)
				pintCmd := exec.Command("vendor/bin/pint", args...)
				pintCmd.Stdout = os.Stdout
				pintCmd.Stderr = os.Stderr
				if err := pintCmd.Run(); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: pint reported an error; continuing\n")
				}
				// Re-stage files modified by pint. A failed re-stage means the
				// formatted content silently misses the commit — warn, don't drop.
				for _, f := range phpFiles {
					if addErr := exec.Command("git", "add", "--", f).Run(); addErr != nil {
						fmt.Fprintf(os.Stderr, "Warning: could not re-stage %s after pint: %v\n", f, addErr)
					}
				}
			}
		}
	}

	// Check if there's anything to commit
	checkCmd := exec.Command("git", "diff", "--cached", "--quiet")
	if err := checkCmd.Run(); err == nil {
		// Exit code 0 = no changes
		fmt.Println("⏭️  nothing to commit — skipping")
		return nil
	}

	// Commit
	commitCmd := exec.Command("git", "commit", "-m", message)
	commitCmd.Stdout = os.Stdout
	commitCmd.Stderr = os.Stderr
	if err := commitCmd.Run(); err != nil {
		return fmt.Errorf("commit failed: %w", err)
	}

	// Show result
	hashOut, _ := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	hash := strings.TrimSpace(string(hashOut))
	fmt.Printf("✅ %s — %s\n", hash, message)

	// Post-commit cleanliness gate (opt-in via --verify-clean)
	if opts.VerifyClean {
		clean, porcelain, err := git.IsClean()
		if err != nil {
			return fmt.Errorf("verify-clean: git status failed: %w", err)
		}
		if clean {
			fmt.Println("✓ Working tree clean — no untracked or modified files left behind")
		} else {
			return fmt.Errorf("❌ Untracked/modified files remain after commit:\n%s\nStage them and commit again, or run `git status` to investigate.", porcelain)
		}
	}

	return nil
}
