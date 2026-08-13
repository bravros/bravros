package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bravros/bravros/cli/internal/git"
	"github.com/bravros/bravros/cli/internal/stack"
	wtpkg "github.com/bravros/bravros/cli/internal/worktree"
)

// initWTRepo bootstraps a throwaway git repo in a fresh tempdir and returns its
// absolute path with symlinks resolved (so on macOS /var/folders/... matches
// what `git worktree list --porcelain` reports). Suffixed WT to avoid name
// collision with initRepo in context_ext_test.go (same package, different sig).
func initWTRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if abs, err := filepath.EvalSymlinks(dir); err == nil {
		dir = abs
	}
	runCmd(t, dir, "git", "init", "-b", "main")
	runCmd(t, dir, "git", "config", "user.email", "test@test.com")
	runCmd(t, dir, "git", "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test"), 0o644); err != nil {
		t.Fatalf("seed README: %v", err)
	}
	runCmd(t, dir, "git", "add", ".")
	runCmd(t, dir, "git", "commit", "-m", "initial")
	return dir
}

func runCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command(args[0], args[1:]...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("command %v (in %s) failed: %v\n%s", args, dir, err, out)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// withCwd runs fn with cwd set to dir, restoring afterwards. Needed because the
// worktree package resolves primary/repo root relative to the working dir.
func withCwd(t *testing.T, dir string, fn func()) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	defer func() { _ = os.Chdir(orig) }()
	fn()
}

// TestSetupFullFrameworks walks every framework branch that dispatch supports
// and asserts the expected package-manager / asset-link fields land in the
// result JSON. Table-driven — add a row for every new framework supported.
//
// `committed` is the tracked content used to initialise the repo (framework
// markers like composer.json, lockfiles, etc.). `postCommit` simulates
// gitignored build outputs in primary AFTER the initial commit — those are the
// dirs setup-full should symlink, because they don't ship into the new
// worktree via `git worktree add`.
func TestSetupFullFrameworks(t *testing.T) {
	cases := []struct {
		name            string
		committed       func(t *testing.T, dir string) // written + committed pre-test
		postCommit      func(t *testing.T, dir string) // written AFTER commit (gitignored-like)
		wantFramework   string
		wantPackageMgr  string
		wantBuildTarget string // relative path that should be symlinked
	}{
		{
			name: "laravel with npm lockfile",
			committed: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "composer.json"), `{"require":{"laravel/framework":"^11.0"}}`)
				writeFile(t, filepath.Join(dir, "package.json"), `{"name":"app"}`)
				writeFile(t, filepath.Join(dir, "package-lock.json"), `{"lockfileVersion":3}`)
			},
			postCommit: func(t *testing.T, dir string) {
				// Build output in primary — simulates gitignored Vite output.
				writeFile(t, filepath.Join(dir, "public", "build", "manifest.json"), `{}`)
			},
			wantFramework:   "laravel",
			wantPackageMgr:  "npm",
			wantBuildTarget: filepath.Join("public", "build"),
		},
		{
			name: "nextjs with pnpm",
			committed: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "package.json"), `{"name":"app","dependencies":{"next":"^14"}}`)
				writeFile(t, filepath.Join(dir, "pnpm-lock.yaml"), "lockfileVersion: 6.0\n")
			},
			postCommit: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, ".next", "BUILD_ID"), "abc")
			},
			wantFramework:   "nextjs",
			wantPackageMgr:  "pnpm",
			wantBuildTarget: ".next",
		},
		{
			name: "node with bun",
			committed: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "package.json"), `{"name":"app"}`)
				writeFile(t, filepath.Join(dir, "bun.lockb"), "binary")
			},
			postCommit: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "dist", "index.js"), "")
			},
			wantFramework:   "node",
			wantPackageMgr:  "bun",
			wantBuildTarget: "dist",
		},
		{
			name: "node with yarn",
			committed: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "package.json"), `{"name":"app"}`)
				writeFile(t, filepath.Join(dir, "yarn.lock"), "# yarn\n")
			},
			postCommit: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "dist", "bundle.js"), "")
			},
			wantFramework:   "node",
			wantPackageMgr:  "yarn",
			wantBuildTarget: "dist",
		},
		{
			name: "python (uv)",
			committed: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "pyproject.toml"), "[project]\nname = \"x\"\n")
			},
			wantFramework:   "none",
			wantPackageMgr:  "",
			wantBuildTarget: "",
		},
		{
			name: "go module",
			committed: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/x\ngo 1.22\n")
				writeFile(t, filepath.Join(dir, "main.go"), "package main\nfunc main() {}\n")
			},
			wantFramework:   "none",
			wantPackageMgr:  "",
			wantBuildTarget: "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			primary := initWTRepo(t)
			if tc.committed != nil {
				tc.committed(t, primary)
				runCmd(t, primary, "git", "add", ".")
				runCmd(t, primary, "git", "commit", "-m", "seed")
			}
			// Post-commit writes simulate gitignored build output — the bytes
			// exist in primary but never land in the new worktree via git.
			if tc.postCommit != nil {
				tc.postCommit(t, primary)
			}

			withCwd(t, primary, func() {
				branch := "feat/0042-setup-test"
				result, err := wtpkg.SetupFull(branch, wtpkg.Opts{
					From:      primary,
					BaseRef:   "main",
					NoInstall: true, // don't actually install; we're asserting dispatch wiring
				})
				if err != nil {
					t.Fatalf("SetupFull: %v\nresult: %+v", err, result)
				}
				defer func() {
					_, _, _ = git.Run("git", "worktree", "remove", "--force", result.Path)
				}()

				if !result.Created {
					t.Errorf("expected Created=true, got %+v", result)
				}
				if tc.wantFramework != "" && result.Framework != tc.wantFramework {
					t.Errorf("framework: got %q want %q", result.Framework, tc.wantFramework)
				}
				if result.PackageManager != tc.wantPackageMgr {
					t.Errorf("package_manager: got %q want %q", result.PackageManager, tc.wantPackageMgr)
				}
				if tc.wantBuildTarget != "" {
					target := filepath.Join(result.Path, tc.wantBuildTarget)
					info, err := os.Lstat(target)
					if err != nil {
						t.Errorf("expected build target %q to exist: %v", target, err)
					} else if info.Mode()&os.ModeSymlink == 0 {
						t.Errorf("build target %q is not a symlink (mode=%v)", target, info.Mode())
					}
					if !result.BuildLinked {
						t.Errorf("expected BuildLinked=true")
					}
				}
				if !result.Ready {
					t.Error("expected Ready=true")
				}
			})
		})
	}
}

