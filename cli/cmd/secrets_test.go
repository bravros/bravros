package cmd

import (
	"testing"

	"github.com/bravros/bravros/cli/internal/secrets"
)

func TestParseBackendArg(t *testing.T) {
	cases := []struct {
		in      string
		want    secrets.Backend
		wantErr bool
	}{
		{"", "", false}, // empty = auto-detect
		{"op", secrets.BackendOp, false},
		{"env", secrets.BackendEnv, false},
		{"none", secrets.BackendNone, false},
		{"keychain", secrets.BackendKeychain, false},
		{"OP", "", true},      // case-sensitive
		{"1password", "", true},
		{"bogus", "", true},
	}
	for _, c := range cases {
		got, err := parseBackendArg(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("parseBackendArg(%q) err=%v wantErr=%v", c.in, err, c.wantErr)
			continue
		}
		if !c.wantErr && got != c.want {
			t.Errorf("parseBackendArg(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
