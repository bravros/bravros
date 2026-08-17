package cmd

// update.go — `bravros update`, the NETWORK half of the split update model
// (P-0015 D2). It resolves the newest published release, downloads the platform
// archive, verifies it against the minisign-signed checksums.txt, and replaces
// the running executable.
//
// WHAT CHANGED. `update` used to be a mere ALIAS for `selfupdate`
// (cmd/selfupdate.go:32 before this phase), whose only extra behavior was
// bypassing the 6h TTL cache — "a manual run must always really check". Both
// halves of that survive, split across the two verbs: `selfupdate --force`
// still bypasses the cache, and `update` still always really checks, because
// asking for it IS the check. What `update` no longer does is deploy skills
// from a fetched payload; skills now ride inside the binary, so a successful
// swap is followed by running the NEW binary's `selfupdate --force`, which
// refreshes components from the new embed under Phase 3's non-destructive rules.
//
// WHY THE SPLIT. Replacing a binary is the one genuinely risky thing this tool
// does to a machine, and it was happening unattended, from a SessionStart hook.
// D2's rule: the risk-free half stays automatic, the risky half becomes explicit.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bravros/bravros/cli/internal/config"
	"github.com/bravros/bravros/cli/internal/fetch"
	"github.com/bravros/bravros/cli/internal/selfupdate"
	"github.com/spf13/cobra"
)

var (
	updateCheckOnly bool
	updateForce     bool
	updateTag       string
)

// updateTimeout bounds the whole network half. Generous compared to the passive
// notice's 3s because this one is an explicit, foregrounded operator action.
const updateTimeout = 120 * time.Second

// updateInstallMethodOverrideEnv lets an operator whose install method is
// misrecorded (or who genuinely wants bravros to own a binary a package manager
// placed) proceed past the refusal. Env rather than flag, deliberately: the
// safe path must be the short one.
const updateInstallMethodOverrideEnv = "BRAVROS_INSTALL_METHOD"

