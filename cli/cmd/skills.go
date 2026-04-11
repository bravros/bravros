package cmd

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bravros/bravros/internal/i18n"
	"github.com/bravros/bravros/internal/paths"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const skillsTarballURL = "https://github.com/bravros/bravros/releases/latest/download/skills.tar.gz"

// skillMeta represents SKILL.md YAML frontmatter.
type skillMeta struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
	Dir         string `yaml:"-" json:"dir"`
	HasEvals    bool   `yaml:"-" json:"has_evals"`
	HasRefs     bool   `yaml:"-" json:"has_refs"`
	// Marketplace fields
	Tier               string `yaml:"-" json:"tier,omitempty"`
	MarketplaceVersion string `yaml:"-" json:"marketplace_version,omitempty"`
	Installed          bool   `yaml:"-" json:"installed"`
	Locked             bool   `yaml:"-" json:"locked"`
}

var skillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "Skill health check and management",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

// ─── list ───────────────────────────────────────────────────────────────────

var (
	skillsListFormat    string
	skillsListTier      string
	skillsListInstalled bool
)

var skillsListCmd = &cobra.Command{
	Use:   "list",
	Short: i18n.T("skills.list.short"),
	Run: func(cmd *cobra.Command, args []string) {
		merged := mergeSkillsWithManifest()

		// Filter by tier
		if skillsListTier != "" && skillsListTier != "all" {
			var filtered []skillMeta
			for _, s := range merged {
				if s.Tier == skillsListTier {
					filtered = append(filtered, s)
				}
			}
			merged = filtered
		}

		// Filter by installed
		if skillsListInstalled {
			var filtered []skillMeta
			for _, s := range merged {
				if s.Installed {
					filtered = append(filtered, s)
				}
			}
			merged = filtered
		}

		if len(merged) == 0 {
			fmt.Println(i18n.T("skills.list.empty"))
			return
		}

		switch strings.ToLower(skillsListFormat) {
		case "json":
			data, _ := json.MarshalIndent(merged, "", "  ")
			fmt.Println(string(data))
		default:
			fmt.Printf("%-25s %-8s %-12s %s\n", "NAME", "TIER", "STATUS", "DESCRIPTION")
			fmt.Println(strings.Repeat("─", 90))
			for _, s := range merged {
				status := "○ " + i18n.T("skills.list.status_available")
				if s.Installed {
					status = "✓ " + i18n.T("skills.list.status_installed")
				} else if s.Locked {
					status = "⚡ " + i18n.T("skills.list.status_locked")
				}
				desc := strings.Join(strings.Fields(s.Description), " ")
				if len(desc) > 40 {
					desc = desc[:37] + "..."
				}
				fmt.Printf("%-25s %-8s %-12s %s\n", s.Name, s.Tier, status, desc)
			}
			fmt.Printf("\n%s\n", i18n.Tf("skills.list.total", len(merged)))
		}
	},
}

// mergeSkillsWithManifest combines installed skills with the embedded manifest.
func mergeSkillsWithManifest() []skillMeta {
	manifest, err := LoadManifest()
	if err != nil {
		// Fallback to installed-only
		return scanSkills()
	}

	installed := scanSkills()
	installedMap := map[string]skillMeta{}
	for _, s := range installed {
		installedMap[s.Name] = s
	}

	// Determine if user has pro license
	isPro := false
	if claims := GetLicense(); claims != nil && claims.Tier == "pro" {
		isPro = true
	}

	var merged []skillMeta
	seen := map[string]bool{}

	// Add all manifest skills first (preserves manifest ordering)
	for _, entry := range manifest.Skills {
		sm := skillMeta{
			Name:               entry.Name,
			Description:        entry.Description,
			Tier:               entry.Tier,
			MarketplaceVersion: entry.Version,
		}
		if inst, ok := installedMap[entry.Name]; ok {
			sm.Installed = true
			sm.Dir = inst.Dir
			sm.HasEvals = inst.HasEvals
			sm.HasRefs = inst.HasRefs
		}
		if entry.Tier == "premium" && !isPro {
			sm.Locked = true
		}
		merged = append(merged, sm)
		seen[entry.Name] = true
	}

	// Add installed skills not in manifest (user-installed / custom)
	for _, s := range installed {
		if !seen[s.Name] {
			s.Installed = true
			s.Tier = "custom"
			merged = append(merged, s)
		}
	}

	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Name < merged[j].Name
	})

	return merged
}

// ─── verify ─────────────────────────────────────────────────────────────────

var skillsVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Validate all skills have required frontmatter, evals, and structure",
	Run: func(cmd *cobra.Command, args []string) {
		skills := scanSkills()
		issues := 0

		for _, s := range skills {
			problems := []string{}

			if s.Name == "" {
				problems = append(problems, "missing 'name' in frontmatter")
			}
			if s.Description == "" {
				problems = append(problems, "missing 'description' in frontmatter")
			}
			if !s.HasEvals {
				problems = append(problems, "no evals/ directory")
			}

			if len(problems) > 0 {
				issues += len(problems)
				fmt.Printf("⚠️  %s:\n", s.Dir)
				for _, p := range problems {
					fmt.Printf("   - %s\n", p)
				}
			}
		}

		if issues == 0 {
			fmt.Printf("✅ All %d skills pass verification.\n", len(skills))
		} else {
			fmt.Printf("\n%d issue(s) across %d skills.\n", issues, len(skills))
			os.Exit(1)
		}
	},
}

// ─── outdated ───────────────────────────────────────────────────────────────

var skillsOutdatedCmd = &cobra.Command{
	Use:   "outdated",
	Short: "Compare source (portable repo skills) vs deployed (~/.claude/skills)",
	Run: func(cmd *cobra.Command, args []string) {
		// portable repo skills
		sourceDir := filepath.Join(paths.PortableRepoDir(), "skills")
		home, _ := os.UserHomeDir()
		deployedDir := filepath.Join(home, ".claude", "skills")

		sourceSkills := scanSkillsIn(sourceDir)
		deployedSkills := scanSkillsIn(deployedDir)

		// Build maps
		srcMap := map[string]string{}
		for _, s := range sourceSkills {
			srcMap[s.Name] = s.Dir
		}
		depMap := map[string]string{}
		for _, s := range deployedSkills {
			depMap[s.Name] = s.Dir
		}

		outdated := 0
		missing := 0

		// Check for outdated (source newer than deployed)
		for _, s := range sourceSkills {
			depDir, exists := depMap[s.Name]
			if !exists {
				fmt.Printf("🆕 %s — in source but not deployed\n", s.Name)
				missing++
				continue
			}

			// Compare SKILL.md modification times
			srcInfo, err1 := os.Stat(filepath.Join(s.Dir, "SKILL.md"))
			depInfo, err2 := os.Stat(filepath.Join(depDir, "SKILL.md"))
			if err1 != nil || err2 != nil {
				continue
			}

			if srcInfo.ModTime().After(depInfo.ModTime()) {
				fmt.Printf("📦 %s — source is newer (run install.sh to update)\n", s.Name)
				outdated++
			}
		}

		// Check for deployed-only (removed from source)
		for _, s := range deployedSkills {
			if _, exists := srcMap[s.Name]; !exists {
				fmt.Printf("🗑️  %s — deployed but not in source (orphaned)\n", s.Name)
			}
		}

		if outdated == 0 && missing == 0 {
			fmt.Printf("✅ All %d deployed skills are up to date.\n", len(deployedSkills))
		} else {
			fmt.Printf("\n%d outdated, %d missing. Run install.sh to sync.\n", outdated, missing)
		}
	},
}

// ─── install ───────────────────────────────────────────────────────────────

var skillsInstallCmd = &cobra.Command{
	Use:   "install <name>",
	Short: i18n.T("skills.install.short"),
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		manifest, err := LoadManifest()
		if err != nil {
			return fmt.Errorf("failed to load manifest: %w", err)
		}

		entry := manifest.FindSkill(name)
		if entry == nil {
			fmt.Fprintln(os.Stderr, i18n.Tf("skills.install.not_found", name))
			os.Exit(1)
		}

		// Tier gate: premium requires pro license
		if entry.Tier == "premium" {
			isPro := false
			if claims := GetLicense(); claims != nil && claims.Tier == "pro" {
				isPro = true
			}
			if !isPro {
				fmt.Fprintln(os.Stderr, i18n.Tf("skills.install.requires_pro", name))
				os.Exit(1)
			}
		}

		// Check if already installed
		skillDir := filepath.Join(paths.SkillsDir(), name)
		if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err == nil {
			fmt.Println(i18n.Tf("skills.install.already_installed", name))
			return nil
		}

		// Download and extract
		fmt.Println(i18n.Tf("skills.install.downloading", name))
		if err := downloadAndExtractSkill(name); err != nil {
			fmt.Fprintln(os.Stderr, i18n.Tf("skills.install.failed", name, err.Error()))
			os.Exit(1)
		}

		fmt.Println(i18n.Tf("skills.install.success", name))
		return nil
	},
}