// TestSetupFullIdempotent confirms that running setup-full twice does not
// re-create the worktree nor re-install / re-link what already exists. The
// second run must report Created=false and at least one entry in Skipped.
func TestSetupFullIdempotent(t *testing.T) {
	primary := initWTRepo(t)
	writeFile(t, filepath.Join(primary, "go.mod"), "module example.com/x\ngo 1.22\n")
	runCmd(t, primary, "git", "add", ".")
	runCmd(t, primary, "git", "commit", "-m", "seed")

	withCwd(t, primary, func() {
		branch := "feat/0099-idempotent"
		opts := wtpkg.Opts{From: primary, BaseRef: "main", NoInstall: true, NoBuild: true, NoEnv: true}

		first, err := wtpkg.SetupFull(branch, opts)
		if err != nil {
			t.Fatalf("first SetupFull: %v", err)
		}
		defer func() {
			_, _, _ = git.Run("git", "worktree", "remove", "--force", first.Path)
		}()
		if !first.Created {
			t.Fatalf("expected first run Created=true")
		}

		second, err := wtpkg.SetupFull(branch, opts)
		if err != nil {
			t.Fatalf("second SetupFull: %v", err)
		}
		if second.Created {
			t.Errorf("expected second run Created=false, got true")
		}
		if !contains(second.Skipped, "worktree_create") {
			t.Errorf("expected skipped[worktree_create] on re-run, got %v", second.Skipped)
		}
	})
}

