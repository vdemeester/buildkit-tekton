package main

import (
	"os"

	"github.com/spf13/cobra"
)

func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "local",
		Aliases: []string{},
		Short:   "Tekton \"local\" commands",
		Annotations: map[string]string{
			"commandType": "main",
		},
	}

	cmd.AddCommand(
		pruneCommand(),
		runCommand(),
	)

	return cmd
}

func main() {
	cmd := Command()
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
