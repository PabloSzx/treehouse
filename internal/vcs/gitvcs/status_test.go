package gitvcs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Cached observation must notice edits made after a clean result, including
// files normal git status hides, without mistaking ignored files for changes.
func TestInspectDirtyObservesChangesAfterCachedCleanResult(t *testing.T) {
	repo := t.TempDir()
	mustGit(t, "", "init", "--initial-branch=main", repo)
	mustGit(t, repo, "config", "user.email", "test@test.com")
	mustGit(t, repo, "config", "user.name", "Test")
	write := func(name, contents string) {
		t.Helper()
		path := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("tracked.txt", "initial\n")
	write(".gitignore", "*.ignored\n")
	mustGit(t, repo, "add", ".")
	mustGit(t, repo, "commit", "-m", "initial")
	mustGit(t, repo, "config", "status.showUntrackedFiles", "no")
	assertDirty := func(want bool) {
		t.Helper()
		dirty, err := InspectDirty(context.Background(), repo)
		if err != nil || dirty != want {
			t.Fatalf("dirty=%v want=%v err=%v", dirty, want, err)
		}
	}
	write("cache.ignored", "ignored\n")
	assertDirty(false)
	assertDirty(false)
	write(filepath.Join("scratch", "nested", "notes.txt"), "untracked\n")
	assertDirty(true)
	if err := os.Remove(filepath.Join(repo, "scratch", "nested", "notes.txt")); err != nil {
		t.Fatal(err)
	}
	assertDirty(false)
	write("tracked.txt", "changed\n")
	assertDirty(true)
	mustGit(t, repo, "add", "tracked.txt")
	assertDirty(true)
}
