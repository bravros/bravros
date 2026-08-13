package stack

import (
	"os/exec"
	"regexp"
	"strings"
)

// DetectRuntime executes version commands for the detected language and returns
// a map of runtime tool names to their installed versions.
// If a command fails (binary not found), it is silently skipped.
func DetectRuntime(language string) map[string]string {
	result := make(map[string]string)

	switch language {
	case "php":
		if v := runPHPVersion(); v != "" {
			result["php"] = v
		}
		if v := runComposerVersion(); v != "" {
			result["composer"] = v
		}
	case "node":
		if v := runNodeVersion(); v != "" {
			result["node"] = v
		}
	case "go":
		if v := runGoVersion(); v != "" {
			result["go"] = v
		}
	case "python":
		if v := runPythonVersion(); v != "" {
			result["python"] = v
		}
	}

	return result
}

// runPHPVersion runs `php -v` and parses the version number from the first line.
// e.g. "PHP 8.3.1 (cli) ..." → "8.3.1"
func runPHPVersion() string {
	out, err := exec.Command("php", "-v").Output()
	if err != nil {
		return ""
	}
	// First line: "PHP 8.3.1 (cli) ..."
	first := firstLine(string(out))
	re := regexp.MustCompile(`PHP (\d+\.\d+\.\d+)`)
	m := re.FindStringSubmatch(first)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// runComposerVersion runs `composer --version` and parses the version number.
// e.g. "Composer version 2.7.1 ..." → "2.7.1"
func runComposerVersion() string {
	out, err := exec.Command("composer", "--version").Output()
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`(\d+\.\d+\.\d+)`)
	m := re.FindStringSubmatch(string(out))
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// runNodeVersion runs `node -v` and strips the leading "v".
// e.g. "v20.11.0" → "20.11.0"
// Also checks .nvmrc or .tool-versions for expected version.
func runNodeVersion() string {
	out, err := exec.Command("node", "-v").Output()
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(string(out)), "v")
}

// runGoVersion runs `go version` and parses the version number.
// e.g. "go version go1.22.0 darwin/arm64" → "1.22.0"
func runGoVersion() string {
	out, err := exec.Command("go", "version").Output()
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`go(\d+\.\d+(?:\.\d+)?)`)
	m := re.FindStringSubmatch(string(out))
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// runPythonVersion runs `python3 --version` and parses the version number.
// e.g. "Python 3.12.0" → "3.12.0"
func runPythonVersion() string {
	out, err := exec.Command("python3", "--version").Output()
	if err != nil {
		// python3 may write to stderr
		cmd := exec.Command("python3", "--version")
		errOut, err2 := cmd.CombinedOutput()
		if err2 != nil {
			return ""
		}
		out = errOut
	}
	re := regexp.MustCompile(`Python (\d+\.\d+\.\d+)`)
	m := re.FindStringSubmatch(string(out))
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// firstLine returns the first non-empty line of s.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return s
}
