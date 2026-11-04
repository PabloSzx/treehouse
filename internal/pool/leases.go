package pool

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

type LeaseCatalogEntry struct {
	Pool        string     `json:"pool"`
	Name        string     `json:"name"`
	Path        string     `json:"path"`
	RemovalRoot string     `json:"removal_root"`
	LeaseID     string     `json:"lease_id"`
	LeaseHolder string     `json:"lease_holder"`
	LeasedAt    *time.Time `json:"leased_at"`
	Quarantined bool       `json:"quarantined"`
}

// LeaseCatalog observes every pool under root without healing or writing state.
func LeaseCatalog(root string, additionalPools []string) ([]LeaseCatalogEntry, error) {
	pools, err := prunePoolDirs(root)
	if err != nil {
		return nil, err
	}
	for _, directory := range additionalPools {
		if !filepath.IsAbs(directory) {
			return nil, fmt.Errorf("pool discovery requires an absolute path")
		}
		if _, err := os.Stat(stateFilePath(directory)); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		pools = append(pools, filepath.Clean(directory))
	}
	slices.Sort(pools)
	pools = slices.Compact(pools)
	result := make([]LeaseCatalogEntry, 0)
	for _, directory := range pools {
		state, err := ReadState(directory)
		if err != nil {
			return nil, fmt.Errorf("reading leases in %s: %w", directory, err)
		}
		for _, entry := range state.Worktrees {
			if !entry.Leased {
				continue
			}
			removalRoot, _ := removableWorktreeContainer(directory, entry.Path)
			item := LeaseCatalogEntry{Pool: directory, Name: entry.Name, Path: entry.Path, RemovalRoot: removalRoot, LeaseID: entry.LeaseID, LeaseHolder: entry.LeaseHolder, Quarantined: IsQuarantinedLeaseHolder(entry.LeaseHolder) || entry.RecoveryError != ""}
			if !entry.LeasedAt.IsZero() {
				value := entry.LeasedAt
				item.LeasedAt = &value
			}
			result = append(result, item)
		}
	}
	return result, nil
}
