package cmd

// merge_lock.go — `kaisser merge-lock` verb (B-0259 Phase 1).
//
// Surfaces the merge-lock primitive in cli/internal/git/mergelock.go through a
// stable CLI shape so skills can:
//
//	kaisser merge-lock acquire [--timeout 60s] [--ttl 10m] [--meta k=v ...] [--hold]
//	kaisser merge-lock release
//	kaisser merge-lock status [--json]
//
// The actual flock mechanism lives in mergelock.go (untouched). The TTL +
// metadata + stale-clear + JSON state semantics are layered on top via the
// helpers in mergelock_state.go.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	gitpkg "github.com/bravros/bravros/cli/internal/git"
	"github.com/spf13/cobra"
)

var (
	mergeLockAcquireTimeout time.Duration
	mergeLockAcquireTTL     time.Duration
	mergeLockAcquireMeta    []string
	mergeLockAcquireHold    bool

	mergeLockStatusJSON bool

	// mergeLockHeartbeatInterval is the interval between heartbeat touches
	// when `--hold` is set. Package-level so tests can override it to a
	// short value without touching production defaults.
	mergeLockHeartbeatInterval = 8 * time.Minute
)

var mergeLockCmd = &cobra.Command{
	Use:   "merge-lock",
	Short: "Atomic merge-lock primitive (acquire / release / status)",
	Long: `Coordinates exclusive merge windows across parallel skill invocations.

  acquire   write a TTL-bounded JSON lock state and exit 0 (or block with --hold)
  release   remove the lockfile (idempotent)
  status    report whether a non-stale lock state is on disk

The lockfile lives at <gitRoot>/.git/kaisser-merge.lock and is held by virtue
of the JSON state's TTL + holder PID being alive. flock atomizes the acquire
write window against concurrent acquirers.`,
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

var mergeLockAcquireCmd = &cobra.Command{
	Use:   "acquire",
	Short: "Acquire the merge lock (writes TTL-bounded JSON state)",
	RunE: func(cmd *cobra.Command, args []string) error {
		meta, err := parseMergeLockMeta(mergeLockAcquireMeta)
		if err != nil {
			return err
		}
		state := &gitpkg.LockState{
			PID:        os.Getpid(),
			AcquiredAt: time.Now().UTC(),
			TTLSeconds: int(mergeLockAcquireTTL.Seconds()),
			Meta:       meta,
		}
		acqErr := gitpkg.AcquireWithLockState(state, mergeLockAcquireTimeout, gitpkg.DefaultStaleGrace)
		if acqErr != nil {
			// Distinguish active-state vs flock-timeout for the operator.
			if errors.Is(acqErr, gitpkg.ErrLockHeldByActiveState) {
				path, _ := gitpkg.MergeLockFilePath()
				fmt.Fprintf(os.Stderr, "merge-lock: another merge is in progress (lock state at %s is still within TTL)\n", path)
				os.Exit(1)
			}
			if gitpkg.IsMergeLockTimeout(acqErr) {
				fmt.Fprintf(os.Stderr, "merge-lock: %s\n", acqErr.Error())
				os.Exit(1)
			}
			return acqErr
		}
		path, _ := gitpkg.MergeLockFilePath()
		fmt.Fprintf(cmd.OutOrStdout(), "merge-lock: acquired (pid=%d, ttl=%s, path=%s)\n",
			state.PID, mergeLockAcquireTTL, path)
		if mergeLockAcquireHold {
			return holdMergeLockUntilSignal(path)
		}
		return nil
	},
}

var mergeLockReleaseCmd = &cobra.Command{
	Use:   "release",
	Short: "Release the merge lock (idempotent)",
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := gitpkg.MergeLockFilePath()
		if err != nil {
			return err
		}
		removeErr := os.Remove(path)
		if removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("merge-lock: release: %w", removeErr)
		}
		if removeErr == nil {
			fmt.Fprintf(cmd.OutOrStdout(), "merge-lock: released (%s)\n", path)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "merge-lock: not held (no lockfile at %s)\n", path)
		}
		return nil
	},
}

// statusOutput is the shape returned by `merge-lock status --json`.
type statusOutput struct {
	Held                bool              `json:"held"`
	PID                 int               `json:"pid,omitempty"`
	AgeSeconds          int               `json:"age_seconds,omitempty"`
	TTLRemainingSeconds int               `json:"ttl_remaining_seconds,omitempty"`
	Meta                map[string]string `json:"meta,omitempty"`
	Path                string            `json:"path,omitempty"`
}

var mergeLockStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Report whether the merge lock is held",
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := gitpkg.MergeLockFilePath()
		if err != nil {
			return err
		}
		state, mtime, ok, err := gitpkg.ReadLockState(path)
		if err != nil {
			return err
		}
		out := statusOutput{Path: path}
		if ok && !gitpkg.IsLockStateStale(state, mtime, gitpkg.DefaultStaleGrace) {
			anchor := state.AcquiredAt
			if mtime.After(anchor) {
				anchor = mtime
			}
			age := int(time.Since(anchor).Seconds())
			ttl := state.TTLSeconds
			remaining := ttl - age
			if remaining < 0 {
				remaining = 0
			}
			out.Held = true
			out.PID = state.PID
			out.AgeSeconds = age
			out.TTLRemainingSeconds = remaining
			out.Meta = state.Meta
		}
		if mergeLockStatusJSON {
			b, _ := json.Marshal(out)
			fmt.Fprintln(cmd.OutOrStdout(), string(b))
			return nil
		}
		if out.Held {
			fmt.Fprintf(cmd.OutOrStdout(), "held (pid=%d age=%ds ttl_remaining=%ds path=%s)\n",
				out.PID, out.AgeSeconds, out.TTLRemainingSeconds, out.Path)
			for k, v := range out.Meta {
				fmt.Fprintf(cmd.OutOrStdout(), "  meta.%s=%s\n", k, v)
			}
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "not held (path=%s)\n", out.Path)
		}
		return nil
	},
}

// parseMergeLockMeta converts repeated --meta key=value flags into a map.
func parseMergeLockMeta(raw []string) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	meta := make(map[string]string, len(raw))
	for _, kv := range raw {
		idx := strings.Index(kv, "=")
		if idx <= 0 {
			return nil, fmt.Errorf("invalid --meta %q: expected key=value", kv)
		}
		key := strings.TrimSpace(kv[:idx])
		val := strings.TrimSpace(kv[idx+1:])
		if key == "" {
			return nil, fmt.Errorf("invalid --meta %q: empty key", kv)
		}
		meta[key] = val
	}
	return meta, nil
}

// holdMergeLockUntilSignal blocks the foreground, ticks a heartbeat every
// mergeLockHeartbeatInterval, and on SIGTERM/SIGINT removes the lockfile and
// returns. Used by `acquire --hold` so a long-running merge can extend its
// effective TTL via mtime bumps and clean up on graceful shutdown.
func holdMergeLockUntilSignal(path string) error {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigs)

	ticker := time.NewTicker(mergeLockHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := gitpkg.TouchLockfile(path); err != nil {
				// Heartbeat failure is informational — keep running so a
				// transient FS hiccup does not kill an active merge.
				fmt.Fprintf(os.Stderr, "merge-lock: heartbeat touch failed: %v\n", err)
			}
		case <-sigs:
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "merge-lock: signal release failed: %v\n", err)
			}
			return nil
		}
	}
}

func init() {
	mergeLockAcquireCmd.Flags().DurationVar(&mergeLockAcquireTimeout, "timeout", 60*time.Second, "Max time to wait for flock before failing")
	mergeLockAcquireCmd.Flags().DurationVar(&mergeLockAcquireTTL, "ttl", 10*time.Minute, "How long the lock is considered valid")
	mergeLockAcquireCmd.Flags().StringSliceVar(&mergeLockAcquireMeta, "meta", nil, "Annotations stored in the lock state (key=value, repeatable)")
	mergeLockAcquireCmd.Flags().BoolVar(&mergeLockAcquireHold, "hold", false, "Stay in foreground and run a heartbeat ticker until SIGTERM/SIGINT")

	mergeLockStatusCmd.Flags().BoolVar(&mergeLockStatusJSON, "json", false, "Emit JSON output instead of human text")

	mergeLockCmd.AddCommand(mergeLockAcquireCmd)
	mergeLockCmd.AddCommand(mergeLockReleaseCmd)
	mergeLockCmd.AddCommand(mergeLockStatusCmd)

	rootCmd.AddCommand(mergeLockCmd)
}
