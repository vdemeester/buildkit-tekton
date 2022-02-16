package tekton

import (
	"fmt"
	"os"
	"strings"

	"github.com/moby/buildkit/client/llb"
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

// Only support TaskRun with embedded Task to start.
func TektonToLLB(l string) (llb.State, error) {

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
		return taskRunToLLB(types.TaskRuns[0])
	} else if len(types.PipelineRuns) > 0 {
		return pipelineRunToLLB(types.PipelineRuns[0])
	}
	return llb.State{}, fmt.Errorf("Invalid state")
}

func taskRunToLLB(tr *v1beta1.TaskRun) (llb.State, error) {
	steps, err := taskSpecToSteps(*tr.Spec.TaskSpec)
	return steps[len(steps)-1].s, err
}

func taskSpecToSteps(t v1beta1.TaskSpec) ([]step, error) {
	steps := make([]step, len(t.Steps))
	for i, s := range t.Steps {
		logrus.Infof("step: %s\n", s.Name)
		// TODO: support script (how?)
		runOpt := []llb.RunOption{
			llb.Args(append(s.Command, s.Args...)),
			// llb.Dir("/dest"), // FIXME: support workdir
			llb.IgnoreCache, // FIXME: see if we can enable the cache on some run
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
	return steps, nil
}

func pipelineRunToLLB(pr *v1beta1.PipelineRun) (llb.State, error) {
	tasks := map[string]task{}
	for _, t := range pr.Spec.PipelineSpec.Tasks {
		logrus.Infof("task: %s\n", t.Name)
		steps := make([]step, len(t.TaskSpec.TaskSpec.Steps))
		for j, s := range t.TaskSpec.TaskSpec.Steps {
			logrus.Infof("step: %s\n", s.Name)
			// TODO: support script (how?)
			runOpt := []llb.RunOption{
				llb.Args(append(s.Command, s.Args...)),
				// llb.Dir("/dest"), // FIXME: support workdir
				llb.IgnoreCache, // FIXME: see if we can enable the cache on some run
			}
			mounts := []llb.RunOption{
				llb.AddMount("/tekton/results", steps[j].s, llb.AsPersistentCacheDir("results", llb.CacheMountShared)),
			}
			if j > 0 {
				// TODO: mount previous results or something to create a dependency
				targetMount := fmt.Sprintf("/tekton-results/%d", j-1)
				mounts = append(mounts,
					llb.AddMount(targetMount, steps[j-1].s, llb.SourcePath("/tekton/results"), llb.Readonly),
				)
			}
			if len(t.RunAfter) > 0 {
				// RunAfter means, the first steps of the current Task needs to start after the last step of the referenced Task
				// We are going to use mounts here too.
				for _, a := range t.RunAfter {
					targetMount := fmt.Sprintf("/tekton/from-task/%s", a)
					mounts = append(mounts,
						llb.AddMount(targetMount, tasks[a].steps[len(tasks[a].steps)-1].s, llb.SourcePath("/tekton/results"), llb.Readonly),
					)
				}
			}
			step := step{}
			step.s = llb.Image(s.Image).Run(append(runOpt, mounts...)...).Root()
			steps[j] = step
		}
		tasks[t.Name] = task{
			steps: steps,
		}
		logrus.Infof("tasks: %+v", tasks)
	}
	ft := llb.Scratch()
	fa := llb.Mkdir("/task", os.FileMode(int(0777)))
	for n, t := range tasks {
		state := t.steps[len(t.steps)-1].s
		taskPath := fmt.Sprintf("/task/%s", n)
		fa = fa.Copy(state, "/tekton", taskPath, &llb.CopyInfo{FollowSymlinks: true, CreateDestPath: true, AllowWildcard: true, AllowEmptyWildcard: true})
	}
	return ft.File(fa), nil
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
