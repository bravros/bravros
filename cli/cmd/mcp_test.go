package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resetMCPRegisterFlags() {
	mcpRegisterFrom = ""
	mcpRegisterSkipMissingSecrets = false
	mcpRegisterDryRun = false
}

func TestMCPRegister_DryRun_ServersArray(t *testing.T) {
	resetMCPRegisterFlags()
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "mcp.json")

	cfg := `{
		"servers": [
			{
				"name": "context7",
				"command": "npx",
				"args": ["-y", "@upstash/context7-mcp@latest"],
				"env": {"API_KEY": "secret"},
				"env_required": ["TEST_MCP_SECRET"],
				"transport": "stdio"
			}
		]
	}`
	if err := os.WriteFile(configFile, []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("TEST_MCP_SECRET", "present_value")

	mcpRegisterFrom = configFile
	mcpRegisterDryRun = true

	var out bytes.Buffer
	mcpRegisterCmd.SetOut(&out)
	mcpRegisterCmd.SetErr(&bytes.Buffer{})

	if err := mcpRegisterCmd.RunE(mcpRegisterCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "[dry-run] would register: context7") {
		t.Errorf("expected dry-run register message, got:\n%s", got)
	}
	if !strings.Contains(got, "1 server(s) processed, 0 skipped.") {
		t.Errorf("expected 1 processed, 0 skipped, got:\n%s", got)
	}
}

func TestMCPRegister_DryRun_McpServersMap(t *testing.T) {
	resetMCPRegisterFlags()
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "mcp.json")

	cfg := `{
		"mcpServers": {
			"zeta": {
				"command": "node",
				"args": ["zeta.js"]
			},
			"alpha": {
				"command": "python",
				"args": ["alpha.py"]
			}
		}
	}`
	if err := os.WriteFile(configFile, []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}

	mcpRegisterFrom = configFile
	mcpRegisterDryRun = true

	var out bytes.Buffer
	mcpRegisterCmd.SetOut(&out)
	mcpRegisterCmd.SetErr(&bytes.Buffer{})

	if err := mcpRegisterCmd.RunE(mcpRegisterCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	got := out.String()
	// Must sort keys alphabetically: alpha before zeta
	alphaIdx := strings.Index(got, "would register: alpha")
	zetaIdx := strings.Index(got, "would register: zeta")
	if alphaIdx == -1 || zetaIdx == -1 || alphaIdx >= zetaIdx {
		t.Errorf("expected alpha registered before zeta in sorted order, got:\n%s", got)
	}
	if !strings.Contains(got, "2 server(s) processed, 0 skipped.") {
		t.Errorf("expected 2 processed, got:\n%s", got)
	}
}

