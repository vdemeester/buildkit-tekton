package tekton

import (
	"fmt"
	// "strings"

	"github.com/ghodss/yaml"
	"github.com/moby/buildkit/client/llb"
	"github.com/sirupsen/logrus"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	// "github.com/tektoncd/pipeline/pkg/client/clientset/versioned/scheme"
)

type builder struct {
	s llb.State
}

type step struct {
	s llb.State
}
type steps []step

// Only support TaskRun with embedded Task to start.
func TektonToLLB(l string) (llb.State, error) {
	b := builder{}
	t := &v1beta1.TaskRun{}
	if err := yaml.Unmarshal([]byte(l), t); err != nil {
		return b.s, fmt.Errorf("failed to unmarshal %v", l)
	}

	steps := make([]step, len(t.Spec.TaskSpec.Steps))
	for i, s := range t.Spec.TaskSpec.Steps {
		logrus.Infof("- step: %s\n", s.Name)
		// TODO: support script (how?)
		runOpt := []llb.RunOption{
			llb.Args(append(s.Command, s.Args...)),
			// llb.Dir("/dest"), // FIXME: support workdir
		}
		mounts := []llb.RunOption{
			llb.AddMount("/tekton/results", steps[i].s, llb.AsPersistentCacheDir("results", llb.CacheMountShared)),
		}
		if i > 0 {
			// TODO: mount previous results or something to create a dependency
			targetMount := fmt.Sprintf("/tekton-results/%d", i-1)
			mounts = append(mounts,
				llb.AddMount(targetMount, steps[i-1].s, llb.SourcePath("/tekton/results"), llb.Readonly),
			)
		}
		step := step{}
		step.s = llb.Image(s.Image).Run(append(runOpt, mounts...)...).Root()
		steps[i] = step
	}
	return steps[len(steps)-1].s, nil
}
