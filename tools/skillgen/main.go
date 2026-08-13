package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// SanitizeRule represents a find-replace pattern for sanitization.
type SanitizeRule struct {
	Name        string
	Pattern     *regexp.Regexp
	Replacement string
}

// SanitizeRules adapts instructions for non-Claude hosts.
var SanitizeRules = []SanitizeRule{
	{Name: "tool-WebFetch", Pattern: regexp.MustCompile(`\bWebFetch\b`), Replacement: "the web fetch tool"},
	{Name: "tool-WebSearch", Pattern: regexp.MustCompile(`\bWebSearch\b`), Replacement: "the web search tool"},
	{Name: "tool-NotebookEdit", Pattern: regexp.MustCompile(`\bNotebookEdit\b`), Replacement: "the notebook editing tool"},
	{Name: "tool-EnterWorktree", Pattern: regexp.MustCompile(`\bEnterWorktree\b`), Replacement: "the worktree entry tool"},
	{Name: "tool-ExitWorktree", Pattern: regexp.MustCompile(`\bExitWorktree\b`), Replacement: "the worktree exit tool"},
	{Name: "tool-Monitor", Pattern: regexp.MustCompile(`\bMonitor\b tool`), Replacement: "the process monitoring tool"},
	{Name: "tool-TaskStop", Pattern: regexp.MustCompile(`\bTaskStop\b`), Replacement: "the task stop tool"},
	{Name: "tool-ToolSearch", Pattern: regexp.MustCompile(`\bToolSearch\b`), Replacement: "the tool search tool"},
	{Name: "tool-Skill", Pattern: regexp.MustCompile(`\bSkill\b tool`), Replacement: "the skill invocation tool"},

	{Name: "model-claude-opus-versioned", Pattern: regexp.MustCompile(`Claude Opus(?:\s+\d+\.\d+)?`), Replacement: "the AI (Opus tier)"},
	{Name: "model-claude-sonnet-versioned", Pattern: regexp.MustCompile(`Claude Sonnet(?:\s+\d+\.\d+)?`), Replacement: "the AI (Sonnet tier)"},
	{Name: "model-claude-haiku-versioned", Pattern: regexp.MustCompile(`Claude Haiku(?:\s+\d+\.\d+)?`), Replacement: "the AI (Haiku tier)"},
	{Name: "model-opus-bold", Pattern: regexp.MustCompile(`\*\*Opus(?:\s+\d+\.\d+)?\*\*`), Replacement: "**the Opus tier**"},
	{Name: "model-sonnet-bold", Pattern: regexp.MustCompile(`\*\*Sonnet(?:\s+\d+\.\d+)?\*\*`), Replacement: "**the Sonnet tier**"},
	{Name: "model-haiku-bold", Pattern: regexp.MustCompile(`\*\*Haiku(?:\s+\d+\.\d+)?\*\*`), Replacement: "**the Haiku tier**"},
	{Name: "model-claude-id-generic", Pattern: regexp.MustCompile(`claude-(?:opus|sonnet|haiku)-\d+-\d+`), Replacement: "the-ai-model"},
	{Name: "model-claude-3-7-sonnet", Pattern: regexp.MustCompile(`claude-3-7-sonnet-\d+`), Replacement: "the-ai-model"},
	{Name: "model-claude-3-5-sonnet", Pattern: regexp.MustCompile(`claude-3-5-sonnet-\d+`), Replacement: "the-ai-model"},
	{Name: "model-claude-3-haiku", Pattern: regexp.MustCompile(`claude-3-haiku-\d+`), Replacement: "the-ai-model"},

	{Name: "brand-Claude-Code-caps", Pattern: regexp.MustCompile(`Claude Code`), Replacement: "the AI CLI"},
	{Name: "brand-claude-code-lower", Pattern: regexp.MustCompile(`claude[- ]code`), Replacement: "the ai cli"},
	{Name: "brand-Anthropic-caps", Pattern: regexp.MustCompile(`Anthropic`), Replacement: "the AI provider"},
	{Name: "brand-anthropic-lower", Pattern: regexp.MustCompile(`anthropic`), Replacement: "the ai provider"},

	{Name: "path-tilde-claude", Pattern: regexp.MustCompile(`~/\.claude\b`), Replacement: "~/.agent_config"},
	{Name: "path-dot-claude", Pattern: regexp.MustCompile(`\.claude/`), Replacement: ".agent_config/"},
	{Name: "path-planning-dir", Pattern: regexp.MustCompile(`\.planning/`), Replacement: ".workflow/"},
	{Name: "path-claude-bin-mcp", Pattern: regexp.MustCompile(`\bclaude\s+(mcp)\b`), Replacement: "the-ai-cli $1"},
	{Name: "path-claude-bin-plugins", Pattern: regexp.MustCompile(`\bclaude\s+(plugins)\b`), Replacement: "the-ai-cli $1"},
	{Name: "path-CLAUDE-md", Pattern: regexp.MustCompile(`CLAUDE\.md`), Replacement: "AGENT.md"},
	{Name: "path-claude-settings", Pattern: regexp.MustCompile(`settings\.json(?:\s+\(Claude\))?`), Replacement: "agent-settings.json"},
	{Name: "path-claude-code-session-id", Pattern: regexp.MustCompile(`CLAUDE_CODE_SESSION_ID`), Replacement: "AGENT_SESSION_ID"},
	{Name: "path-claude-session-id", Pattern: regexp.MustCompile(`CLAUDE_SESSION_ID`), Replacement: "AGENT_SESSION_ID"},

	{Name: "hook-SessionStart", Pattern: regexp.MustCompile(`\bSessionStart\b`), Replacement: "SessionInit"},
	{Name: "hook-PreToolUse", Pattern: regexp.MustCompile(`\bPreToolUse\b`), Replacement: "BeforeTool"},
	{Name: "hook-PostToolUse", Pattern: regexp.MustCompile(`\bPostToolUse\b`), Replacement: "AfterTool"},

	{Name: "term-sdlc", Pattern: regexp.MustCompile(`\bSDLC\b`), Replacement: "the workflow system"},
	{Name: "term-plan-file-pattern", Pattern: regexp.MustCompile(`P-\d{4}-[a-z0-9-]+-todo\.md`), Replacement: "plan-NNNN-<name>-todo.md"},
	{Name: "term-backlog-id", Pattern: regexp.MustCompile(`B-\d{4}\b`), Replacement: "BACKLOG-ID"},
	{Name: "term-plan-id", Pattern: regexp.MustCompile(`P-\d{4}\b`), Replacement: "PLAN-ID"},

	{Name: "gh-pr-view", Pattern: regexp.MustCompile(`gh pr view`), Replacement: "get-pr-info"},
	{Name: "gh-pr-create", Pattern: regexp.MustCompile(`gh pr create`), Replacement: "create-pr"},
	{Name: "gh-pr-merge", Pattern: regexp.MustCompile(`gh pr merge`), Replacement: "merge-pr"},
	{Name: "gh-issue-generic", Pattern: regexp.MustCompile(`gh issue (\w+)`), Replacement: "manage-issue $1"},
	{Name: "gh-workflow-generic", Pattern: regexp.MustCompile(`gh workflow (\w+)`), Replacement: "trigger-workflow $1"},

	{Name: "owner-skaisser-github", Pattern: regexp.MustCompile(`skaisser/claude-cli`), Replacement: "your-org/your-cli"},
	{Name: "owner-skaisser-email", Pattern: regexp.MustCompile(`skaisser@gmail\.com`), Replacement: "your@email.com"},
	{Name: "owner-skaisser-github-user", Pattern: regexp.MustCompile(`@skaisser\b`), Replacement: "@your-github-user"},
	{Name: "owner-sites-claude", Pattern: regexp.MustCompile(`~/Sites/claude\b`), Replacement: "~/your-project"},
}

