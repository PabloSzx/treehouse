package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/kunchenguid/treehouse/v3/internal/pool"
)

// Status must remain usable during a slow lifecycle operation, without
// rewriting that operation's state or moving files during quarantine recovery.
func TestStatusReadsSnapshotDuringLifecycleOperation(t *testing.T) {
	for _, recovered := range []bool{false, true} {
		name := "ordinary lease"
		if recovered {
			name = "quarantined lease"
		}
		t.Run(name, func(t *testing.T) {
			repoDir, homeDir := setupTestRepo(t)
			lease := acquireLeaseJSON(t, repoDir, homeDir, "home")
			poolDir := filepath.Dir(filepath.Dir(lease.Path))
			locked, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
			go func() {
				finished <- pool.WithStateLock(poolDir, func() error {
					close(locked)
					<-release
					return nil
				})
			}()
			t.Cleanup(func() {
				close(release)
				if err := <-finished; err != nil {
					t.Error(err)
				}
			})
			select {
			case <-locked:
			case <-time.After(10 * time.Second):
				t.Fatal("could not acquire fixture's pool lock")
			}

			statePath := filepath.Join(poolDir, "treehouse-state.json")
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			wantHolder := "home"
			if recovered {
				var state map[string]any
				if err := json.Unmarshal(before, &state); err != nil {
					t.Fatal(err)
				}
				entry := state["worktrees"].([]any)[0].(map[string]any)
				delete(entry, "seed_inventory_known")
				delete(entry, "seed_inventory_digest")
				before, err = json.Marshal(state)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(statePath, before, 0o644); err != nil {
					t.Fatal(err)
				}
				wantHolder = pool.RecoveredLeaseHolder
			}
			notes := filepath.Join(lease.Path, "private-notes.txt")
			if err := os.WriteFile(notes, []byte("keep here\n"), 0o600); err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, treehouseBin, "status", "--json")
			cmd.Dir, cmd.Env = repoDir, buildEnv(homeDir)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("status must finish while the pool lock is held: %v, %v, stderr=%s", err, ctx.Err(), stderr.String())
			}
			var statuses []statusJSONResult
			if err := json.Unmarshal(out, &statuses); err != nil {
				t.Fatal(err)
			}
			if len(statuses) != 1 || statuses[0].Status != "leased" || statuses[0].LeaseHolder != wantHolder || statuses[0].LeaseID != lease.LeaseID {
				t.Fatalf("snapshot loses the reservation: %s", out)
			}
			after, err := os.ReadFile(statePath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("status changes state: err=%v", err)
			}
			contents, err := os.ReadFile(notes)
			if err != nil || string(contents) != "keep here\n" {
				t.Fatalf("status moves or changes untracked files: contents=%q err=%v", contents, err)
			}
		})
	}
}
