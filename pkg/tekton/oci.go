package tekton

import (
	"context"

	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/frontend/gateway/client"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	k8scheme "k8s.io/client-go/kubernetes/scheme"
)

func resolveTaskInBundle(ctx context.Context, c client.Client, taskref v1beta1.TaskRef) (*v1beta1.Task, error) {
	var task *v1beta1.Task
	obj, err := resolveBundle(ctx, c, taskref.Bundle, taskref.Name)
	if err != nil {
		return nil, err
	}
	switch o := obj.(type) {
	case *v1beta1.Task:
		task = o
	default:
		return nil, errors.Errorf("Unknow type: %+v", o)
	}
	task.SetDefaults(ctx)
	return task, nil
}

func resolvePipelineInBundle(ctx context.Context, c client.Client, pipelineref v1beta1.PipelineRef) (*v1beta1.Pipeline, error) {
	var pipeline *v1beta1.Pipeline
	obj, err := resolveBundle(ctx, c, pipelineref.Bundle, pipelineref.Name)
	if err != nil {
		return nil, err
	}
	switch o := obj.(type) {
	case *v1beta1.Pipeline:
		pipeline = o
	default:
		return nil, errors.Errorf("Unknow type: %+v", o)
	}
	pipeline.SetDefaults(ctx)
	return pipeline, nil
}

func resolveBundle(ctx context.Context, c client.Client, bundle, name string) (runtime.Object, error) {
	def, err := llb.Image(bundle).Marshal(ctx)
	if err != nil {
		return nil, err
	}
	res, err := c.Solve(ctx, client.SolveRequest{
		Definition: def.ToPB(),
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to resolve oci bundle: %s", bundle)
	}

	ref, err := res.SingleRef()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to resolve oci bundle: %s", bundle)
	}

	dt, err := ref.ReadFile(ctx, client.ReadRequest{
		Filename: name,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to resolve %s in oci bundle: %s", name, bundle)
	}
	logrus.Infof("dt: %s", string(dt))
	decoder := k8scheme.Codecs.UniversalDeserializer()
	obj, _, err := decoder.Decode(dt, nil, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to resolve %s in oci bundle: %s", name, bundle)
	}
	return obj, nil
}
