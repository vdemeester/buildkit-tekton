package buildkit

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/docker/distribution/reference"
	"github.com/moby/buildkit/client"
	_ "github.com/moby/buildkit/client/connhelper/dockercontainer" // import the container connection driver
	_ "github.com/moby/buildkit/client/connhelper/podmancontainer" // import the container connection driver
	_ "github.com/moby/buildkit/client/connhelper/ssh"             // import the container connection driver
	"github.com/pkg/errors"
)

const (
	defaultBuildkitSocket   = "unix:///run/buildkit/buildkitd.sock"
	defaultContainerdSocker = "unix:///run/containerd/containerd.sock"
	defaultDockerSocket     = "unix:///var/run/docker.sock"

	defaultContainerName = "tekton-buildkitd"
	defaultVolumeName    = "tekton-buildkitd"
	defaultHost          = "docker-container://" + defaultContainerName
)

var (
	// vendoredVersion is filled in by init()
	vendoredVersion string
)

func init() {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}

	for _, d := range bi.Deps {
		if d.Path == "github.com/moby/buildkit" {
			vendoredVersion = d.Version
			break
		}
	}
}

// NewClient returns a new Buildkit client based on the given host *or* environment variables.
//
// Support host "schema":
// - docker-container://<container>?context=<context> : buildkitd runs into a docker container (create and start if doesn't exists)
// - podman-container://<container> : buildkitd runs into a podman container (create and start if doesn't exists)
// - unix://<path> : buildkitd runs directly on the host (fail if doesn't exists)
// - ssh://<host>/<path>
//
// If host is empty, we will look at the follow environment variables
// - TKN_LOCAL_HOST : same schema as host parameter
// - BUILDKIT_HOST : same as unix://<path>
// - DOCKER_HOST : same as docker-container://<container>, can be used with TKN_LOCAL_HOST
//
// "Possible" Future schema to be supported:
// - containerd-container://<container> : same as podman or docker but with containerd
// - runc://<container> : standalone runner on top of runc
// - crun://<container> : standalone runner on top of crun
func NewClient(ctx context.Context, host string) (*client.Client, error) {
	// If host is empty, look at some environment variables
	if host == "" {
		host = getFromEnv()
	}
	if host == "" {
		host = defaultHost
	}
	fmt.Fprintf(os.Stderr, "Use host %q to run\n", host)
	if err := start(ctx, host); err != nil {
		return nil, err
	}

	opts := []client.ClientOpt{}

	c, err := client.New(ctx, host, opts...)
	if err != nil {
		return nil, fmt.Errorf("buildkit client: %w", err)
	}
	return c, nil
}

func start(ctx context.Context, host string) error {
	switch {
	case strings.HasPrefix(host, "unix://") || strings.HasPrefix(host, "ssh://"):
		return nil
	case strings.HasPrefix(host, "docker-container://"):
		if err := checkDocker(ctx); err != nil {
			return err
		}
		containerName := strings.TrimPrefix(host, "docker-container://")
		if err := checkBuildkitInDocker(ctx, containerName); err != nil {
			return err
		}
		return waitBuildkit(ctx, host)
	case strings.HasPrefix(host, "podman-container://"):
		if err := checkPodman(ctx); err != nil {
			return err
		}
		containerName := strings.TrimPrefix(host, "podman-container://")
		if err := checkBuildkitInPodman(ctx, containerName); err != nil {
			return err
		}
		return waitBuildkit(ctx, host)
	}
	return fmt.Errorf("Host %q not support", host)
}

func getFromEnv() string {
	envs := []string{
		"TKN_LOCAL_HOST",
		"BUILDKIT_HOST",
	}
	for _, e := range envs {
		if env := os.Getenv(e); env != "" {
			return env
		}
	}
	return ""
}

func removeBuildkit(ctx context.Context, command, containerName string) error {
	cmd := exec.CommandContext(ctx,
		command,
		"rm",
		"-fv",
		containerName,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errors.Wrapf(err, "error while removing buildkit: %s", output)
	}
	return nil
}

func installBuildkit(ctx context.Context, command, containerName string) error {
	cmd := exec.CommandContext(ctx,
		command,
		"pull",
		"docker.io/moby/buildkit:"+vendoredVersion,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errors.Wrapf(err, "error pulling buildkit image: %s", output)
	}
	cmd = exec.CommandContext(ctx,
		command,
		"run",
		// "--net=host",
		"-d",
		"--restart", "always",
		"-v", defaultVolumeName+":/var/lib/buildkit",
		"--name", containerName,
		"--privileged", // TODO: try to remove privileged at some point
		"docker.io/moby/buildkit:"+vendoredVersion,
	)
	output, err = cmd.CombinedOutput()
	if err != nil {
		return errors.Wrapf(err, "error starting buildkit: %s", output)
	}
	return nil
}

func startBuildkit(ctx context.Context, command, containerName string) error {
	cmd := exec.CommandContext(ctx,
		command,
		"start",
		containerName,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errors.Wrapf(err, "error starting buildkit: %s", output)
	}
	return nil
}

func waitBuildkit(ctx context.Context, host string) error {
	c, err := client.New(ctx, host)
	if err != nil {
		return err
	}

	// FIXME Does output "failed to wait: signal: broken pipe"
	defer c.Close()

	// Try to connect every 100ms up to 100 times (10 seconds total)
	const (
		retryPeriod   = 100 * time.Millisecond
		retryAttempts = 100
	)

	for retry := 0; retry < retryAttempts; retry++ {
		_, err = c.ListWorkers(ctx)
		if err == nil {
			return nil
		}
		time.Sleep(retryPeriod)
	}
	return errors.New("buildkit failed to respond")
}

func getBuildkitInformation(ctx context.Context, command, containerName string) (*buildkitInformation, error) {
	formatString := "{{.Config.Image}};{{.State.Running}};{{if index .NetworkSettings.Networks \"host\"}}{{\"true\"}}{{else}}{{\"false\"}}{{end}}"
	cmd := exec.CommandContext(ctx,
		command,
		"inspect",
		"--format",
		formatString,
		containerName,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}

	s := strings.Split(string(output), ";")

	// Retrieve the tag
	ref, err := reference.ParseNormalizedNamed(strings.TrimSpace(s[0]))
	if err != nil {
		return nil, err
	}
	tag, ok := ref.(reference.Tagged)
	if !ok {
		return nil, fmt.Errorf("failed to parse image: %s", output)
	}

	// Retrieve the state
	isActive, err := strconv.ParseBool(strings.TrimSpace(s[1]))
	if err != nil {
		return nil, err
	}

	// Retrieve the check on if the host network is configured
	haveHostNetwork, err := strconv.ParseBool(strings.TrimSpace(s[2]))
	if err != nil {
		return nil, err
	}

	return &buildkitInformation{
		Version:         tag.Tag(),
		IsActive:        isActive,
		HaveHostNetwork: haveHostNetwork,
	}, nil
}

type buildkitInformation struct {
	Version         string
	IsActive        bool
	HaveHostNetwork bool
}
