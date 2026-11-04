package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/kunchenguid/treehouse/v3/internal/config"
	"github.com/kunchenguid/treehouse/v3/internal/pool"
	"github.com/spf13/cobra"
)

func init() {
	var jsonOutput bool
	var additionalPools []string
	command := &cobra.Command{
		Use:   "leases",
		Short: "List durable lease identities across all repositories without changing state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadGlobal()
			if err != nil {
				return err
			}
			root, err := config.ResolvePoolRoot("", config.ResolveRoot(rootFlag, cfg))
			if err != nil {
				return err
			}
			registered, err := pool.RegisteredPools()
			if err != nil {
				return err
			}
			entries, err := pool.LeaseCatalog(root, append(registered, additionalPools...))
			if err != nil {
				return err
			}
			if jsonOutput {
				return json.NewEncoder(os.Stdout).Encode(entries)
			}
			for _, entry := range entries {
				fmt.Fprintf(os.Stdout, "%s\t%s\t%s\t%v\n", entry.Path, entry.LeaseID, entry.LeaseHolder, entry.LeasedAt)
			}
			return nil
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "Print lease identities and acquisition timestamps as JSON")
	command.Flags().StringArrayVar(&additionalPools, "pool", nil, "Also inspect this known pool directory without registering it")
	rootCmd.AddCommand(command)
}
