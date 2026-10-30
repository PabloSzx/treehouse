package pool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A valid registration does not prove its index can be inspected.
// A failed cleanliness check must not advertise the slot as available.
func TestListFailedCleanlinessCheckIsUnverified(t *testing.T) {
	repo, poolDir := setupRepo(t)
	path, err := Acquire(repo, poolDir, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	clearOwnerReservation(t, poolDir, path)
	indexPath := gitOut(t, path, "rev-parse", "--git-path", "index")
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(path, indexPath)
	}
	if err := os.WriteFile(indexPath, []byte("invalid index\n"), 0o644); err != nil {
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
