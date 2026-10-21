package pool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An existing marker can identify Git while pointing at missing metadata.
// A failed inspection must not advertise that slot as clean and available.
func TestListFailedCleanlinessCheckIsUnverified(t *testing.T) {
	repo, poolDir := setupRepo(t)
	path, err := Acquire(repo, poolDir, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	clearOwnerReservation(t, poolDir, path)
	marker := "gitdir: " + filepath.Join(t.TempDir(), "missing") + "\n"
	if err := os.WriteFile(filepath.Join(path, ".git"), []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}
	var statuses []WorktreeStatus
	stderr := captureStderr(t, func() {
		statuses, err = List(poolDir)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].Status != StatusUnverified {
		t.Fatalf("failed cleanliness check reads available: %+v", statuses)
	}
	if !strings.Contains(stderr, "WARNING") || !strings.Contains(stderr, path) {
		t.Fatalf("inspection failure has no diagnostic naming the worktree: %q", stderr)
	}
}
