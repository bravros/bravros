package stack

import (
	"path/filepath"
	"testing"
)

// --- ParseComposerLock ---

func TestParseComposerLock_BasicPackages(t *testing.T) {
	dir := setupTestDir(t)
	lockPath := filepath.Join(dir, "composer.lock")
	writeFile(t, lockPath, `{
		"packages": [
			{"name": "laravel/framework", "version": "v11.0.0"},
			{"name": "pestphp/pest",      "version": "v2.3.0"},
			{"name": "laravel/sanctum",   "version": "v4.0.2"}
		]
	}`)

	got, err := ParseComposerLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"laravel": "11.0.0",
		"pest":    "2.3.0",
		"sanctum": "4.0.2",
	}
	for key, want := range cases {
		if got[key] != want {
			t.Errorf("ParseComposerLock[%q] = %q, want %q", key, got[key], want)
		}
	}
}

func TestParseComposerLock_AllInterestingPackages(t *testing.T) {
	dir := setupTestDir(t)
	lockPath := filepath.Join(dir, "composer.lock")
	writeFile(t, lockPath, `{
		"packages": [
			{"name": "laravel/framework",        "version": "v11.0.0"},
			{"name": "livewire/livewire",         "version": "v3.4.0"},
			{"name": "filament/filament",         "version": "v3.2.0"},
			{"name": "pestphp/pest",              "version": "v2.3.0"},
			{"name": "laravel/sanctum",           "version": "v4.0.2"},
			{"name": "spatie/laravel-permission", "version": "v6.0.0"}
		]
	}`)

	got, err := ParseComposerLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"laravel":           "11.0.0",
		"livewire":          "3.4.0",
		"filament":          "3.2.0",
		"pest":              "2.3.0",
		"sanctum":           "4.0.2",
		"spatie-permission": "6.0.0",
	}
	for key, want := range cases {
		if got[key] != want {
			t.Errorf("ParseComposerLock[%q] = %q, want %q", key, got[key], want)
		}
	}
}

func TestParseComposerLock_DevPackages(t *testing.T) {
	dir := setupTestDir(t)
	lockPath := filepath.Join(dir, "composer.lock")
	writeFile(t, lockPath, `{
		"packages": [
			{"name": "laravel/framework", "version": "v11.0.0"}
		],
		"packages-dev": [
			{"name": "pestphp/pest", "version": "v2.3.0"},
			{"name": "filament/filament", "version": "v3.2.0"}
		]
	}`)

	got, err := ParseComposerLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	if got["laravel"] != "11.0.0" {
		t.Errorf("expected laravel=11.0.0, got %s", got["laravel"])
	}
	if got["pest"] != "2.3.0" {
		t.Errorf("expected pest=2.3.0 (from packages-dev), got %s", got["pest"])
	}
	if got["filament"] != "3.2.0" {
		t.Errorf("expected filament=3.2.0 (from packages-dev), got %s", got["filament"])
	}
}

func TestParseComposerLock_MissingFileReturnsEmpty(t *testing.T) {
	got, err := ParseComposerLock("/nonexistent/composer.lock")
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestParseComposerLock_MalformedJSON(t *testing.T) {
	dir := setupTestDir(t)
	lockPath := filepath.Join(dir, "composer.lock")
	writeFile(t, lockPath, `{not valid json`)

	_, err := ParseComposerLock(lockPath)
	if err == nil {
		t.Error("expected error for malformed JSON, got nil")
	}
}

func TestParseComposerLock_StripLeadingV(t *testing.T) {
	dir := setupTestDir(t)
	lockPath := filepath.Join(dir, "composer.lock")
	writeFile(t, lockPath, `{
		"packages": [
			{"name": "laravel/framework", "version": "v12.3.4"}
		]
	}`)

	got, err := ParseComposerLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if got["laravel"] != "12.3.4" {
		t.Errorf("expected leading v stripped, got %q", got["laravel"])
	}
}

// --- ParsePackageLock ---

func TestParsePackageLock_NewFormat(t *testing.T) {
	dir := setupTestDir(t)
	lockPath := filepath.Join(dir, "package-lock.json")
	writeFile(t, lockPath, `{
		"lockfileVersion": 3,
		"packages": {
			"node_modules/next":       {"version": "14.2.0"},
			"node_modules/react":      {"version": "18.3.0"},
			"node_modules/typescript": {"version": "5.4.0"},
			"node_modules/tailwindcss":{"version": "3.4.1"}
		}
	}`)

	got, err := ParsePackageLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"next":        "14.2.0",
		"react":       "18.3.0",
		"typescript":  "5.4.0",
		"tailwindcss": "3.4.1",
	}
	for key, want := range cases {
		if got[key] != want {
			t.Errorf("ParsePackageLock[%q] = %q, want %q", key, got[key], want)
		}
	}
}

func TestParsePackageLock_OldFormat(t *testing.T) {
	dir := setupTestDir(t)
	lockPath := filepath.Join(dir, "package-lock.json")
	writeFile(t, lockPath, `{
		"lockfileVersion": 1,
		"dependencies": {
			"react":      {"version": "17.0.2"},
			"jest":       {"version": "27.5.1"},
			"typescript": {"version": "4.9.5"}
		}
	}`)

	got, err := ParsePackageLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"react":      "17.0.2",
		"jest":       "27.5.1",
		"typescript": "4.9.5",
	}
	for key, want := range cases {
		if got[key] != want {
			t.Errorf("ParsePackageLock[%q] = %q, want %q", key, got[key], want)
		}
	}
}

