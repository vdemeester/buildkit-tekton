package build

import (
	"context"

	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/frontend/dockerfile/dockerfile2llb"
	"github.com/moby/buildkit/frontend/gateway/client"
	"github.com/pkg/errors"
	"github.com/vdemeester/buildkit-tekton/pkg/tekton"
)

const (
	localNameDockerfile = "dockerfile" // This is there to make it work with docker build -f …
	keyFilename         = "filename"
	defaultTaskName     = "task.yaml"
)

func Build(ctx context.Context, c client.Client) (*client.Result, error) {
	cfg, err := GetTektonResource(ctx, c)
	if err != nil {
		return nil, errors.Wrap(err, "getting tekton task")
	}
	st, err := tekton.TektonToLLB(c)(ctx, cfg)
	if err != nil {
		return nil, err
	}

	def, err := st.Marshal(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to marshal local source")
	}
	res, err := c.Solve(ctx, client.SolveRequest{
		Definition: def.ToPB(),
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to resolve dockerfile")
	}
	ref, err := res.SingleRef()
	if err != nil {
		return nil, err
	}

	res.SetRef(ref)

	return res, nil
}

func GetTektonResource(ctx context.Context, c client.Client) (string, error) {
	opts := c.BuildOpts().Opts
	filename := opts[keyFilename]
	if filename == "" {
		filename = defaultTaskName
	}

	name := "load tekton"
	if filename != "task.yaml" {
		name += " from " + filename
	}

	src := llb.Local(localNameDockerfile,
		// llb.IncludePatterns([]string{filename, "*"}),
		llb.SessionID(c.BuildOpts().SessionID),
		// llb.SharedKeyHint(defaultTaskName),
		dockerfile2llb.WithInternalName(name),
	)

	def, err := src.Marshal(ctx)
	if err != nil {
		return "", errors.Wrapf(err, "failed to marshal local source")
	}

	var dtDockerfile []byte
	res, err := c.Solve(ctx, client.SolveRequest{
		Definition: def.ToPB(),
	})
	if err != nil {
		return "", errors.Wrapf(err, "failed to resolve tekton.yaml")
	}

	ref, err := res.SingleRef()
	if err != nil {
		return "", err
	}

	dtDockerfile, err = ref.ReadFile(ctx, client.ReadRequest{
		Filename: filename,
	})
	if err != nil {
		return "", errors.Wrapf(err, "failed to read tekton yaml")
	}

	return string(dtDockerfile), nil
}
