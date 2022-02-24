package tekton

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/docker/distribution/reference"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/frontend/gateway/client"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	"github.com/tektoncd/pipeline/pkg/names"
	"github.com/tektoncd/pipeline/pkg/reconciler/taskrun/resources"
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
	workspaces map[string]llb.MountOption
}

type mountOptionFn func(llb.State) llb.RunOption

// TaskRunToLLB converts a TaskRun into a BuildKit LLB State.
func TaskRunToLLB(ctx context.Context, c client.Client, tr *v1beta1.TaskRun) (llb.State, error) {
	var err error
	// Validation
	if err = validateTaskRun(ctx, tr); err != nil {
		return llb.State{}, err
	}

	var ts *v1beta1.TaskSpec
	if tr.Spec.TaskSpec != nil {
		ts = tr.Spec.TaskSpec
	} else if tr.Spec.TaskRef != nil && tr.Spec.TaskRef.Bundle != "" {
		resolvedTask, err := resolveTaskInBundle(ctx, c, *tr.Spec.TaskRef)
		if err != nil {
			return llb.State{}, err
		}
		ts = &resolvedTask.Spec
	}

	// Interpolation
	spec, err := applyTaskRunSubstitution(ctx, tr, ts)
	if err != nil {
		return llb.State{}, errors.Wrap(err, "variable interpolation failed")
	}

	// Execution
	workspaces := map[string]llb.MountOption{}
	for _, w := range tr.Spec.Workspaces {
		workspaces[w.Name] = llb.AsPersistentCacheDir(tr.Name+"/"+w.Name, llb.CacheMountShared)
	}
	steps, err := taskSpecToPSteps(ctx, c, spec, tr.Name, workspaces)
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

func applyTaskRunSubstitution(ctx context.Context, tr *v1beta1.TaskRun, ts *v1beta1.TaskSpec) (v1beta1.TaskSpec, error) {
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
	mergedSteps, err := v1beta1.MergeStepsWithStepTemplate(t.StepTemplate, t.Steps)
	if err != nil {
		return steps, errors.Wrap(err, "couldn't merge steps with StepTemplate")
	}
	for i, step := range mergedSteps {
		logrus.Infof("steps.image: %s", step.Image)
		ref, err := reference.ParseNormalizedNamed(step.Image)
		if err != nil {
			return steps, err
		}
		runOptions := []llb.RunOption{
			llb.IgnoreCache,
			llb.WithCustomName(name + "/" + step.Name),
		}
		if step.Script != "" {
			// Check for a shebang, and add a default if it's not set.
			// The shebang must be the first non-empty line.
			cleaned := strings.TrimSpace(step.Script)
			hasShebang := strings.HasPrefix(cleaned, "#!")

			script := step.Script
			if !hasShebang {
				script = defaultScriptPreamble + step.Script
			}
			filename := names.SimpleNameGenerator.RestrictLengthWithRandomSuffix(fmt.Sprintf("script-%d", i))
			scriptFile := filepath.Join(scriptsDir, filename)
			sourcePath := "/"
			data := script
			scriptSt := llb.Scratch().Dir("/").File(
				llb.Mkfile(filename, 0755, []byte(data)),
				llb.WithCustomName(name+"/"+step.Name+": preparing script"),
			)
			runOptions = append(runOptions,
				llb.AddMount(scriptsDir, scriptSt, llb.SourcePath(sourcePath), llb.Readonly),
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

func pstepToState(c client.Client, steps []pstep, resultState llb.State, additionnalMounts []llb.RunOption) ([]llb.State, error) {
	stepStates := make([]llb.State, len(steps))
	for i, step := range steps {
		logrus.Infof("step-%d: %s", i, step.name)
		logrus.Infof("step-%d: %s", i, step.image)
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

func validateTaskRun(ctx context.Context, tr *v1beta1.TaskRun) error {
	if tr.Name == "" && tr.GenerateName != "" {
		tr.Name = tr.GenerateName + "generated"
	}
	tr.SetDefaults(ctx)
	if err := tr.Validate(ctx); err != nil {
		return errors.Wrapf(err, "validation failed for Taskrun %s", tr.Name)
	}
	if tr.Spec.TaskRef != nil {
		if tr.Spec.TaskRef.Bundle == "" {
			return errors.New("TaskRef is only supported with bundle")
		}
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
