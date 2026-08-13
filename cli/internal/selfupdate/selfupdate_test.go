package selfupdate_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bravros/bravros/cli/internal/selfupdate"
)

func TestIsBehindOriginMain_NonGitDir(t *testing.T) {
	tmpDir := t.TempDir()
	// Non-git directory — should return false gracefully (fail-safe)
	result := selfupdate.IsBehindOriginMain(tmpDir)
	if result {
		t.Error("IsBehindOriginMain should return false for a non-git directory")
	}
}

func TestUpdatePortableRepo_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	// Non-git dir in dry-run should succeed without touching anything
	if err := selfupdate.UpdatePortableRepo(tmpDir, true); err != nil {
		t.Fatalf("UpdatePortableRepo dry-run returned error: %v", err)
	}
}

func TestUpdatePortableRepo_NonGitDir(t *testing.T) {
	tmpDir := t.TempDir()
	// Non-git dir should fail with a meaningful error (not panic)
	err := selfupdate.UpdatePortableRepo(tmpDir, false)
	if err == nil {
		t.Error("expected error for non-git directory, got nil")
	}
	if !strings.Contains(err.Error(), "selfupdate:") {
		t.Errorf("error should be prefixed with 'selfupdate:': %v", err)
	}
}

func TestUpdatePortableRepo_UntrackedFilesNotBlocked(t *testing.T) {
	// Untracked files must NOT block UpdatePortableRepo — `git checkout origin/main -- .`
	// never touches untracked files, so there is no risk of data loss.
	// Regression: the original porcelain check included "??" lines, which caused the
	// real portable repo (always has some untracked files) to refuse every selfupdate.
	tmpDir := t.TempDir()

	mustGit := func(args ...string) {
		t.Helper()
		allArgs := append([]string{"-C", tmpDir}, args...)
		cmd := exec.Command("git", allArgs...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	mustGit("init")
	mustGit("config", "user.email", "test@test.com")
	mustGit("config", "user.name", "test")

	// Commit a tracked file so the repo has a valid HEAD
	tracked := filepath.Join(tmpDir, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGit("add", "tracked.txt")
	mustGit("commit", "-m", "initial commit")

	// Add an untracked file (no git add)
	untracked := filepath.Join(tmpDir, "untracked.log")
	if err := os.WriteFile(untracked, []byte("log output\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// UpdatePortableRepo should NOT error due to the untracked file.
	// (It will still fail because there is no origin/main remote, but the error must
	// come from the checkout step, not from the dirty-tree guard.)
	err := selfupdate.UpdatePortableRepo(tmpDir, false)
	if err != nil && strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("untracked files must not block UpdatePortableRepo, got: %v", err)
	}
	// Any other error (no remote, etc.) is acceptable — the guard itself passed.
}

func TestUpdatePortableRepo_DirtyTree(t *testing.T) {
	// Create a real git repo with a tracked file that has been modified without committing.
	// UpdatePortableRepo must refuse with a "uncommitted changes" error.
	tmpDir := t.TempDir()

	mustGit := func(args ...string) {
		t.Helper()
		allArgs := append([]string{"-C", tmpDir}, args...)
		cmd := exec.Command("git", allArgs...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	mustGit("init")
	mustGit("config", "user.email", "test@test.com")
	mustGit("config", "user.name", "test")

	// Add and commit a tracked file
	trackedFile := filepath.Join(tmpDir, "tracked.txt")
	if err := os.WriteFile(trackedFile, []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGit("add", "tracked.txt")
	mustGit("commit", "-m", "initial commit")

	// Now modify the tracked file without committing → dirty working tree
	if err := os.WriteFile(trackedFile, []byte("modified\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err := selfupdate.UpdatePortableRepo(tmpDir, false)
	if err == nil {
		t.Fatal("expected error for dirty working tree, got nil")
	}
	if !strings.Contains(err.Error(), "uncommitted changes") {
		t.Errorf("expected 'uncommitted changes' in error, got: %v", err)
	}
}

// TestIsBehindOriginMain_DivergedReturnsFalse pins the regression where homolog had commits
// ahead of main AND main had new commits — the original implementation returned true and
// `UpdatePortableRepo` then clobbered homolog's tracked files with main's content (resurrecting
// already-archived plan files, reverting in-flight edits). The fix: HEAD must be an ancestor
// of origin/main for "behind" to hold; diverged HEADs return false.
func TestIsBehindOriginMain_DivergedReturnsFalse(t *testing.T) {
	tmpDir := t.TempDir()

	mustGit := func(args ...string) {
		t.Helper()
		allArgs := append([]string{"-C", tmpDir}, args...)
		cmd := exec.Command("git", allArgs...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	mustGit("init", "-b", "main")
	mustGit("config", "user.email", "test@test.com")
	mustGit("config", "user.name", "test")

	// Initial common ancestor commit on main.
	seed := filepath.Join(tmpDir, "seed.txt")
	if err := os.WriteFile(seed, []byte("seed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGit("add", "seed.txt")
	mustGit("commit", "-m", "seed")

	// Branch homolog off main and add a homolog-only commit (homolog is ahead of main).
	mustGit("checkout", "-b", "homolog")
	homologOnly := filepath.Join(tmpDir, "homolog.txt")
	if err := os.WriteFile(homologOnly, []byte("homolog\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGit("add", "homolog.txt")
	mustGit("commit", "-m", "homolog only")

	// Add a main-only commit so origin/main also moves forward → diverged from homolog.
	mustGit("checkout", "main")
	mainOnly := filepath.Join(tmpDir, "main.txt")
	if err := os.WriteFile(mainOnly, []byte("main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGit("add", "main.txt")
	mustGit("commit", "-m", "main only")

	// Synthesize an origin/main ref that points at main's tip (no real remote needed).
	mustGit("update-ref", "refs/remotes/origin/main", "main")

	// Switch back to homolog and verify IsBehindOriginMain returns false despite divergence.
	mustGit("checkout", "homolog")

	if selfupdate.IsBehindOriginMain(tmpDir) {
		t.Fatal("IsBehindOriginMain returned true for diverged HEAD — would clobber homolog commits via checkout overlay")
	}
}

// TestIsBehindOriginMain_StrictlyBehindReturnsTrue pins the happy path: HEAD is an ancestor
// of origin/main (linear-history catch-up), so the function correctly reports "behind".
func TestIsBehindOriginMain_StrictlyBehindReturnsTrue(t *testing.T) {
	tmpDir := t.TempDir()

	mustGit := func(args ...string) {
		t.Helper()
		allArgs := append([]string{"-C", tmpDir}, args...)
		cmd := exec.Command("git", allArgs...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	mustGit("init", "-b", "main")
	mustGit("config", "user.email", "test@test.com")
	mustGit("config", "user.name", "test")

	// First commit (HEAD will sit here).
	first := filepath.Join(tmpDir, "first.txt")
	if err := os.WriteFile(first, []byte("first\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGit("add", "first.txt")
	mustGit("commit", "-m", "first")
	headSha, err := exec.Command("git", "-C", tmpDir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}

	// Second commit moves main forward (HEAD will be reset back to first below).
	second := filepath.Join(tmpDir, "second.txt")
	if err := os.WriteFile(second, []byte("second\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGit("add", "second.txt")
	mustGit("commit", "-m", "second")
	mustGit("update-ref", "refs/remotes/origin/main", "main")

	// Reset main back to the first commit so HEAD is strictly an ancestor of origin/main.
	mustGit("reset", "--hard", strings.TrimSpace(string(headSha)))

	if !selfupdate.IsBehindOriginMain(tmpDir) {
		t.Fatal("IsBehindOriginMain returned false for strictly-behind HEAD — should report behind")
	}
}

func TestHasOriginMainUpdates_DivergedReturnsTrue(t *testing.T) {
	tmpDir := t.TempDir()

	mustGit := func(args ...string) {
		t.Helper()
		allArgs := append([]string{"-C", tmpDir}, args...)
		cmd := exec.Command("git", allArgs...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	mustGit("init", "-b", "main")
	mustGit("config", "user.email", "test@test.com")
	mustGit("config", "user.name", "test")

	seed := filepath.Join(tmpDir, "seed.txt")
	if err := os.WriteFile(seed, []byte("seed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGit("add", "seed.txt")
	mustGit("commit", "-m", "seed")

	mustGit("checkout", "-b", "homolog")
	homologOnly := filepath.Join(tmpDir, "homolog.txt")
	if err := os.WriteFile(homologOnly, []byte("homolog\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGit("add", "homolog.txt")
	mustGit("commit", "-m", "homolog only")

	mustGit("checkout", "main")
	mainOnly := filepath.Join(tmpDir, "main.txt")
	if err := os.WriteFile(mainOnly, []byte("main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGit("add", "main.txt")
	mustGit("commit", "-m", "main only")
	mustGit("update-ref", "refs/remotes/origin/main", "main")

	mustGit("checkout", "homolog")
	if !selfupdate.HasOriginMainUpdates(tmpDir) {
		t.Fatal("HasOriginMainUpdates returned false for diverged HEAD — skill-only main updates would be missed on homolog")
	}
}

func TestHasOriginMainUpdates_AheadOnlyReturnsFalse(t *testing.T) {
	tmpDir := t.TempDir()

	mustGit := func(args ...string) {
		t.Helper()
		allArgs := append([]string{"-C", tmpDir}, args...)
		cmd := exec.Command("git", allArgs...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	mustGit("init", "-b", "main")
	mustGit("config", "user.email", "test@test.com")
	mustGit("config", "user.name", "test")

	seed := filepath.Join(tmpDir, "seed.txt")
	if err := os.WriteFile(seed, []byte("seed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGit("add", "seed.txt")
	mustGit("commit", "-m", "seed")
	mustGit("update-ref", "refs/remotes/origin/main", "main")

	aheadOnly := filepath.Join(tmpDir, "ahead.txt")
	if err := os.WriteFile(aheadOnly, []byte("ahead\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGit("add", "ahead.txt")
	mustGit("commit", "-m", "ahead only")

	if selfupdate.HasOriginMainUpdates(tmpDir) {
		t.Fatal("HasOriginMainUpdates returned true when HEAD already contains origin/main")
	}
}

func TestUpdatePortableRepo_AllowsDirtyDisposableConfig(t *testing.T) {
	for _, name := range []string{".kaisser.yml", ".skaisser.yml"} {
		t.Run(name, func(t *testing.T) {
			tmpDir := t.TempDir()

			mustGit := func(args ...string) {
				t.Helper()
				allArgs := append([]string{"-C", tmpDir}, args...)
				cmd := exec.Command("git", allArgs...)
				cmd.Env = append(os.Environ(),
					"GIT_AUTHOR_NAME=test",
					"GIT_AUTHOR_EMAIL=test@test.com",
					"GIT_COMMITTER_NAME=test",
					"GIT_COMMITTER_EMAIL=test@test.com",
				)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
			}

			mustGit("init", "-b", "main")
			mustGit("config", "user.email", "test@test.com")
			mustGit("config", "user.name", "test")

			cfg := filepath.Join(tmpDir, name)
			if err := os.WriteFile(cfg, []byte("staging_branch: homolog\n"), 0644); err != nil {
				t.Fatal(err)
			}
			mustGit("add", name)
			mustGit("commit", "-m", "initial config")
			mustGit("update-ref", "refs/remotes/origin/main", "main")

			if err := os.WriteFile(cfg, []byte("staging_branch: homolog\nlanguage: auto\n"), 0644); err != nil {
				t.Fatal(err)
			}

			err := selfupdate.UpdatePortableRepo(tmpDir, false)
			if err != nil && strings.Contains(err.Error(), "uncommitted changes") {
				t.Fatalf("dirty %s must not block selfupdate, got: %v", name, err)
			}
		})
	}
}

func TestUpdatePortableRepo_BlocksOtherTrackedDirtyFiles(t *testing.T) {
	tmpDir := t.TempDir()

	mustGit := func(args ...string) {
		t.Helper()
		allArgs := append([]string{"-C", tmpDir}, args...)
		cmd := exec.Command("git", allArgs...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	mustGit("init", "-b", "main")
	mustGit("config", "user.email", "test@test.com")
	mustGit("config", "user.name", "test")

	tracked := filepath.Join(tmpDir, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGit("add", "tracked.txt")
	mustGit("commit", "-m", "initial tracked file")
	mustGit("update-ref", "refs/remotes/origin/main", "main")

	if err := os.WriteFile(tracked, []byte("local edit\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err := selfupdate.UpdatePortableRepo(tmpDir, false)
	if err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("non-disposable tracked file must still block selfupdate, got: %v", err)
	}
}

// TestUpdatePortableRepo_BlocksRenameFromTrackedToDisposableConfig pins the regression where
// a staged rename from a non-disposable tracked file to .kaisser.yml was treated as disposable
// dirt because isDisposablePortableConfigDirty only checked the destination path. That allowed
// UpdatePortableRepo to proceed and discard the original tracked-file change.
func TestUpdatePortableRepo_BlocksRenameFromTrackedToDisposableConfig(t *testing.T) {
	for _, dstName := range []string{".kaisser.yml", ".skaisser.yml"} {
		t.Run(dstName, func(t *testing.T) {
			tmpDir := t.TempDir()

			mustGit := func(args ...string) {
				t.Helper()
				allArgs := append([]string{"-C", tmpDir}, args...)
				cmd := exec.Command("git", allArgs...)
				cmd.Env = append(os.Environ(),
					"GIT_AUTHOR_NAME=test",
					"GIT_AUTHOR_EMAIL=test@test.com",
					"GIT_COMMITTER_NAME=test",
					"GIT_COMMITTER_EMAIL=test@test.com",
				)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
			}

			mustGit("init", "-b", "main")
			mustGit("config", "user.email", "test@test.com")
			mustGit("config", "user.name", "test")

			// Commit a tracked file that we will later rename into a disposable config name.
			tracked := filepath.Join(tmpDir, "tracked.txt")
			if err := os.WriteFile(tracked, []byte("original\n"), 0644); err != nil {
				t.Fatal(err)
			}
			mustGit("add", "tracked.txt")
			mustGit("commit", "-m", "initial tracked file")
			mustGit("update-ref", "refs/remotes/origin/main", "main")

			// Stage a rename from tracked.txt -> <disposable config>
			dst := filepath.Join(tmpDir, dstName)
			if err := os.Rename(tracked, dst); err != nil {
				t.Fatal(err)
			}
			mustGit("add", "-A")

			err := selfupdate.UpdatePortableRepo(tmpDir, false)
			if err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
				t.Fatalf("rename from tracked file to %s must block selfupdate, got: %v", dstName, err)
			}
		})
	}
}

// TestUpdatePortableRepo_AllowsRenameBetweenDisposableConfigs verifies that a rename
// between the two disposable config files (.kaisser.yml <-> .skaisser.yml) is still
// treated as disposable dirt and does not block the update.
func TestUpdatePortableRepo_AllowsRenameBetweenDisposableConfigs(t *testing.T) {
	tmpDir := t.TempDir()

	mustGit := func(args ...string) {
		t.Helper()
		allArgs := append([]string{"-C", tmpDir}, args...)
		cmd := exec.Command("git", allArgs...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	mustGit("init", "-b", "main")
	mustGit("config", "user.email", "test@test.com")
	mustGit("config", "user.name", "test")

	cfg := filepath.Join(tmpDir, ".kaisser.yml")
	if err := os.WriteFile(cfg, []byte("staging_branch: homolog\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGit("add", ".kaisser.yml")
	mustGit("commit", "-m", "initial config")
	mustGit("update-ref", "refs/remotes/origin/main", "main")

	// Rename .kaisser.yml -> .skaisser.yml and stage it.
	newCfg := filepath.Join(tmpDir, ".skaisser.yml")
	if err := os.Rename(cfg, newCfg); err != nil {
		t.Fatal(err)
	}
	mustGit("add", "-A")

	// UpdatePortableRepo should NOT error due to the rename between disposable configs.
	err := selfupdate.UpdatePortableRepo(tmpDir, false)
	if err != nil && strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("rename between disposable configs must not block selfupdate, got: %v", err)
	}
}