// Test seams. Production leaves all three nil/empty.
var (
	// updateResolverOverride replaces the real tag resolver.
	updateResolverOverride selfupdate.TagResolver
	// updateInstallerOverride replaces the real Updater. The seam is an
	// interface so cmd-level tests can assert the ORCHESTRATION (refusals,
	// version gate, post-swap refresh) without rebuilding a signed release;
	// the trust chain itself is exercised end to end against a real archive
	// and a real signature in internal/selfupdate/binary_test.go.
	updateInstallerOverride updateInstaller
	// updateExePathOverride replaces os.Executable(), so a test never risks
	// replacing the binary running the test.
	updateExePathOverride string
	// updateRefreshHook replaces "exec the new binary's selfupdate --force".
	updateRefreshHook func(exePath string) error
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Download, verify and install the newest bravros binary",
	Long: `Resolves the newest published release, downloads the archive for this platform,
verifies it against the minisign-signed checksums.txt, and atomically replaces the
running bravros executable. Then it runs the new binary's 'selfupdate --force' so
the components under ~/.claude are refreshed from the payload embedded in it.

This is the explicit half of the split update model: 'bravros selfupdate' (the
SessionStart hook) never touches the network and never replaces a binary.

It refuses when the install method recorded in <config dir>/state/setup.json says
a package manager owns the binary — replacing a brew- or scoop-managed file would
leave that package manager's metadata pointing at something it no longer controls,
and its next upgrade would silently roll the user back. The right command is named
in the refusal.

  bravros update            # check, and install if a newer release exists
  bravros update --check    # report only, replace nothing
  bravros update --force    # reinstall even when already on the newest version
  bravros update --tag v3.2.0`,
	Args: cobra.NoArgs,
	// A refusal or a failed download is not a usage error; dumping the flag
	// list under the message buries the one line that tells the operator what
	// to run instead.
	SilenceUsage: true,
	RunE:         runUpdate,
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()
	root := config.ConfigDir()

	exePath, err := updateExecutablePath()
	if err != nil {
		return err
	}

	guard := updateOwnershipGuard(root, exePath)
	if guard.refuse {
		fmt.Fprintln(out, selfupdate.RefusalMessage(guard.method, guard.upgradeCmd))
		fmt.Fprintf(out, "(install method %s; set %s=installer to override)\n", guard.source, updateInstallMethodOverrideEnv)
		return fmt.Errorf("refusing to replace a %s-managed binary", strings.ToLower(guard.method))
	}
	for _, line := range guard.notes {
		fmt.Fprintln(out, line)
	}

	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()

	tag := updateTag
	if tag == "" {
		tag, err = updateResolveTag(ctx)
		if err != nil {
			return fmt.Errorf("resolve latest release: %w", err)
		}
	}

	current := Version
	newer := selfupdate.IsNewer(current, tag)

	// Record the check so the passive notice does not immediately repeat it.
	_ = selfupdate.SaveNoticeState(selfupdate.NoticeStatePath(), selfupdate.NoticeState{
		LastCheck: time.Now(),
		LatestTag: tag,
	})

	switch {
	case updateCheckOnly:
		if newer {
			fmt.Fprintf(out, "%s is available (you have v%s) — run `bravros update`\n", tag, strings.TrimPrefix(current, "v"))
		} else {
			fmt.Fprintf(out, "bravros v%s is up to date (latest published: %s)\n", strings.TrimPrefix(current, "v"), tag)
		}
		return nil
	case !newer && !updateForce:
		fmt.Fprintf(out, "bravros v%s is already up to date (latest published: %s)\n", strings.TrimPrefix(current, "v"), tag)
		return nil
	}

	updater := updateUpdater()
	fmt.Fprintf(out, "Downloading %s from %s…\n", updater.AssetName(), tag)

	res, err := updater.Install(ctx, tag, exePath)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "✓ installed %s at %s\n", tag, res.ExePath)
	fmt.Fprintf(out, "  sha256 %s (verified against minisign-signed %s)\n", res.SHA256, selfupdate.ChecksumsAsset)
	if res.BackupPath != "" {
		// Windows: the running image still holds its own file open. The
		// replacement is already in place; the leftover is swept next run.
		fmt.Fprintf(out, "  previous binary kept at %s — it is removed on the next run\n", res.BackupPath)
	}

	// A new binary carries a new embedded payload, and the TTL marker would
	// otherwise suppress the refresh for up to 6h. Drop it, then refresh now.
	updateDropCheckMarker()

	if err := updateRefreshComponents(res.ExePath); err != nil {
		fmt.Fprintf(out, "  ⚠️  component refresh failed: %v\n", err)
		fmt.Fprintln(out, "     the new binary is installed; run `bravros selfupdate --force` to retry")
		return nil
	}
	fmt.Fprintln(out, "  components refreshed from the new embedded payload")
	return nil
}

// updateExecutablePath resolves the binary this process is running from,
// following symlinks so a swap replaces the real file rather than a link.
func updateExecutablePath() (string, error) {
	if updateExePathOverride != "" {
		return updateExePathOverride, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate the running bravros binary: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved, nil
	}
	return exe, nil
}

// updateOwnership is the verdict on "may bravros replace the file at exePath?".
type updateOwnership struct {
	refuse     bool
	method     string
	source     string
	upgradeCmd string
	notes      []string
}

