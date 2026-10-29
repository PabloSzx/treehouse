package gitvcs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Cached observation must notice edits made after a clean result, including
// files normal git status hides, without mistaking ignored files for changes.
func TestInspectDirtyObservesChangesAfterCachedCleanResult(t *testing.T) {
	for _, checker := range []struct {
		name  string
		check func(string) (bool, error)
	}{
		{"status", func(repo string) (bool, error) { return InspectDirty(context.Background(), repo) }},
		{"lifecycle", IsDirty},
	} {
		t.Run(checker.name, func(t *testing.T) {
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
				dirty, err := checker.check(repo)
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
		})
	}
}

func TestIsDirtyDoesNotEnumerateUntrackedDescendants(t *testing.T) {
	repo := initRepo(t)
	for i := range 64 {
		writeTestFile(t, repo, filepath.Join("scratch", "nested", fmt.Sprintf("file-%d", i)), "untracked\n")
	}
	trace := filepath.Join(t.TempDir(), "trace.json")
	t.Setenv("GIT_TRACE2_EVENT", trace)
	if dirty, err := IsDirty(repo); err != nil || !dirty {
		t.Fatalf("nested untracked files: dirty=%v err=%v", dirty, err)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var event struct {
			Category string `json:"category"`
			Key      string `json:"key"`
			Value    string `json:"value"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event.Category == "status" && event.Key == "count/untracked" {
			if event.Value != "1" {
				t.Fatalf("boolean dirty check enumerated %s untracked entries; one directory entry suffices", event.Value)
			}
			return
		}
	}
	t.Fatal("git trace did not report the untracked entry count")
}

func TestIsDirtyFailedInspectionIsNotClean(t *testing.T) {
	if dirty, err := IsDirty(t.TempDir()); err == nil || !dirty {
		t.Fatalf("unverified directory: dirty=%v err=%v", dirty, err)
	}
}
