package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/kunchenguid/treehouse/v3/internal/config"
	"github.com/kunchenguid/treehouse/v3/internal/pool"
	"github.com/kunchenguid/treehouse/v3/internal/vcs"
	"github.com/spf13/cobra"
)

type capacityJSON struct {
	Pool        string `json:"pool"`
	MaxTrees    int    `json:"max_trees"`
	Total       int    `json:"total"`
	Leased      int    `json:"leased"`
	Unleased    int    `json:"unleased"`
	Unallocated int    `json:"unallocated"`
}

func init() {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capacity",
		Short: "Show configured pool capacity and lease counts without inspecting checkouts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, err := vcs.FindMainRepoRoot()
			if err != nil {
				return err
			}
			cfg, err := config.Load(repoRoot)
			if err != nil {
				return err
			}
			poolDir, err := config.ResolvePoolDir(repoRoot, config.ResolveRoot(rootFlag, cfg))
			if err != nil {
				return err
			}
			state, err := pool.ReadState(poolDir)
			if err != nil {
				return err
			}
			result := capacityJSON{Pool: poolDir, MaxTrees: cfg.MaxTrees, Total: len(state.Worktrees)}
			for _, entry := range state.Worktrees {
				if entry.Leased {
					result.Leased++
				} else {
					result.Unleased++
				}
			}
			result.Unallocated = max(0, cfg.MaxTrees-result.Total)
			if jsonOutput {
				return json.NewEncoder(os.Stdout).Encode(result)
			}
			_, err = fmt.Fprintf(os.Stdout, "%d/%d leased; %d unleased checkouts; %d unallocated slots\n", result.Leased, result.MaxTrees, result.Unleased, result.Unallocated)
			return err
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "Print configured capacity and reservation counts as JSON; unleased checkouts are not verified reusable")
	rootCmd.AddCommand(command)
}
