package plan

import (
	"os"
	"sync"
	"testing"
)

// setupBatchProgressDir creates a temp dir with a .planning/ subdir,
// chdirs into it, and returns a cleanup func that restores the original dir.
func setupBatchProgressDir(t *testing.T) func() {
	t.Helper()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir to tmp: %v", err)
	}
	if err := os.Mkdir(".planning", 0o755); err != nil {
		t.Fatalf("mkdir .planning: %v", err)
	}
	return func() {
		if err := os.Chdir(origDir); err != nil {
			t.Logf("warning: failed to restore cwd: %v", err)
		}
	}
}

func TestBatchProgressWriteRead(t *testing.T) {
	cleanup := setupBatchProgressDir(t)
	defer cleanup()

	if err := WriteBatchProgress(1, "0060", "executing"); err != nil {
		t.Fatalf("write 0060: %v", err)
	}
	if err := WriteBatchProgress(1, "0061", "queued"); err != nil {
		t.Fatalf("write 0061: %v", err)
	}

	bp, err := ReadBatchProgress()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(bp.Rounds) != 1 {
		t.Fatalf("expected 1 round, got %d", len(bp.Rounds))
	}
	if bp.Rounds[0].Round != 1 {
		t.Errorf("expected round 1, got %d", bp.Rounds[0].Round)
	}
	if len(bp.Rounds[0].Plans) != 2 {
		t.Fatalf("expected 2 plans, got %d", len(bp.Rounds[0].Plans))
	}

	found := map[string]string{}
	for _, pe := range bp.Rounds[0].Plans {
		found[pe.Plan] = pe.State
	}
	if found["0060"] != "executing" {
		t.Errorf("0060: expected executing, got %q", found["0060"])
	}
	if found["0061"] != "queued" {
		t.Errorf("0061: expected queued, got %q", found["0061"])
	}
}

func TestBatchProgressUpdate(t *testing.T) {
	cleanup := setupBatchProgressDir(t)
	defer cleanup()

	if err := WriteBatchProgress(1, "0060", "executing"); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := WriteBatchProgress(1, "0060", "merged"); err != nil {
		t.Fatalf("second write: %v", err)
	}

	bp, err := ReadBatchProgress()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(bp.Rounds) != 1 {
		t.Fatalf("expected 1 round, got %d", len(bp.Rounds))
	}
	if len(bp.Rounds[0].Plans) != 1 {
		t.Fatalf("expected 1 plan entry (update, not append), got %d", len(bp.Rounds[0].Plans))
	}
	if bp.Rounds[0].Plans[0].State != "merged" {
		t.Errorf("expected state merged, got %q", bp.Rounds[0].Plans[0].State)
	}
}

func TestBatchProgressInvalidState(t *testing.T) {
	cleanup := setupBatchProgressDir(t)
	defer cleanup()

	err := WriteBatchProgress(1, "0060", "bogus")
	if err == nil {
		t.Fatal("expected error for invalid state, got nil")
	}
	if msg := err.Error(); len(msg) == 0 {
		t.Fatal("error message is empty")
	}
	// Error must mention "valid" to guide the user.
	found := false
	for i := 0; i+4 <= len(err.Error()); i++ {
		if err.Error()[i:i+5] == "valid" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("error %q does not contain the word \"valid\"", err.Error())
	}
}

func TestBatchProgressClear(t *testing.T) {
	cleanup := setupBatchProgressDir(t)
	defer cleanup()

	if err := WriteBatchProgress(1, "0060", "queued"); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Verify file exists.
	if _, err := os.Stat(batchProgressFile); err != nil {
		t.Fatalf("file should exist after write: %v", err)
	}

	if err := ClearBatchProgress(); err != nil {
		t.Fatalf("first clear: %v", err)
	}
	// File should be gone.
	if _, err := os.Stat(batchProgressFile); !os.IsNotExist(err) {
		t.Errorf("file should not exist after clear (err=%v)", err)
	}

	// Second clear should be a no-op (nil error).
	if err := ClearBatchProgress(); err != nil {
		t.Fatalf("second clear (no-op) returned error: %v", err)
	}
}

func TestBatchProgressConcurrent(t *testing.T) {
	cleanup := setupBatchProgressDir(t)
	defer cleanup()

	plans := []string{"p1", "p2", "p3", "p4", "p5"}
	var wg sync.WaitGroup
	errs := make([]error, len(plans))

	for i, p := range plans {
		wg.Add(1)
		go func(idx int, plan string) {
			defer wg.Done()
			errs[idx] = WriteBatchProgress(1, plan, "queued")
		}(i, p)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d (%s) error: %v", i, plans[i], err)
		}
	}

	bp, err := ReadBatchProgress()
	if err != nil {
		t.Fatalf("read after concurrent writes: %v", err)
	}
	if len(bp.Rounds) != 1 {
		t.Fatalf("expected 1 round, got %d", len(bp.Rounds))
	}
	if len(bp.Rounds[0].Plans) != 5 {
		t.Errorf("expected 5 plans after concurrent writes, got %d", len(bp.Rounds[0].Plans))
	}
}
