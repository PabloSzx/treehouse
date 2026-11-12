package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCapacityReportsConfiguredHeadroomWithoutAllocating(t *testing.T) {
	repo, home := setupTestRepo(t)
	writeRepoConfig(t, repo, "max_trees = 80\n")
	read := func(cwd string) capacityJSON {
		t.Helper()
		out, stderr, code := runTreehouse(t, cwd, home, nil, "capacity", "--json")
		if code != 0 {
			t.Fatalf("capacity: exit=%d stderr=%s", code, stderr)
		}
		var report capacityJSON
		if err := json.Unmarshal([]byte(out), &report); err != nil {
			t.Fatal(err)
		}
		return report
	}
	empty := read(repo)
	if empty.MaxTrees != 80 || empty.Total != 0 || empty.Leased != 0 || empty.Unallocated != 80 {
		t.Fatalf("fresh pool capacity: %+v", empty)
	}
	if _, err := os.Stat(empty.Pool); !os.IsNotExist(err) {
		t.Fatalf("capacity creates a pool: %v", err)
	}
	first := acquireLeaseJSON(t, repo, home, "first")
	second := acquireLeaseJSON(t, repo, home, "external-consumer")
	_, stderr, code := runTreehouse(t, repo, home, nil, "return", second.Path)
	if code != 0 {
		t.Fatalf("return: exit=%d stderr=%s", code, stderr)
	}
	before, err := os.ReadFile(filepath.Join(empty.Pool, "treehouse-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{80, 1} {
		if limit == 1 {
			writeRepoConfig(t, repo, "max_trees = 1\n")
		}
		// Running inside a pooled checkout must use the main repository's limit.
		report := read(first.Path)
		wantFree := 78
		if limit == 1 {
			wantFree = 0
		}
		if report.MaxTrees != limit || report.Total != 2 || report.Leased != 1 || report.Unleased != 1 || report.Unallocated != wantFree {
			t.Fatalf("capacity at limit %d: %+v", limit, report)
		}
	}
	after, err := os.ReadFile(filepath.Join(empty.Pool, "treehouse-state.json"))
	if err != nil || string(before) != string(after) {
		t.Fatalf("capacity changes reservations: %v", err)
	}
}
