package secrets

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---- helpers ----------------------------------------------------------------

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// countOccurrences counts non-overlapping occurrences of sub in s.
func countOccurrences(s, sub string) int { return strings.Count(s, sub) }

// ---- bootstrap / render -----------------------------------------------------

func TestBootstrap_PrintOnly_RendersBlock(t *testing.T) {
	var buf bytes.Buffer
	res, err := Bootstrap(BootstrapOpts{Backend: BackendEnv, PrintOnly: true, Output: &buf})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Backend != BackendEnv {
		t.Errorf("backend: got %q", res.Backend)
	}
	out := buf.String()
	if !strings.Contains(out, markerV2Start) || !strings.Contains(out, markerV2End) {
		t.Error("print-only output missing v2 markers")
	}
	if !strings.Contains(out, "bravros_secret()") {
		t.Error("print-only output missing bravros_secret function definition")
	}
}

// TestMigration_StripsV1Block: a pre-existing v1 block AND eager op-export lines
// must be removed; surrounding user lines survive; exactly one v2 block remains.
func TestMigration_StripsV1Block(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".zshrc")
	orig := strings.Join([]string{
		"export PATH=/usr/local/bin:$PATH",
		"alias gs='git status'",
		"",
		markerV1Start,
		`export FIRECRAWL_API_KEY=$(op read 'op://ClaudeCode/Firecrawl API/credencial')`,
		`export HASS_TOKEN=$(op read 'op://ClaudeCode/hass/credential')`,
		markerV1End,
		"",
		`export FIRECRAWL_API_KEY=$(op read 'op://ClaudeCode/Firecrawl API/credencial')`, // stray eager line outside any block
		"alias ll='ls -la'",
		"",
	}, "\n")
	if err := os.WriteFile(rc, []byte(orig), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := Bootstrap(BootstrapOpts{ShellRC: rc, Backend: BackendNone}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	got := readFile(t, rc)
	if strings.Contains(got, markerV1Start) || strings.Contains(got, markerV1End) {
		t.Error("v1 markers should be stripped")
	}
	if strings.Contains(got, "op read") {
		t.Errorf("eager op-read lines should be stripped, got:\n%s", got)
	}
	// User lines must survive.
	for _, want := range []string{"export PATH=/usr/local/bin:$PATH", "alias gs='git status'", "alias ll='ls -la'"} {
		if !strings.Contains(got, want) {
			t.Errorf("user line %q lost", want)
		}
	}
	if n := countOccurrences(got, markerV2Start); n != 1 {
		t.Errorf("expected exactly 1 v2 block, got %d", n)
	}
}

// TestMigration_Idempotent: running twice yields a byte-identical file with
// exactly one v2 block.
func TestMigration_Idempotent(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".zshrc")
	os.WriteFile(rc, []byte("export EDITOR=vim\n"), 0644)

	if _, err := Bootstrap(BootstrapOpts{ShellRC: rc, Backend: BackendEnv}); err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}
	after1 := readFile(t, rc)

	if _, err := Bootstrap(BootstrapOpts{ShellRC: rc, Backend: BackendEnv}); err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	after2 := readFile(t, rc)

	if after1 != after2 {
		t.Errorf("not idempotent.\n--- run1 ---\n%s\n--- run2 ---\n%s", after1, after2)
	}
	if n := countOccurrences(after2, markerV2Start); n != 1 {
		t.Errorf("expected exactly 1 v2 block after two runs, got %d", n)
	}
	if !strings.Contains(after2, "export EDITOR=vim") {
		t.Error("user line lost across runs")
	}
}

// TestMigration_HalfWrittenBlock: a start marker with NO end marker (a crash
// mid-write) must stop at EOF — never delete past it — and user lines BEFORE the
// half block must survive.
func TestMigration_HalfWrittenBlock(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".zshrc")
	orig := strings.Join([]string{
		"export KEEP_ME=1",
		"alias before='echo before'",
		markerV2Start,
		"# partially written block, machine crashed here",
		`export BRAVROS_SECRETS_BACKEND=op`,
		// NO end marker, NO trailing content
	}, "\n")
	os.WriteFile(rc, []byte(orig), 0644)

	if _, err := Bootstrap(BootstrapOpts{ShellRC: rc, Backend: BackendNone}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	got := readFile(t, rc)

	if !strings.Contains(got, "export KEEP_ME=1") {
		t.Error("user line before half-block was deleted")
	}
	if !strings.Contains(got, "alias before='echo before'") {
		t.Error("user alias before half-block was deleted")
	}
	if strings.Contains(got, "machine crashed here") {
		t.Error("orphaned half-block content survived (should be consumed to EOF)")
	}
	if n := countOccurrences(got, markerV2Start); n != 1 {
		t.Errorf("expected exactly 1 v2 block after repair, got %d", n)
	}
}

// TestBackendNone_NoOpReferences: the none block must contain ZERO op refs.
func TestBackendNone_NoOpReferences(t *testing.T) {
	block := RenderBlock(BackendNone)
	for _, forbidden := range []string{"op read", "op://", "op_lazy", "op whoami"} {
		if strings.Contains(block, forbidden) {
			t.Errorf("none backend leaked %q:\n%s", forbidden, block)
		}
	}
	if !strings.Contains(block, "export "+backendEnvVar+"=none") {
		t.Error("none backend missing backend marker")
	}
	// Via the rc path too.
	dir := t.TempDir()
	rc := filepath.Join(dir, ".zshrc")
	Bootstrap(BootstrapOpts{ShellRC: rc, Backend: BackendNone})
	got := readFile(t, rc)
	for _, forbidden := range []string{"op read", "op://", "op_lazy"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("none backend rc leaked %q", forbidden)
		}
	}
}