func TestParsePackageLock_ScopedPackage(t *testing.T) {
	dir := setupTestDir(t)
	lockPath := filepath.Join(dir, "package-lock.json")
	writeFile(t, lockPath, `{
		"lockfileVersion": 3,
		"packages": {
			"node_modules/@tanstack/react-query": {"version": "5.28.0"}
		}
	}`)

	got, err := ParsePackageLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if got["@tanstack/react-query"] != "5.28.0" {
		t.Errorf("expected @tanstack/react-query=5.28.0, got %q", got["@tanstack/react-query"])
	}
}

func TestParsePackageLock_MissingFileReturnsEmpty(t *testing.T) {
	got, err := ParsePackageLock("/nonexistent/package-lock.json")
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestParsePackageLock_MalformedJSON(t *testing.T) {
	dir := setupTestDir(t)
	lockPath := filepath.Join(dir, "package-lock.json")
	writeFile(t, lockPath, `{not valid json`)

	_, err := ParsePackageLock(lockPath)
	if err == nil {
		t.Error("expected error for malformed JSON, got nil")
	}
}

// --- ParseGoMod ---

func TestParseGoMod_GoVersionAndModules(t *testing.T) {
	dir := setupTestDir(t)
	modPath := filepath.Join(dir, "go.mod")
	writeFile(t, modPath, `module example.com/myapp

go 1.22.0

require (
	github.com/go-chi/chi/v5 v5.0.12
	github.com/stretchr/testify v1.9.0
	github.com/spf13/cobra v1.8.0
)
`)

	got, err := ParseGoMod(modPath)
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"go":      "1.22.0",
		"chi":     "5.0.12",
		"testify": "1.9.0",
	}
	for key, want := range cases {
		if got[key] != want {
			t.Errorf("ParseGoMod[%q] = %q, want %q", key, got[key], want)
		}
	}

	// cobra is not in the interesting set
	if _, ok := got["cobra"]; ok {
		t.Error("expected cobra not in result")
	}
}

func TestParseGoMod_GormAndGin(t *testing.T) {
	dir := setupTestDir(t)
	modPath := filepath.Join(dir, "go.mod")
	writeFile(t, modPath, `module example.com/api

go 1.21.0

require (
	github.com/gin-gonic/gin v1.10.0
	gorm.io/gorm v1.25.9
	gorm.io/driver/postgres v1.5.7
)
`)

	got, err := ParseGoMod(modPath)
	if err != nil {
		t.Fatal(err)
	}

	if got["gin"] != "1.10.0" {
		t.Errorf("expected gin=1.10.0, got %q", got["gin"])
	}
	if got["gorm"] != "1.25.9" {
		t.Errorf("expected gorm=1.25.9, got %q", got["gorm"])
	}
}

func TestParseGoMod_MissingFileReturnsEmpty(t *testing.T) {
	got, err := ParseGoMod("/nonexistent/go.mod")
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

// --- ParsePyProject ---

func TestParsePyProject_DjangoAndPytest(t *testing.T) {
	dir := setupTestDir(t)
	pyPath := filepath.Join(dir, "pyproject.toml")
	writeFile(t, pyPath, `[project]
name = "myapp"
requires-python = ">=3.11"
dependencies = [
    "django>=4.2.0",
    "pytest>=7.4.0",
]
`)

	got, err := ParsePyProject(pyPath)
	if err != nil {
		t.Fatal(err)
	}

	if got["python"] == "" {
		t.Error("expected python version extracted")
	}
	if got["django"] == "" {
		t.Error("expected django version extracted")
	}
	if got["pytest"] == "" {
		t.Error("expected pytest version extracted")
	}
}

func TestParsePyProject_RequirementsTxtFallback(t *testing.T) {
	dir := setupTestDir(t)
	// No pyproject.toml, only requirements.txt
	reqPath := filepath.Join(dir, "requirements.txt")
	writeFile(t, reqPath, `flask==2.3.0
pytest==7.4.0
sqlalchemy==2.0.0
`)

	// Pass pyproject.toml path — ParsePyProject will derive requirements.txt path
	pyPath := filepath.Join(dir, "pyproject.toml")
	got, err := ParsePyProject(pyPath)
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"flask":      "2.3.0",
		"pytest":     "7.4.0",
		"sqlalchemy": "2.0.0",
	}
	for key, want := range cases {
		if got[key] != want {
			t.Errorf("ParsePyProject[%q] = %q, want %q", key, got[key], want)
		}
	}
}

func TestParsePyProject_MissingBothFilesReturnsEmpty(t *testing.T) {
	got, err := ParsePyProject("/nonexistent/pyproject.toml")
	if err != nil {
		t.Fatalf("expected no error when both files missing, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestParsePyProject_FastapiAndPydantic(t *testing.T) {
	dir := setupTestDir(t)
	pyPath := filepath.Join(dir, "pyproject.toml")
	writeFile(t, pyPath, `[project]
requires-python = ">=3.12"
dependencies = [
    "fastapi>=0.110.0",
    "pydantic>=2.6.0",
]
`)

	got, err := ParsePyProject(pyPath)
	if err != nil {
		t.Fatal(err)
	}
	if got["fastapi"] == "" {
		t.Error("expected fastapi version extracted")
	}
	if got["pydantic"] == "" {
		t.Error("expected pydantic version extracted")
	}
}