// Sanitize replaces Claude-specific words with generic equivalents outside code blocks.
func Sanitize(content string) string {
	lines := strings.Split(content, "\n")
	inCodeBlock := false
	for i, line := range lines {
		if strings.HasPrefix(line, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock {
			continue
		}
		for _, rule := range SanitizeRules {
			line = rule.Pattern.ReplaceAllString(line, rule.Replacement)
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

// Linter rejects harness-specific tokens in the raw skill file content.
func LintSkill(path, content string) error {
	forbidden := []struct {
		pattern *regexp.Regexp
		name    string
	}{
		{regexp.MustCompile(`\bAskUserQuestion\b`), "AskUserQuestion"},
		{regexp.MustCompile(`\bAgent\s*\(`), "Agent("},
		{regexp.MustCompile(`\bmcp__`), "mcp__"},
		{regexp.MustCompile(`~/\.claude\b`), "~/.claude"},
		// No word boundaries: `\bkaisser\b` does NOT match inside "skaisser", which is
		// how 49 `file:///Users/skaisser/...` links shipped past this lint on 2026-08-13.
		{regexp.MustCompile(`(?i)kaisser`), "kaisser/skaisser"},
		// Absolute local paths are broken on every machine but the author's, and they
		// leak the author's username into a public repo.
		{regexp.MustCompile(`file://`), "absolute file:// URL"},
		{regexp.MustCompile(`/Users/[^/\s]+/`), "absolute /Users/ path"},
		{regexp.MustCompile(`/home/[^/\s]+/`), "absolute /home/ path"},
	}

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		for _, f := range forbidden {
			if f.pattern.MatchString(line) {
				return fmt.Errorf("%s:%d: forbidden token %q found", path, i+1, f.name)
			}
		}
	}
	return nil
}

type SkillMeta struct {
	Name        string `yaml:"name"`
	Category    string `yaml:"category"`
	Description string `yaml:"description"`
	Core        bool   `yaml:"core"`
}

type SkillItem struct {
	Meta SkillMeta
	Body string
	Path string
	Dir  string
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
}

// LintTree lints every text file under skills/, not just SKILL.md. references/ and
// skill.yaml ship to users exactly like the skill body does, so they need the same gate.
func LintTree(skillsDir string) error {
	lintable := map[string]bool{".md": true, ".yaml": true, ".yml": true, ".sh": true, ".txt": true}
	var violations []string
	err := filepath.WalkDir(skillsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !lintable[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read file %s: %w", path, err)
		}
		// Collect rather than fail fast: a partial list reads like "one bug" when it is
		// a systemic gap, and the operator needs the whole surface to plan the fix.
		if lintErr := LintSkill(path, string(data)); lintErr != nil {
			violations = append(violations, lintErr.Error())
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(violations) > 0 {
		return fmt.Errorf("%d file(s) contain forbidden tokens:\n  %s",
			len(violations), strings.Join(violations, "\n  "))
	}
	return nil
}

func main() {
	skillsDir := "skills"
	if _, err := os.Stat(skillsDir); os.IsNotExist(err) {
		fmt.Printf("Error: skills directory not found\n")
		os.Exit(1)
	}

	if err := LintTree(skillsDir); err != nil {
		fmt.Printf("Lint error: %v\n", err)
		os.Exit(1)
	}

	var skills []SkillItem

	err := filepath.WalkDir(skillsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != "SKILL.md" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read file %s: %w", path, err)
		}

		content := string(data)
		if err := LintSkill(path, content); err != nil {
			return err
		}

		parts := strings.SplitN(content, "---", 3)
		if len(parts) < 3 {
			return fmt.Errorf("invalid skill format (missing frontmatter separators) in %s", path)
		}

		var meta SkillMeta
		if err := yaml.Unmarshal([]byte(parts[1]), &meta); err != nil {
			return fmt.Errorf("unmarshal frontmatter in %s: %w", path, err)
		}

		dir := filepath.Dir(path)
		yamlPath := filepath.Join(dir, "skill.yaml")
		if _, err := os.Stat(yamlPath); err == nil {
			yamlData, err := os.ReadFile(yamlPath)
			if err == nil {
				var sy struct {
					Category string `yaml:"category"`
					Metadata map[string]struct {
						Name        string `yaml:"name"`
						Description string `yaml:"description"`
					} `yaml:"metadata"`
				}
				if err := yaml.Unmarshal(yamlData, &sy); err == nil {
					if sy.Category != "" {
						meta.Category = sy.Category
					}
					if enMeta, ok := sy.Metadata["en"]; ok {
						if enMeta.Name != "" {
							meta.Name = enMeta.Name
						}
						if enMeta.Description != "" {
							meta.Description = enMeta.Description
						}
					}
				}
			}
		}

		skills = append(skills, SkillItem{
			Meta: meta,
			Body: strings.TrimSpace(parts[2]),
			Path: path,
			Dir:  dir,
		})

		return nil
	})

	if err != nil {
		fmt.Printf("Generation failed: %v\n", err)
		os.Exit(1)
	}

	// 1. Generate aggregated always-on instructions files for each host
	hosts := []string{"claude", "gemini", "agents", "cursor"}
	for _, host := range hosts {
		var buf bytes.Buffer
		buf.WriteString("# Bravros Agent Toolkit Instructions\n\n")
		buf.WriteString("This file contains the instructions for the Bravros agent toolkit.\n\n")

		for _, s := range skills {
			name := s.Meta.Name
			desc := s.Meta.Description
			body := s.Body

			if host != "claude" {
				// Namespace prefixing for non-Claude hosts
				name = "bravros-" + name
				body = Sanitize(body)
				desc = Sanitize(desc)
			}

			buf.WriteString(fmt.Sprintf("## Skill: %s\n", name))
			buf.WriteString(fmt.Sprintf("%s\n\n", desc))
			buf.WriteString(fmt.Sprintf("%s\n\n", body))
			buf.WriteString("---\n\n")
		}

		outputFilename := "CLAUDE.md"
		switch host {
		case "gemini":
			outputFilename = "GEMINI.md"
		case "agents":
			outputFilename = "AGENTS.md"
		case "cursor":
			outputFilename = ".cursorrules"
		}

		// Write to root
		if err := os.WriteFile(outputFilename, buf.Bytes(), 0644); err != nil {
			fmt.Printf("Error writing %s: %v\n", outputFilename, err)
			os.Exit(1)
		}

		// Write to expected/
		expectedDir := filepath.Join("tools", "skillgen", "expected")
		if err := os.MkdirAll(expectedDir, 0755); err != nil {
			fmt.Printf("Error creating expected dir: %v\n", err)
			os.Exit(1)
		}
		expectedPath := filepath.Join(expectedDir, outputFilename)
		if err := os.WriteFile(expectedPath, buf.Bytes(), 0644); err != nil {
			fmt.Printf("Error writing golden file %s: %v\n", expectedPath, err)
			os.Exit(1)
		}

		fmt.Printf("Successfully generated %s\n", outputFilename)
	}

	// 2. Packaging splits: core plugin + 6 category plugins under plugins/
	categories := []string{"sdlc", "design", "web", "deploy", "content", "tools"}

	// Core vs Category assignments
	coreSkills := []SkillItem{}
	categorySkills := map[string][]SkillItem{}
	for _, cat := range categories {
		categorySkills[cat] = []SkillItem{}
	}

	for _, s := range skills {
		if s.Meta.Core {
			coreSkills = append(coreSkills, s)
		} else {
			cat := strings.ToLower(s.Meta.Category)
			if cat == "" {
				cat = "tools" // default fallback
			}
			categorySkills[cat] = append(categorySkills[cat], s)
		}
	}

	// Clear out and recreate plugins directory
	pluginsDir := "plugins"
	_ = os.RemoveAll(pluginsDir)
	_ = os.MkdirAll(pluginsDir, 0755)

	// Deploy Core plugin
	corePluginDir := filepath.Join(pluginsDir, "core")
	_ = os.MkdirAll(filepath.Join(corePluginDir, "skills"), 0755)
	for _, s := range coreSkills {
		err := copyDir(s.Dir, filepath.Join(corePluginDir, "skills", s.Meta.Name))
		if err != nil {
			fmt.Printf("Error copying core skill %s: %v\n", s.Meta.Name, err)
			os.Exit(1)
		}
	}

	// Construct core dependencies (only for categories with actual skills)
	deps := map[string]string{}
	for cat, list := range categorySkills {
		if len(list) > 0 {
			deps["bravros-"+cat] = "plugins/" + cat
		}
	}

	coreManifest := map[string]interface{}{
		"name":         "bravros",
		"description":  "Bravros core agent toolkit: plan, build, test, and ship autonomously.",
		"version":      "0.1.0",
		"license":      "MIT",
		"dependencies": deps,
	}
	coreManifestBytes, _ := json.MarshalIndent(coreManifest, "", "  ")
	_ = os.WriteFile(filepath.Join(corePluginDir, "plugin.json"), coreManifestBytes, 0644)

	// Deploy Category plugins
	for cat, list := range categorySkills {
		if len(list) == 0 {
			continue // skip empty plugins
		}

		catPluginDir := filepath.Join(pluginsDir, cat)
		_ = os.MkdirAll(filepath.Join(catPluginDir, "skills"), 0755)

		for _, s := range list {
			err := copyDir(s.Dir, filepath.Join(catPluginDir, "skills", s.Meta.Name))
			if err != nil {
				fmt.Printf("Error copying category skill %s: %v\n", s.Meta.Name, err)
				os.Exit(1)
			}
		}

		catManifest := map[string]interface{}{
			"name":        "bravros-" + cat,
			"description": "Bravros " + strings.ToUpper(cat) + " category plugin for agent toolkits.",
			"version":     "0.1.0",
			"license":     "MIT",
		}
		catManifestBytes, _ := json.MarshalIndent(catManifest, "", "  ")
		_ = os.WriteFile(filepath.Join(catPluginDir, "plugin.json"), catManifestBytes, 0644)
	}

	fmt.Printf("Successfully structured core and category plugins under plugins/\n")
}
