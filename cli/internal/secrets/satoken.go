package secrets

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

// keychainAccount resolves the account name used to seed the keychain SA-token
// item. The rendered lookup line reads it back via `security ... -a "$USER"`,
// so we must seed with the same value the shell will present. $USER is set in
// every interactive shell but is NOT POSIX-guaranteed (empty on some CI runners
// / Docker images); fall back to the OS-reported username so the seed never
// lands under an empty account that the lookup can never match.
func keychainAccount() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

// SA-token bootstrap (Layer 1, B-0337). This manages the 1Password Service
// Account token block in ~/.zshenv — the token that lets `op whoami` pass
// headlessly in non-interactive shells. It is DISTINCT from the per-secret
// resolver (Layer 2, bootstrap.go) which manages ~/.zshrc.
//
// HARD multi-user constraint: this code never bakes in an owner-specific
// 1Password vault/item/field. All op coordinates are USER-PROVIDED at call time
// (a full op:// ref, or vault+item+field). The keychain service name is
// machine-DERIVED (claude-sa-<machine>), which is generic, not owner data.

const (
	saMarkerStart = "# >>> bravros sa-token (v1) >>>"
	saMarkerEnd   = "# <<< bravros sa-token (v1) <<<"

	// saExportVar is the canonical env var the toolkit + op consume. We export
	// both it and OP_SERVICE_ACCOUNT_TOKEN (op's native var) from it.
	saExportVar = "OP_CLAUDE_CODE_SERVICE_ACCOUNT_SECRET"

	// swarmGate is emitted at the TOP of the block so spawned swarm agents never
	// inherit the SA token (they run token-less by design).
	swarmGate = `if [ -n "$SWARM_AGENT_NAME" ]; then
  return 2>/dev/null || true
fi`
)

// SATokenBackend enumerates the supported SA-token storage strategies. It is a
// superset of the resolver Backend constants because plaintext is meaningful
// here (a literal in ~/.zshenv for op-less/keychain-less users) but not a
// resolver backend.
type SATokenBackend string

const (
	SABackendOp        SATokenBackend = "op"        // $(op read <ref>) lookup line
	SABackendKeychain  SATokenBackend = "keychain"  // $(security find-generic-password ...) lookup line
	SABackendPlaintext SATokenBackend = "plaintext" // literal token (op-less / keychain-less)
	SABackendNone      SATokenBackend = "none"      // skip — write no block
)

// SATokenSpec is the fully-resolved, non-interactive input to the writer. The
// interactive UX (skill / --interactive TTY menu) collects these and hands them
// in; the writer itself does no prompting.
type SATokenSpec struct {
	Backend SATokenBackend

	// op backend: either OpRef (a full op://vault/item/field), or the discrete
	// Vault/Item/Field trio. OpRef wins when set. ALL user-provided.
	OpRef   string
	OpVault string
	OpItem  string
	OpField string

	// keychain / plaintext: the literal token, read from stdin (never an arg).
	// For keychain it is seeded into the login keychain and the block reads it
	// back; for plaintext it is written as a literal.
	Token string

	// KeychainService overrides the derived claude-sa-<machine> service name
	// (used for testing / advanced cases). Empty → DeriveKeychainSAService().
	KeychainService string
}

// machineNameFn is the seam tests use to fake the machine name without shelling
// to scutil/hostname. Production uses machineName.
var machineNameFn = machineName

// machineName derives a short, lowercased machine identifier for the keychain
// service name. Prefers `scutil --get LocalHostName` (the stable, GUI-set name),
// falls back to `hostname -s`, then to "unknown". This is DERIVED data, not
// owner data — safe to bake into the generated block.
func machineName() string {
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("/usr/sbin/scutil", "--get", "LocalHostName").Output(); err == nil {
			if n := strings.ToLower(strings.TrimSpace(string(out))); n != "" {
				return n
			}
		}
	}
	if out, err := exec.Command("hostname", "-s").Output(); err == nil {
		if n := strings.ToLower(strings.TrimSpace(string(out))); n != "" {
			return n
		}
	}
	return "unknown"
}

// DeriveKeychainSAService returns the machine-specific keychain service name for
// the SA token: claude-sa-<machine>. Per-machine by design (blast-radius
// control — each box's SA token is a distinct keychain item).
func DeriveKeychainSAService() string {
	return "claude-sa-" + machineNameFn()
}

