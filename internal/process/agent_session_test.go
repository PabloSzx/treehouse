//go:build !windows

package process

import (
	"errors"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestIsAgentHarnessName(t *testing.T) {
	cases := []struct {
		name  string
		match bool
	}{
		{"pi", true},
		{"Pi", true},
		{"pi.exe", true},
		{"codex", true},
		{"claude", true},
		{"cursor-agent", true},
		{"agent", false},
		{"bash", false},
		{"pianobar", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isAgentHarnessName(tc.name); got != tc.match {
			t.Errorf("isAgentHarnessName(%q) = %v, want %v", tc.name, got, tc.match)
		}
	}
}

func TestDropAgentSessions_SparesHarnessAndDescendants(t *testing.T) {
	original := snapshotProcessTable
	t.Cleanup(func() { snapshotProcessTable = original })
	snapshotProcessTable = func() (processTable, error) {
		return processTable{
			names: map[int32]string{
				100: "pi",
				101: "zsh",
				102: "direnv",
				200: "codex",
				300: "node",
				400: "sleep",
			},
			argv0: map[int32]string{},
			children: map[int32][]int32{
				100: {101},
				101: {102},
				200: {300},
			},
		}, nil
	}

	procs := []ProcessInfo{
		{PID: 100, Name: "pi"},
		{PID: 101, Name: "zsh"},
		{PID: 102, Name: "direnv"},
		{PID: 200, Name: "codex"},
		{PID: 300, Name: "node"},
		{PID: 400, Name: "sleep"},
	}

	filtered, err := DropAgentSessions(procs)
	if err != nil {
		t.Fatalf("DropAgentSessions: %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("expected 1 process after filtering, got %d: %+v", len(filtered), filtered)
	}
	if filtered[0].PID != 400 {
		t.Fatalf("expected only pid 400 to remain, got %+v", filtered[0])
	}
}

func TestDropAgentSessions_KillsDescendantOfExternalHarness(t *testing.T) {
	original := snapshotProcessTable
	t.Cleanup(func() { snapshotProcessTable = original })
	snapshotProcessTable = func() (processTable, error) {
		return processTable{
			names: map[int32]string{
				100: "pi",
				400: "sleep",
			},
			argv0: map[int32]string{},
			children: map[int32][]int32{
				100: {400},
			},
		}, nil
	}

	// Only the foreign descendant is in the scan; its harness stands elsewhere,
	// so the descendant is an ordinary target and must not be spared.
	filtered, err := DropAgentSessions([]ProcessInfo{{PID: 400, Name: "sleep"}})
	if err != nil {
		t.Fatalf("DropAgentSessions: %v", err)
	}
	if len(filtered) != 1 || filtered[0].PID != 400 {
		t.Fatalf("expected pid 400 to remain an ordinary target, got %+v", filtered)
	}
}

func TestDropAgentSessions_MatchesArgv0WhenCommDiffers(t *testing.T) {
	original := snapshotProcessTable
	t.Cleanup(func() { snapshotProcessTable = original })
	snapshotProcessTable = func() (processTable, error) {
		return processTable{
			names:    map[int32]string{500: "node"},
			argv0:    map[int32]string{500: "claude"},
			children: map[int32][]int32{},
		}, nil
	}

	filtered, err := DropAgentSessions([]ProcessInfo{{PID: 500, Name: "node"}})
	if err != nil {
		t.Fatalf("DropAgentSessions: %v", err)
	}
	if len(filtered) != 0 {
		t.Fatalf("expected the script-based harness to be dropped, got %+v", filtered)
	}
}

func TestDropAgentSessions_PropagatesSnapshotFailure(t *testing.T) {
	original := snapshotProcessTable
	t.Cleanup(func() { snapshotProcessTable = original })
	snapshotProcessTable = func() (processTable, error) {
		return processTable{}, errors.New("process table unreadable")
	}

	if _, err := DropAgentSessions([]ProcessInfo{{PID: 1, Name: "sleep"}}); err == nil {
		t.Fatal("expected an error when the process table cannot be read")
	}
}

func TestDropAgentSessions_EmptyInput(t *testing.T) {
	filtered, err := DropAgentSessions(nil)
	if err != nil {
		t.Fatalf("DropAgentSessions: %v", err)
	}
	if len(filtered) != 0 {
		t.Fatalf("expected no processes, got %+v", filtered)
	}
}

func TestTerminateWorktreeProcesses_SparesAgentSession(t *testing.T) {
	dir := t.TempDir()

	cmd := exec.Command("sleep", "60")
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sleep: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	// Give the OS a moment to record the process cwd.
	time.Sleep(200 * time.Millisecond)

	original := snapshotProcessTable
	t.Cleanup(func() { snapshotProcessTable = original })
	snapshotProcessTable = func() (processTable, error) {
		return processTable{
			names:    map[int32]string{int32(cmd.Process.Pid): "pi"},
			argv0:    map[int32]string{},
			children: map[int32][]int32{},
		}, nil
	}

	procs, err := TerminateWorktreeProcesses(dir, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("TerminateWorktreeProcesses: %v", err)
	}
	if len(procs) != 0 {
		t.Fatalf("expected the agent session to be spared, got targets %+v", procs)
	}

	// The sleep must still be alive: the session was never signalled.
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("agent session was terminated despite protection: %v", err)
	}
}

func TestUnprotectedProcessesInWorktree_SparesAgentSession(t *testing.T) {
	dir := t.TempDir()

	cmd := exec.Command("sleep", "60")
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sleep: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	time.Sleep(200 * time.Millisecond)

	original := snapshotProcessTable
	t.Cleanup(func() { snapshotProcessTable = original })
	snapshotProcessTable = func() (processTable, error) {
		return processTable{
			names:    map[int32]string{int32(cmd.Process.Pid): "claude"},
			argv0:    map[int32]string{},
			children: map[int32][]int32{},
		}, nil
	}

	procs, err := UnprotectedProcessesInWorktree(dir)
	if err != nil {
		t.Fatalf("UnprotectedProcessesInWorktree: %v", err)
	}
	if len(procs) != 0 {
		t.Fatalf("expected the agent session to be excluded, got %+v", procs)
	}
}
