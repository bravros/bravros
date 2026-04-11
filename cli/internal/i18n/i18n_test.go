package i18n

import (
	"os"
	"strings"
	"testing"
)

// reset resets locale to "en" after each test that changes it.
func reset() {
	SetLocale("en")
}

// ---------------------------------------------------------------------------
// T()
// ---------------------------------------------------------------------------

func TestT_KnownKeyEnglish(t *testing.T) {
	reset()
	got := T("audit.block_planning_mv")
	want := "Use 'git mv' instead of 'mv' for .planning/ files to preserve git history"
	if got != want {
		t.Errorf("T(audit.block_planning_mv) = %q, want %q", got, want)
	}
}

func TestT_UnknownKeyFallbackToKey(t *testing.T) {
	reset()
	key := "nonexistent.key.that.does.not.exist"
	got := T(key)
	if got != key {
		t.Errorf("T(unknown) = %q, want key itself %q", got, key)
	}
}

// ---------------------------------------------------------------------------
// Tf()
// ---------------------------------------------------------------------------

func TestTf_FormatsCorrectly(t *testing.T) {
	reset()
	got := Tf("audit.block_skill_read_first", "foo.tsx", "frontend-design")
	if !strings.Contains(got, "foo.tsx") {
		t.Errorf("Tf(audit.block_skill_read_first) = %q, want to contain 'foo.tsx'", got)
	}
	if !strings.Contains(got, "frontend-design") {
		t.Errorf("Tf(audit.block_skill_read_first) = %q, want to contain 'frontend-design'", got)
	}
}

// ---------------------------------------------------------------------------
// SetLocale()
// ---------------------------------------------------------------------------

func TestSetLocale_PortugueseBrazil(t *testing.T) {
	defer reset()
	SetLocale("pt-BR")
	got := T("audit.block_planning_mv")
	if got == "" {
		t.Fatal("expected non-empty pt-BR translation")
	}
	// pt-BR should differ from English
	en := allTranslations["en"]["audit.block_planning_mv"]
	ptBR := allTranslations["pt-BR"]["audit.block_planning_mv"]
	if ptBR == en {
		t.Error("pt-BR translation should differ from English for this key")
	}
	if got != ptBR {
		t.Errorf("T() after SetLocale('pt-BR') = %q, want %q", got, ptBR)
	}
}

func TestSetLocale_Spanish(t *testing.T) {
	defer reset()
	SetLocale("es")
	got := T("audit.prefix_stop")
	if got == "" {
		t.Fatal("expected non-empty es translation")
	}
	if !strings.Contains(got, "Detener") {
		t.Errorf("T(audit.prefix_stop) with es locale = %q, expected Spanish 'Detener'", got)
	}
}

func TestSetLocale_InvalidLocaleKeepsCurrent(t *testing.T) {
	reset()
	before := CurrentLocale()
	SetLocale("xx-invalid")
	after := CurrentLocale()
	if before != after {
		t.Errorf("SetLocale('xx-invalid') changed locale from %q to %q; should keep current", before, after)
	}
}

// ---------------------------------------------------------------------------
// DetectLocale()
// ---------------------------------------------------------------------------

func TestDetectLocale_DefaultEnglish(t *testing.T) {
	defer reset()

	// Run from temp dir with no .bravros.yml and no locale env vars
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	// Clear locale env vars temporarily
	origLC := os.Getenv("LC_ALL")
	origLANG := os.Getenv("LANG")
	os.Unsetenv("LC_ALL")
	os.Unsetenv("LANG")
	defer func() {
		if origLC != "" {
			os.Setenv("LC_ALL", origLC)
		}
		if origLANG != "" {
			os.Setenv("LANG", origLANG)
		}
	}()

	got := DetectLocale()
	if got != "en" {
		t.Errorf("DetectLocale() = %q, want 'en' (no config, no env)", got)
	}
}

func TestDetectLocale_FromBravrosYML(t *testing.T) {
	defer reset()

	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	// Write a .bravros.yml with language: pt-BR
	os.WriteFile(".bravros.yml", []byte("language: pt-BR\n"), 0644)

	// Clear env vars so only .bravros.yml is used
	origLC := os.Getenv("LC_ALL")
	origLANG := os.Getenv("LANG")
	os.Unsetenv("LC_ALL")
	os.Unsetenv("LANG")
	defer func() {
		if origLC != "" {
			os.Setenv("LC_ALL", origLC)
		}
		if origLANG != "" {
			os.Setenv("LANG", origLANG)
		}
	}()

	got := DetectLocale()
	if got != "pt-BR" {
		t.Errorf("DetectLocale() = %q, want 'pt-BR' (from .bravros.yml)", got)
	}
}

// ---------------------------------------------------------------------------
// CurrentLocale()
// ---------------------------------------------------------------------------

func TestCurrentLocale_ReturnsActiveLocale(t *testing.T) {
	defer reset()

	SetLocale("en")
	if got := CurrentLocale(); got != "en" {
		t.Errorf("CurrentLocale() = %q, want 'en'", got)
	}

	SetLocale("pt-BR")
	if got := CurrentLocale(); got != "pt-BR" {
		t.Errorf("CurrentLocale() = %q, want 'pt-BR'", got)
	}

	SetLocale("es")
	if got := CurrentLocale(); got != "es" {
		t.Errorf("CurrentLocale() = %q, want 'es'", got)
	}
}

// ---------------------------------------------------------------------------
// Key count parity
// ---------------------------------------------------------------------------

func TestLocaleKeyCountParity(t *testing.T) {
	enCount := len(allTranslations["en"])
	ptBRCount := len(allTranslations["pt-BR"])
	esCount := len(allTranslations["es"])

	if enCount == 0 {
		t.Fatal("English locale has no keys loaded")
	}
	if ptBRCount != enCount {
		t.Errorf("pt-BR has %d keys, en has %d; counts must match", ptBRCount, enCount)
	}
	if esCount != enCount {
		t.Errorf("es has %d keys, en has %d; counts must match", esCount, enCount)
	}
}
