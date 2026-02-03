package tekton

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/distribution/reference"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/frontend/gateway/client"
	"github.com/pkg/errors"
	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"github.com/tektoncd/pipeline/pkg/reconciler/taskrun/resources"
	"github.com/vdemeester/buildkit-tekton/pkg/tekton/files"
	corev1 "k8s.io/api/core/v1"
)

const (
	defaultScriptPreamble = "#!/bin/sh\nset -e\n"
	scriptsDir            = "/tekton/scripts"
)

type pstep struct {
	name         string
	image        string // might be ref
	results      []mountOptionFn
	runOptions   []llb.RunOption
	workspaces   []mountOptionFn
	volumeMounts []mountOptionFn
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

	var ts *v1.TaskSpec
	var name string
	if tr.Spec.TaskSpec != nil {
		ts = tr.Spec.TaskSpec
		name = "embedded"
		// FIXME: we need to support this
		// } else if tr.Spec.TaskRef != nil && tr.Spec.TaskRef.Bundle != "" {
		// 	resolvedTask, err := resolveTaskInBundle(ctx, c, *tr.Spec.TaskRef)
		// 	if err != nil {
		// 		return llb.State{}, err
		// 	}
		// 	ts = &resolvedTask.Spec
		// 	name = tr.Spec.TaskRef.Name
	} else if tr.Spec.TaskRef != nil && tr.Spec.TaskRef.Name != "" {
		t, ok := r.tasks[tr.Spec.TaskRef.Name]
		if !ok {
			return llb.State{}, errors.Errorf("Taskref %s not found in context", tr.Spec.TaskRef.Name)
		}
		t.SetDefaults(ctx)
		ts = &t.Spec
		name = tr.Spec.TaskRef.Name
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
	steps, err := taskSpecToPSteps(ctx, c, spec, tr.Name, workspaces, nil, r.configs, r.secrets)
	if err != nil {
		return llb.State{}, errors.Wrap(err, "couldn't translate TaskSpec to builtkit llb")
	}

	resultState := llb.Scratch()
	stepStates, err := pstepToState(c, steps, resultState, []llb.RunOption{})
	if err != nil {
		return llb.State{}, err
	}
	return stepStates[len(stepStates)-1], nil
}

func applyTaskRunSubstitution(ctx context.Context, tr *v1.TaskRun, ts *v1.TaskSpec, taskName string) (v1.TaskSpec, error) {
	var defaults []v1.ParamSpec
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
	ts = resources.ApplyResults(ts)

	// Apply step exitCode path substitution
	ts = resources.ApplyStepExitCodePath(ts)

	if err := ts.Validate(ctx); err != nil {
		return *ts, err
	}
	return *ts, nil
}

func taskSpecToPSteps(ctx context.Context, c client.Client, t v1.TaskSpec, name string, workspaces []mountOptionFn, taskTimeout *time.Duration, configs map[string]*corev1.ConfigMap, secrets map[string]*corev1.Secret) ([]pstep, error) {
	steps := make([]pstep, len(t.Steps))
	cacheDirName := name + "/results"
	mergedSteps, err := v1.MergeStepsWithStepTemplate(t.StepTemplate, t.Steps)
	if err != nil {
		return steps, errors.Wrap(err, "couldn't merge steps with StepTemplate")
	}

	// Build volume states map for emptyDir volumes
	volumeStates := make(map[string]llb.State)
	for _, vol := range t.Volumes {
		if vol.EmptyDir != nil {
			// Use a persistent cache for emptyDir to share data between steps
			volumeStates[vol.Name] = llb.Scratch()
		}
	}

	for i, step := range mergedSteps {
		ref, err := reference.ParseNormalizedNamed(step.Image)
		if err != nil {
			return steps, err
		}

		// Check if this step should continue on error
		continueOnError := step.OnError == v1.Continue

		// Get step timeout as time.Duration pointer
		// If step has its own timeout, use it; otherwise fall back to task timeout
		var stepTimeout *time.Duration
		if step.Timeout != nil {
			d := step.Timeout.Duration
			stepTimeout = &d
		} else if taskTimeout != nil {
			// Use task timeout as fallback for steps without their own timeout
			stepTimeout = taskTimeout
		}

		runOptions := []llb.RunOption{
			llb.IgnoreCache,
			llb.WithCustomName("[tekton] " + name + "/" + step.Name),
		}
		if step.Script != "" {
			filename, scriptSt := files.Script(name+"/"+step.Name, fmt.Sprintf("script-%d", i), step.Script, continueOnError, stepTimeout)
			scriptFile := filepath.Join(scriptsDir, filename)
			runOptions = append(runOptions,
				llb.AddMount(scriptsDir, scriptSt, llb.SourcePath("/"), llb.Readonly),
				llb.Args([]string{scriptFile}),
			)
		} else if (continueOnError || stepTimeout != nil) && len(step.Command) > 0 {
			// For commands with OnError: continue or with timeout, wrap in a script
			filename, scriptSt := files.CommandWrapper(name+"/"+step.Name, fmt.Sprintf("cmd-%d", i), step.Command, step.Args, continueOnError, stepTimeout)
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
		// Handle EnvFrom - load environment variables from ConfigMaps/Secrets
		for _, envFrom := range step.EnvFrom {
			prefix := envFrom.Prefix
			if envFrom.ConfigMapRef != nil {
				cm, ok := configs[envFrom.ConfigMapRef.Name]
				if ok && cm != nil {
					for k, v := range cm.Data {
						runOptions = append(runOptions,
							llb.AddEnv(prefix+k, v),
						)
					}
				}
				// If ConfigMap not found and it's not optional, we could error
				// For now, silently skip if not found (similar to how Kubernetes handles optional refs)
			}
			if envFrom.SecretRef != nil {
				sec, ok := secrets[envFrom.SecretRef.Name]
				if ok && sec != nil {
					for k, v := range sec.Data {
						runOptions = append(runOptions,
							llb.AddEnv(prefix+k, string(v)),
						)
					}
				}
				// If Secret not found and it's not optional, we could error
				// For now, silently skip if not found
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

		// Handle VolumeMounts
		volumeMounts := []mountOptionFn{}
		for _, vm := range step.VolumeMounts {
			volName := vm.Name
			mountPath := vm.MountPath
			subPath := vm.SubPath
			readOnly := vm.ReadOnly

			if _, ok := volumeStates[volName]; ok {
				volumeMounts = append(volumeMounts, func(state llb.State) llb.RunOption {
					opts := []llb.MountOption{
						llb.AsPersistentCacheDir(name+"/volume/"+volName, llb.CacheMountShared),
					}
					if subPath != "" {
						opts = append(opts, llb.SourcePath(subPath))
					}
					if readOnly {
						opts = append(opts, llb.Readonly)
					}
					return llb.AddMount(mountPath, state, opts...)
				})
			}
		}

		steps[i] = pstep{
			name:         step.Name,
			image:        ref.String(),
			runOptions:   runOptions,
			results:      results,
			workspaces:   workspaces,
			volumeMounts: volumeMounts,
		}
	}
	return steps, nil
}

func pstepToState(c client.Client, steps []pstep, resultState llb.State, additionnalMounts []llb.RunOption) ([]llb.State, error) {
	stepStates := make([]llb.State, len(steps))
	for i, step := range steps {
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
		for _, wf := range step.workspaces {
			mounts = append(mounts, wf(stepStates[i]))
		}
		// Add volume mounts
		for _, vm := range step.volumeMounts {
			mounts = append(mounts, vm(resultState))
		}
		runOptions = append(runOptions, mounts...)
		runOptions = append(runOptions, additionnalMounts...)
		state := llb.
			Image(step.image, llb.WithMetaResolver(c)).
			Run(runOptions...).
			Root()
		stepStates[i] = state
	}
	return stepStates, nil
}

func validateTaskRun(ctx context.Context, tr *v1.TaskRun) error {
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

func validateTaskSpec(ctx context.Context, t v1.TaskSpec) error {
	if len(t.Sidecars) > 0 {
		return errors.New("Sidecars are not supported")
	}
	// Volumes are now supported (emptyDir only for now)
	for _, vol := range t.Volumes {
		if vol.EmptyDir == nil {
			return errors.Errorf("Volume %s: only emptyDir volumes are supported", vol.Name)
		}
	}
	for i, s := range t.Steps {
		// Step Timeout is now supported (wrapped with timeout command)
		// OnError is now supported (continue and stopAndFail)
		// EnvFrom is now supported (load env vars from ConfigMaps/Secrets)
		// VolumeMounts are now supported
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
