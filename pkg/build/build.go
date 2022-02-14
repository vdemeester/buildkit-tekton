package build

import (
	"context"

	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/frontend/dockerfile/dockerfile2llb"
	"github.com/moby/buildkit/frontend/gateway/client"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/vdemeester/buildkit-tekton/pkg/tekton"
)

const (
	localNameDockerfile   = "dockerfile"
	keyFilename           = "filename"
	defaultDockerfileName = "task.yaml"
)

func Build(ctx context.Context, c client.Client) (*client.Result, error) {
	cfg, err := GetDockerfile(ctx, c)
	if err != nil {
		return nil, errors.Wrap(err, "getting tekton task")
	}
	logrus.Infof("cfg: %v", cfg)
	st, err := tekton.TektonToLLB(cfg)
	if err != nil {
		return nil, err
	}

	logrus.Infof("st: %+v", st)
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

func GetDockerfile(ctx context.Context, c client.Client) (string, error) {
	logrus.Infof("ctx: %+v", ctx)
	opts := c.BuildOpts().Opts
	logrus.Infof("opts: %+v", opts)
	filename := opts[keyFilename]
	if filename == "" {
		filename = defaultDockerfileName
	}

	name := "load tekton"
	if filename != "task.yaml" {
		name += " from " + filename
	}

	logrus.Infof("filename: %s", filename)
	logrus.Infof("name: %s", name)
	logrus.Infof("localNameDockerfile: %s", localNameDockerfile)
	logrus.Infof("defaultDockerfileName: %s", defaultDockerfileName)
	src := llb.Local(localNameDockerfile,
		// llb.IncludePatterns([]string{filename, "*"}),
		llb.SessionID(c.BuildOpts().SessionID),
		// llb.SharedKeyHint(defaultDockerfileName),
		dockerfile2llb.WithInternalName(name),
	)

	def, err := src.Marshal(ctx)
	if err != nil {
		return "", errors.Wrapf(err, "failed to marshal local source")
	}

	logrus.Infof("def: %+v", def)
	// logrus.Infof("def.ToPB: %+v", def.ToPB())
	var dtDockerfile []byte
	res, err := c.Solve(ctx, client.SolveRequest{
		Definition: def.ToPB(),
	})
	if err != nil {
		return "", errors.Wrapf(err, "failed to resolve tekton.yaml")
	}

	logrus.Infof("res: %+v", res)
	ref, err := res.SingleRef()
	if err != nil {
		return "", err
	}

	logrus.Infof("ref: %+v", ref)
	state, _ := ref.ToState()
	logrus.Infof("ref.ToState: %+v", state)
	dtDockerfile, err = ref.ReadFile(ctx, client.ReadRequest{
		Filename: filename,
	})
	logrus.Infof("dtDockerfile: %+v", dtDockerfile)
	if err != nil {
		return "", errors.Wrapf(err, "failed to read tekton yaml")
	}

	return string(dtDockerfile), nil
}
