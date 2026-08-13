package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/bravros/bravros/cli/internal/secrets"
	"github.com/spf13/cobra"
)

var secretsCmd = &cobra.Command{
	Use:   "secrets",
	Short: "Manage kaisser secrets (op / env / none backends)",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var (
	secretsBootstrapShellRC   string
	secretsBootstrapDryRun    bool
	secretsBootstrapPrintOnly bool
	secretsBootstrapBackend   string
)

// secretsBootstrapCmd implements `kaisser secrets bootstrap`.
var secretsBootstrapCmd = &cobra.Command{
	Use:   "bootstrap",
	Short: "Write the kaisser secrets v2 block into your shell RC file",
	Long: `Rewrite the managed kaisser secrets block (a fenced v2 region) in your shell
RC file (default: ~/.zshrc) for the selected backend.

Backends:
  op        resolve from 1Password (op-whoami-gated; never prompts at shell startup)
  env       resolve from $KAISSER_ENV_FILE (KEY=value lines)
  keychain  resolve from the macOS login keychain (darwin-only, opt-in; ZERO op refs)
  none      no secret hydration; ZERO 1Password references written

When --backend is omitted, the backend is auto-detected: op is chosen only when
the 1Password CLI is installed AND a live session exists; otherwise env. keychain
is never auto-selected — it is opt-in only (like none).

The block always defines a kaisser_secret() shell helper FIRST, then per-backend
gate lines. Rewriting is atomic (temp file + rename) and idempotent — running
twice with the same backend yields a byte-identical RC with exactly one block.
Legacy v1 blocks and eager 'export FIRECRAWL_API_KEY=$(op read …)' / HASS_TOKEN
lines are stripped on migration.

Flags:
  --backend     op | env | keychain | none (default: auto-detect)
  --shell-rc    override the target RC file
  --dry-run     print the rendered block to stdout without touching the RC
  --print-only  alias of --dry-run for the block`,
	RunE: func(cmd *cobra.Command, args []string) error {
		backend, err := parseBackendArg(secretsBootstrapBackend)
		if err != nil {
			return err
		}
		opts := secrets.BootstrapOpts{
			ShellRC:   secretsBootstrapShellRC,
			Backend:   backend,
			DryRun:    secretsBootstrapDryRun,
			PrintOnly: secretsBootstrapPrintOnly,
		}

		result, err := secrets.Bootstrap(opts)
		if err != nil {
			return err
		}

		if !opts.PrintOnly && !opts.DryRun {
			b, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(b))
		}
		return nil
	},
}

var (
	secretsTemplateOutput    string
	secretsTemplatePrintOnly bool
)

// secretsExportTemplateCmd implements `kaisser secrets export-template`.
var secretsExportTemplateCmd = &cobra.Command{
	Use:   "export-template",
	Short: "Write a template env file with blank placeholders for all known secrets",
	Long: `Write ~/.config/kaisser/secrets-template.env with one blank export line per
known kaisser secret.  Non-1Password users can fill in values manually and
source the file in their shell RC.

Flags:
  --output      custom output file path
  --print-only  print to stdout instead of writing to a file`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := secrets.TemplateOpts{
			OutputFile: secretsTemplateOutput,
			PrintOnly:  secretsTemplatePrintOnly,
		}
		path, err := secrets.ExportTemplate(opts)
		if err != nil {
			return err
		}
		if path != "" {
			fmt.Fprintf(os.Stderr, "✅ secrets template written to %s\n", path)
		}
		return nil
	},
}

var secretsBackendShellRC string

// secretsBackendCmd implements `kaisser secrets backend <op|env|none>`.
var secretsBackendCmd = &cobra.Command{
	Use:   "backend <op|env|keychain|none>",
	Short: "Switch the secrets backend and rewrite the shell RC block",
	Long: `Validate and switch the active secrets backend, persisting it by rewriting the
managed v2 block in your shell RC file. Switching away from op strips every
1Password reference from the block (the new backend writes ZERO op refs unless
it is op).

The chosen backend is encoded in the block as 'export KAISSER_SECRETS_BACKEND=…',
so it persists for new shells without an external state file.

Flags:
  --shell-rc  override the target RC file`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		backend, err := parseBackendArg(args[0])
		if err != nil {
			return err
		}
		if backend == "" {
			return fmt.Errorf("backend is required: one of op, env, keychain, none")
		}
		result, err := secrets.Bootstrap(secrets.BootstrapOpts{
			ShellRC: secretsBackendShellRC,
			Backend: backend,
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "✅ secrets backend set to %s in %s\n", backend, result.RCFile)
		fmt.Fprintf(os.Stderr, "   open a new shell or `source %s` to apply\n", result.RCFile)
		return nil
	},
}

var secretsSetBackend string

