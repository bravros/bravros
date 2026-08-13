package stack

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// DetectRuntime tests
// ---------------------------------------------------------------------------

func TestDetectRuntime_Go(t *testing.T) {
	result := DetectRuntime("go")
	v, ok := result["go"]
	if !ok {
		t.Fatal("expected 'go' key in runtime map")
	}
	if v == "" {
		t.Error("expected non-empty go version")
	}
	if !strings.Contains(v, ".") {
		t.Errorf("expected version with dot, got %s", v)
	}
}

func TestDetectRuntime_PHP(t *testing.T) {
	if _, err := exec.LookPath("php"); err != nil {
		t.Skip("php not available in test environment")
	}
	result := DetectRuntime("php")
	v, ok := result["php"]
	if !ok {
		t.Fatal("expected 'php' key in runtime map")
	}
	if v == "" {
		t.Error("expected non-empty php version")
	}
	if !strings.Contains(v, ".") {
		t.Errorf("expected version with dot, got %s", v)
	}
}

func TestDetectRuntime_Node(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available in test environment")
	}
	result := DetectRuntime("node")
	v, ok := result["node"]
	if !ok {
		t.Fatal("expected 'node' key in runtime map")
	}
	if v == "" {
		t.Error("expected non-empty node version")
	}
	if !strings.Contains(v, ".") {
		t.Errorf("expected version with dot, got %s", v)
	}
}

func TestDetectRuntime_UnknownLanguage(t *testing.T) {
	result := DetectRuntime("unknown_language")
	if len(result) != 0 {
		t.Errorf("expected empty map for unknown language, got %v", result)
	}
}

// ---------------------------------------------------------------------------
// DetectProjectType tests
// ---------------------------------------------------------------------------

func TestDetectProjectType_LaravelWebapp(t *testing.T) {
	dir := setupTestDir(t)
	writeFile(t, filepath.Join(dir, "routes", "web.php"), "<?php // routes")
	writeFile(t, filepath.Join(dir, "resources", "views", "welcome.blade.php"), "<html></html>")

	result := DetectProjectType("php", "laravel", dir)
	if result != "webapp" {
		t.Errorf("expected 'webapp', got %s", result)
	}
}

func TestDetectProjectType_LaravelAPI(t *testing.T) {
	dir := setupTestDir(t)
	writeFile(t, filepath.Join(dir, "routes", "api.php"), "<?php // api routes")

	result := DetectProjectType("php", "laravel", dir)
	if result != "api" {
		t.Errorf("expected 'api', got %s", result)
	}
}

func TestDetectProjectType_GoCLI(t *testing.T) {
	dir := setupTestDir(t)
	writeFile(t, filepath.Join(dir, "main.go"), "package main\nfunc main() {}")

	result := DetectProjectType("go", "none", dir)
	if result != "cli" {
		t.Errorf("expected 'cli', got %s", result)
	}
}

func TestDetectProjectType_ExpoMobile(t *testing.T) {
	dir := setupTestDir(t)
	writeFile(t, filepath.Join(dir, "app.json"), `{"expo": {"name": "myapp"}}`)
	writeFile(t, filepath.Join(dir, "package.json"), `{"dependencies": {"expo": "^51.0"}}`)

	result := DetectProjectType("node", "expo", dir)
	if result != "mobile" {
		t.Errorf("expected 'mobile', got %s", result)
	}
}

func TestDetectProjectType_GoLibrary(t *testing.T) {
	dir := setupTestDir(t)
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/lib\n\ngo 1.22.0\n")

	result := DetectProjectType("go", "none", dir)
	if result != "library" {
		t.Errorf("expected 'library', got %s", result)
	}
}

func TestDetectProjectType_NextjsWebapp(t *testing.T) {
	dir := setupTestDir(t)
	writeFile(t, filepath.Join(dir, "package.json"), `{"dependencies": {"next": "^14.0"}}`)
	writeFile(t, filepath.Join(dir, "app", "page.tsx"), "export default function Home() {}")

	result := DetectProjectType("node", "nextjs", dir)
	if result != "webapp" {
		t.Errorf("expected 'webapp', got %s", result)
	}
}
