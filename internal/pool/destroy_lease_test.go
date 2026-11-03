package pool

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kunchenguid/treehouse/v3/internal/process"
)

func TestDestroyWorktree_ConditionalLease(t *testing.T) {
	for _, state := range []string{"matching", "different", "released", "empty", "container-in-use"} {
		t.Run(state, func(t *testing.T) {
			repo, pool := setupRepo(t)
			lease, err := AcquireLeaseInfo(repo, pool, 1, nil, "finished-task")
			if err != nil {
				t.Fatal(err)
			}
			expected := lease.LeaseID
			if state == "different" {
				expected = strings.Repeat("0", 32)
			}
			if state == "empty" {
				expected = ""
			}
			if state == "released" {
				if err := ReleaseConditional(pool, lease.Path, "", ReleasePreconditions{ExpectedLeaseID: &lease.LeaseID}, nil); err != nil {
					t.Fatal(err)
				}
			}
			runGit(t, lease.Path, "checkout", "-b", "pablosaez/expired-task")
			runGit(t, lease.Path, "commit", "--allow-empty", "-m", "unpublished expired commit")
			container := filepath.Dir(lease.Path)
			if err := os.WriteFile(filepath.Join(container, "local-signing.key"), []byte("synthetic signing input\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			head, err := exec.Command("git", "-C", repo, "rev-parse", "refs/heads/pablosaez/expired-task").Output()
			if err != nil {
				t.Fatal(err)
			}
			for name, content := range map[string]string{"unfinished.txt": "discard local work\n", ".gitignore": "*.env\nnode_modules\n", "private.env": "synthetic private configuration\n"} {
				if err := os.WriteFile(filepath.Join(lease.Path, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			external := filepath.Join(t.TempDir(), "outside.env")
			if err := os.WriteFile(external, []byte("external data\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if runtime.GOOS != "windows" {
				if err := os.Symlink(external, filepath.Join(lease.Path, "linked.env")); err != nil {
					t.Fatal(err)
				}
			}
			if state == "container-in-use" {
				oldScan := findProcessesInWorktree
				findProcessesInWorktree = func(target string) ([]process.ProcessInfo, error) {
					if target == filepath.Dir(lease.Path) {
						return []process.ProcessInfo{{PID: int32(os.Getpid()), Name: "active-slot-container"}}, nil
					}
					return nil, nil
				}
				t.Cleanup(func() { findProcessesInWorktree = oldScan })
			}
			result, err := DestroyWorktree(pool, lease.Path, DestroyOptions{
				IncludeLeased: true, IncludeUnlanded: true, ExpectedLeaseID: &expected,
			})
			if state == "matching" {
				if err != nil || len(result.Destroyed) != 1 {
					t.Fatalf("conditional destroy = %#v, %v", result, err)
				}
				if _, err := os.Stat(container); !os.IsNotExist(err) {
					t.Fatalf("expired checkout container remains: %v", err)
				}
			} else {
				if len(result.Destroyed) != 0 {
					t.Fatalf("protected checkout destroyed: %#v", result)
				}
				if content, err := os.ReadFile(filepath.Join(lease.Path, "private.env")); err != nil || string(content) != "synthetic private configuration\n" {
					t.Fatalf("refused checkout changed: %q, %v", content, err)
				}
			}
			if content, err := os.ReadFile(external); err != nil || string(content) != "external data\n" {
				t.Fatalf("external target changed: %q, %v", content, err)
			}
			after, err := exec.Command("git", "-C", repo, "rev-parse", "refs/heads/pablosaez/expired-task").Output()
			if err != nil || !bytes.Equal(head, after) {
				t.Fatalf("branch ref changed: %q -> %q (%v)", head, after, err)
			}
		})
	}
}

func TestDestroyWorktree_ConditionalLeaseRechecksBeforeReservation(t *testing.T) {
	repo, pool := setupRepo(t)
	original, err := AcquireLeaseInfo(repo, pool, 1, nil, "expired-task")
	if err != nil {
		t.Fatal(err)
	}
	oldScan := findProcessesInWorktree
	swapped := false
	var replacement LeaseInfo
	findProcessesInWorktree = func(target string) ([]process.ProcessInfo, error) {
		if target == filepath.Dir(original.Path) && !swapped {
			swapped = true
			if err := ReleaseConditional(pool, original.Path, "", ReleasePreconditions{ExpectedLeaseID: &original.LeaseID}, nil); err != nil {
				t.Fatal(err)
			}
			var err error
			replacement, err = AcquireLeaseInfo(repo, pool, 1, nil, "new-owner")
			if err != nil {
				t.Fatal(err)
			}
			if replacement.Path != original.Path {
				t.Fatalf("fixture did not recycle the same slot: %s", replacement.Path)
			}
			if err := os.WriteFile(filepath.Join(replacement.Path, "new-work"), []byte("keep new acquisition\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return oldScan(target)
	}
	t.Cleanup(func() { findProcessesInWorktree = oldScan })
	result, err := DestroyWorktree(pool, original.Path, DestroyOptions{
		IncludeLeased: true, IncludeUnlanded: true, ExpectedLeaseID: &original.LeaseID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !swapped || len(result.Destroyed) != 0 || len(result.Skipped) != 1 {
		t.Fatalf("stale destruction = %#v; swapped=%v", result, swapped)
	}
	if data, err := os.ReadFile(filepath.Join(replacement.Path, "new-work")); err != nil || string(data) != "keep new acquisition\n" {
		t.Fatalf("new work changed: %q, %v", data, err)
	}
	state, err := ReadState(pool)
	if err != nil || len(state.Worktrees) != 1 || state.Worktrees[0].LeaseID != replacement.LeaseID || state.Worktrees[0].Destroying {
		t.Fatalf("new lease changed: %#v (%v)", state, err)
	}
}
