package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kunchenguid/treehouse/v3/internal/pool"
)

func TestLeaseCatalogIsReadOnlyAcrossRepositories(t *testing.T) {
	repo, home := setupTestRepo(t)
	other := setupTestRepoWithHome(t, home, "other")
	first := acquireLeaseJSON(t, repo, home, "task:first")
	second := acquireLeaseJSON(t, other, home, "legacy")
	custom := setupTestRepoWithHome(t, home, "custom")
	output, diagnostic, code := runTreehouse(t, custom, home, nil, "--root", filepath.Join(home, "custom-pools"), "get", "--lease", "--json")
	if code != 0 {
		t.Fatal(diagnostic)
	}
	var third leaseJSONResult
	if err := json.Unmarshal([]byte(output), &third); err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(home, ".local", "state", "treehouse", "registered-pools.json")
	registryBefore, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(filepath.Dir(filepath.Dir(first.Path)), "treehouse-state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	out, stderr, code := runTreehouse(t, repo, home, nil, "leases", "--json")
	if code != 0 {
		t.Fatalf("catalog failed: %s", stderr)
	}
	var entries []struct {
		Path     string     `json:"path"`
		LeaseID  string     `json:"lease_id"`
		Pool     string     `json:"pool"`
		LeasedAt *time.Time `json:"leased_at"`
	}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	for _, entry := range entries {
		if entry.Pool == "" || entry.LeasedAt == nil {
			t.Fatalf("missing acquisition identity: %+v", entry)
		}
		found[entry.Path] = entry.LeaseID
	}
	if len(found) != 3 || found[first.Path] != first.LeaseID || found[second.Path] != second.LeaseID || found[third.Path] != third.LeaseID {
		t.Fatalf("incomplete global catalog: %s", out)
	}
	after, err := os.ReadFile(statePath)
	if err != nil || string(after) != string(before) {
		t.Fatalf("catalog changed allocation state: %v", err)
	}
	registryAfter, err := os.ReadFile(registryPath)
	if err != nil || string(registryAfter) != string(registryBefore) {
		t.Fatalf("catalog changed discovery state: %v", err)
	}
	if err := os.WriteFile(registryPath, []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, code := runTreehouse(t, repo, home, nil, "leases", "--json"); code == 0 {
		t.Fatal("corrupt discovery was silently treated as an empty inventory")
	}
}

func TestLeaseExpiryRequiresCurrentOldGeneration(t *testing.T) {
	for _, scenario := range []string{"fresh", "expired", "unknown-age", "future-age", "wrong-generation", "malformed-id", "recovered", "missing-marker"} {
		t.Run(scenario, func(t *testing.T) {
			repo, home := setupTestRepo(t)
			lease := acquireLeaseJSON(t, repo, home, "legacy-owner")
			gitCmd(t, lease.Path, "switch", "-c", "retained-work")
			gitCmd(t, lease.Path, "config", "user.email", "test@test.com")
			gitCmd(t, lease.Path, "config", "user.name", "Test")
			if err := os.WriteFile(filepath.Join(lease.Path, "committed"), []byte("retained"), 0o600); err != nil {
				t.Fatal(err)
			}
			gitCmd(t, lease.Path, "add", "committed")
			gitCmd(t, lease.Path, "commit", "-m", "local work")
			head := strings.TrimSpace(gitCmd(t, lease.Path, "rev-parse", "HEAD"))
			if err := os.WriteFile(filepath.Join(lease.Path, "README.md"), []byte("discarded edit"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(lease.Path, "untracked"), []byte("discarded file"), 0o600); err != nil {
				t.Fatal(err)
			}
			poolDir := filepath.Dir(filepath.Dir(lease.Path))
			err := pool.WithStateLock(poolDir, func() error {
				state, err := pool.ReadState(poolDir)
				if err != nil {
					return err
				}
				for i := range state.Worktrees {
					if state.Worktrees[i].LeaseID != lease.LeaseID {
						continue
					}
					switch scenario {
					case "expired", "wrong-generation", "missing-marker":
						state.Worktrees[i].LeasedAt = time.Now().Add(-8 * 24 * time.Hour)
					case "malformed-id":
						state.Worktrees[i].LeaseID = "malformed"
						state.Worktrees[i].LeasedAt = time.Now().Add(-8 * 24 * time.Hour)
					case "recovered":
						state.Worktrees[i].LeaseHolder = pool.RecoveredLeaseHolder
						state.Worktrees[i].LeasedAt = time.Now().Add(-8 * 24 * time.Hour)
					case "unknown-age":
						state.Worktrees[i].LeasedAt = time.Time{}
					case "future-age":
						state.Worktrees[i].LeasedAt = time.Now().Add(24 * time.Hour)
					}
				}
				return pool.WriteState(poolDir, state)
			})
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "missing-marker" {
				if err := os.Remove(filepath.Join(lease.Path, ".git")); err != nil {
					t.Fatal(err)
				}
			}
			expected := lease.LeaseID
			if scenario == "malformed-id" {
				expected = "malformed"
			}
			if scenario == "wrong-generation" {
				expected = strings.Repeat("0", 32)
			}
			_, stderr, code := runTreehouse(t, repo, home, nil, "destroy", lease.Path, "--yes", "--include-leased", "--include-unlanded", "--include-in-use", "--if-lease-id", expected, "--expire-after", "168h", "--terminate-agent-sessions")
			if scenario == "expired" {
				if code != 0 {
					t.Fatalf("expired generation not reclaimed: %s", stderr)
				}
				if _, err := os.Stat(lease.Path); !os.IsNotExist(err) {
					t.Fatalf("expired checkout remains: %v", err)
				}
				if got := strings.TrimSpace(gitCmd(t, repo, "rev-parse", "refs/heads/retained-work")); got != head {
					t.Fatalf("local branch lost: %s", got)
				}
			} else {
				if code == 0 {
					t.Fatalf("unsafe expiry accepted: %s", scenario)
				}
				if contents, err := os.ReadFile(filepath.Join(lease.Path, "README.md")); err != nil || string(contents) != "discarded edit" {
					t.Fatalf("refused expiry changed local files: %v", err)
				}
			}
		})
	}
}
