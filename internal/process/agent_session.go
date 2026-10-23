package process

import (
	"path/filepath"
	"strings"

	gopsutilprocess "github.com/shirou/gopsutil/v4/process"
)

// agentHarnessNames lists the interactive agent-harness binaries whose sessions
// must never be terminated by pool lifecycle commands (get, return, destroy).
// A harness session represents a live conversation whose state lives outside
// the worktree, so killing it destroys the session without making the slot any
// safer to reset; the pool skips these processes and lets the lifecycle
// command proceed. Matched on both the comm name and the basename of argv[0],
// because script-based harnesses can report the interpreter as their comm.
// Deliberately absent: generic names such as `agent` (a wrapper alias for
// cursor-agent here), which would also match unrelated processes.
var agentHarnessNames = map[string]struct{}{
	"pi":           {},
	"codex":        {},
	"claude":       {},
	"cursor-agent": {},
}

// processTable is one point-in-time snapshot of the process table, keeping
// enough identity to recognise harnesses and enough ancestry to spare their
// descendants.
type processTable struct {
	// names maps pid to comm name.
	names map[int32]string
	// argv0 maps pid to the basename of the first command-line token.
	argv0 map[int32]string
	// children maps pid to its direct children.
	children map[int32][]int32
}

// snapshotProcessTable is a seam so tests can drive agent-session filtering
// deterministically instead of racing the real process table.
var snapshotProcessTable = snapshotProcessTableLive

func snapshotProcessTableLive() (processTable, error) {
	procs, err := gopsutilprocess.Processes()
	if err != nil {
		return processTable{}, err
	}

	table := processTable{
		names:    make(map[int32]string, len(procs)),
		argv0:    make(map[int32]string, len(procs)),
		children: make(map[int32][]int32),
	}
	for _, p := range procs {
		pid := p.Pid
		if name, err := p.Name(); err == nil && name != "" {
			table.names[pid] = name
		}
		if argv, err := p.CmdlineSlice(); err == nil && len(argv) > 0 && argv[0] != "" {
			table.argv0[pid] = filepath.Base(argv[0])
		}
		if ppid, err := p.Ppid(); err == nil && ppid > 0 {
			table.children[ppid] = append(table.children[ppid], pid)
		}
	}
	return table, nil
}

// isAgentHarnessName reports whether a process name identifies an agent
// harness. Windows executable names carry a .exe suffix that the comm name
// never shows, so both spellings match.
func isAgentHarnessName(name string) bool {
	normalized := strings.TrimSuffix(strings.ToLower(name), ".exe")
	_, ok := agentHarnessNames[normalized]
	return ok
}

// isAgentHarness reports whether the snapshot identifies the pid as an agent
// harness, by comm name or argv[0] basename.
func (t processTable) isAgentHarness(pid int32) bool {
	if name, ok := t.names[pid]; ok && isAgentHarnessName(name) {
		return true
	}
	if name, ok := t.argv0[pid]; ok && isAgentHarnessName(name) {
		return true
	}
	return false
}

// agentSessionPIDs returns the harness PIDs present in procs plus all
// transitive descendants of those harnesses, so an agent session standing in
// the worktree and its in-flight commands (shells, direnv, build tools) are
// spared together. Descendant sparing is scoped to harnesses inside procs: a
// session whose harness stands elsewhere is not part of THIS worktree's scan,
// and its processes found here are ordinary targets. Callers therefore keep
// deterministic semantics no matter which harness the lifecycle command itself
// happens to run under.
func agentSessionPIDs(procs []ProcessInfo, table processTable) map[int32]struct{} {
	protected := make(map[int32]struct{})
	queue := make([]int32, 0)

	seed := func(pid int32) {
		if _, seen := protected[pid]; !seen {
			protected[pid] = struct{}{}
			queue = append(queue, pid)
		}
	}
	for _, proc := range procs {
		if table.isAgentHarness(proc.PID) {
			seed(proc.PID)
		}
	}

	for len(queue) > 0 {
		pid := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for _, child := range table.children[pid] {
			seed(child)
		}
	}
	return protected
}

// DropAgentSessions removes agent-harness sessions (harness processes whose cwd
// is inside the worktree, plus those sessions' in-flight descendants) from
// procs, so lifecycle commands never terminate a live agent conversation, and
// never count one as a survivor blocking the worktree from being returned.
func DropAgentSessions(procs []ProcessInfo) ([]ProcessInfo, error) {
	if len(procs) == 0 {
		return procs, nil
	}

	table, err := snapshotProcessTable()
	if err != nil {
		return nil, err
	}
	protected := agentSessionPIDs(procs, table)

	filtered := make([]ProcessInfo, 0, len(procs))
	for _, proc := range procs {
		if _, skip := protected[proc.PID]; skip {
			continue
		}
		filtered = append(filtered, proc)
	}
	return filtered, nil
}
