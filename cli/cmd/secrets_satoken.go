package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/bravros/bravros/cli/internal/secrets"
	"github.com/spf13/cobra"
)

var (
	saTokenBackend    string
	saTokenOpRef      string
	saTokenVault      string
	saTokenItem       string
	saTokenField      string
	saTokenZshenv     string
	saTokenInteractive bool
	saTokenJSON       bool
)

// secretsSATokenCmd implements `bravros secrets sa-token`.
//
// It writes a marker-fenced, idempotent 1Password Service-Account-token block
// into ~/.zshenv. NON-INTERACTIVE + flag-driven by default (testable); the
// interactive UX is the /secrets-setup skill, with an optional --interactive
// TTY menu as a non-Claude fallback.
//
// MULTI-USER design: no owner-specific op vault/item/field is ever baked in.
// All op coordinates are user-provided (--op-ref OR --vault/--item/--field).
// The keychain service name is machine-derived (claude-sa-<machine>).
var secretsSATokenCmd = &cobra.Command{
	Use:   "sa-token",
	Short: "Write the 1Password Service-Account-token block into ~/.zshenv",
	Long: `Create or refresh a marker-fenced, idempotent OP Service-Account-token block
in ~/.zshenv. This is the token that makes 'op whoami' pass headlessly in
non-interactive shells.

Backends (--backend):
  op         write a $(op read <ref>) lookup line. Provide EITHER --op-ref
             'op://Vault/Item/Field' OR the trio --vault/--item/--field.
  keychain   read the token from stdin, seed it into the login keychain under a
             machine-specific service (claude-sa-<machine>), and write a
             $(security find-generic-password ...) lookup line. macOS only.
  plaintext  read the token from stdin and write it as a literal (for op-less /
             keychain-less users).
  none       skip — write no block (strips any existing one).

The token is NEVER taken as a command-line argument — keychain/plaintext read it
from stdin so it never lands in shell history or process listings.

MULTI-USER: no owner-specific 1Password vault/item/field is baked in — all op
coordinates are user-provided. The keychain service name is machine-derived.

REGRESSION GUARD: a plaintext write is REFUSED when an existing keychain/op
lookup line is present (never downgrade an encrypted-at-rest lookup to a literal).

Flags:
  --backend      op | keychain | plaintext | none  (required unless --interactive)
  --op-ref       full op:// reference (op backend)
  --vault        op vault (op backend; with --item/--field)
  --item         op item (op backend)
  --field        op field (op backend; default 'credential')
  --zshenv       override target file (default ~/.zshenv) — for testing
  --interactive  TTY menu fallback (non-Claude use); prompts for the backend
  --json         emit the result as JSON`,
	RunE: func(cmd *cobra.Command, args []string) error {
		spec := secrets.SATokenSpec{
			Backend: secrets.SATokenBackend(saTokenBackend),
			OpRef:   saTokenOpRef,
			OpVault: saTokenVault,
			OpItem:  saTokenItem,
			OpField: saTokenField,
		}

		if saTokenInteractive {
			if err := runSATokenInteractive(cmd, &spec); err != nil {
				return err
			}
		}

		if spec.Backend == "" {
			return fmt.Errorf("--backend is required: one of op, keychain, plaintext, none")
		}

		// Validate + collect per-backend inputs.
		switch spec.Backend {
		case secrets.SABackendOp:
			if spec.OpRef == "" && (spec.OpVault == "" || spec.OpItem == "") {
				return fmt.Errorf("op backend needs --op-ref OR both --vault and --item")
			}
		case secrets.SABackendKeychain, secrets.SABackendPlaintext:
			tok, err := readTokenStdin(cmd.InOrStdin())
			if err != nil {
				return err
			}
			if tok == "" {
				return fmt.Errorf("%s backend needs the token on stdin (e.g. printf %%s \"$TOKEN\" | bravros secrets sa-token --backend %s)", spec.Backend, spec.Backend)
			}
			spec.Token = tok
		case secrets.SABackendNone:
			// nothing to collect
		default:
			return fmt.Errorf("invalid --backend %q: must be op, keychain, plaintext, or none", spec.Backend)
		}

		res, err := secrets.WriteSAToken(spec, saTokenZshenv)
		if err != nil {
			return err
		}

		if saTokenJSON {
			b, _ := json.MarshalIndent(res, "", "  ")
			fmt.Println(string(b))
			return nil
		}
		if spec.Backend == secrets.SABackendNone {
			fmt.Fprintf(os.Stderr, "✅ sa-token block removed from %s\n", res.ZshenvPath)
			return nil
		}
		fmt.Fprintf(os.Stderr, "✅ sa-token (%s) written to %s\n", res.Backend, res.ZshenvPath)
		if res.KeychainSeeded {
			fmt.Fprintf(os.Stderr, "   keychain seeded: service=%s\n", res.KeychainService)
		}
		fmt.Fprintf(os.Stderr, "   open a new shell to apply\n")
		return nil
	},
}

