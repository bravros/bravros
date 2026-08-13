package plan

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var planIDPrefix = regexp.MustCompile(`^(?:[A-Z]-)?(\d{4})-`)

// findPlanByID scans planningDir (non-recursive) for a plan file whose filename
// starts with the given 4-digit ID. Returns "" if not found. Prefers `-todo.md`
// files over `-complete.md` when both exist.
func findPlanByID(planningDir, id string) string {
	// Normalize to 4-digit zero-padded.
	for len(id) < 4 {
		id = "0" + id
	}
	entries, err := os.ReadDir(planningDir)
	if err != nil {
		return ""
	}
	var todoMatch, completeMatch string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		m := planIDPrefix.FindStringSubmatch(name)
		if m == nil || m[1] != id {
			continue
		}
		full := filepath.Join(planningDir, name)
		if strings.HasSuffix(name, "-todo.md") {
			todoMatch = full
		} else if strings.HasSuffix(name, "-complete.md") {
			completeMatch = full
		} else if todoMatch == "" {
			todoMatch = full
		}
	}
	if todoMatch != "" {
		return todoMatch
	}
	return completeMatch
}
