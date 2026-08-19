package cmd

// mcp.go — install.sh-replacement verb for MCP server registration (B-0098).
//
// Implements:
//   bravros mcp register --from config/mcp.json [--skip-missing-secrets] [--dry-run]
//
// DRY replacement for the inline `claude mcp add-json` block in install.sh.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bravros/bravros/cli/internal/github"
	"github.com/spf13/cobra"
)

// MCPServerConfig is one entry in the config/mcp.json "servers" array.
type MCPServerConfig struct {
	Name        string            `json:"name"`
	Command     string            `json:"command"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	EnvRequired []string          `json:"env_required,omitempty"`
	Transport   string            `json:"transport,omitempty"`
}

// MCPServerInline is an entry in the mcpServers map shape of config/mcp.json.
// The map key is the server name (not stored here — callers use it as Name).
type MCPServerInline struct {
	Command   string            `json:"command"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Transport string            `json:"transport,omitempty"`
}

// MCPConfig is the top-level structure of config/mcp.json.
//
// A "retired" key is intentionally unmapped. In the kaisser-era shell installer it was the
// unregister list, consumed by a `claude mcp remove` + sidecar-purge pass on every install;
// that pass was not carried across, so nothing in this repo reads it today. Unmarshalling
// ignores unknown keys, so a config still carrying one is accepted and its entries simply
// stay registered.
type MCPConfig struct {
	Servers    []MCPServerConfig          `json:"servers"`
	McpServers map[string]MCPServerInline `json:"mcpServers,omitempty"`
}

// mcpStateFilePath is where bravros tracks managed MCP servers.
func mcpStateFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "state", "bravros-managed-mcps.json")
}

