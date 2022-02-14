package main

import (
	"github.com/spf13/cobra"
)

func newFrontendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "frontend",
		Short: "Frontend entrypoint",
		Args:  cobra.NoArgs,
		RunE:  frontendAction,
	}
	return cmd
}

// mimic dockerfile.v1 frontend
const (
	localNameContext    = "context"
	localNameDockerfile = "dockerfile"
	keyFilename         = "filename"
)

func frontendAction(cmd *cobra.Command, args []string) error {
	return nil
}
