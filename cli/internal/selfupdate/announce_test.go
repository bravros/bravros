package selfupdate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenderAnnouncement(t *testing.T) {
	cases := []struct {
		name     string
		template string
		language string
		version  string
		want     string
	}{
		{
			name:     "explicit template substitutes version",
			template: "update landed: {version}",
			language: "en",
			version:  "2.14.4",
			want:     "update landed: 2.14.4",
		},
		{
			name:     "leading v is stripped",
			template: "update landed: {version}",
			language: "en",
			version:  "v2.14.4",
			want:     "update landed: 2.14.4",
		},
		{
			name:     "explicit template beats language",
			template: "custom {version}",
			language: "pt-BR",
			version:  "1.0.0",
			want:     "custom 1.0.0",
		},
		{
			name:     "unknown language falls back to en",
			template: "",
			language: "fr-FR",
			version:  "1.0.0",
			want:     "bravros updated itself to version 1.0.0.",
		},
		{
			name:     "empty language falls back to en",
			template: "",
			language: "",
			version:  "1.0.0",
			want:     "bravros updated itself to version 1.0.0.",
		},
		{
			name:     "pt-BR built-in exact output",
			template: "",
			language: "pt-BR",
			version:  "v2.14.4",
			want:     "Nova versão do bravros instalada. Versão 2.14.4.",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RenderAnnouncement(c.template, c.language, c.version); got != c.want {
				t.Errorf("RenderAnnouncement(%q, %q, %q) = %q, want %q",
					c.template, c.language, c.version, got, c.want)
			}
		})
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("os.UserHomeDir unavailable: %v", err)
	}

	cases := []struct {
		name string
		path string
		want string
	}{
		{
			name: "expands a leading ~/",
			path: "~/scripts/announce.sh",
			want: filepath.Join(home, "scripts/announce.sh"),
		},
		{
			name: "absolute path unchanged",
			path: "/usr/local/bin/announce.sh",
			want: "/usr/local/bin/announce.sh",
		},
		{
			name: "relative path unchanged",
			path: "scripts/announce.sh",
			want: "scripts/announce.sh",
		},
		{
			name: "bare tilde unchanged",
			path: "~",
			want: "~",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ExpandHome(c.path); got != c.want {
				t.Errorf("ExpandHome(%q) = %q, want %q", c.path, got, c.want)
			}
		})
	}
}