// secretsSetCmd implements `kaisser secrets set KEY=VALUE`.
var secretsSetCmd = &cobra.Command{
	Use:   "set KEY=VALUE",
	Short: "Write or update a secret in the env file or macOS keychain",
	Long: `Write or update KEY=VALUE in the active backend's store.

Backends:
  env       write KEY=VALUE into $KAISSER_ENV_FILE (default ~/.config/kaisser/
            secrets.env, 0600). This is the env-backend equivalent of storing a
            value in 1Password.
  keychain  write the value into the macOS login keychain under the registry's
            service/account mapping (default service "kaisser-secrets",
            account=KEY) via security add-generic-password -A -U.

Without --backend, the active backend is auto-detected (KAISSER_SECRETS_BACKEND).
The op backend has no writable store here — store op secrets in 1Password directly.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key, value, err := secrets.SplitKeyValue(args[0])
		if err != nil {
			return err
		}

		backend, err := parseBackendArg(secretsSetBackend)
		if err != nil {
			return err
		}
		if backend == "" {
			backend = secrets.DetectBackend()
		}

		switch backend {
		case secrets.BackendKeychain:
			service, account, err := secrets.SetKeychainSecret(key, value)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "✅ %s written to login keychain (service=%s account=%s)\n", key, service, account)
		case secrets.BackendOp:
			return fmt.Errorf("the op backend has no writable store — store %s in 1Password directly, or use --backend env|keychain", key)
		default:
			path, err := secrets.SetEnvSecret(key, value)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "✅ %s written to %s\n", key, path)
		}
		return nil
	},
}

var secretsStatusJSON bool

// secretsStatusCmd implements `kaisser secrets status`.
var secretsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the active backend, env-file path, and op availability",
	RunE: func(cmd *cobra.Command, args []string) error {
		st := secrets.Status()
		if secretsStatusJSON {
			b, _ := json.MarshalIndent(st, "", "  ")
			fmt.Println(string(b))
			return nil
		}
		fmt.Printf("backend:        %s\n", st.Backend)
		fmt.Printf("env_file:       %s\n", st.EnvFile)
		fmt.Printf("env_file_set:   %t\n", st.EnvFileExists)
		fmt.Printf("op_available:   %t\n", st.OpAvailable)
		fmt.Printf("sa_token_block: %s\n", st.SATokenBlock)
		return nil
	},
}

// secretsResolveCmd implements `kaisser secrets resolve ENVVAR OP_ITEM OP_FIELD`.
// This is the binary side of the kaisser_secret() shell helper. It prints the
// resolved value to stdout (empty on miss) and is intentionally quiet.
var secretsResolveCmd = &cobra.Command{
	Use:    "resolve ENVVAR OP_ITEM OP_FIELD",
	Short:  "Resolve a single managed secret (used by the kaisser_secret shell helper)",
	Args:   cobra.RangeArgs(1, 3),
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		envVar := args[0]
		var opItem, opField string
		if len(args) > 1 {
			opItem = args[1]
		}
		if len(args) > 2 {
			opField = args[2]
		}
		fmt.Print(secrets.Resolve(envVar, opItem, opField))
		return nil
	},
}

// parseBackendArg validates a user-supplied backend string. Empty → "" (means
// auto-detect for bootstrap). Any non-empty value must be exactly op|env|none.
func parseBackendArg(s string) (secrets.Backend, error) {
	switch s {
	case "":
		return "", nil
	case string(secrets.BackendOp):
		return secrets.BackendOp, nil
	case string(secrets.BackendEnv):
		return secrets.BackendEnv, nil
	case string(secrets.BackendNone):
		return secrets.BackendNone, nil
	case string(secrets.BackendKeychain):
		return secrets.BackendKeychain, nil
	default:
		return "", fmt.Errorf("invalid backend %q: must be one of op, env, none, keychain", s)
	}
}

func init() {
	secretsBootstrapCmd.Flags().StringVar(&secretsBootstrapBackend, "backend", "", "Backend: op | env | none (default: auto-detect)")
	secretsBootstrapCmd.Flags().StringVar(&secretsBootstrapShellRC, "shell-rc", "", "Target shell RC file (default: ~/.zshrc)")
	secretsBootstrapCmd.Flags().BoolVar(&secretsBootstrapDryRun, "dry-run", false, "Print the rendered block without modifying the file")
	secretsBootstrapCmd.Flags().BoolVar(&secretsBootstrapPrintOnly, "print-only", false, "Print the rendered block to stdout (alias of --dry-run)")

	secretsExportTemplateCmd.Flags().StringVar(&secretsTemplateOutput, "output", "", "Output file path (default: ~/.config/kaisser/secrets-template.env)")
	secretsExportTemplateCmd.Flags().BoolVar(&secretsTemplatePrintOnly, "print-only", false, "Print template to stdout instead of writing to file")

	secretsBackendCmd.Flags().StringVar(&secretsBackendShellRC, "shell-rc", "", "Target shell RC file (default: ~/.zshrc)")

	secretsSetCmd.Flags().StringVar(&secretsSetBackend, "backend", "", "Backend store: env | keychain (default: auto-detect)")

	secretsStatusCmd.Flags().BoolVar(&secretsStatusJSON, "json", false, "Emit machine-readable JSON")

	secretsCmd.AddCommand(secretsBootstrapCmd)
	secretsCmd.AddCommand(secretsExportTemplateCmd)
	secretsCmd.AddCommand(secretsBackendCmd)
	secretsCmd.AddCommand(secretsSetCmd)
	secretsCmd.AddCommand(secretsStatusCmd)
	secretsCmd.AddCommand(secretsResolveCmd)
	rootCmd.AddCommand(secretsCmd)
}
