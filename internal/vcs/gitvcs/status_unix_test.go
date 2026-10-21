//go:build !windows

package gitvcs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestInspectDirtyCancelsBlockedGit(t *testing.T) {
	repo := t.TempDir()
	mustGit(t, "", "init", "--initial-branch=main", repo)
	ready := filepath.Join(t.TempDir(), "ready")
	t.Setenv("TREEHOUSE_STATUS_READY", ready)
	hook := filepath.Join(t.TempDir(), "fsmonitor")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nprintf '%s' \"$$\" > \"$TREEHOUSE_STATUS_READY\"\nexec sleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "config", "core.fsmonitor", hook)
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() {
		_, err := InspectDirty(ctx, repo)
		finished <- err
	}()
	t.Cleanup(func() {
		cancel()
		data, _ := os.ReadFile(ready)
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			if proc, err := os.FindProcess(pid); err == nil {
				_ = proc.Kill()
			}
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case err := <-finished:
			t.Fatalf("Git exits before reaching the blocking fsmonitor hook: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("Git never reaches the blocking fsmonitor hook")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled observation returns %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled Git observation remains blocked")
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "index.lock")); !os.IsNotExist(err) {
		t.Fatalf("canceled observation leaves an index lock: %v", err)
	}
}
