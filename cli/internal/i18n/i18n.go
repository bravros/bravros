package i18n

import (
	"embed"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed locales/*.yml
var localeFiles embed.FS

var (
	currentLocale  = "en"
	translations   map[string]string
	allTranslations map[string]map[string]string
)

func init() {
	allTranslations = make(map[string]map[string]string)
	loadLocales()
	translations = allTranslations["en"]
}

func loadLocales() {
	entries, err := localeFiles.ReadDir("locales")
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}
		lang := strings.TrimSuffix(entry.Name(), ".yml")
		data, err := localeFiles.ReadFile("locales/" + entry.Name())
		if err != nil {
			continue
		}
		var raw map[string]interface{}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			continue
		}
		flat := flattenMap(raw, "")
		allTranslations[lang] = flat
	}
}

// flattenMap converts a nested YAML map into dot-notation keys.
// If the value is already a flat string (dot-key format), it is kept as-is.
func flattenMap(m map[string]interface{}, prefix string) map[string]string {
	result := make(map[string]string)
	for k, v := range m {
		fullKey := k
		if prefix != "" {
			fullKey = prefix + "." + k
		}
		switch val := v.(type) {
		case string:
			result[fullKey] = val
		case map[string]interface{}:
			nested := flattenMap(val, fullKey)
			for nk, nv := range nested {
				result[nk] = nv
			}
		default:
			result[fullKey] = fmt.Sprintf("%v", val)
		}
	}
	return result
}

// SetLocale sets the current locale explicitly.
func SetLocale(lang string) {
	if t, ok := allTranslations[lang]; ok {
		currentLocale = lang
		translations = t
	}
	// If unknown lang, keep current locale
}

// DetectLocale reads locale from .bravros.yml → $LANG/$LC_ALL → default "en".
// Sets the locale automatically and returns the detected language.
func DetectLocale() string {
	// 1. Try .bravros.yml language field
	if lang := readBravrosYMLLanguage(); lang != "" {
		SetLocale(lang)
		return lang
	}

	// 2. Try $LANG / $LC_ALL environment variables
	for _, envVar := range []string{"LC_ALL", "LANG"} {
		val := os.Getenv(envVar)
		if val == "" {
			continue
		}
		lang := normalizeEnvLang(val)
		if lang != "" {
			SetLocale(lang)
			return lang
		}
	}

	// 3. Default to "en"
	SetLocale("en")
	return "en"
}

// readBravrosYMLLanguage attempts to read the language field from .bravros.yml
// in the current working directory. Returns empty string if not found.
func readBravrosYMLLanguage() string {
	data, err := os.ReadFile(".bravros.yml")
	if err != nil {
		return ""
	}

	var config map[string]interface{}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return ""
	}

	if lang, ok := config["language"]; ok {
		if s, ok := lang.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// normalizeEnvLang converts POSIX locale strings like "pt_BR.UTF-8" to BCP 47 "pt-BR".
// Returns empty string if the input cannot be parsed or is "C"/"POSIX".
func normalizeEnvLang(val string) string {
	// Strip encoding suffix (e.g. ".UTF-8")
	if idx := strings.Index(val, "."); idx != -1 {
		val = val[:idx]
	}
	// Strip modifier (e.g. "@euro")
	if idx := strings.Index(val, "@"); idx != -1 {
		val = val[:idx]
	}
	val = strings.TrimSpace(val)

	if val == "" || val == "C" || val == "POSIX" {
		return ""
	}

	// Convert underscore to hyphen: pt_BR → pt-BR
	val = strings.ReplaceAll(val, "_", "-")
	return val
}

// T returns the translated string for key in the current locale.
// Falls back to the key itself if the translation is not found.
func T(key string) string {
	if translations != nil {
		if s, ok := translations[key]; ok {
			return s
		}
	}
	// Fallback to English
	if en, ok := allTranslations["en"]; ok {
		if s, ok := en[key]; ok {
			return s
		}
	}
	// Last resort: return the key itself
	return key
}

// Tf returns a formatted translated string using fmt.Sprintf.
func Tf(key string, args ...interface{}) string {
	return fmt.Sprintf(T(key), args...)
}

// CurrentLocale returns the currently active locale code.
func CurrentLocale() string {
	return currentLocale
}
