package tekton

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/docker/distribution/reference"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/frontend/gateway/client"
	"github.com/pkg/errors"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	"github.com/tektoncd/pipeline/pkg/reconciler/taskrun/resources"
	"github.com/vdemeester/buildkit-tekton/pkg/tekton/files"
	corev1 "k8s.io/api/core/v1"
)

const (
	defaultScriptPreamble = "#!/bin/sh\nset -e\n"
	scriptsDir            = "/tekton/scripts"
)

type pstep struct {
	name       string
	image      string // might be ref
	results    []mountOptionFn
	runOptions []llb.RunOption
	workspaces []mountOptionFn
}

type mountOptionFn func(llb.State) llb.RunOption

// TaskRunToLLB converts a TaskRun into a BuildKit LLB State.
func TaskRunToLLB(ctx context.Context, c client.Client, r TaskRun) (llb.State, error) {
	var err error
	tr := r.main
	// Validation
	if err = validateTaskRun(ctx, tr); err != nil {
		return llb.State{}, err
	}

	name, ts, err := resolveTaskNameAndSpec(ctx, tr.Spec.TaskSpec, tr.Spec.TaskRef, r.tasks, func(ctx context.Context, ref v1beta1.TaskRef) (*v1beta1.Task, error) {
		return resolveTaskInBundle(ctx, c, ref)
	})
	if err != nil {
		return llb.State{}, err
	}

	// Interpolation
	spec, err := applyTaskRunSubstitution(ctx, tr, ts, name)
	if err != nil {
		return llb.State{}, errors.Wrap(err, "variable interpolation failed")
	}

	// Execution
	workspaces := []mountOptionFn{}
	for _, w := range tr.Spec.Workspaces {
		workspaces = append(workspaces,
			func(state llb.State) llb.RunOption {
				return llb.AddMount(w.Name, state, llb.AsPersistentCacheDir(tr.Name+"/"+w.Name, llb.CacheMountShared))
			},
		)
	}
	steps, err := taskSpecToPSteps(ctx, c, spec, tr.Name, workspaces)
	if err != nil {
		return llb.State{}, errors.Wrap(err, "couldn't translate TaskSpec to builtkit llb")
	}

	stepStates, err := pstepToState(c, steps, []llb.RunOption{})
	if err != nil {
		return llb.State{}, err
	}
	return stepStates[len(stepStates)-1], nil
}