// ─── remove ────────────────────────────────────────────────────────────────

var skillsRemoveForce bool

var skillsRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: i18n.T("skills.remove.short"),
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		skillDir := filepath.Join(paths.SkillsDir(), name)
		if _, err := os.Stat(skillDir); os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, i18n.Tf("skills.remove.not_installed", name))
			os.Exit(1)
		}

		// Core skill protection
		manifest, _ := LoadManifest()
		if manifest != nil {
			if entry := manifest.FindSkill(name); entry != nil && entry.Tier == "core" && !skillsRemoveForce {
				fmt.Fprintln(os.Stderr, i18n.Tf("skills.remove.core_warning", name))
				os.Exit(1)
			}
		}

		// Confirmation
		if !skillsRemoveForce {
			fmt.Print(i18n.Tf("skills.remove.confirm", name))
			reader := bufio.NewReader(os.Stdin)
			answer, _ := reader.ReadString('\n')
			answer = strings.TrimSpace(strings.ToLower(answer))
			if answer != "y" && answer != "yes" {
				fmt.Println(i18n.T("skills.remove.aborted"))
				return nil
			}
		}

		if err := os.RemoveAll(skillDir); err != nil {
			return fmt.Errorf("failed to remove %s: %w", name, err)
		}

		fmt.Println(i18n.Tf("skills.remove.success", name))
		return nil
	},
}

// ─── update ────────────────────────────────────────────────────────────────

var skillsUpdateCmd = &cobra.Command{
	Use:   "update [name]",
	Short: i18n.T("skills.update.short"),
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		installed := scanSkills()
		if len(installed) == 0 {
			fmt.Println(i18n.T("skills.update.nothing_installed"))
			return nil
		}

		// If a specific skill name is provided
		if len(args) == 1 {
			name := args[0]
			found := false
			for _, s := range installed {
				if s.Name == name {
					found = true
					break
				}
			}
			if !found {
				fmt.Fprintln(os.Stderr, i18n.Tf("skills.update.not_installed", name))
				os.Exit(1)
			}
			installed = []skillMeta{{Name: name}}
		}

		fmt.Println(i18n.T("skills.update.fetching"))

		// Download tarball once to temp file
		tmpFile, err := downloadTarball()
		if err != nil {
			return fmt.Errorf("failed to download skills: %w", err)
		}
		defer os.Remove(tmpFile)

		updated := 0
		for _, s := range installed {
			fmt.Println(i18n.Tf("skills.update.updating", s.Name))
			if err := extractSkillFromTarball(tmpFile, s.Name); err != nil {
				fmt.Fprintf(os.Stderr, "  warning: could not update %s: %v\n", s.Name, err)
				continue
			}
			fmt.Println(i18n.Tf("skills.update.done", s.Name))
			updated++
		}

		fmt.Println(i18n.Tf("skills.update.summary", updated))
		return nil
	},
}

// ─── download helpers ──────────────────────────────────────────────────────

// downloadAndExtractSkill downloads the release tarball and extracts a single skill.
func downloadAndExtractSkill(name string) error {
	tmpFile, err := downloadTarball()
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile)
	return extractSkillFromTarball(tmpFile, name)
}