// TestBackendEnv_resolvesFromFile: under the env backend, bravros_secret resolves
// the value from $BRAVROS_ENV_FILE (verified through Resolve, the same code the
// shell helper calls via `bravros secrets resolve`).
func TestBackendEnv_resolvesFromFile(t *testing.T) {
	orig := opAvailableFn
	opAvailableFn = func() bool { return true } // op "available" — env backend must ignore it
	defer func() { opAvailableFn = orig }()

	envFile := filepath.Join(t.TempDir(), "secrets.env")
	os.WriteFile(envFile, []byte("FIRECRAWL_API_KEY=fc-from-file\n"), 0600)
	t.Setenv("BRAVROS_ENV_FILE", envFile)
	t.Setenv(backendEnvVar, "env")
	t.Setenv("FIRECRAWL_API_KEY", "")

	if got := Resolve("FIRECRAWL_API_KEY", "Firecrawl API", "credencial"); got != "fc-from-file" {
		t.Errorf("env backend should resolve from file, got %q", got)
	}

	// And the rendered env block wires it up with env gate lines, no op refs.
	block := RenderBlock(BackendEnv)
	if strings.Contains(block, "op read") || strings.Contains(block, "op://") {
		t.Errorf("env block leaked op refs:\n%s", block)
	}
	if !strings.Contains(block, "bravros_secret 'FIRECRAWL_API_KEY'") {
		t.Errorf("env block missing bravros_secret call:\n%s", block)
	}
	if !strings.Contains(block, "export "+backendEnvVar+"=env") {
		t.Error("env block missing backend marker")
	}
}

// TestBackendOp_RefsMatchRegistry: the op block must reference the REAL op item
// names (STEP-0 reconciliation), gated behind op whoami.
func TestBackendOp_RefsMatchRegistry(t *testing.T) {
	block := RenderBlock(BackendOp)
	if !strings.Contains(block, "op whoami") {
		t.Error("op block missing op-whoami gate")
	}
	if !strings.Contains(block, "'Firecrawl API'") {
		t.Errorf("op block missing real Firecrawl item name:\n%s", block)
	}
	if !strings.Contains(block, "'credencial'") {
		t.Errorf("op block missing real Firecrawl field 'credencial':\n%s", block)
	}
	if !strings.Contains(block, "'Home Assistant - Long Lived Token'") {
		t.Errorf("op block missing real HA item name:\n%s", block)
	}
	if !strings.Contains(block, "'password'") {
		t.Errorf("op block missing real HA field 'password':\n%s", block)
	}
}

// TestWriteRCAtomic: writes 0644, leaves no .tmp leftover, and on a forced
// failure leaves the original file intact.
func TestWriteRCAtomic(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".zshrc")

	if err := writeRCAtomic(rc, "hello\n"); err != nil {
		t.Fatalf("writeRCAtomic: %v", err)
	}
	fi, err := os.Stat(rc)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0644 {
		t.Errorf("expected 0644, got %o", fi.Mode().Perm())
	}
	// No .tmp leftover.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".bravros-rc-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}

	// Failure path: rename target is a directory → rename fails, original intact.
	os.WriteFile(rc, []byte("ORIGINAL\n"), 0644)
	badPath := filepath.Join(dir, "subdir") // a directory, can't be replaced by a file rename
	os.Mkdir(badPath, 0755)
	if err := writeRCAtomic(badPath, "new content\n"); err == nil {
		t.Error("expected error when rename target is a directory")
	}
	if got := readFile(t, rc); got != "ORIGINAL\n" {
		t.Errorf("original rc mutated on failure path: %q", got)
	}
	// No temp leftover after the failure either.
	entries, _ = os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".bravros-rc-") {
			t.Errorf("leftover temp file after failure: %s", e.Name())
		}
	}
}

// TestRenderedBlock_ShellSyntax renders every backend to a temp file and runs
// BOTH `bash -n` and `zsh -n` on it, then asserts bravros_secret is defined.
func TestRenderedBlock_ShellSyntax(t *testing.T) {
	shells := []string{}
	for _, sh := range []string{"bash", "zsh"} {
		if _, err := exec.LookPath(sh); err == nil {
			shells = append(shells, sh)
		}
	}
	if len(shells) == 0 {
		t.Skip("no bash/zsh available to syntax-check")
	}

	for _, backend := range []Backend{BackendOp, BackendEnv, BackendNone} {
		block := RenderBlock(backend)
		if !strings.Contains(block, "bravros_secret()") {
			t.Errorf("backend %s: bravros_secret not defined", backend)
		}
		f := filepath.Join(t.TempDir(), "block-"+string(backend)+".sh")
		if err := os.WriteFile(f, []byte(block), 0644); err != nil {
			t.Fatal(err)
		}
		for _, sh := range shells {
			cmd := exec.Command(sh, "-n", f)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Errorf("%s -n failed for backend %s: %v\n%s\n--- block ---\n%s", sh, backend, err, out, block)
			}
		}
	}
}

// TestStripManaged_NoMarkers leaves a marker-free file untouched.
func TestStripManaged_NoMarkers(t *testing.T) {
	in := "export A=1\nalias x='y'\n"
	if got := stripManaged(in); got != in {
		t.Errorf("marker-free text mutated: %q", got)
	}
}