var (
	mcpRegisterFrom               string
	mcpRegisterSkipMissingSecrets bool
	mcpRegisterDryRun             bool
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "MCP server management",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var mcpRegisterCmd = &cobra.Command{
	Use:   "register",
	Short: "Register MCP servers from config/mcp.json (B-0098)",
	Long: `Idempotent MCP server registration. Reads a JSON config file listing
servers to register and calls claude mcp add-json for each one; add-json
returning non-zero because the server already exists is logged and skipped,
which is what makes repeat runs safe.

  bravros mcp register --from config/mcp.json
  bravros mcp register --from config/mcp.json --skip-missing-secrets
  bravros mcp register --from config/mcp.json --dry-run

Two config shapes are accepted (both backward-compatible):

  1) Legacy "servers" array:
     {
       "servers": [
         {
           "name": "context7",
           "command": "npx",
           "args": ["-y", "@upstash/context7-mcp@latest"],
           "env_required": []
         }
       ]
     }

  2) "mcpServers" map (matches the canonical config/mcp.json shape):
     {
       "mcpServers": {
         "context7": {
           "command": "npx",
           "args": ["-y", "@upstash/context7-mcp@latest"],
           "env": { "CONTEXT7_API_KEY": "__OP_INJECT__" }
         }
       }
     }

For "mcpServers" entries, EnvRequired defaults to the sorted keys of "env"
(so 1Password-injected secrets still gate registration). Use
--skip-missing-secrets to register servers whose required env vars are empty.

When BOTH "servers" and "mcpServers" are present in the same file, the legacy
"servers" array takes precedence and "mcpServers" is silently ignored.

State is tracked at ~/.claude/state/bravros-managed-mcps.json.

This verb only ever ADDS. It never calls claude mcp list and never removes a
server, and no other bravros verb does either — there is currently no
unregistration path. Removing a server is a manual claude mcp remove.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		from := mcpRegisterFrom
		if from == "" {
			return fmt.Errorf("--from is required (path to mcp.json config)")
		}

		data, err := os.ReadFile(from)
		if err != nil {
			return fmt.Errorf("cannot read config file %s: %w", from, err)
		}

		var cfg MCPConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("invalid JSON in %s: %w", from, err)
		}

		// Synthesize cfg.Servers from the mcpServers map shape if the legacy
		// servers array is empty. Iterate in sorted key order for deterministic output.
		// Precedence rule: if BOTH "servers" and "mcpServers" are present in the same
		// config file, the legacy "servers" array wins and "mcpServers" is silently
		// dropped. This is intentional (legacy-first) and documented in `--help`.
		if len(cfg.Servers) == 0 && len(cfg.McpServers) > 0 {
			keys := make([]string, 0, len(cfg.McpServers))
			for k := range cfg.McpServers {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				v := cfg.McpServers[k]
				// Use nil-default (not []string{}) to match how the legacy
				// servers[] shape represents "no required env vars" via
				// `json:"env_required,omitempty"`. Functionally identical
				// but keeps both code paths consistent.
				var envRequired []string
				if len(v.Env) > 0 {
					envKeys := make([]string, 0, len(v.Env))
					for ek := range v.Env {
						envKeys = append(envKeys, ek)
					}
					sort.Strings(envKeys)
					envRequired = envKeys
				}
				cfg.Servers = append(cfg.Servers, MCPServerConfig{
					Name:        k,
					Command:     v.Command,
					Args:        v.Args,
					Env:         v.Env,
					EnvRequired: envRequired,
					Transport:   v.Transport,
				})
			}
		}

		out := cmd.OutOrStdout()

		// Process each server entry.
		registered := 0
		skipped := 0
		for _, srv := range cfg.Servers {
			// Check env_required vars.
			missingEnv := []string{}
			for _, envKey := range srv.EnvRequired {
				if os.Getenv(envKey) == "" {
					missingEnv = append(missingEnv, envKey)
				}
			}
			if len(missingEnv) > 0 {
				if mcpRegisterSkipMissingSecrets {
					fmt.Fprintf(out, "⚠️  skipping %s (missing env vars: %s)\n", srv.Name, strings.Join(missingEnv, ", "))
					skipped++
					continue
				}
				return fmt.Errorf("server %s requires env vars: %s (use --skip-missing-secrets to skip)", srv.Name, strings.Join(missingEnv, ", "))
			}

			// Build the JSON payload for claude mcp add-json.
			serverJSON := buildMCPServerJSON(srv)

			if mcpRegisterDryRun {
				fmt.Fprintf(out, "[dry-run] would register: %s\n  payload: %s\n", srv.Name, serverJSON)
				registered++
				continue
			}

			// Call claude mcp add-json.
			_, _, callErr := github.Run("claude", "mcp", "add-json", srv.Name, serverJSON)
			if callErr != nil {
				// add-json may return non-zero if server already exists — log and continue.
				fmt.Fprintf(out, "ℹ️  %s: already registered or add-json returned non-zero (continuing)\n", srv.Name)
				registered++
				continue
			}
			fmt.Fprintf(out, "✅ registered: %s\n", srv.Name)
			registered++
		}

		// Update state file.
		if !mcpRegisterDryRun {
			if err := saveMCPState(cfg.Servers); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not save state file: %v\n", err)
			}
		}

		fmt.Fprintf(out, "\n%d server(s) processed, %d skipped.\n", registered, skipped)
		return nil
	},
}

// buildMCPServerJSON constructs the JSON payload for `claude mcp add-json`.
func buildMCPServerJSON(srv MCPServerConfig) string {
	payload := map[string]interface{}{
		"command": srv.Command,
	}
	if len(srv.Args) > 0 {
		payload["args"] = srv.Args
	}
	if len(srv.Env) > 0 {
		payload["env"] = srv.Env
	}
	if srv.Transport != "" {
		payload["transport"] = srv.Transport
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

// saveMCPState persists the list of managed server names to the state file.
func saveMCPState(servers []MCPServerConfig) error {
	statePath := mcpStateFilePath()
	if err := os.MkdirAll(filepath.Dir(statePath), 0755); err != nil {
		return err
	}
	names := make([]string, 0, len(servers))
	for _, s := range servers {
		names = append(names, s.Name)
	}
	state := map[string]interface{}{"managed": names}
	b, _ := json.MarshalIndent(state, "", "  ")
	return os.WriteFile(statePath, b, 0644)
}

func init() {
	mcpRegisterCmd.Flags().StringVar(&mcpRegisterFrom, "from", "", "Path to MCP config JSON file (required)")
	mcpRegisterCmd.Flags().BoolVar(&mcpRegisterSkipMissingSecrets, "skip-missing-secrets", false, "Skip servers with missing required env vars instead of failing")
	mcpRegisterCmd.Flags().BoolVar(&mcpRegisterDryRun, "dry-run", false, "Show what would be registered without calling claude mcp add-json")

	mcpCmd.AddCommand(mcpRegisterCmd)
	rootCmd.AddCommand(mcpCmd)
}
