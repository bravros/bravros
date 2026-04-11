package plan

import (
	"testing"
)

const taskTestPlanContent = `---
title: test plan
status: in-progress
---

## Phases

### Phase 1: setup

- [x] [H] Create project structure
- [x] [S] Set up CI pipeline
- [ ] [H] Add README

### Phase 2: implementation

- [x] [O] Design architecture
- [ ] [S] Implement core logic
- [ ] [H] Add logging

## Notes

Some notes here.
`

func TestParseTasks(t *testing.T) {
	tasks := ParseTasks(taskTestPlanContent)

	if len(tasks) != 6 {
		t.Fatalf("expected 6 tasks, got %d", len(tasks))
	}

	// Verify first task
	if tasks[0].Text != "[H] Create project structure" {
		t.Errorf("unexpected text: %q", tasks[0].Text)
	}
	if !tasks[0].Checked {
		t.Error("expected task 0 to be checked")
	}
	if tasks[0].Phase != "### Phase 1: setup" {
		t.Errorf("unexpected phase: %q", tasks[0].Phase)
	}

	// Verify unchecked task
	if tasks[2].Checked {
		t.Error("expected task 2 to be unchecked")
	}
	if tasks[2].Text != "[H] Add README" {
		t.Errorf("unexpected text: %q", tasks[2].Text)
	}

	// Verify phase 2 task
	if tasks[3].Phase != "### Phase 2: implementation" {
		t.Errorf("unexpected phase for task 3: %q", tasks[3].Phase)
	}

	// Count checked
	checked := 0
	for _, task := range tasks {
		if task.Checked {
			checked++
		}
	}
	if checked != 3 {
		t.Errorf("expected 3 checked tasks, got %d", checked)
	}
}

func TestParseTasksWithMarkers(t *testing.T) {
	tasks := ParseTasks(taskTestPlanContent)

	expected := []struct {
		marker string
		text   string
	}{
		{"H", "[H] Create project structure"},
		{"S", "[S] Set up CI pipeline"},
		{"H", "[H] Add README"},
		{"O", "[O] Design architecture"},
		{"S", "[S] Implement core logic"},
		{"H", "[H] Add logging"},
	}

	if len(tasks) != len(expected) {
		t.Fatalf("expected %d tasks, got %d", len(expected), len(tasks))
	}

	for i, e := range expected {
		if tasks[i].Marker != e.marker {
			t.Errorf("task %d: expected marker %q, got %q", i, e.marker, tasks[i].Marker)
		}
	}
}

func TestParseTasksNoMarker(t *testing.T) {
	content := `### Phase 1: test
- [x] Do something without a marker
- [ ] Another plain task
`
	tasks := ParseTasks(content)
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[0].Marker != "" {
		t.Errorf("expected empty marker, got %q", tasks[0].Marker)
	}
	if tasks[1].Marker != "" {
		t.Errorf("expected empty marker, got %q", tasks[1].Marker)
	}
}

func TestParseTasksCaseInsensitive(t *testing.T) {
	content := `### Phase 1: test
- [X] Uppercase X task
- [x] Lowercase x task
- [ ] Unchecked task
`
	tasks := ParseTasks(content)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
	if !tasks[0].Checked {
		t.Error("uppercase X should be checked")
	}
	if !tasks[1].Checked {
		t.Error("lowercase x should be checked")
	}
	if tasks[2].Checked {
		t.Error("space should not be checked")
	}
}

func TestTaskDiffDeletedTasks(t *testing.T) {
	baseline := []TaskLine{
		{Text: "[H] Task A", Checked: false, Phase: "Phase 1"},
		{Text: "[S] Task B", Checked: false, Phase: "Phase 1"},
		{Text: "[H] Task C", Checked: false, Phase: "Phase 2"},
	}
	current := []TaskLine{
		{Text: "[H] Task A", Checked: false, Phase: "Phase 1"},
		{Text: "[H] Task C", Checked: false, Phase: "Phase 2"},
	}

	result := computeDiff(baseline, current)

	if result.DeletedCount != 1 {
		t.Errorf("expected 1 deleted, got %d", result.DeletedCount)
	}
	if result.Deleted[0].Text != "[S] Task B" {
		t.Errorf("expected deleted task B, got %q", result.Deleted[0].Text)
	}
	if result.AddedCount != 0 {
		t.Errorf("expected 0 added, got %d", result.AddedCount)
	}
}

