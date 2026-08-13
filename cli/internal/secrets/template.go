package secrets

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// TemplateOpts configures the export-template operation.
type TemplateOpts struct {
	OutputFile string    // default: ~/.config/bravros/secrets-template.env
	PrintOnly  bool      // write to stdout instead of file
	Output     io.Writer // for testing
}

// ExportTemplate writes a template env file with one blank export per known secret.
// Non-1Password users can fill in the values manually.
func ExportTemplate(opts TemplateOpts) (string, error) {
	var sb strings.Builder
	sb.WriteString("# bravros secrets template\n")
	sb.WriteString("# Fill in the values below and source this file in your shell RC.\n\n")

	for _, s := range Registry() {
		sb.WriteString(fmt.Sprintf("export %s=\"\"\n", s.EnvVar))
	}

	content := sb.String()

	if opts.PrintOnly {
		out := opts.Output
		if out == nil {
			out = os.Stdout
		}
		fmt.Fprint(out, content)
		return "", nil
	}

	outFile := opts.OutputFile
	if outFile == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine home dir: %w", err)
		}
		outFile = filepath.Join(home, ".config", "bravros", "secrets-template.env")
	}

	if err := os.MkdirAll(filepath.Dir(outFile), 0755); err != nil {
		return "", fmt.Errorf("cannot create dir for template: %w", err)
	}
	if err := os.WriteFile(outFile, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("cannot write template: %w", err)
	}
	return outFile, nil
}
