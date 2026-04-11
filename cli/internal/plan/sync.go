package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	phaseHeaderRe = regexp.MustCompile(`(?m)^### Phase \d+`)
	h2Re          = regexp.MustCompile(`(?m)^## `)
	checkedRe     = regexp.MustCompile(`(?i)- \[x\]`)
	uncheckedRe   = regexp.MustCompile(`- \[ \]`)
	sessionNewRe  = regexp.MustCompile(`(?m)^> - ` + "`" + `([a-f0-9-]+)` + "`" + `\s+(\S+ \S+)\s*-\s*(.+?)(?:\s*—\s*` + "`" + `claude -r [^` + "`" + `]+` + "`" + `)?$`)
	sessionOldRe  = regexp.MustCompile(`(?m)^> - Session \d+: (.+)$`)
)

// SyncResult holds the output of a sync operation.
type SyncResult struct {
	TasksDone   int
	TasksTotal  int
	PhasesDone  int
	PhasesTotal int
	Sessions    int
	Finished    bool
	PR          string
}

// SyncPlanFile syncs frontmatter counts from checkboxes in the plan body.
func SyncPlanFile(planFile string, finish bool, prNumber string) (*SyncResult, error) {
	if planFile == "" {
		// Auto-detect
		matches, _ := filepath.Glob(".planning/*-todo.md")
		if len(matches) == 0 {
			return nil, fmt.Errorf("no plan file found")
		}
		planFile = matches[0]
	}

	content, err := os.ReadFile(planFile)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", planFile, err)
	}

	text := string(content)
	parts := strings.SplitN(text, "---", 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid frontmatter in %s", planFile)
	}

	// Parse YAML frontmatter into ordered map
	var fm yaml.Node
	if err := yaml.Unmarshal([]byte(parts[1]), &fm); err != nil {
		return nil, fmt.Errorf("YAML parse error: %w", err)
	}

	// Use a simpler map for manipulation
	var fmMap map[string]interface{}
	if err := yaml.Unmarshal([]byte(parts[1]), &fmMap); err != nil {
		return nil, fmt.Errorf("YAML parse error: %w", err)
	}
	if fmMap == nil {
		fmMap = make(map[string]interface{})
	}

	body := parts[2]

	// Find phases section boundary
	phaseMatches := phaseHeaderRe.FindAllStringIndex(body, -1)
	phasesEnd := len(body)
	if len(phaseMatches) > 0 {
		afterLastPhase := phaseMatches[len(phaseMatches)-1][1]
		remaining := body[afterLastPhase:]
		nextH2 := h2Re.FindStringIndex(remaining)
		if nextH2 != nil {
			phasesEnd = afterLastPhase + nextH2[0]
		}
	}
	phasesBody := body[:phasesEnd]

	// Count tasks (scoped to phases section)
	tasksDone := len(checkedRe.FindAllString(phasesBody, -1))
	tasksOpen := len(uncheckedRe.FindAllString(phasesBody, -1))
	tasksTotal := tasksDone + tasksOpen

	fmMap["tasks_done"] = tasksDone
	fmMap["tasks_total"] = tasksTotal

	// Count phases
	phasesTotal := len(phaseMatches)
	phasesDone := 0
	for i, match := range phaseMatches {
		start := match[0]
		end := phasesEnd
		if i+1 < len(phaseMatches) {
			end = phaseMatches[i+1][0]
		}
		phaseBody := body[start:end]
		done := len(checkedRe.FindAllString(phaseBody, -1))
		open := len(uncheckedRe.FindAllString(phaseBody, -1))
		if done > 0 && open == 0 {
			phasesDone++
		}
	}

	fmMap["phases_done"] = phasesDone
	fmMap["phases_total"] = phasesTotal

	// Parse sessions
	newSessions := sessionNewRe.FindAllStringSubmatch(body, -1)
	if len(newSessions) > 0 {
		sessions := make([]map[string]string, 0, len(newSessions))
		for _, m := range newSessions {
			sessions = append(sessions, map[string]string{
				"id":   m[1],
				"date": m[2],
				"note": strings.TrimSpace(m[3]),
			})
		}
		fmMap["sessions"] = sessions
	} else {
		oldSessions := sessionOldRe.FindAllStringSubmatch(body, -1)
		if len(oldSessions) > 0 {
			sessions := make([]string, 0, len(oldSessions))
			for _, m := range oldSessions {
				sessions = append(sessions, m[1])
			}
			fmMap["sessions"] = sessions
		}
	}

	// Finish mode
	if finish {
		fmMap["status"] = "completed"
		fmMap["completed"] = time.Now().Format("2006-01-02")
		if prNumber != "" {
			// Try to parse as int
			var prVal interface{} = prNumber
			var n int
			if cnt, _ := fmt.Sscanf(prNumber, "%d", &n); cnt == 1 {
				prVal = n
			}
			fmMap["pr"] = prVal
		}
	}

	// Write back
	yamlBytes, err := yaml.Marshal(fmMap)
	if err != nil {
		return nil, fmt.Errorf("YAML marshal error: %w", err)
	}

	output := "---\n" + string(yamlBytes) + "---" + body
	if err := os.WriteFile(planFile, []byte(output), 0644); err != nil {
		return nil, fmt.Errorf("write error: %w", err)
	}

	sessCount := 0
	if s, ok := fmMap["sessions"]; ok {
		switch v := s.(type) {
		case []map[string]string:
			sessCount = len(v)
		case []string:
			sessCount = len(v)
		case []interface{}:
			sessCount = len(v)
		}
	}

	return &SyncResult{
		TasksDone:   tasksDone,
		TasksTotal:  tasksTotal,
		PhasesDone:  phasesDone,
		PhasesTotal: phasesTotal,
		Sessions:    sessCount,
		Finished:    finish,
		PR:          prNumber,
	}, nil
}
