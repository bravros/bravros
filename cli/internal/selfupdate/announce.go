package selfupdate

// Announcement text is what an unattended auto-update tells the operator was
// installed — spoken through an operator-supplied notifier command (the
// setup.json announce_command field). Rendering is pure: no I/O, no exec, so
// it can be tested without touching the filesystem or a shell. The caller
// that actually runs the notifier command lives elsewhere in this package.

import (
	"os"
	"path/filepath"
	"strings"
)

// Built-in announcement templates, keyed by language. Both are one sentence,
// ~20 words or fewer, and carry no origin clause — a binary swap is a
// machine-level event with no branch or project to disambiguate it (P-0020
// Closed decisions).
const (
	announceTemplateEN   = "bravros updated itself to version {version}."
	announceTemplatePtBR = "Nova versão do bravros instalada. Versão {version}."
)

// RenderAnnouncement renders the announcement text for an auto-update.
//
// template, when non-empty, is the operator-supplied override: "{version}"
// inside it is substituted and language is ignored. An empty template falls
// back to the built-in for language — "pt-BR" selects the Portuguese line
// (Alexa's voice), anything else, including an empty language, selects the
// English one.
//
// version is passed through the package's existing normalizeVersion first
// (adds a missing "v", maps "" to "unknown"), then the "v" is stripped:
// kaisser's fix, verbatim — Alexa reads "2.14.4" correctly but says "vê" for
// a leading "v".
func RenderAnnouncement(template, language, version string) string {
	t := template
	if t == "" {
		if language == "pt-BR" {
			t = announceTemplatePtBR
		} else {
			t = announceTemplateEN
		}
	}
	spoken := strings.TrimPrefix(normalizeVersion(version), "v")
	return strings.ReplaceAll(t, "{version}", spoken)
}

// ExpandHome expands a leading "~/" in path against os.UserHomeDir(). Any
// other input — an absolute path, a relative path, a bare "~", or a "~/..."
// path when the home directory cannot be resolved — is returned unchanged.
// Go's exec.Command does not expand "~" itself, so this must run before a
// notifier command built from an operator-supplied path is executed.
func ExpandHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}