func TestTaskDiffAddedTasks(t *testing.T) {
	baseline := []TaskLine{
		{Text: "[H] Task A", Checked: false},
	}
	current := []TaskLine{
		{Text: "[H] Task A", Checked: false},
		{Text: "[S] Task B", Checked: false},
		{Text: "[O] Task C", Checked: false},
	}

	result := computeDiff(baseline, current)

	if result.AddedCount != 2 {
		t.Errorf("expected 2 added, got %d", result.AddedCount)
	}
	if result.DeletedCount != 0 {
		t.Errorf("expected 0 deleted, got %d", result.DeletedCount)
	}
}

func TestTaskDiffModifiedTasks(t *testing.T) {
	baseline := []TaskLine{
		{Text: "[H] Task A", Checked: false},
		{Text: "[S] Task B", Checked: true},
	}
	current := []TaskLine{
		{Text: "[H] Task A", Checked: true},  // was unchecked, now checked
		{Text: "[S] Task B", Checked: false}, // was checked, now unchecked
	}

	result := computeDiff(baseline, current)

	if result.ModifiedCount != 2 {
		t.Errorf("expected 2 modified, got %d", result.ModifiedCount)
	}
	if result.DeletedCount != 0 {
		t.Errorf("expected 0 deleted, got %d", result.DeletedCount)
	}
	if result.AddedCount != 0 {
		t.Errorf("expected 0 added, got %d", result.AddedCount)
	}
}

func TestTaskDiffNoChanges(t *testing.T) {
	tasks := []TaskLine{
		{Text: "[H] Task A", Checked: true},
		{Text: "[S] Task B", Checked: false},
	}

	result := computeDiff(tasks, tasks)

	if result.DeletedCount != 0 {
		t.Errorf("expected 0 deleted, got %d", result.DeletedCount)
	}
	if result.AddedCount != 0 {
		t.Errorf("expected 0 added, got %d", result.AddedCount)
	}
	if result.ModifiedCount != 0 {
		t.Errorf("expected 0 modified, got %d", result.ModifiedCount)
	}
}

func TestTaskDiffMixed(t *testing.T) {
	baseline := []TaskLine{
		{Text: "[H] Keep same", Checked: false},
		{Text: "[S] Will be deleted", Checked: false},
		{Text: "[O] Will be modified", Checked: false},
	}
	current := []TaskLine{
		{Text: "[H] Keep same", Checked: false},
		{Text: "[O] Will be modified", Checked: true}, // checked status changed
		{Text: "[H] Brand new task", Checked: false},
	}

	result := computeDiff(baseline, current)

	if result.DeletedCount != 1 {
		t.Errorf("expected 1 deleted, got %d", result.DeletedCount)
	}
	if result.AddedCount != 1 {
		t.Errorf("expected 1 added, got %d", result.AddedCount)
	}
	if result.ModifiedCount != 1 {
		t.Errorf("expected 1 modified, got %d", result.ModifiedCount)
	}
}

func TestParseTasksLineNumbers(t *testing.T) {
	content := `line 1
line 2
### Phase 1: test
- [x] First task
line 5
- [ ] Second task
`
	tasks := ParseTasks(content)
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[0].Line != 4 {
		t.Errorf("expected line 4, got %d", tasks[0].Line)
	}
	if tasks[1].Line != 6 {
		t.Errorf("expected line 6, got %d", tasks[1].Line)
	}
}

func TestParseTasksEmptyContent(t *testing.T) {
	tasks := ParseTasks("")
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks from empty content, got %d", len(tasks))
	}
}

func TestParseTasksNoPhase(t *testing.T) {
	content := `- [x] Orphan task with no phase header
`
	tasks := ParseTasks(content)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Phase != "" {
		t.Errorf("expected empty phase, got %q", tasks[0].Phase)
	}
}
