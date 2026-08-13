// Package secrets implements the bravros secrets bootstrap and export-template
// verbs (B-0118).
package secrets

// KnownSecret describes a managed secret and how to reference it in 1Password
// (op backend) or the macOS login keychain (keychain backend).
//
// EnvVar / OPItem / OPField are the single source of truth for the op backend:
// bootstrap.go renders every backend's shell from these fields, and Resolve()
// (resolve.go) reads the same OPItem/OPField at runtime. They MUST match the
// real 1Password refs that install.sh reads — see the STEP-0 reconciliation
// note below.
//
// KeychainService / KeychainAccount are OPTIONAL, explicit fields for the
// keychain backend. They are deliberately NOT overloaded onto OPItem: a
// keychain service name is generic toolkit data, not an owner-specific
// 1Password item, so the two name spaces stay independent. When unset they
// default (see KeychainServiceFor / KeychainAccountFor) to a fixed generic
// service (`bravros-secrets`) keyed by account = EnvVar.
type KnownSecret struct {
	EnvVar  string // e.g. FIRECRAWL_API_KEY
	OPItem  string // 1Password item name (must match install.sh's `op read` refs)
	OPField string // 1Password field label (default: "credential")

	// KeychainService overrides the macOS keychain service name for this secret.
	// Empty → defaultKeychainService ("bravros-secrets"). Never an owner-specific
	// op vault/item — purely a generic, machine-local keychain bucket name.
	KeychainService string
	// KeychainAccount overrides the macOS keychain account for this secret.
	// Empty → the secret's EnvVar.
	KeychainAccount string
}

// defaultKeychainService is the fixed, generic keychain service used when a
// KnownSecret does not pin its own KeychainService. It is intentionally NOT
// machine-derived and NOT owner-specific — registered per-secret values are
// disambiguated by account (the EnvVar) within this one service bucket.
const defaultKeychainService = "bravros-secrets"

// KeychainServiceFor returns the keychain service name for a registered secret:
// the explicit KeychainService when set, else the generic default.
func KeychainServiceFor(s KnownSecret) string {
	if s.KeychainService != "" {
		return s.KeychainService
	}
	return defaultKeychainService
}

// KeychainAccountFor returns the keychain account for a registered secret: the
// explicit KeychainAccount when set, else the EnvVar (the secret's own name).
func KeychainAccountFor(s KnownSecret) string {
	if s.KeychainAccount != "" {
		return s.KeychainAccount
	}
	return s.EnvVar
}

// keychainCoords returns the (service, account) pair for an arbitrary env var,
// consulting the registry for an explicit override and falling back to the
// generic default service keyed by the var name. Used by Resolve and the
// keychain Set path so a non-registered var still resolves predictably.
func keychainCoords(envVar string) (service, account string) {
	for _, s := range Registry() {
		if s.EnvVar == envVar {
			return KeychainServiceFor(s), KeychainAccountFor(s)
		}
	}
	return defaultKeychainService, envVar
}

// Registry returns the list of all known bravros secrets.
//
// STEP-0 DATA-BUG RECONCILIATION (B-0322): the previous registry declared
// OPItem "firecrawl-api-key"/field "credential" and "hass-token"/field
// "credential". Those refs DID NOT EXIST in 1Password — install.sh actually
// reads:
//
//	Firecrawl: op read "op://ClaudeCode/Firecrawl API/credencial"   (Portuguese field)
//	HA:        op item get "Home Assistant - Long Lived Token" --fields label=password
//
// With the old values, an op-backend bravros_secret() call would have queried a
// non-existent ref and silently resolved empty. The OPItem/OPField below now
// mirror install.sh exactly, so op-backend reads actually work. (Note the old
// eager ShellLine field is gone entirely — bootstrap.go renders self-gating
// bravros_secret calls from EnvVar/OPItem/OPField instead.)
func Registry() []KnownSecret {
	return []KnownSecret{
		{
			EnvVar:  "FIRECRAWL_API_KEY",
			OPItem:  "Firecrawl API",
			OPField: "credencial", // Portuguese field label — matches install.sh
		},
		{
			EnvVar:  "HASS_TOKEN",
			OPItem:  "Home Assistant - Long Lived Token",
			OPField: "password",
		},
	}
}