// existingLookupKind classifies the lookup line inside an already-present
// sa-token block, so the writer's clobber-guard can refuse to overwrite a
// keychain/op lookup with a plaintext literal.
type existingLookupKind int

const (
	lookupNone      existingLookupKind = iota // no managed block present
	lookupPlaintext                           // export VAR="literal"
	lookupKeychain                            // export VAR="$(... security find-generic-password ...)"
	lookupOp                                  // export VAR="$(op read ...)"
)

// classifyExisting inspects the FULL ~/.zshenv text and reports whether a
// managed sa-token block exists and, if so, what kind of lookup it holds. The
// classification keys off the saExportVar line within the marker fence.
func classifyExisting(text string) existingLookupKind {
	inBlock := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == saMarkerStart {
			inBlock = true
			continue
		}
		if trimmed == saMarkerEnd {
			inBlock = false
			continue
		}
		if !inBlock {
			continue
		}
		if !strings.HasPrefix(trimmed, "export "+saExportVar+"=") {
			continue
		}
		switch {
		case strings.Contains(trimmed, "security find-generic-password"):
			return lookupKeychain
		case strings.Contains(trimmed, "op read"):
			return lookupOp
		default:
			return lookupPlaintext
		}
	}
	return lookupNone
}

// SATokenBlockKind classifies the SA-token block in the user's ~/.zshenv (or the
// override path). It reads the file, runs classifyExisting on its contents, and
// returns one of "none" | "plaintext" | "keychain" | "op". A missing/unreadable
// ~/.zshenv classifies as "none". This is the exported, IO-performing wrapper
// around the pure classifyExisting() classifier — it answers "is the SA-token
// block configured on this machine?" for `bravros secrets status`.
//
// home is the home directory to resolve ~/.zshenv under; pass "" to use the
// OS-reported home dir.
func SATokenBlockKind(home string) string {
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "none"
		}
		home = h
	}
	data, err := os.ReadFile(filepath.Join(home, ".zshenv"))
	if err != nil {
		// Missing or unreadable ~/.zshenv → no managed block.
		return "none"
	}
	switch classifyExisting(string(data)) {
	case lookupPlaintext:
		return "plaintext"
	case lookupKeychain:
		return "keychain"
	case lookupOp:
		return "op"
	default:
		return "none"
	}
}

// ErrClobberGuard is returned when a plaintext write would overwrite an existing
// keychain/op lookup line. The caller (or install.sh) must NEVER downgrade an
// encrypted-at-rest lookup back to a plaintext literal.
type ErrClobberGuard struct {
	Existing existingLookupKind
}

func (e *ErrClobberGuard) Error() string {
	kind := "keychain"
	if e.Existing == lookupOp {
		kind = "op"
	}
	return fmt.Sprintf("refusing to overwrite an existing %s SA-token lookup with a plaintext literal (clobber guard)", kind)
}

// RenderSATokenBlock is the PURE writer core: given the chosen spec and the
// already-classified existing lookup kind, it returns the new marker-fenced
// block (or an error). It performs ZERO IO — it is the unit-tested heart of the
// clobber guard. Callers pass the existing kind from classifyExisting().
//
// Clobber guard: when spec.Backend is plaintext AND an existing keychain/op
// lookup is present, it returns *ErrClobberGuard rather than emitting a
// downgrade. (op→keychain or keychain→op upgrades are allowed; same-or-better.)
func RenderSATokenBlock(spec SATokenSpec, existing existingLookupKind) (string, error) {
	if spec.Backend == SABackendNone {
		return "", nil
	}
	if spec.Backend == SABackendPlaintext &&
		(existing == lookupKeychain || existing == lookupOp) {
		return "", &ErrClobberGuard{Existing: existing}
	}

	var lookup string
	switch spec.Backend {
	case SABackendOp:
		ref := spec.OpRef
		if ref == "" {
			field := spec.OpField
			if field == "" {
				field = "credential"
			}
			ref = "op://" + spec.OpVault + "/" + spec.OpItem + "/" + field
		}
		// Guard the op read on op being installed so a no-op shell stays silent.
		lookup = fmt.Sprintf(`export %s="$(command -v op >/dev/null 2>&1 && op read %s 2>/dev/null)"`,
			saExportVar, shellQuote(ref))
	case SABackendKeychain:
		service := spec.KeychainService
		if service == "" {
			service = DeriveKeychainSAService()
		}
		lookup = fmt.Sprintf(`export %s="$(/usr/bin/security find-generic-password -a "$USER" -s %s -w 2>/dev/null)"`,
			saExportVar, shellQuote(service))
	case SABackendPlaintext:
		lookup = fmt.Sprintf(`export %s=%s`, saExportVar, shellQuote(spec.Token))
	default:
		return "", fmt.Errorf("unknown sa-token backend %q", spec.Backend)
	}

	var b strings.Builder
	b.WriteString(saMarkerStart + "\n")
	b.WriteString("# Managed by `bravros secrets sa-token`. Do not edit between these markers.\n")
	b.WriteString("# Backend: " + string(spec.Backend) + "\n")
	b.WriteString(swarmGate + "\n")
	b.WriteString(lookup + "\n")
	b.WriteString(`export OP_SERVICE_ACCOUNT_TOKEN="$` + saExportVar + `"` + "\n")
	b.WriteString(saMarkerEnd + "\n")
	return b.String(), nil
}