// downloadTarball fetches the skills tarball to a temporary file and returns its path.
// It tries gh CLI first (handles private repo auth), then falls back to direct HTTP.
func downloadTarball() (string, error) {
	tmp, err := os.CreateTemp("", "bravros-skills-*.tar.gz")
	if err != nil {
		return "", err
	}
	tmp.Close()

	// Try gh CLI first (handles private repo auth automatically)
	ghPath, ghErr := exec.LookPath("gh")
	if ghErr == nil {
		// Use --output only (--dir and --output are mutually exclusive in gh)
		cmd := exec.Command(ghPath, "release", "download", "--repo", "bravros/bravros",
			"--pattern", "skills.tar.gz", "--output", tmp.Name(), "--clobber")
		if out, err := cmd.CombinedOutput(); err == nil {
			info, _ := os.Stat(tmp.Name())
			if info != nil && info.Size() > 0 {
				return tmp.Name(), nil
			}
		} else {
			// skills.tar.gz asset may not exist in the release — try repo archive fallback
			_ = out
		}

		// Fallback: download source archive via gh api and use skills/ from the repo tree
		cmd = exec.Command(ghPath, "api", "repos/bravros/bravros/tarball",
			"--header", "Accept: application/vnd.github+json")
		archiveData, err := cmd.Output()
		if err == nil && len(archiveData) > 0 {
			if writeErr := os.WriteFile(tmp.Name(), archiveData, 0644); writeErr == nil {
				return tmp.Name(), nil
			}
		}
	}

	// Fallback: direct HTTP (works for public repos or with GITHUB_TOKEN)
	resp, err := http.Get(skillsTarballURL)
	if err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	f, err := os.Create(tmp.Name())
	if err != nil {
		return "", err
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

// extractSkillFromTarball extracts a single skill directory from a tarball.
// It handles two tarball formats:
//   - Release asset: entries under skills/<name>/
//   - Repo archive:  entries under <repo-prefix>/skills/<name>/ (strip first component)
func extractSkillFromTarball(tarballPath, skillName string) error {
	f, err := os.Open(tarballPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip error: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	suffix := "skills/" + skillName + "/"
	destDir := paths.SkillsDir()
	found := false

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar error: %w", err)
		}

		// Find the skills/<name>/ portion in the path.
		// Release tarballs: "skills/<name>/..."
		// Repo archives:    "<prefix>/skills/<name>/..."
		idx := strings.Index(header.Name, suffix)
		if idx < 0 {
			continue
		}

		found = true
		// relPath is "<name>/..." (relative to the skills dir)
		relPath := header.Name[idx+len("skills/"):]
		targetPath := filepath.Join(destDir, relPath)

		// Prevent path traversal
		if !strings.HasPrefix(filepath.Clean(targetPath), filepath.Clean(destDir)) {
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(targetPath, 0755)
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(targetPath), 0755)
			outFile, err := os.Create(targetPath)
			if err != nil {
				return err
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
			os.Chmod(targetPath, os.FileMode(header.Mode))
		}
	}

	if !found {
		return fmt.Errorf("skill '%s' not found in tarball", skillName)
	}
	return nil
}

// ─── helpers ────────────────────────────────────────────────────────────────

func scanSkills() []skillMeta {
	home, _ := os.UserHomeDir()
	skillsDir := filepath.Join(home, ".claude", "skills")
	return scanSkillsIn(skillsDir)
}

func scanSkillsIn(dir string) []skillMeta {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var skills []skillMeta
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillDir := filepath.Join(dir, e.Name())
		skillFile := filepath.Join(skillDir, "SKILL.md")

		data, err := os.ReadFile(skillFile)
		if err != nil {
			continue
		}

		meta := parseSkillFrontmatter(data)
		meta.Dir = skillDir
		if meta.Name == "" {
			meta.Name = e.Name()
		}

		// Check for evals
		if info, err := os.Stat(filepath.Join(skillDir, "evals")); err == nil && info.IsDir() {
			meta.HasEvals = true
		}

		// Check for references
		if info, err := os.Stat(filepath.Join(skillDir, "references")); err == nil && info.IsDir() {
			meta.HasRefs = true
		}

		skills = append(skills, meta)
	}
	return skills
}

func parseSkillFrontmatter(data []byte) skillMeta {
	content := string(data)
	if !strings.HasPrefix(content, "---") {
		return skillMeta{}
	}

	end := strings.Index(content[3:], "---")
	if end < 0 {
		return skillMeta{}
	}

	yamlBlock := content[3 : 3+end]
	var meta skillMeta
	yaml.Unmarshal([]byte(yamlBlock), &meta)
	return meta
}

func init() {
	skillsListCmd.Flags().StringVar(&skillsListFormat, "format", "table", "Output format: table or json")
	skillsListCmd.Flags().StringVar(&skillsListTier, "tier", "all", "Filter by tier: core, premium, or all")
	skillsListCmd.Flags().BoolVar(&skillsListInstalled, "installed", false, "Show only installed skills")

	skillsRemoveCmd.Flags().BoolVar(&skillsRemoveForce, "force", false, "Skip confirmation and core skill protection")

	skillsCmd.AddCommand(skillsListCmd)
	skillsCmd.AddCommand(skillsVerifyCmd)
	skillsCmd.AddCommand(skillsOutdatedCmd)
	skillsCmd.AddCommand(skillsInstallCmd)
	skillsCmd.AddCommand(skillsRemoveCmd)
	skillsCmd.AddCommand(skillsUpdateCmd)

	rootCmd.AddCommand(skillsCmd)
}
