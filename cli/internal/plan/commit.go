package plan

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Commit stages plan files and optional extra files, runs pint on PHP, and commits.
func Commit(message string, extraFiles []string) error {
	if message == "" {
		return fmt.Errorf("commit message required")
	}

	if len(extraFiles) > 0 {
		// Stage specified files
		for _, f := range extraFiles {
			cmd := exec.Command("git", "add", f)
			if err := cmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not stage %s\n", f)
			}
		}
	} else {
		// No files specified — stage all tracked modified files
		cmd := exec.Command("git", "add", "-u")
		cmd.Run()
	}

	// Always stage .planning/ files
	cmd := exec.Command("git", "add", ".planning/")
	cmd.Run() // ignore error if no .planning dir

	// Find staged PHP files
	out, err := exec.Command("git", "diff", "--cached", "--name-only", "--", "*.php").Output()
	if err == nil && strings.TrimSpace(string(out)) != "" {
		phpFiles := strings.Split(strings.TrimSpace(string(out)), "\n")
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
				// Re-stage files modified by pint
				for _, f := range phpFiles {
					exec.Command("git", "add", f).Run()
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
	return nil
}
