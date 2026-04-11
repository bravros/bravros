//go:build personal

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var reposCmd = &cobra.Command{
	Use:   "repos",
	Short: "Repository utilities",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var reposCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check git status in all subdirectories",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("🔍 Checking git status in all subfolders...")
		fmt.Println("===========================================")
		fmt.Println()

		entries, err := os.ReadDir(".")
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ Cannot read directory\n")
			os.Exit(1)
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			gitDir := filepath.Join(entry.Name(), ".git")
			if _, err := os.Stat(gitDir); os.IsNotExist(err) {
				continue
			}

			fmt.Printf("📁 %s/\n", entry.Name())

			// Check for uncommitted changes
			porcelainCmd := exec.Command("git", "status", "--porcelain")
			porcelainCmd.Dir = entry.Name()
			dirty, _ := porcelainCmd.Output()

			// Check branch status
			sbCmd := exec.Command("git", "status", "-sb")
			sbCmd.Dir = entry.Name()
			sbOut, _ := sbCmd.Output()
			branchStatus := ""
			if lines := strings.Split(string(sbOut), "\n"); len(lines) > 0 {
				branchStatus = lines[0]
			}

			dirtyStr := strings.TrimSpace(string(dirty))
			if dirtyStr != "" {
				fmt.Println("  ⚠️  Uncommitted changes:")
				shortCmd := exec.Command("git", "status", "--short")
				shortCmd.Dir = entry.Name()
				shortCmd.Stdout = os.Stdout
				shortCmd.Run()
			} else if strings.Contains(branchStatus, "ahead") || strings.Contains(branchStatus, "behind") {
				fmt.Printf("  🔄 Not fully synced with remote: %s\n", branchStatus)
			} else {
				fmt.Printf("  ✅ Clean and synced: %s\n", branchStatus)
			}
			fmt.Println()
		}

		fmt.Println("✅ Done scanning all repositories.")
	},
}

func init() {
	reposCmd.AddCommand(reposCheckCmd)
	rootCmd.AddCommand(reposCmd)
}
