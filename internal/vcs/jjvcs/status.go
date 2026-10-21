package jjvcs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// InspectDirty keeps jj's working-copy snapshot semantics while allowing a
// status observation to time out without changing lifecycle safety checks.
func InspectDirty(ctx context.Context, worktreePath string) (bool, error) {
	cmd := exec.CommandContext(ctx, "jj", "--color", "never", "log", "-r", "@", "--no-graph", "-T", `if(empty && description == "", "clean", "dirty")`)
	cmd.Dir = worktreePath
	if runtime.GOOS != "windows" {
		cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	}
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return false, fmt.Errorf("jj log: %w", ctx.Err())
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return false, fmt.Errorf("jj log: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return false, err
	}
	return strings.TrimSpace(string(out)) != "clean", nil
}
