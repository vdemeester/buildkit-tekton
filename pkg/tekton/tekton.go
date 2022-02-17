package tekton

import (
	"context"
	"fmt"
	"strings"

	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/frontend/gateway/client"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	k8scheme "k8s.io/client-go/kubernetes/scheme"
)

type types struct {
	PipelineRuns []*v1beta1.PipelineRun
	TaskRuns     []*v1beta1.TaskRun
}

type task struct {
	steps []step
}

type step struct {
	s llb.State
}

type prestep struct {
	image      ocispecs.Image
	mounts     []llb.RunOption
	runoptions []llb.RunOption
}

// Only support TaskRun with embedded Task to start.
func TektonToLLB(c client.Client) func(context.Context, string) (llb.State, error) {
	return func(ctx context.Context, l string) (llb.State, error) {
		s := k8scheme.Scheme
		if err := v1beta1.AddToScheme(s); err != nil {
			return llb.State{}, err
		}

		types := readTypes(l)
		if len(types.TaskRuns) > 0 && len(types.PipelineRuns) > 0 {
			return llb.State{}, fmt.Errorf("failed to unmarshal %v, multiple objects not yet supported", l)
		} else if len(types.TaskRuns) == 0 && len(types.PipelineRuns) == 0 {
			return llb.State{}, fmt.Errorf("failed to unmarshal %v, unknown object", l)
		} else if len(types.TaskRuns) > 1 || len(types.PipelineRuns) > 1 {
			return llb.State{}, fmt.Errorf("failed to unmarshal %v, multiple objects not yet supported", l)
		}

		if len(types.TaskRuns) > 0 {
			return TaskRunToLLB(ctx, c, types.TaskRuns[0])
		} else if len(types.PipelineRuns) > 0 {
			return PipelineRunToLLB(ctx, c, types.PipelineRuns[0])
		}
		return llb.State{}, fmt.Errorf("Invalid state")
	}
}

func readTypes(data string) types {
	types := types{}
	decoder := k8scheme.Codecs.UniversalDeserializer()

	for _, doc := range strings.Split(strings.Trim(data, "-"), "---") {
		logrus.Debugf("fooo")
		if strings.TrimSpace(doc) == "" {
			continue
		}

		obj, _, err := decoder.Decode([]byte(doc), nil, nil)
		if err != nil {
			logrus.Infof("Skipping document not looking like a kubernetes resources: %v", err)
			continue
		}
		switch o := obj.(type) {
		case *v1beta1.PipelineRun:
			types.PipelineRuns = append(types.PipelineRuns, o)
		case *v1beta1.TaskRun:
			types.TaskRuns = append(types.TaskRuns, o)
		default:
			logrus.Info("Skipping document not looking like a tekton resource we can Resolve.")
		}
	}

	return types
}