func applyTaskRunSubstitution(ctx context.Context, tr *v1beta1.TaskRun, ts *v1beta1.TaskSpec, taskName string) (v1beta1.TaskSpec, error) {
	var defaults []v1beta1.ParamSpec
	if len(ts.Params) > 0 {
		defaults = append(defaults, ts.Params...)
	}
	// Apply parameter substitution from the taskrun.
	ts = resources.ApplyParameters(ts, tr, defaults...)

	// Apply context substitution from the taskrun
	ts = resources.ApplyContexts(ts, taskName, tr)

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

func taskSpecToPSteps(ctx context.Context, c client.Client, t v1beta1.TaskSpec, name string, workspaces []mountOptionFn) ([]pstep, error) {
	steps := make([]pstep, len(t.Steps))
	cacheDirName := name + "/results"
	mergedSteps, err := v1beta1.MergeStepsWithStepTemplate(t.StepTemplate, t.Steps)
	if err != nil {
		return steps, errors.Wrap(err, "couldn't merge steps with StepTemplate")
	}
	for i, step := range mergedSteps {
		ref, err := reference.ParseNormalizedNamed(step.Image)
		if err != nil {
			return steps, err
		}
		runOptions := []llb.RunOption{
			llb.IgnoreCache,
			llb.WithCustomName("[tekton] " + name + "/" + step.Name),
		}
		if step.Script != "" {
			filename, scriptSt := files.Script(name+"/"+step.Name, fmt.Sprintf("script-%d", i), step.Script)
			scriptFile := filepath.Join(scriptsDir, filename)
			runOptions = append(runOptions,
				llb.AddMount(scriptsDir, scriptSt, llb.SourcePath("/"), llb.Readonly),
				llb.Args([]string{scriptFile}),
			)
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
		if len(step.Env) > 0 {
			for _, e := range step.Env {
				runOptions = append(runOptions,
					llb.AddEnv(e.Name, e.Value),
				)
			}
		}
		if step.SecurityContext != nil {
			if step.SecurityContext.RunAsUser != nil {
				user := fmt.Sprintf("%d", *step.SecurityContext.RunAsUser)
				runOptions = append(runOptions,
					llb.With(llb.User(user)),
				)
			}
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

func pstepToState(c client.Client, steps []pstep, additionnalMounts []llb.RunOption) ([]llb.State, error) {
	stepStates := make([]llb.State, len(steps))
	for i, step := range steps {
		runOptions := step.runOptions
		mounts := []llb.RunOption{}
		// mounts := make([]llb.RunOption, len(step.results))
		// for i, r := range step.results {
		// 	mounts[i] = r(resultState)
		// }
		// If not the first step, we need to create the chain to execute things in sequence
		input := llb.Image(step.image, llb.WithMetaResolver(c)).
			File(llb.Mkdir("/tekton/results", os.FileMode(int(0777)), llb.WithParents(true)))
		if i > 0 {
			// TODO decide what to mount exactly
			// targetMount := fmt.Sprintf("/previous/%d", i-1)
			// mounts = append(mounts,
			// 	llb.AddMount(targetMount, stepStates[i-1], llb.SourcePath("/"), llb.Readonly),
			// )
			results := llb.Copy(stepStates[i-1], "/tekton/results", "/tekton/results", &llb.CopyInfo{FollowSymlinks: true, CreateDestPath: true, AllowWildcard: true, AllowEmptyWildcard: true, CopyDirContentsOnly: true})
			input = input.File(results, llb.IgnoreCache)
		}
		for _, wf := range step.workspaces {
			mounts = append(mounts, wf(stepStates[i]))
		}
		runOptions = append(runOptions, mounts...)
		runOptions = append(runOptions, additionnalMounts...)
		state := input.
			Run(runOptions...).
			Root()
		stepStates[i] = state
	}
	return stepStates, nil
}

func validateTaskRun(ctx context.Context, tr *v1beta1.TaskRun) error {
	if tr.Name == "" && tr.GenerateName != "" {
		tr.Name = tr.GenerateName + "generated"
	}
	tr.SetDefaults(ctx)
	if err := tr.Validate(ctx); err != nil {
		return errors.Wrapf(err, "validation failed for Taskrun %s", tr.Name)
	}
	if tr.Spec.PodTemplate != nil {
		return errors.New("PodTemplate not supported")
	}
	if tr.Spec.TaskSpec != nil {
		return validateTaskSpec(ctx, *tr.Spec.TaskSpec)
	}
	return nil
}

func validateTaskSpec(ctx context.Context, t v1beta1.TaskSpec) error {
	if t.Resources != nil {
		return errors.New("PipelineResources are not supported")
	}
	if len(t.Sidecars) > 0 {
		return errors.New("Sidecars are not supported")
	}
	if len(t.Volumes) > 0 {
		return errors.New("Volumes not supported")
	}
	for i, s := range t.Steps {
		if s.Timeout != nil {
			return errors.Errorf("Step %d: Timeout not supported", i)
		}
		if s.OnError != "" {
			return errors.Errorf("Step %d: OnError not supported", i)
		}
		if len(s.EnvFrom) > 0 {
			return errors.Errorf("Step %d: EnvFrom not supported", i)
		}
		if len(s.VolumeMounts) > 0 {
			return errors.Errorf("Step %d: VolumeMounts not supported", i)
		}
		if len(s.VolumeDevices) > 0 {
			return errors.Errorf("Step %d: VolumeDevices not supported", i)
		}
		// Silently ignore Ports
		// Silently ignore LivenessProbe
		// Silently ignore ReadinessProbe
		// Silently ignore StartupProbe
		// Silently ignore Lifecycle
		// Silently ignore TerminationMessagePath
		// Silently ignore TerminationMessagePolicy
		// Silently ignore Resources (for now)
		// Silently ignore ImagePullPolicy
		// Silently ignore Stdin
		// Silently ignore StdinOnce
		// Silently ignore TTY
	}
	return nil
}