// stripSAManaged removes any existing sa-token marker-fenced block from text.
// Mirrors stripManaged's EOF-safe scan.
func stripSAManaged(text string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	inBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if inBlock {
			if trimmed == saMarkerEnd {
				inBlock = false
			}
			continue
		}
		if trimmed == saMarkerStart {
			inBlock = true
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// composeSAZshenv returns the full new ~/.zshenv content given the existing
// content and the freshly-rendered block. The block is placed at the TOP (after
// any shebang-free leading content is preserved) so the swarm gate's early
// `return` fires before the rest of ~/.zshenv runs for swarm agents. We
// actually prepend the block so the gate is first.
func composeSAZshenv(existing, block string) string {
	cleaned := stripSAManaged(existing)
	cleaned = strings.TrimLeft(cleaned, "\n")
	if block == "" {
		// none backend: just the stripped body.
		out := strings.TrimRight(cleaned, "\n")
		if out == "" {
			return ""
		}
		return out + "\n"
	}
	var b strings.Builder
	b.WriteString(block)
	if strings.TrimSpace(cleaned) != "" {
		b.WriteString("\n")
		b.WriteString(strings.TrimRight(cleaned, "\n"))
		b.WriteString("\n")
	}
	return b.String()
}

// SATokenResult reports the outcome of a WriteSAToken call.
type SATokenResult struct {
	Backend         SATokenBackend `json:"backend"`
	ZshenvPath      string         `json:"zshenv_path"`
	KeychainService string         `json:"keychain_service,omitempty"`
	KeychainSeeded  bool           `json:"keychain_seeded,omitempty"`
	Wrote           bool           `json:"wrote"`
}

// WriteSAToken applies the spec to ~/.zshenv (or zshenvPath when overridden). It
// classifies the existing block (clobber guard), seeds the keychain when the
// keychain backend is chosen and a token was supplied, renders the block, and
// writes atomically. Pure logic lives in RenderSATokenBlock / composeSAZshenv;
// this is the IO shell around it.
func WriteSAToken(spec SATokenSpec, zshenvPath string) (*SATokenResult, error) {
	if zshenvPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cannot determine home dir: %w", err)
		}
		zshenvPath = filepath.Join(home, ".zshenv")
	}

	var existing string
	if data, err := os.ReadFile(zshenvPath); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("cannot read %s: %w", zshenvPath, err)
	}

	kind := classifyExisting(existing)
	res := &SATokenResult{Backend: spec.Backend, ZshenvPath: zshenvPath}

	// Seed the keychain BEFORE writing the lookup line so the first new shell
	// resolves a real value. Only when a token was provided (stdin).
	if spec.Backend == SABackendKeychain {
		service := spec.KeychainService
		if service == "" {
			service = DeriveKeychainSAService()
		}
		res.KeychainService = service
		if spec.Token != "" {
			if !securityAvailableFn() {
				return nil, fmt.Errorf("keychain SA-token requires macOS `security` (not available on this host)")
			}
			if err := keychainAddFn(service, keychainAccount(), spec.Token); err != nil {
				return nil, fmt.Errorf("seeding keychain SA token: %w", err)
			}
			res.KeychainSeeded = true
		}
	}

	block, err := RenderSATokenBlock(spec, kind)
	if err != nil {
		return nil, err
	}

	content := composeSAZshenv(existing, block)
	if err := writeRCAtomic(zshenvPath, content); err != nil {
		return nil, err
	}
	res.Wrote = true
	return res, nil
}