// TestSetupFullWorktreeExistsSymlinkResolution — the `worktreeExists` helper
// resolves symlinks before matching against porcelain output. On macOS where
// $TMPDIR is symlinked to /private/var/..., it must report the worktree as
// present regardless of which variant the caller passes in.
func TestSetupFullWorktreeExistsSymlinkResolution(t *testing.T) {
	primary := initWTRepo(t)

	withCwd(t, primary, func() {
		branch := "feat/0055-symlink"
		first, err := wtpkg.SetupFull(branch, wtpkg.Opts{From: primary, BaseRef: "main", NoInstall: true, NoBuild: true, NoEnv: true})
		if err != nil {
			t.Fatalf("SetupFull: %v", err)
		}
		defer func() {
			_, _, _ = git.Run("git", "worktree", "remove", "--force", first.Path)
		}()

		// Build a sibling path via /var instead of /private/var (macOS symlink).
		alt := first.Path
		if strings.HasPrefix(alt, "/private/") {
			alt = strings.TrimPrefix(alt, "/private")
		} else if strings.HasPrefix(alt, "/var/") {
			alt = "/private" + alt
		}

		if !git.WorktreeExistsAt(alt) && alt != first.Path {
			// Only fail when the path is actually a symlink variant.
			info, serr := os.Lstat(alt)
			if serr == nil && info.Mode()&os.ModeSymlink != 0 {
				t.Errorf("WorktreeExistsAt(%q) should resolve symlinks, returned false", alt)
			}
		}

		// Canonical path must always match.
		if !git.WorktreeExistsAt(first.Path) {
			t.Errorf("WorktreeExistsAt(%q) returned false for canonical path", first.Path)
		}
	})
}

// TestSetupFullEnvCopy verifies the .env copy step actually reads bytes from
// primary and writes a plain file (not a symlink) into the worktree.
//
// We commit package.json (so primary has a detectable stack) but write .env
// AFTER the commit — real projects gitignore .env, so it lives in primary but
// never lands in the worktree via `git worktree add`.
func TestSetupFullEnvCopy(t *testing.T) {
	primary := initWTRepo(t)
	// Ensure primary has a package.json so setup-full recognises a stack.
	writeFile(t, filepath.Join(primary, "package.json"), `{"name":"app"}`)
	runCmd(t, primary, "git", "add", ".")
	runCmd(t, primary, "git", "commit", "-m", "seed env")
	// Post-commit so it mirrors a real gitignored .env.
	writeFile(t, filepath.Join(primary, ".env"), "APP_ENV=local\nAPP_URL=http://primary.test\n")

	withCwd(t, primary, func() {
		result, err := wtpkg.SetupFull("feat/0077-env", wtpkg.Opts{
			From:      primary,
			BaseRef:   "main",
			NoInstall: true,
			NoBuild:   true,
		})
		if err != nil {
			t.Fatalf("SetupFull: %v", err)
		}
		defer func() {
			_, _, _ = git.Run("git", "worktree", "remove", "--force", result.Path)
		}()

		envPath := filepath.Join(result.Path, ".env")
		info, err := os.Lstat(envPath)
		if err != nil {
			t.Fatalf("expected worktree .env to exist: %v", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Errorf(".env must be a copy, not a symlink (mode=%v)", info.Mode())
		}
		data, _ := os.ReadFile(envPath)
		if !strings.Contains(string(data), "APP_ENV=local") {
			t.Errorf(".env content not copied: %q", data)
		}
		if !result.EnvCopied {
			t.Error("expected EnvCopied=true")
		}
	})
}

// TestDetectNodePackageManager exercises the lockfile heuristic in isolation
// (the one bravros is missing and kaisser ships first).
func TestDetectNodePackageManager(t *testing.T) {
	tests := []struct {
		name     string
		files    []string
		expected string
	}{
		{"bun.lockb", []string{"package.json", "bun.lockb"}, "bun"},
		{"bun.lock text", []string{"package.json", "bun.lock"}, "bun"},
		{"pnpm", []string{"package.json", "pnpm-lock.yaml"}, "pnpm"},
		{"yarn", []string{"package.json", "yarn.lock"}, "yarn"},
		{"npm", []string{"package.json", "package-lock.json"}, "npm"},
		{"no lockfile defaults to npm", []string{"package.json"}, "npm"},
		{"no package.json returns empty", []string{}, ""},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tc.files {
				writeFile(t, filepath.Join(dir, f), "")
			}
			got := stack.DetectNodePackageManager(dir)
			if got != tc.expected {
				t.Errorf("DetectNodePackageManager(%v) = %q, want %q", tc.files, got, tc.expected)
			}
		})
	}
}

