package main

import (
	"github.com/spf13/cobra"
)

type pruneOption struct{}

func pruneCommand() *cobra.Command {
	// opts := &pruneOption{}
	cmd := &cobra.Command{
		Use:     "prune",
		Aliases: []string{},
		Short:   "Run a tekton resource",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
	// c.Flags().BoolVarP(&opts.ForceDelete, "force", "f", false, "Whether to force deletion (default: false)")

	return cmd
}
