package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/bravros/bravros/cli/internal/nowtime"
	"github.com/bravros/bravros/cli/internal/plan"
	"github.com/spf13/cobra"
)

// fieldExtract extracts a value from JSON using dot-path notation.
// Examples:
//
//	fieldExtract(`{"base_branch": "main"}`, "base_branch") → "main"
//	fieldExtract(`{"stack": {"framework": "laravel"}}`, "stack.framework") → "laravel"
//	fieldExtract(`{"has_ci": true}`, "has_ci") → "true"
//	fieldExtract(`{"missing": "field"}`, "nonexistent") → ""
func fieldExtract(jsonStr string, field string) string {
	val, _ := fieldExtractFound(jsonStr, field)
	return val
}

// fieldExtractFound is fieldExtract's strict sibling: it returns (value, found)
// so callers can distinguish "found, value is empty string" from "path does not
// exist in the JSON tree." Lenient callers keep using fieldExtract; strict
// callers (e.g. `kaisser meta --field`) use this and exit non-zero when found
// is false. P-0121 follow-up #15 — silent empty-on-typo regressed real fixes.
func fieldExtractFound(jsonStr string, field string) (string, bool) {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return "", false
	}

	parts := strings.Split(field, ".")
	var current interface{} = data

	for i, part := range parts {
		switch m := current.(type) {
		case map[string]interface{}:
			// Try exact key first
			val, found := m[part]
			if !found {
				// Try joining remaining parts with dots as a single key (for keys containing dots/slashes)
				remaining := strings.Join(parts[i:], ".")
				val, found = m[remaining]
				if found {
					current = val
					goto nextPart
				}
				return "", false
			}
			current = val
		case []interface{}:
			// Support integer index access (e.g. "phases.0.total")
			idx, err := strconv.Atoi(part)
			if err != nil || idx < 0 || idx >= len(m) {
				return "", false
			}
			current = m[idx]
		default:
			return "", false
		}
	nextPart:
	}

	switch v := current.(type) {
	case string:
		return v, true
	case bool:
		return fmt.Sprintf("%v", v), true
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v)), true
		}
		return fmt.Sprintf("%v", v), true
	case nil:
		return "", true
	default:
		// For nested objects/arrays, return JSON
		b, err := json.Marshal(v)
		if err != nil {
			return "", true
		}
		return string(b), true
	}
}

// ─── nextid ────────────────────────────────────────────────────────────────

// nextidReserveJSON is the flag for --json output on nextid reserve.
var nextidReserveJSON bool

// nextidReserveSlug is the optional slug for directory-kind entities (e.g. debug).
var nextidReserveSlug string

// nextidReserveScanMode controls whether the cross-worktree scan is used.
// "auto" (default) calls ScanAllSources; "single-tree" uses the old single-dir scan.
var nextidReserveScanMode string

// nextidReserveVerbose enables verbose diagnostic output on stderr.
var nextidReserveVerbose bool

