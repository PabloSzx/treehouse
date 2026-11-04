package pool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

func registryDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "treehouse"), nil
}

// RegisteredPools is a discovery index, never authority for a slot or lease.
func RegisteredPools() ([]string, error) {
	root, err := registryDirectory()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(root, "registered-pools.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var paths []string
	if err := json.Unmarshal(data, &paths); err != nil {
		return nil, fmt.Errorf("invalid pool discovery index: %w", err)
	}
	if paths == nil {
		return nil, fmt.Errorf("invalid null pool discovery index")
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("non-absolute path in pool discovery index")
		}
	}
	return paths, nil
}

// RegisterPool records discovery before issuing a lease, including custom roots.
func RegisterPool(poolDir string) error {
	if !filepath.IsAbs(poolDir) {
		return fmt.Errorf("pool discovery requires an absolute path")
	}
	root, err := registryDirectory()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(root, "registered-pools.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := lockFile(file); err != nil {
		return err
	}
	defer unlockFile(file)
	paths, err := RegisteredPools()
	if err != nil {
		return err
	}
	poolDir = filepath.Clean(poolDir)
	if slices.Contains(paths, poolDir) {
		return nil
	}
	paths = append(paths, poolDir)
	slices.Sort(paths)
	data, err := json.Marshal(paths)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(root, "registered-pools.json"), data, 0o600)
}