// TestSetupFullClonesRuntimeDirsFromParent proves the end-to-end wiring for
// behavior 1 (APFS CoW runtime-dir cloning): when the parent worktree already
// has node_modules on disk and --install is not passed, SetupFull must clone
// it instead of shelling out to a package manager (which would require
// network access this test suite must not depend on).
func TestSetupFullClonesRuntimeDirsFromParent(t *testing.T) {
	primary := initWTRepo(t)
	writeFile(t, filepath.Join(primary, "package.json"), `{"name":"app"}`)
	runCmd(t, primary, "git", "add", ".")
	runCmd(t, primary, "git", "commit", "-m", "seed")
	// Simulates an already-installed, gitignored node_modules in the parent.
	writeFile(t, filepath.Join(primary, "node_modules", "leftpad", "index.js"), "module.exports = {}")

	withCwd(t, primary, func() {
		result, err := wtpkg.SetupFull("feat/0100-clone", wtpkg.Opts{
			From:    primary,
			BaseRef: "main",
			NoBuild: true,
			NoEnv:   true,
		})
		if err != nil {
			t.Fatalf("SetupFull: %v\nresult: %+v", err, result)
		}
		defer func() {
			_, _, _ = git.Run("git", "worktree", "remove", "--force", result.Path)
		}()

		if !contains(result.Cloned, "node_modules") {
			t.Errorf("expected Cloned to contain node_modules, got %v", result.Cloned)
		}
		if len(result.DepsInstalled) != 0 {
			t.Errorf("expected no real install to run when cloning succeeded, got DepsInstalled=%v", result.DepsInstalled)
		}
		if _, err := os.Stat(filepath.Join(result.Path, "node_modules", "leftpad", "index.js")); err != nil {
			t.Errorf("expected cloned node_modules content in worktree: %v", err)
		}
	})
}

// TestSetupFullInstallFlagSkipsCloning proves --install (Opts.Install) routes
// past the cloning step even when the parent has the runtime dir on disk.
func TestSetupFullInstallFlagSkipsCloning(t *testing.T) {
	primary := initWTRepo(t)
	writeFile(t, filepath.Join(primary, "package.json"), `{"name":"app"}`)
	runCmd(t, primary, "git", "add", ".")
	runCmd(t, primary, "git", "commit", "-m", "seed")
	writeFile(t, filepath.Join(primary, "node_modules", "leftpad", "index.js"), "module.exports = {}")

	withCwd(t, primary, func() {
		result, err := wtpkg.SetupFull("feat/0101-install-flag", wtpkg.Opts{
			From:    primary,
			BaseRef: "main",
			NoBuild: true,
			NoEnv:   true,
			Install: true,
		})
		if err != nil {
			t.Fatalf("SetupFull: %v\nresult: %+v", err, result)
		}
		defer func() {
			_, _, _ = git.Run("git", "worktree", "remove", "--force", result.Path)
		}()

		if len(result.Cloned) != 0 {
			t.Errorf("expected Cloned to be empty with --install, got %v", result.Cloned)
		}
	})
}

// TestSetupFullLockfileDrift proves behavior 2 (lockfile drift check) fires
// when the new worktree's base ref carries a different package.json than the
// primary worktree's own HEAD. The drift check compares the worktree against
// `primary`'s tracked HEAD — so branching off an older ref while primary has
// since moved forward is exactly the leak this check exists to catch.
func TestSetupFullLockfileDrift(t *testing.T) {
	primary := initWTRepo(t)
	writeFile(t, filepath.Join(primary, "package.json"), `{"name":"app","version":"1.0.0"}`)
	runCmd(t, primary, "git", "add", ".")
	runCmd(t, primary, "git", "commit", "-m", "v1")
	runCmd(t, primary, "git", "branch", "feature-old")

	// primary's HEAD moves forward with a different package.json.
	writeFile(t, filepath.Join(primary, "package.json"), `{"name":"app","version":"2.0.0"}`)
	runCmd(t, primary, "git", "add", ".")
	runCmd(t, primary, "git", "commit", "-m", "v2")

	withCwd(t, primary, func() {
		result, err := wtpkg.SetupFull("feat/0102-drift", wtpkg.Opts{
			From:      primary,
			BaseRef:   "feature-old",
			NoInstall: true,
			NoBuild:   true,
			NoEnv:     true,
		})
		if err != nil {
			t.Fatalf("SetupFull: %v\nresult: %+v", err, result)
		}
		defer func() {
			_, _, _ = git.Run("git", "worktree", "remove", "--force", result.Path)
		}()

		found := false
		for _, w := range result.Warnings {
			if strings.Contains(w, "package.json") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected a package.json drift warning, got Warnings=%v", result.Warnings)
		}
	})
}

func contains(slice []string, target string) bool {
	for _, s := range slice {
		if s == target {
			return true
		}
	}
	return false
}
