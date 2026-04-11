package stack

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// ParseComposerLock reads composer.lock and returns a map of short package name → version
// for a set of well-known Laravel ecosystem packages.
// It returns an empty map (not an error) when the file does not exist.
func ParseComposerLock(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}

	var lock struct {
		Packages []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"packages"`
		PackagesDev []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"packages-dev"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, err
	}

	interesting := map[string]string{
		"laravel/framework":         "laravel",
		"livewire/livewire":         "livewire",
		"filament/filament":         "filament",
		"pestphp/pest":              "pest",
		"laravel/sanctum":           "sanctum",
		"spatie/laravel-permission": "spatie-permission",
	}

	result := make(map[string]string)
	for _, pkg := range lock.Packages {
		if short, ok := interesting[pkg.Name]; ok {
			result[short] = strings.TrimPrefix(pkg.Version, "v")
		}
	}
	for _, pkg := range lock.PackagesDev {
		if short, ok := interesting[pkg.Name]; ok {
			result[short] = strings.TrimPrefix(pkg.Version, "v")
		}
	}
	return result, nil
}

// ParsePackageLock reads package-lock.json and returns a map of package name → exact version
// for a set of well-known Node ecosystem packages.
// Supports both npm lockfile v2/v3 (packages map) and v1 (dependencies map).
// It returns an empty map (not an error) when the file does not exist.
func ParsePackageLock(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}

	// Use a generic map so we can handle both formats without two full structs.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	interesting := map[string]bool{
		"next":                  true,
		"react":                 true,
		"typescript":            true,
		"tailwindcss":           true,
		"daisyui":               true,
		"@tanstack/react-query": true,
		"prisma":                true,
		"vitest":                true,
		"jest":                  true,
	}

	result := make(map[string]string)

	// Detect lockfile version to avoid v1 fallback on v2/v3 files
	var lockfileVersion int
	if vRaw, ok := raw["lockfileVersion"]; ok {
		_ = json.Unmarshal(vRaw, &lockfileVersion)
	}

	// v2/v3 format: packages["node_modules/<name>"] = {version: "..."}
	if packagesRaw, ok := raw["packages"]; ok && lockfileVersion >= 2 {
		var packages map[string]struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(packagesRaw, &packages); err != nil {
			return nil, err
		}
		for key, pkg := range packages {
			// key is "node_modules/react" or "node_modules/@tanstack/react-query"
			name := strings.TrimPrefix(key, "node_modules/")
			if interesting[name] {
				result[name] = pkg.Version
			}
		}
		return result, nil
	}

	// v1 format (or unknown version): dependencies["react"] = {version: "..."}
	if depsRaw, ok := raw["dependencies"]; ok {
		var deps map[string]struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(depsRaw, &deps); err != nil {
			return nil, err
		}
		for name, dep := range deps {
			if interesting[name] {
				result[name] = dep.Version
			}
		}
	}

	return result, nil
}

// ParseGoMod reads go.mod and returns a map of short module name → version.
// The Go toolchain version is stored under the key "go".
// It returns an empty map (not an error) when the file does not exist.
func ParseGoMod(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}

	// Map of full module prefix → short name.
	// We use prefix matching so "github.com/go-chi/chi/v5" → "chi".
	interesting := map[string]string{
		"github.com/go-chi/chi":       "chi",
		"github.com/gin-gonic/gin":    "gin",
		"github.com/labstack/echo":    "echo",
		"github.com/jmoiron/sqlx":     "sqlx",
		"gorm.io/gorm":                "gorm",
		"github.com/stretchr/testify": "testify",
	}

	result := make(map[string]string)
	inRequire := false

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Capture Go version: "go 1.22.0"
		if strings.HasPrefix(line, "go ") && !inRequire {
			result["go"] = strings.TrimPrefix(line, "go ")
			continue
		}

		// Track require blocks
		if line == "require (" {
			inRequire = true
			continue
		}
		if inRequire && line == ")" {
			inRequire = false
			continue
		}

		// Single-line require: "require github.com/foo/bar v1.0.0"
		if strings.HasPrefix(line, "require ") {
			line = strings.TrimPrefix(line, "require ")
		}

		if inRequire || strings.HasPrefix(line, "github.com/") || strings.HasPrefix(line, "gorm.io/") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				modulePath := parts[0]
				version := strings.TrimSuffix(parts[1], " // indirect")
				for prefix, short := range interesting {
					if modulePath == prefix || strings.HasPrefix(modulePath, prefix+"/") {
						result[short] = strings.TrimPrefix(version, "v")
						break
					}
				}
			}
		}
	}

	return result, nil
}

// ParsePyProject reads pyproject.toml (and requirements.txt as fallback) and returns
// a map of package name → version for a set of well-known Python ecosystem packages.
// Uses simple line-by-line text parsing — no full TOML parser required.
// It returns an empty map (not an error) when neither file exists.
func ParsePyProject(path string) (map[string]string, error) {
	result := make(map[string]string)

	interesting := []string{"django", "flask", "fastapi", "pytest", "sqlalchemy", "pydantic"}

	// Try pyproject.toml first.
	if data, err := os.ReadFile(path); err == nil {
		lower := strings.ToLower(string(data))
		for _, line := range strings.Split(lower, "\n") {
			line = strings.TrimSpace(line)

			// Capture requires-python = ">=3.11"
			if strings.HasPrefix(line, "requires-python") {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					result["python"] = strings.Trim(strings.TrimSpace(parts[1]), `"'>=~^`)
				}
				continue
			}

			// Dependency lines: `"django>=4.2"`, `django = "^4.2"`, `"django==4.2.0"`
			for _, pkg := range interesting {
				if strings.Contains(line, pkg) {
					version := extractPyVersion(line, pkg)
					if version != "" {
						result[pkg] = version
					}
				}
			}
		}
	}

	// Fallback / supplement: requirements.txt
	reqPath := strings.Replace(path, "pyproject.toml", "requirements.txt", 1)
	if data, err := os.ReadFile(reqPath); err == nil {
		for _, line := range strings.Split(strings.ToLower(string(data)), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			for _, pkg := range interesting {
				if _, already := result[pkg]; already {
					continue
				}
				if strings.HasPrefix(line, pkg) {
					version := extractPyVersion(line, pkg)
					if version != "" {
						result[pkg] = version
					}
				}
			}
		}
	}

	return result, nil
}

// extractPyVersion parses a version string from a dependency line.
// Handles formats: "django>=4.2", "django==4.2.0", `"django>=4.2"`, `django = "^4.2"`.
func extractPyVersion(line, pkg string) string {
	// Strip surrounding quotes and whitespace
	line = strings.Trim(line, `"' `)

	// Find where the package name ends
	idx := strings.Index(line, pkg)
	if idx < 0 {
		return ""
	}
	rest := line[idx+len(pkg):]
	rest = strings.TrimSpace(rest)

	// Handle `= "..."` (TOML style)
	if strings.HasPrefix(rest, "=") {
		rest = strings.TrimPrefix(rest, "=")
		rest = strings.Trim(strings.TrimSpace(rest), `"'^~>=`)
	} else {
		// PEP 508 style: >=4.2, ==4.2.0
		rest = strings.TrimLeft(rest, `><=~^!`)
		rest = strings.Trim(rest, `"' `)
	}

	// Take only the version part (stop at comma, space, bracket)
	for _, sep := range []string{",", " ", ";", "]"} {
		if i := strings.Index(rest, sep); i >= 0 {
			rest = rest[:i]
		}
	}

	return rest
}
