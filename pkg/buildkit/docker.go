package buildkit

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/pkg/errors"
)

func checkDocker(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "info")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errors.Errorf("failed to run docker: %s", string(output))
	}
	return nil
}

func checkBuildkitInDocker(ctx context.Context, containerName string) error {
	command := "docker"
	info, err := getBuildkitInformation(ctx, command, containerName)
	if err != nil {
		if err := removeBuildkit(ctx, command, containerName); err != nil {
			fmt.Fprintf(os.Stderr, "error removing buildkit")
		}

		if err := installBuildkit(ctx, command, containerName); err != nil {
			return err
		}

	} else {
		// validate version
		if info.Version != vendoredVersion {
			if err := removeBuildkit(ctx, command, containerName); err != nil {
				return err
			}
			if err := installBuildkit(ctx, command, containerName); err != nil {
				return err
			}
		}
		// start buildkit if need be
		if !info.IsActive {
			if err := startBuildkit(ctx, command, containerName); err != nil {
				return err
			}
		}
	}
	return nil
}
