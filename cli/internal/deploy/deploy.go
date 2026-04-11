package deploy

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DeployOpts configures the deploy operation.
type DeployOpts struct {
	DryRun    bool
	CountOnly bool
	SourceDir string // defaults to cwd
	TargetDir string // defaults to ~/.claude/
}

// DeployResult holds the outcome of a deploy operation.
type DeployResult struct {
	FilesDeployed int      `json:"files_deployed"`
	Dirs          []string `json:"dirs"`
	DryRun        bool     `json:"dry_run"`
	CountOnly     bool     `json:"count_only,omitempty"`
	Files         []string `json:"files,omitempty"`
}

// dirMappings maps source subdirs to target subdirs (recursive copy).
var dirMappings = []struct {
	src string // relative to SourceDir
	dst string // relative to TargetDir
}{
	{"skills", "skills"},
	{"hooks", "hooks"},
	{"templates", "templates"},
}

// fileMappings maps individual source files to target paths.
var fileMappings = []struct {
	src string
	dst string
}{
	{"config/settings.json", "settings.json"},
	{"config/statusline.sh", "statusline.sh"},
	{"CLAUDE.md", "CLAUDE.md"},
}

// Deploy copies the claude config repo to ~/.claude/.
func Deploy(opts DeployOpts) (*DeployResult, error) {
	if opts.SourceDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("cannot determine cwd: %w", err)
		}
		opts.SourceDir = cwd
	}

	// Validate: must be the claude config repo
	if !IsClaudeRepo(opts.SourceDir) {
		return nil, fmt.Errorf("not the claude config repo (cwd basename must be \"claude\")")
	}

	if opts.TargetDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cannot determine home dir: %w", err)
		}
		opts.TargetDir = filepath.Join(home, ".claude")
	}

	// Collect all files to deploy
	var files []string
	dirSet := map[string]bool{}

	for _, dm := range dirMappings {
		srcDir := filepath.Join(opts.SourceDir, dm.src)
		if _, err := os.Stat(srcDir); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			// Skip .DS_Store
			if d.Name() == ".DS_Store" {
				return nil
			}
			rel, _ := filepath.Rel(opts.SourceDir, path)
			files = append(files, rel)
			dirSet[dm.dst] = true
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walking %s: %w", dm.src, err)
		}
	}

	for _, fm := range fileMappings {
		srcPath := filepath.Join(opts.SourceDir, fm.src)
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			continue
		}
		files = append(files, fm.src)
	}

	sort.Strings(files)

	// Build sorted dirs list
	var dirs []string
	for d := range dirSet {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	result := &DeployResult{
		FilesDeployed: len(files),
		Dirs:          dirs,
		DryRun:        opts.DryRun,
		CountOnly:     opts.CountOnly,
	}

	if opts.CountOnly {
		return result, nil
	}

	// Include file list for dry-run
	if opts.DryRun {
		result.Files = files
		return result, nil
	}

	// Actual deploy
	result.Files = files
	for _, rel := range files {
		dstRel := mapSourceToDest(rel)
		src := filepath.Join(opts.SourceDir, rel)
		dst := filepath.Join(opts.TargetDir, dstRel)

		if err := copyFile(src, dst); err != nil {
			return nil, fmt.Errorf("copying %s → %s: %w", rel, dstRel, err)
		}
	}

	return result, nil
}

// IsClaudeRepo checks if the given directory is the claude config repo.
func IsClaudeRepo(dir string) bool {
	return filepath.Base(dir) == "claude"
}

// mapSourceToDest converts a source-relative path to its target-relative path.
func mapSourceToDest(rel string) string {
	// Check file mappings first (config/settings.json → settings.json)
	for _, fm := range fileMappings {
		if rel == fm.src {
			return fm.dst
		}
	}
	// Directory mappings keep the same relative structure
	return rel
}

// copyFile copies src to dst, creating parent directories as needed.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// DeployableFile returns true if the given path (relative to repo root) is
// part of the deploy set.
func DeployableFile(rel string) bool {
	for _, fm := range fileMappings {
		if rel == fm.src {
			return true
		}
	}
	for _, dm := range dirMappings {
		if strings.HasPrefix(rel, dm.src+"/") {
			return true
		}
	}
	return false
}