// nextidReserveCmd reserves a single ID for one entity type and writes a
// placeholder file atomically. Skills call this instead of bare `kaisser nextid`
// to avoid ID collisions in parallel flows. B-0116 / P-0116.
//
// For directory-kind entities (debug), the command creates the investigation
// directory D-NNNN-<slug>-open/ directly — no placeholder file, no consumer-side mv.
var nextidReserveCmd = &cobra.Command{
	Use:   "reserve <entity>",
	Short: "Reserve a single ID (plan|backlog|report|user_report|debug) and write a placeholder or create a directory",
	Long: `Reserve the next available ID for the given entity.

File-kind entities (plan, backlog, report, user_report):
  Writes a <id>.placeholder sentinel file. Consumer skills MUST rename it to
  the final filename rather than double-calling kaisser nextid. On abort, call
  'kaisser nextid release <ID>' to delete the placeholder.

Directory-kind entities (debug):
  Creates the investigation directory D-NNNN-<slug>-open/ directly. The slug
  is provided via --slug (falls back to "investigation" when omitted).

Entity → location mapping:
  plan        → .planning/              (prefix P, file)
  backlog     → .planning/backlog/      (prefix B, file)
  report      → .planning/reports/     (prefix R, file)
  user_report → .planning/user-reports/ (prefix U, file)
  debug       → .planning/debug/       (prefix D, directory)

Scan modes (--scan-mode):
  auto        — default; scans all local branches and active worktrees to
                find the true highest ID, preventing cross-worktree collisions
                (P-0170). Reads KAISSER_NEXTID_SCAN_MODE env for override.
  single-tree — original behaviour (B-0208): only scans the entity directory
                in the current checkout's .planning/ tree. Use as an escape
                hatch when git commands are unavailable or slow.

Output: just the reserved ID (e.g. "P-0119", "D-0001") unless --json is passed.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		entity := strings.ToLower(strings.TrimSpace(args[0]))

		// Use the write root (current checkout) for the placeholder location.
		// The scan is handled by GetNextNumAtomic / ScanAllSources independently.
		gitRoot := plan.ResolveWriteRoot()
		planningRoot := filepath.Join(gitRoot, ".planning")

		eDef, ok := plan.EntityByName(entity)
		if !ok {
			// Build the list of valid names from the registry so it stays in
			// sync automatically when new entities are added.
			names := make([]string, 0, len(plan.AllEntities))
			for _, e := range plan.AllEntities {
				names = append(names, e.Name)
			}
			return fmt.Errorf("unknown entity %q — must be one of: %s", entity, strings.Join(names, ", "))
		}
		entityDir := eDef.AbsDir(planningRoot)
		if err := os.MkdirAll(entityDir, 0755); err != nil {
			return fmt.Errorf("cannot create directory %s: %w", entityDir, err)
		}

		// P-0170: collect scan diagnostics for --verbose / --json before reserving.
		// We call ScanAllSources here directly so that we have source-list metadata
		// (branch count, worktree count, highest source) to surface in output.
		// The actual reservation below will call GetNextNumAtomic again (internally
		// via ReservePlaceholder) which repeats the scan — this is intentional per
		// locked decision #3 (full scan every call, no cache).
		var (
			scanMode         = nextidReserveScanMode
			branchesScanned  int
			worktreesScanned int
			highestSource    string
			highestGlobal    int // highest ID across all sources (cross-ref scan)
			highestLocal     int // highest ID in the current worktree-fs only
		)
		if scanMode == "" {
			scanMode = "auto"
		}
		if scanMode == "auto" && eDef.Kind == plan.EntityKindFile {
			// P-0170 locked decision #5: hard fail on scan errors in auto mode.
			// The caller can pass --scan-mode single-tree to bypass.
			var sources []plan.ScanSource
			var scanErr error
			highestGlobal, sources, scanErr = plan.ScanAllSources(eDef.Prefix)
			if scanErr != nil {
				return fmt.Errorf("cross-worktree scan failed: %w\n(use --scan-mode single-tree to bypass)", scanErr)
			}

			// B-0312: emit verbose diagnostic header.
			if nextidReserveVerbose {
				fmt.Fprintf(os.Stderr, "[verbose] nextid: scanning prefix %s\n", eDef.Prefix)
			}

			// Walk each source to collect per-source highs for diagnostics and
			// to identify both the global-highest source and the local (current
			// worktree-fs) highest for the warn-threshold check.
			for _, src := range sources {
				switch src.Kind {
				case "worktree-fs":
					worktreesScanned++
					n, _ := plan.ScanWorktreeFSForDiag(src.Path, eDef)
					if nextidReserveVerbose {
						fmt.Fprintf(os.Stderr, "[verbose] nextid: ref=%s highest=%d file=%s\n",
							src.Path, n, src.Path)
					}
					if n > highestGlobal {
						// Safety: re-max in case ScanAllSources missed a concurrent write.
						highestGlobal = n
					}
					// The current worktree is identified by gitRoot.
					if src.Path == gitRoot {
						if n > highestLocal {
							highestLocal = n
						}
					}
				case "branch-tree":
					branchesScanned++
					n, _ := plan.ScanBranchTreeForDiag(src.Branch, eDef)
					if nextidReserveVerbose {
						fmt.Fprintf(os.Stderr, "[verbose] nextid: ref=%s highest=%d file=%s\n",
							src.Branch, n, src.Branch)
					}
					if n > highestGlobal {
						highestGlobal = n
					}
				case "history":
					// Ids ever allocated across all refs, including deleted ones.
					n, _ := plan.ScanHistoryForDiag(eDef)
					if nextidReserveVerbose {
						fmt.Fprintf(os.Stderr, "[verbose] nextid: ref=history(--all) highest=%d "+
							"(includes deleted ids — never reissued)\n", n)
					}
					if n > highestGlobal {
						highestGlobal = n
					}
				}
			}

			// Determine which source contributed the global highest.
			if highestGlobal > 0 {
				highestSource = fmt.Sprintf("%s-%04d", eDef.Prefix, highestGlobal)
				for _, src := range sources {
					var n int
					if src.Kind == "worktree-fs" {
						n, _ = plan.ScanWorktreeFSForDiag(src.Path, eDef)
					} else if src.Kind == "branch-tree" {
						n, _ = plan.ScanBranchTreeForDiag(src.Branch, eDef)
					}
					if n == highestGlobal {
						if src.Kind == "worktree-fs" {
							highestSource = src.Path
						} else {
							highestSource = src.Branch
						}
						break
					}
				}
			}
		}

		// Directory-kind entities (debug): create the investigation directory directly.
		if eDef.Kind == plan.EntityKindDirectory {
			id, dirPath, err := plan.ReserveDebugDir(entityDir, nextidReserveSlug, scanMode)
			if err != nil {
				return fmt.Errorf("reserve failed: %w", err)
			}
			if nextidReserveJSON {
				result := map[string]interface{}{
					"id":   id,
					"path": dirPath,
				}
				if scanMode == "auto" {
					result["scan"] = map[string]interface{}{
						"mode":              scanMode,
						"branches_scanned":  branchesScanned,
						"worktrees_scanned": worktreesScanned,
						"highest_source":    highestSource,
					}
				}
				b, _ := json.MarshalIndent(result, "", "  ")
				fmt.Println(string(b))
			} else {
				fmt.Println(id)
			}
			return nil
		}

		// Dual-kind plan entity WITH --slug: create a folder-plan directly
		// (P-0180), mirroring the directory-kind (debug) branch above. Without
		// --slug, plan falls through to the file-kind placeholder path below —
		// unchanged, so `/plan` (which reserves with no slug) still gets a
		// single-file placeholder.
		if eDef.IsDualKind() && nextidReserveSlug != "" {
			id, dirPath, err := plan.ReservePlanDir(entityDir, nextidReserveSlug, scanMode)
			if err != nil {
				return fmt.Errorf("reserve failed: %w", err)
			}
			if nextidReserveJSON {
				result := map[string]interface{}{
					"id":   id,
					"path": dirPath,
				}
				if scanMode == "auto" {
					result["scan"] = map[string]interface{}{
						"mode":              scanMode,
						"branches_scanned":  branchesScanned,
						"worktrees_scanned": worktreesScanned,
						"highest_source":    highestSource,
					}
				}
				b, _ := json.MarshalIndent(result, "", "  ")
				fmt.Println(string(b))
			} else {
				fmt.Println(id)
			}
			return nil
		}

		// File-kind entities: write a placeholder file.
		id, phPath, err := plan.ReservePlaceholder(entityDir, eDef.Prefix, scanMode)
		if err != nil {
			return fmt.Errorf("reserve failed: %w", err)
		}

		// B-0312: post-reserve diagnostics (verbose + always-on warn).
		if scanMode == "auto" {
			// Parse the chosen number from the returned ID (e.g. "P-0200" → 200).
			chosenNum := parseIDNumber(id)

			// Verbose: final "chose" line.
			if nextidReserveVerbose {
				sourceKind := "single-tree"
				if highestGlobal > 0 {
					sourceKind = "cross-ref"
				}
				fmt.Fprintf(os.Stderr, "[verbose] nextid: chose %s (source=%s)\n", id, sourceKind)
			}

			// Always-on warn: if the chosen ID is more than 100 above the local highest,
			// something is inflating the cross-ref scan (phantom commit, stale remote ref, etc.).
			if chosenNum > 0 && (chosenNum-highestLocal) > 100 {
				fmt.Fprintf(os.Stderr,
					"[warn] nextid reserve: chose %s but highest local is %d (delta=%d) — run with --verbose to investigate\n",
					id, highestLocal, chosenNum-highestLocal)
			}
		}

		if nextidReserveJSON {
			result := map[string]interface{}{
				"id":   id,
				"path": phPath,
			}
			if scanMode == "auto" {
				result["scan"] = map[string]interface{}{
					"mode":              scanMode,
					"branches_scanned":  branchesScanned,
					"worktrees_scanned": worktreesScanned,
					"highest_source":    highestSource,
				}
			}
			b, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(b))
		} else {
			fmt.Println(id)
		}
		return nil
	},
}

// nextidReleaseCmd deletes a placeholder or investigation directory created by nextid reserve.
var nextidReleaseCmd = &cobra.Command{
	Use:   "release <ID>",
	Short: "Delete a placeholder or debug directory created by 'nextid reserve' (idempotent)",
	Long: `Delete the reservation for the given ID (e.g. P-0119, B-0042, D-0001).

For file-kind entities (P/B/R/U): deletes the <id>.placeholder file.
For directory-kind entities (D): removes the D-NNNN-<slug>-open/ directory.

The entity type is derived from the ID prefix:
  P → .planning/              (file)
  B → .planning/backlog/      (file)
  R → .planning/reports/      (file)
  U → .planning/user-reports/ (file)
  D → .planning/debug/        (directory)

Idempotent: exits 0 when the reservation is already gone.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := strings.TrimSpace(args[0])
		if len(id) < 2 {
			return fmt.Errorf("invalid ID %q — expected P-NNNN / B-NNNN / R-NNNN / U-NNNN / D-NNNN", id)
		}
		// P-0170: release (delete) targets the calling checkout's .planning/.
		// ResolveWriteRoot returns the current worktree's git toplevel — now the
		// only resolver after P-0172 Phase 4 removed the deprecated alternative.
		gitRoot := plan.ResolveWriteRoot()
		planningRoot := filepath.Join(gitRoot, ".planning")
		prefix := strings.ToUpper(string(id[0]))
		eDef, ok := plan.EntityByPrefix(prefix)
		if !ok {
			// Build expected prefix list from registry.
			prefixes := make([]string, 0, len(plan.AllEntities))
			for _, e := range plan.AllEntities {
				prefixes = append(prefixes, e.Prefix)
			}
			return fmt.Errorf("unknown ID prefix %q in %q — expected %s", prefix, id, strings.Join(prefixes, ", "))
		}
		// Directory-kind entities use ReleaseDebugDir (removes the D-NNNN-*-open/ dir).
		if eDef.Kind == plan.EntityKindDirectory {
			return plan.ReleaseDebugDir(eDef.AbsDir(planningRoot), id)
		}
		// File-kind entities use ReleasePlaceholder.
		if err := plan.ReleasePlaceholder(eDef.AbsDir(planningRoot), id); err != nil {
			return err
		}
		return nil
	},
}

