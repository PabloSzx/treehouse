package cmd

import (
	"os"
	"strings"
	"testing"
)

func TestDestroyLeasedSinglePathCondition(t *testing.T) {
	repo, home := setupTestRepo(t)
	lease := acquireLeaseJSON(t, repo, home, "expired-task")
	for _, flags := range [][]string{
		{"--if-lease-id", strings.Repeat("0", 32)},
		{"--if-lease-id", ""},
		{"--all", "--if-lease-id", lease.LeaseID},
	} {
		args := append([]string{"destroy", lease.Path, "--include-leased", "--yes"}, flags...)
		_, stderr, code := runTreehouse(t, repo, home, nil, args...)
		if code == 0 {
			t.Fatalf("invalid lease condition accepted: %v (%s)", flags, stderr)
		}
		if _, err := os.Stat(lease.Path); err != nil {
			t.Fatalf("refused command removed the checkout: %v", err)
		}
	}
	_, stderr, code := runTreehouse(t, repo, home, nil,
		"destroy", lease.Path, "--include-leased", "--if-lease-id", lease.LeaseID, "--yes")
	if code != 0 {
		t.Fatalf("matching conditional destruction failed: %s", stderr)
	}
	if _, err := os.Stat(lease.Path); !os.IsNotExist(err) {
		t.Fatalf("expired checkout still exists: %v", err)
	}
}