// readTokenStdin reads a single token from r, trimming a trailing newline. It
// reads the whole stream so a multi-line paste collapses to its first line's
// content is NOT done — we take the full trimmed content (tokens are single
// line; trailing whitespace is stripped).
func readTokenStdin(r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("reading token from stdin: %w", err)
	}
	return strings.TrimRight(string(data), "\r\n"), nil
}

// runSATokenInteractive is the optional TTY fallback (non-Claude use). It does a
// minimal numbered-menu prompt for the backend and any op coordinates. The
// canonical interactive UX is the /secrets-setup skill via AskUserQuestion.
func runSATokenInteractive(cmd *cobra.Command, spec *secrets.SATokenSpec) error {
	out := cmd.OutOrStdout()
	in := bufio.NewReader(cmd.InOrStdin())
	fmt.Fprintln(out, "Select SA-token backend:")
	fmt.Fprintln(out, "  1) 1Password (op read lookup)")
	fmt.Fprintln(out, "  2) macOS keychain (encrypted at rest)")
	fmt.Fprintln(out, "  3) plaintext literal")
	fmt.Fprintln(out, "  4) none (skip)")
	fmt.Fprint(out, "> ")
	choice, _ := in.ReadString('\n')
	switch strings.TrimSpace(choice) {
	case "1":
		spec.Backend = secrets.SABackendOp
		fmt.Fprint(out, "op:// reference (blank to enter vault/item/field): ")
		ref, _ := in.ReadString('\n')
		spec.OpRef = strings.TrimSpace(ref)
		if spec.OpRef == "" {
			fmt.Fprint(out, "vault: ")
			v, _ := in.ReadString('\n')
			spec.OpVault = strings.TrimSpace(v)
			fmt.Fprint(out, "item: ")
			it, _ := in.ReadString('\n')
			spec.OpItem = strings.TrimSpace(it)
			fmt.Fprint(out, "field [credential]: ")
			f, _ := in.ReadString('\n')
			spec.OpField = strings.TrimSpace(f)
		}
	case "2":
		spec.Backend = secrets.SABackendKeychain
	case "3":
		spec.Backend = secrets.SABackendPlaintext
	case "4":
		spec.Backend = secrets.SABackendNone
	default:
		return fmt.Errorf("invalid selection %q", strings.TrimSpace(choice))
	}
	return nil
}

func init() {
	secretsSATokenCmd.Flags().StringVar(&saTokenBackend, "backend", "", "op | keychain | plaintext | none")
	secretsSATokenCmd.Flags().StringVar(&saTokenOpRef, "op-ref", "", "Full op:// reference (op backend)")
	secretsSATokenCmd.Flags().StringVar(&saTokenVault, "vault", "", "op vault (op backend)")
	secretsSATokenCmd.Flags().StringVar(&saTokenItem, "item", "", "op item (op backend)")
	secretsSATokenCmd.Flags().StringVar(&saTokenField, "field", "", "op field (op backend; default 'credential')")
	secretsSATokenCmd.Flags().StringVar(&saTokenZshenv, "zshenv", "", "Target ~/.zshenv path (default: ~/.zshenv)")
	secretsSATokenCmd.Flags().BoolVar(&saTokenInteractive, "interactive", false, "TTY menu fallback (non-Claude use)")
	secretsSATokenCmd.Flags().BoolVar(&saTokenJSON, "json", false, "Emit the result as JSON")

	secretsCmd.AddCommand(secretsSATokenCmd)
}