// parseIDNumber extracts the numeric portion from a formatted entity ID such as
// "P-0200" or "B-0042". Returns 0 if the ID cannot be parsed (e.g. empty string
// or unknown format). Used for the B-0312 warn-threshold check.
func parseIDNumber(id string) int {
	if id == "" {
		return 0
	}
	parts := strings.SplitN(id, "-", 2)
	if len(parts) != 2 {
		return 0
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0
	}
	return n
}

// nextidAuditFormat is the output format flag for nextid audit.
var nextidAuditFormat string

// errCollisionsFound is the sentinel error returned by nextidAuditCmd when ≥1
// divergent duplicate ID is found. The main.go Execute() path converts any
// non-nil error to exit 1. Tests can type-assert against this sentinel to
// distinguish "collisions found" from a scan/git error.
var errCollisionsFound = fmt.Errorf("divergent duplicate IDs found")

// nextidAuditCmd detects ID collisions that already exist across branches and
// worktrees. Unlike nextid reserve (which prevents FUTURE collisions), audit
// surfaces IDs that are already divergent — same numeric ID, different content —
// across ≥2 sources. Report-only: no files are renamed or modified.
//
// Exit codes:
//
//	0 — clean (no divergent duplicates found)
//	1 — ≥1 divergent duplicate found (or git subprocess error)
var nextidAuditCmd = &cobra.Command{
	Use:   "audit [entity]",
	Short: "Detect divergent duplicate IDs across branches/worktrees (report-only)",
	Long: `Scan all branches and active worktrees for the same numeric ID appearing in ≥2
sources with divergent content (different git blob SHA / file hash). Reports
each collision as a table row or JSON object. No files are modified.

The optional [entity] argument limits the scan to one entity type (plan, backlog,
report, user_report, debug, or a prefix letter P/B/R/U/D). When omitted all five
entity streams are scanned.

Exit codes:
  0 — clean (no divergent duplicates found)
  1 — ≥1 divergent duplicate found, or git subprocess error

Output format (--format):
  table  — human-readable table (default); on a clean scan prints one summary line.
  json   — JSON array of collision objects; [] on a clean scan.`,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		var collisions []plan.IDCollision
		var sources []plan.ScanSource
		var err error

		if len(args) == 1 {
			// Try entity name first ("plan", "backlog", …) then prefix letter ("P", "B", …).
			entityArg := args[0]
			eDef, byName := plan.EntityByName(strings.ToLower(entityArg))
			if byName {
				collisions, sources, err = plan.AuditDuplicateIDs(eDef.Prefix)
			} else {
				_, byPrefix := plan.EntityByPrefix(strings.ToUpper(entityArg))
				if !byPrefix {
					// Build valid names list for the error message.
					names := make([]string, 0, len(plan.AllEntities))
					for _, e := range plan.AllEntities {
						names = append(names, e.Name+"/"+e.Prefix)
					}
					return fmt.Errorf("unknown entity %q — must be one of: %s", entityArg, strings.Join(names, ", "))
				}
				collisions, sources, err = plan.AuditDuplicateIDs(strings.ToUpper(entityArg))
			}
		} else {
			collisions, sources, err = plan.AuditAllEntities()
		}

		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: audit scan failed: %v\n", err)
			return err
		}

		format := strings.ToLower(strings.TrimSpace(nextidAuditFormat))

		switch format {
		case "json":
			if len(collisions) == 0 {
				fmt.Println("[]")
				return nil
			}
			b, jsonErr := json.MarshalIndent(collisions, "", "  ")
			if jsonErr != nil {
				return fmt.Errorf("json marshal: %w", jsonErr)
			}
			fmt.Println(string(b))
			return errCollisionsFound
		default: // "table"
			if len(collisions) == 0 {
				fmt.Printf("no duplicate IDs found across %d sources\n", len(sources))
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "PREFIX\tID\tSOURCE KIND\tLOCATION\tCONTENT HASH")
			fmt.Fprintln(w, "------\t--\t-----------\t--------\t------------")
			for _, col := range collisions {
				for i, src := range col.Sources {
					hash := src.ContentHash
					if len(hash) > 16 {
						hash = hash[:16] + "..."
					}
					if i == 0 {
						fmt.Fprintf(w, "%s\t%04d\t%s\t%s\t%s\n",
							col.Prefix+"-", col.ID, src.Kind, src.Location, hash)
					} else {
						fmt.Fprintf(w, "\t\t%s\t%s\t%s\n",
							src.Kind, src.Location, hash)
					}
				}
			}
			w.Flush()
			return errCollisionsFound
		}
	},
}

