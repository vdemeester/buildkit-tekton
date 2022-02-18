package tekton

import (
	"context"
	"fmt"

	"github.com/docker/distribution/reference"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/frontend/gateway/client"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	"github.com/tektoncd/pipeline/pkg/reconciler/taskrun/resources"
	corev1 "k8s.io/api/core/v1"
)

type pstep struct {
	name       string
	image      string // might be ref
	results    []mountOptionFn
	runOptions []llb.RunOption
	workspaces map[string]llb.MountOption
}

type mountOptionFn func(llb.State) llb.RunOption

func TaskRunToLLB(ctx context.Context, c client.Client, tr *v1beta1.TaskRun) (llb.State, error) {
	// Validation
	if tr.Name == "" && tr.GenerateName != "" {
		tr.Name = tr.GenerateName + "generated"
	}
	tr.SetDefaults(ctx)
	if err := tr.Validate(ctx); err != nil {
		return llb.State{}, errors.Wrapf(err, "validation failed for Taskrun %s", tr.Name)
	}
	if tr.Spec.TaskSpec == nil {
		return llb.State{}, errors.New("TaskRef not supported")
	}
	// TODO(vdemeester) bail out on other unsupported field, like PipelineResources, …

	// Interpolation
	ts, err := applyTaskRunSubstitution(ctx, tr)
	if err != nil {
		return llb.State{}, errors.Wrap(err, "variable interpolation failed")
	}
	logrus.Infof("TaskSpec: %+v", ts)

	// Execution
	workspaces := map[string]llb.MountOption{}
	for _, w := range tr.Spec.Workspaces {
		workspaces[w.Name] = llb.AsPersistentCacheDir(tr.Name+"/"+w.Name, llb.CacheMountShared)
	}
	steps, err := taskSpecToPSteps(ctx, c, ts, tr.Name, workspaces)
	if err != nil {
		return llb.State{}, errors.Wrap(err, "couldn't translate TaskSpec to builtkit llb")
	}

	resultState := llb.Scratch()
	logrus.Infof("steps: %+v", steps)
	stepStates, err := pstepToState(c, steps, resultState, []llb.RunOption{})
	if err != nil {
		return llb.State{}, err
	}
	return stepStates[len(stepStates)-1], nil
}

func applyTaskRunSubstitution(ctx context.Context, tr *v1beta1.TaskRun) (v1beta1.TaskSpec, error) {
	ts := tr.Spec.TaskSpec.DeepCopy()

	var defaults []v1beta1.ParamSpec
	if len(ts.Params) > 0 {
		defaults = append(defaults, ts.Params...)
	}
	// Apply parameter substitution from the taskrun.
	ts = resources.ApplyParameters(ts, tr, defaults...)

	// Apply context substitution from the taskrun
	ts = resources.ApplyContexts(ts, &resources.ResolvedTaskResources{TaskName: "embedded"}, tr) // FIXME(vdemeester) handle this "embedded" better

	// TODO(vdemeester) support PipelineResource ?
	// Apply bound resource substitution from the taskrun.
	// ts = resources.ApplyResources(ts, inputResources, "inputs")
	// ts = resources.ApplyResources(ts, outputResources, "outputs")

	// Apply workspace resource substitution
	workspaceVolumes := map[string]corev1.Volume{}
	for _, v := range tr.Spec.Workspaces {
		workspaceVolumes[v.Name] = corev1.Volume{Name: v.Name}
	}
	ts = resources.ApplyWorkspaces(ctx, ts, ts.Workspaces, tr.Spec.Workspaces, workspaceVolumes)

	// Apply task result substitution
	ts = resources.ApplyTaskResults(ts)

	// Apply step exitCode path substitution
	ts = resources.ApplyStepExitCodePath(ts)

	if err := ts.Validate(ctx); err != nil {
		return *ts, err
	}
	return *ts, nil
}

func taskSpecToPSteps(ctx context.Context, c client.Client, t v1beta1.TaskSpec, name string, workspaces map[string]llb.MountOption) ([]pstep, error) {
	steps := make([]pstep, len(t.Steps))
	cacheDirName := name + "/results"
	taskWorkspaces := map[string]llb.MountOption{}
	for _, w := range t.Workspaces {
		taskWorkspaces["/workspace/"+w.Name] = workspaces[w.Name]
	}
	logrus.Infof("+taskWorkspaces: %+v", taskWorkspaces)
	for i, step := range t.Steps {
		ref, err := reference.ParseNormalizedNamed(step.Image)
		if err != nil {
			return steps, err
		}
		runOptions := []llb.RunOption{
			llb.IgnoreCache,
			llb.WithCustomName(name + "/" + step.Name),
		}
		if step.Script != "" {
			return steps, errors.New("script not supported")
		} else {
			runOptions = append(runOptions,
				llb.Args(append(step.Command, step.Args...)),
			)
		}
		if step.WorkingDir != "" {
			runOptions = append(runOptions,
				llb.With(llb.Dir(step.WorkingDir)),
			)
		}
		results := []mountOptionFn{
			func(state llb.State) llb.RunOption {
				return llb.AddMount("/tekton/results", state, llb.AsPersistentCacheDir(cacheDirName, llb.CacheMountShared))
			},
		}
		steps[i] = pstep{
			name:       step.Name,
			image:      ref.String(),
			runOptions: runOptions,
			results:    results,
			workspaces: workspaces,
		}
	}
	return steps, nil
}

func pstepToState(c client.Client, steps []pstep, resultState llb.State, additionnalMounts []llb.RunOption) ([]llb.State, error) {
	stepStates := make([]llb.State, len(steps))
	for i, step := range steps {
		logrus.Infof("step-%d: %s", i, step.name)
		runOptions := step.runOptions
		mounts := make([]llb.RunOption, len(step.results))
		for i, r := range step.results {
			mounts[i] = r(resultState)
		}
		// If not the first step, we need to create the chain to execute things in sequence
		if i > 0 {
			// TODO decide what to mount exactly
			targetMount := fmt.Sprintf("/previous/%d", i-1)
			mounts = append(mounts,
				llb.AddMount(targetMount, stepStates[i-1], llb.SourcePath("/"), llb.Readonly),
			)
		}
		for workspacePath, workspaceOptions := range step.workspaces {
			logrus.Infof("Mount in %s: %+v", workspacePath, workspaceOptions)
			mounts = append(mounts,
				llb.AddMount(workspacePath, stepStates[i], workspaceOptions),
			)
		}
		runOptions = append(runOptions, mounts...)
		runOptions = append(runOptions, additionnalMounts...)
		state := llb.
			Image(step.image, llb.WithMetaResolver(c), llb.WithCustomName("load metadata from +"+step.image)).
			Run(runOptions...).
			Root()
		stepStates[i] = state
	}
	return stepStates, nil
}
