package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bravros/private/internal/paths"
	"github.com/spf13/cobra"
)

var selfupdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Auto-update bravros from portable repo",
	Long:  "Checks the portable repo for upstream changes, pulls if behind, and re-runs install.sh. If already up-to-date, checks for stale deployed skills.",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil // silent exit
		}

		repo := paths.PortableRepoDir()
		cli := filepath.Join(home, ".claude", "bin", "bravros")

		// Skip if repo doesn't exist
		gitDir := filepath.Join(repo, ".git")
		if info, err := os.Stat(gitDir); err != nil || !info.IsDir() {
			return nil
		}

		// Fetch latest (silent)
		fetchCmd := exec.Command("git", "-C", repo, "fetch", "origin", "main", "--quiet")
		if err := fetchCmd.Run(); err != nil {
			return nil // silent exit on fetch failure
		}

		// Check if behind
		localOut, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
		if err != nil {
			return nil
		}
		remoteOut, err := exec.Command("git", "-C", repo, "rev-parse", "origin/main").Output()
		if err != nil {
			return nil
		}

		local := strings.TrimSpace(string(localOut))
		remote := strings.TrimSpace(string(remoteOut))

		if local != remote {
			// Pull fast-forward only
			pullCmd := exec.Command("git", "-C", repo, "pull", "--ff-only", "origin", "main", "--quiet")
			if err := pullCmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "⚠️  SDLC update available but pull failed (merge conflict?). Run manually: cd %s && git pull && bash install.sh\n", repo)
				return nil
			}

			// Run install.sh
			installCmd := exec.Command("bash", filepath.Join(repo, "install.sh"))
			installCmd.Stdout = nil
			installCmd.Stderr = nil
			if err := installCmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "⚠️  SDLC pulled successfully but install.sh failed: %v\n", err)
				return nil
			}

			// Get new version
			newVer := "unknown"
			if verOut, err := exec.Command(cli, "version").Output(); err == nil {
				newVer = strings.TrimSpace(string(verOut))
			}
			fmt.Fprintf(os.Stderr, "🔄 SDLC auto-updated to %s\n", newVer)
		} else {
			// Same commit — check if deployed skills are stale
			if info, err := os.Stat(cli); err != nil || info.IsDir() {
				return nil
			}

			out, err := exec.Command(cli, "skills", "outdated").Output()
			if err != nil {
				return nil
			}

			output := string(out)
			if strings.Contains(output, "outdated") || strings.Contains(output, "missing") {
				fmt.Fprintf(os.Stderr, "⚠️  Skills out of sync — run: bash %s/install.sh\n", repo)
				for _, line := range strings.Split(output, "\n") {
					if strings.HasPrefix(line, "📦") || strings.HasPrefix(line, "🆕") {
						fmt.Fprintln(os.Stderr, line)
					}
				}
			}
		}

		return nil
	},
}