var nextidCmd = &cobra.Command{
	Use:   "nextid",
	Short: "Atomically reserve next plan, backlog, report, and user-report IDs (JSON)",
	Long: `Return the next available IDs for all four entity streams as a JSON object.
Deprecated for write workflows: use 'kaisser nextid reserve <entity>' instead,
which atomically reserves a single ID and writes a placeholder file.`,
	Run: func(cmd *cobra.Command, args []string) {
		// P-0170: use the calling checkout's write root so placeholders land in the
		// caller's .planning/, not redirected to the primary worktree (B-0208 redirect
		// was for plan-file resolution only; write paths use ResolveWriteRoot).
		// The cross-worktree ID scan is handled inside GetNextNumAtomic automatically.
		gitRoot := plan.ResolveWriteRoot()
		planningRoot := filepath.Join(gitRoot, ".planning")

		// Derive per-entity directories from the canonical registry.
		planDef, _ := plan.EntityByName("plan")
		backlogDef, _ := plan.EntityByName("backlog")
		reportDef, _ := plan.EntityByName("report")
		userReportDef, _ := plan.EntityByName("user_report")

		planDir := planDef.AbsDir(planningRoot)
		backlogDir := backlogDef.AbsDir(planningRoot)
		reportsDir := reportDef.AbsDir(planningRoot)
		userReportsDir := userReportDef.AbsDir(planningRoot)

		os.MkdirAll(planDir, 0755)
		os.MkdirAll(backlogDir, 0755)
		os.MkdirAll(reportsDir, 0755)
		os.MkdirAll(userReportsDir, 0755)

		// Pass "" scanMode so the env-var fallback (KAISSER_NEXTID_SCAN_MODE) applies.
		// The deprecated all-ids verb is read-only and non-interactive; env-var override
		// is the correct escape hatch for callers that cannot pass flags here.
		planNum, _, err := plan.GetNextNumAtomic(planDir, planDef.Prefix, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reserving plan ID: %v\n", err)
			os.Exit(1)
		}

		backlogNum, _, err := plan.GetNextNumAtomic(backlogDir, backlogDef.Prefix, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reserving backlog ID: %v\n", err)
			os.Exit(1)
		}

		reportNum := plan.GetNextReportNum(reportsDir, reportDef.Prefix)
		userReportNum := plan.GetNextReportNum(userReportsDir, userReportDef.Prefix)

		result := map[string]string{
			"plan":        planDef.Prefix + "-" + planNum,
			"backlog":     backlogDef.Prefix + "-" + backlogNum,
			"report":      reportNum,
			"user_report": userReportNum,
			"created":     nowtime.Now(),
		}
		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(b))
	},
}

