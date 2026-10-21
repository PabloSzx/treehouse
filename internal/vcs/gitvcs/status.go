package gitvcs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// InspectDirty includes untracked files even when normal status hides them.
// Directory-level output is enough for the boolean answer; full per-file
// enumeration and rename detection add no information. Git's untracked cache
// validates directory mtimes and ignore rules on every run; optional index
// writes let Git retain that cache without changing repository configuration.
// Git skips the optional write when another operation owns index.lock.
func InspectDirty(ctx context.Context, worktreePath string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-c", "core.untrackedCache=true", "-c", "submodule.recurse=false", "status", "--porcelain", "--untracked-files=normal", "--no-renames")
	cmd.Dir = worktreePath
	// Let Git remove its optional index.lock before escalating to a kill.
	if runtime.GOOS != "windows" {
		cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	}
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return false, fmt.Errorf("git status: %w", ctx.Err())
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return false, fmt.Errorf("git status: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return false, err
	}
	return len(out) != 0, nil
}