func TestMCPRegister_MissingSecrets_Fails(t *testing.T) {
	resetMCPRegisterFlags()
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "mcp.json")

	cfg := `{
		"servers": [
			{
				"name": "secret-server",
				"command": "echo",
				"env_required": ["UNSET_TEST_KEY_123"]
			}
		]
	}`
	if err := os.WriteFile(configFile, []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("UNSET_TEST_KEY_123", "")
	mcpRegisterFrom = configFile
	mcpRegisterSkipMissingSecrets = false

	var out bytes.Buffer
	mcpRegisterCmd.SetOut(&out)
	mcpRegisterCmd.SetErr(&bytes.Buffer{})

	err := mcpRegisterCmd.RunE(mcpRegisterCmd, nil)
	if err == nil {
		t.Fatal("expected error when required secret is missing, got nil")
	}
	if !strings.Contains(err.Error(), "requires env vars: UNSET_TEST_KEY_123") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMCPRegister_SkipMissingSecrets(t *testing.T) {
	resetMCPRegisterFlags()
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "mcp.json")

	cfg := `{
		"servers": [
			{
				"name": "secret-server",
				"command": "echo",
				"env_required": ["UNSET_TEST_KEY_456"]
			}
		]
	}`
	if err := os.WriteFile(configFile, []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("UNSET_TEST_KEY_456", "")
	mcpRegisterFrom = configFile
	mcpRegisterSkipMissingSecrets = true

	var out bytes.Buffer
	mcpRegisterCmd.SetOut(&out)
	mcpRegisterCmd.SetErr(&bytes.Buffer{})

	if err := mcpRegisterCmd.RunE(mcpRegisterCmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "skipping secret-server (missing env vars: UNSET_TEST_KEY_456)") {
		t.Errorf("expected skip warning, got:\n%s", got)
	}
	if !strings.Contains(got, "0 server(s) processed, 1 skipped.") {
		t.Errorf("expected 0 processed, 1 skipped, got:\n%s", got)
	}
}

func TestMCPRegister_Precedence_ServersArrayWins(t *testing.T) {
	resetMCPRegisterFlags()
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "mcp.json")

	cfg := `{
		"servers": [
			{
				"name": "legacy-server",
				"command": "echo"
			}
		],
		"mcpServers": {
			"modern-server": {
				"command": "echo"
			}
		}
	}`
	if err := os.WriteFile(configFile, []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}

	mcpRegisterFrom = configFile
	mcpRegisterDryRun = true

	var out bytes.Buffer
	mcpRegisterCmd.SetOut(&out)
	mcpRegisterCmd.SetErr(&bytes.Buffer{})

	if err := mcpRegisterCmd.RunE(mcpRegisterCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "legacy-server") {
		t.Errorf("expected legacy-server to be processed, got:\n%s", got)
	}
	if strings.Contains(got, "modern-server") {
		t.Errorf("expected modern-server to be ignored due to servers array precedence, got:\n%s", got)
	}
}

func TestMCPRegister_ValidationErrors(t *testing.T) {
	resetMCPRegisterFlags()

	// 1. Missing --from
	mcpRegisterFrom = ""
	err := mcpRegisterCmd.RunE(mcpRegisterCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--from is required") {
		t.Errorf("expected --from is required error, got %v", err)
	}

	// 2. Non-existent file
	mcpRegisterFrom = "/non/existent/path/mcp.json"
	err = mcpRegisterCmd.RunE(mcpRegisterCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot read config file") {
		t.Errorf("expected cannot read config file error, got %v", err)
	}

	// 3. Invalid JSON
	dir := t.TempDir()
	badJSON := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(badJSON, []byte("{invalid json"), 0644)
	mcpRegisterFrom = badJSON
	err = mcpRegisterCmd.RunE(mcpRegisterCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("expected invalid JSON error, got %v", err)
	}
}

func TestBuildMCPServerJSON(t *testing.T) {
	srv := MCPServerConfig{
		Name:      "test-srv",
		Command:   "node",
		Args:      []string{"--version"},
		Env:       map[string]string{"FOO": "bar"},
		Transport: "stdio",
	}

	raw := buildMCPServerJSON(srv)
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("unmarshal buildMCPServerJSON: %v", err)
	}

	if parsed["command"] != "node" {
		t.Errorf("command = %v; want node", parsed["command"])
	}
	if parsed["transport"] != "stdio" {
		t.Errorf("transport = %v; want stdio", parsed["transport"])
	}
}

func TestSaveMCPState(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	servers := []MCPServerConfig{
		{Name: "server-a"},
		{Name: "server-b"},
	}

	if err := saveMCPState(servers); err != nil {
		t.Fatalf("saveMCPState: %v", err)
	}

	statePath := filepath.Join(tempHome, ".claude", "state", "bravros-managed-mcps.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}

	var state struct {
		Managed []string `json:"managed"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}

	if len(state.Managed) != 2 || state.Managed[0] != "server-a" || state.Managed[1] != "server-b" {
		t.Errorf("unexpected managed servers in state: %+v", state.Managed)
	}
}