// ─── commit ─────────────────────────────────────────────────────────────────

var commitVerifyClean bool
var commitScopeStaged bool

var commitCmd = &cobra.Command{
	Use:   "commit <message> [files...]",
	Short: "Commit plan + code changes",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		message := args[0]
		files := args[1:]
		opts := plan.CommitOptions{
			VerifyClean: commitVerifyClean,
			ScopeStaged: commitScopeStaged,
		}
		if err := plan.CommitWithOptions(message, files, opts); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	// commit flags
	commitCmd.Flags().BoolVar(&commitVerifyClean, "verify-clean", false, "Error if untracked/modified files remain after commit (opt-in clean gate)")
	commitCmd.Flags().BoolVar(&commitScopeStaged, "scope-staged", false, "Commit only pre-staged index; skip git add -u and .planning/ sweep (parallel-session safety, B-0077)")
	commitCmd.Flags().BoolVar(&commitScopeStaged, "no-add", false, "Alias for --scope-staged")
	commitCmd.Flags().MarkHidden("no-add")

	rootCmd.AddCommand(commitCmd)
	nextidReserveCmd.Flags().BoolVar(&nextidReserveJSON, "json", false, "Output JSON {id, path} instead of bare ID")
	nextidReserveCmd.Flags().StringVar(&nextidReserveSlug, "slug", "", "Slug for directory/folder entities: debug (D-NNNN-<slug>-open/) always; plan (P-NNNN-<slug>/ folder-plan) when --slug is given. Defaults to 'investigation' for debug when omitted")
	nextidReserveCmd.Flags().StringVar(&nextidReserveScanMode, "scan-mode", "auto", "Scan mode: auto (cross-worktree, default) or single-tree (old behaviour, B-0208 escape hatch)")
	nextidReserveCmd.Flags().BoolVar(&nextidReserveVerbose, "verbose", false, "Print scan diagnostics to stderr: branches + worktrees scanned and highest source")
	nextidAuditCmd.Flags().StringVar(&nextidAuditFormat, "format", "table", "Output format: table (default, human-readable) or json")
	nextidCmd.AddCommand(nextidReserveCmd)
	nextidCmd.AddCommand(nextidReleaseCmd)
	nextidCmd.AddCommand(nextidAuditCmd)
	rootCmd.AddCommand(nextidCmd)
}