// updateOwnershipGuard decides whether the binary about to be replaced belongs
// to a package manager.
//
// OBSERVED REALITY WINS; state.json is the fallback. The only question that
// matters is who owns the INODE at exePath — that is the file being replaced —
// and exePath has already been through filepath.EvalSymlinks
// (updateExecutablePath). Resolving first is load-bearing, not hygiene:
// Apple-Silicon brew lives at /opt/homebrew/bin/bravros, whose raw path happens
// to contain "/homebrew/", but INTEL-mac brew is /usr/local/bin/bravros — a
// symlink into ../Cellar/. Only the resolved path carries "/Cellar/", so a
// raw-path check misses Intel brew entirely.
//
// Precedence:
//  1. $BRAVROS_INSTALL_METHOD — the explicit operator/installer override.
//  2. Detection from the resolved path. brew/scoop here ⇒ REFUSE, whatever the
//     record says.
//  3. state.json's record. brew/scoop here while detection says otherwise ⇒
//     PROCEED with one informational line: the running binary is demonstrably
//     not the package manager's copy, so replacing it damages nothing.
//
// A missing record is never grounds to refuse — detection alone decides — and a
// `source` build (a local `go build` path) proceeds with a line naming what is
// about to be replaced, because refusing there would break the dev loop.
func updateOwnershipGuard(root, exePath string) updateOwnership {
	if v := strings.TrimSpace(os.Getenv(updateInstallMethodOverrideEnv)); v != "" {
		return updateOwnership{
			refuse:     selfupdate.PackageManagerCommand(v) != "",
			method:     v,
			source:     "from $" + updateInstallMethodOverrideEnv,
			upgradeCmd: selfupdate.PackageManagerCommand(v),
		}
	}

	observed := detectInstallMethod(exePath, root)
	if cmd := selfupdate.PackageManagerCommand(observed); cmd != "" {
		return updateOwnership{
			refuse:     true,
			method:     observed,
			source:     "detected from the resolved binary path " + exePath,
			upgradeCmd: cmd,
		}
	}

	out := updateOwnership{method: observed, source: "detected from the resolved binary path " + exePath}

	if st, err := readSetupState(root); err == nil && st != nil {
		recorded := strings.TrimSpace(st.InstallMethod)
		if recorded != "" && selfupdate.PackageManagerCommand(recorded) != "" {
			out.notes = append(out.notes, fmt.Sprintf(
				"note: %s records install_method %q, but %s is not a %s-managed file — replacing it leaves that install alone.",
				setupStatePath(root), recorded, exePath, strings.ToLower(recorded)))
		}
	}
	if observed == "source" {
		out.notes = append(out.notes, "note: this looks like a locally built binary — replacing "+exePath+" with the published release.")
	}
	return out
}

func updateResolveTag(ctx context.Context) (string, error) {
	resolver := updateResolverOverride
	if resolver == nil {
		resolver = fetch.NewClient()
	}
	tag, err := resolver.ResolveLatestTag(ctx)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(tag) == "" {
		return "", errors.New("empty tag in release redirect")
	}
	return tag, nil
}

// updateInstaller is the consumer-side view of selfupdate.Updater.
type updateInstaller interface {
	AssetName() string
	Install(ctx context.Context, tag, exePath string) (selfupdate.UpdateResult, error)
}

func updateUpdater() updateInstaller {
	if updateInstallerOverride != nil {
		return updateInstallerOverride
	}
	return &selfupdate.Updater{}
}

// updateDropCheckMarker removes the TTL marker so the next SessionStart run
// refreshes against the new embed instead of cache-hitting.
func updateDropCheckMarker() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	_ = os.Remove(filepath.Join(legacyClaudeStateDir(home), selfupdateCheckMarker))
}

// updateRefreshComponents runs the NEW binary's refresh. It has to be the new
// binary: the payload is embedded, so the process that is still running carries
// the OLD skills and templates and could only ever redeploy those.
func updateRefreshComponents(exePath string) error {
	if updateRefreshHook != nil {
		return updateRefreshHook(exePath)
	}
	cmd := exec.Command(exePath, "selfupdate", "--force")
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		trimmed := strings.TrimSpace(string(out))
		if trimmed != "" {
			return fmt.Errorf("%w: %s", err, trimmed)
		}
		return err
	}
	return nil
}

func init() {
	updateCmd.Flags().BoolVar(&updateCheckOnly, "check", false, "report whether a newer release exists; replace nothing")
	updateCmd.Flags().BoolVar(&updateForce, "force", false, "install even when the running version is already the newest")
	updateCmd.Flags().StringVar(&updateTag, "tag", "", "install a specific release tag instead of the newest")
	rootCmd.AddCommand(updateCmd)
}
