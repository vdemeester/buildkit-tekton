package tekton

import (
	"context"
	"fmt"
	"time"

	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/frontend/gateway/client"
	"github.com/pkg/errors"
	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"github.com/tektoncd/pipeline/pkg/reconciler/pipelinerun/resources"
	"github.com/vdemeester/buildkit-tekton/pkg/tekton/files"
	"k8s.io/apimachinery/pkg/runtime"
)

type pipelineMountOptionFn func(string) mountOptionFn

// PipelineRunToLLB converts a PipelineRun into a BuildKit LLB State.
func PipelineRunToLLB(ctx context.Context, c client.Client, r PipelineRun) (llb.State, error) {
	pr := r.main
	// Validation
	if err := validatePipelineRun(ctx, pr); err != nil {
		return llb.State{}, err
	}

	var ps *v1.PipelineSpec
	var name string
	if pr.Spec.PipelineSpec != nil {
		ps = pr.Spec.PipelineSpec
		name = "embedded"
		// FIXME: we need to support this
		// } else if pr.Spec.PipelineRef != nil && pr.Spec.PipelineRef.Bundle != "" {
		// 	resolvedPipeline, err := resolvePipelineInBundle(ctx, c, *pr.Spec.PipelineRef)
		// 	if err != nil {
		// 		return llb.State{}, err
		// 	}
		// 	ps = &resolvedPipeline.Spec
		// 	name = pr.Spec.PipelineRef.Name
	} else if pr.Spec.PipelineRef != nil && pr.Spec.PipelineRef.Name != "" {
		p, ok := r.pipelines[pr.Spec.PipelineRef.Name]
		if !ok {
			return llb.State{}, errors.Errorf("PipelineRef %s not found in context", pr.Spec.PipelineRef.Name)
		}
		p.SetDefaults(ctx)
		ps = &p.Spec
		name = pr.Spec.PipelineRef.Name
	}

	// Interpolation
	spec, err := applyPipelineRunSubstitution(ctx, pr, ps, name)
	if err != nil {
		return llb.State{}, errors.Wrap(err, "variable interpolation failed")
	}

	// Execution
	pipelineWorkspaces := map[string]pipelineMountOptionFn{}
	for _, w := range pr.Spec.Workspaces {
		switch {
		case w.ConfigMap != nil:
			configmap, ok := r.configs[w.ConfigMap.Name]
			if !ok {
				return llb.State{}, errors.Errorf("Configmap %s not found in context", w.ConfigMap.Name)
			}
			configmapState, err := files.ConfigMap(configmap, w.ConfigMap)
			if err != nil {
				return llb.State{}, err
			}
			pipelineWorkspaces[w.Name] = func(name string) mountOptionFn {
				return func(_ llb.State) llb.RunOption {
					return llb.AddMount(name, configmapState, llb.SourcePath("/"), llb.Readonly)
				}
			}
		case w.Secret != nil:
			secret, ok := r.secrets[w.Secret.SecretName]
			if !ok {
				return llb.State{}, errors.Errorf("secret %s not found in context", w.Secret.SecretName)
			}
			secretState, err := files.Secret(secret, w.Secret)
			if err != nil {
				return llb.State{}, err
			}
			pipelineWorkspaces[w.Name] = func(name string) mountOptionFn {
				return func(_ llb.State) llb.RunOption {
					return llb.AddMount(name, secretState, llb.SourcePath("/"), llb.Readonly)
				}
			}
		case w.EmptyDir != nil ||
			w.VolumeClaimTemplate != nil ||
			w.PersistentVolumeClaim != nil:
			pipelineWorkspaces[w.Name] = func(name string) mountOptionFn {
				return func(state llb.State) llb.RunOption {
					return llb.AddMount(name, state, llb.AsPersistentCacheDir(pr.Name+"/"+w.Name, llb.CacheMountShared))
				}
			}
		}
	}
	tasks := map[string][]llb.State{}
	skippedTasks := map[string]bool{} // Track tasks skipped due to WhenExpressions
	for _, t := range spec.Tasks {
		// Evaluate WhenExpressions - skip task if conditions not met
		if len(t.When) > 0 {
			if !evaluateWhenExpressions(t.When) {
				skippedTasks[t.Name] = true
				continue
			}
		}

		var ts v1.TaskSpec
		var name string
		if t.TaskRef != nil {
			name = t.TaskRef.Name
			// FIXME: we need to support this
			// if t.TaskRef.Bundle != "" {
			// 	resolvedTask, err := resolveTaskInBundle(ctx, c, *t.TaskRef)
			// 	if err != nil {
			// 		return llb.State{}, err
			// 	}
			// 	ts = resolvedTask.Spec
			// } else {
			task, ok := r.tasks[t.TaskRef.Name]
			if !ok {
				return llb.State{}, errors.Errorf("Taskref %s not found in context", t.TaskRef.Name)
			}
			task.SetDefaults(ctx)
			ts = task.Spec
			// }
		} else if t.TaskSpec != nil {
			name = "embedded"
			ts = t.TaskSpec.TaskSpec
		}

		ts, err = applyTaskRunSubstitution(ctx, &v1.TaskRun{
			Spec: v1.TaskRunSpec{
				Params:   t.Params,
				TaskSpec: &ts,
			},
		}, &ts, name)
		if err != nil {
			return llb.State{}, errors.Wrapf(err, "variable interpolation failed for %s", t.Name)
		}

		taskWorkspaces := []mountOptionFn{}
		for _, w := range t.Workspaces {
			fn := pipelineWorkspaces[w.Workspace]
			taskWorkspaces = append(taskWorkspaces, fn("/workspace/"+w.Name))
		}
		// Get task timeout as time.Duration pointer
		var taskTimeout *time.Duration
		if t.Timeout != nil {
			d := t.Timeout.Duration
			taskTimeout = &d
		}
		steps, err := taskSpecToPSteps(ctx, c, ts, t.Name, taskWorkspaces, taskTimeout, r.configs, r.secrets)
		if err != nil {
			return llb.State{}, errors.Wrap(err, "couldn't translate TaskSpec to llb")
		}
		mounts := []llb.RunOption{}
		if len(t.RunAfter) > 0 {
			// RunAfter means, the first steps of the current Task needs to start after the last step of the referenced Task
			// We create dependencies by mounting the previous task's state (for ordering) and its results cache
			for _, a := range t.RunAfter {
				// Mount previous task's state for dependency ordering (mount the root as a hidden path)
				depMount := fmt.Sprintf("/tekton/.deps/%s", a)
				mounts = append(mounts,
					llb.AddMount(depMount, tasks[a][len(tasks[a])-1], llb.SourcePath("/"), llb.Readonly),
				)
				// Mount previous task's results cache to access its results
				targetMount := fmt.Sprintf("/tekton/from-task/%s", a)
				mounts = append(mounts,
					llb.AddMount(targetMount, llb.Scratch(), llb.AsPersistentCacheDir(a+"/results", llb.CacheMountShared), llb.Readonly),
				)
			}
		}
		resultState := llb.Scratch()
		stepStates, err := pstepToState(c, steps, resultState, mounts)
		if err != nil {
			return llb.State{}, err
		}
		tasks[t.Name] = stepStates
	}

	// Process Finally blocks - they run after ALL regular tasks complete
	finallyTasks := map[string][]llb.State{}
	if len(spec.Finally) > 0 {
		// Build mounts from all regular tasks to ensure Finally runs after them
		finallyMounts := []llb.RunOption{}
		for taskName, taskStates := range tasks {
			if len(taskStates) > 0 {
				// Mount previous task's state for dependency ordering
				depMount := fmt.Sprintf("/tekton/.deps/%s", taskName)
				finallyMounts = append(finallyMounts,
					llb.AddMount(depMount, taskStates[len(taskStates)-1], llb.SourcePath("/"), llb.Readonly),
				)
				// Mount previous task's results cache to access its results
				targetMount := fmt.Sprintf("/tekton/from-task/%s", taskName)
				finallyMounts = append(finallyMounts,
					llb.AddMount(targetMount, llb.Scratch(), llb.AsPersistentCacheDir(taskName+"/results", llb.CacheMountShared), llb.Readonly),
				)
			}
		}

		for _, t := range spec.Finally {
			var ts v1.TaskSpec
			var name string
			if t.TaskRef != nil {
				name = t.TaskRef.Name
				task, ok := r.tasks[t.TaskRef.Name]
				if !ok {
					return llb.State{}, errors.Errorf("Finally Taskref %s not found in context", t.TaskRef.Name)
				}
				task.SetDefaults(ctx)
				ts = task.Spec
			} else if t.TaskSpec != nil {
				name = "embedded"
				ts = t.TaskSpec.TaskSpec
			}

			ts, err = applyTaskRunSubstitution(ctx, &v1.TaskRun{
				Spec: v1.TaskRunSpec{
					Params:   t.Params,
					TaskSpec: &ts,
				},
			}, &ts, name)
			if err != nil {
				return llb.State{}, errors.Wrapf(err, "variable interpolation failed for finally task %s", t.Name)
			}

			taskWorkspaces := []mountOptionFn{}
			for _, w := range t.Workspaces {
				fn := pipelineWorkspaces[w.Workspace]
				if fn != nil {
					taskWorkspaces = append(taskWorkspaces, fn("/workspace/"+w.Name))
				}
			}
			// Get task timeout as time.Duration pointer
			var taskTimeout *time.Duration
			if t.Timeout != nil {
				d := t.Timeout.Duration
				taskTimeout = &d
			}
			steps, err := taskSpecToPSteps(ctx, c, ts, "finally/"+t.Name, taskWorkspaces, taskTimeout, r.configs, r.secrets)
			if err != nil {
				return llb.State{}, errors.Wrap(err, "couldn't translate Finally TaskSpec to llb")
			}
			resultState := llb.Scratch()
			stepStates, err := pstepToState(c, steps, resultState, finallyMounts)
			if err != nil {
				return llb.State{}, err
			}
			finallyTasks[t.Name] = stepStates
		}
	}

	// Build the final result state by mounting all task results caches
	// First, collect all task states to establish dependencies
	allStates := []llb.State{}
	resultCacheMounts := []llb.RunOption{}

	for n, t := range tasks {
		if len(t) > 0 {
			allStates = append(allStates, t[len(t)-1])
			// Mount the results cache for this task
			resultCacheMounts = append(resultCacheMounts,
				llb.AddMount(fmt.Sprintf("/task/%s", n), llb.Scratch(), llb.AsPersistentCacheDir(n+"/results", llb.CacheMountShared), llb.Readonly),
			)
		}
	}
	for n, t := range finallyTasks {
		if len(t) > 0 {
			allStates = append(allStates, t[len(t)-1])
			resultCacheMounts = append(resultCacheMounts,
				llb.AddMount(fmt.Sprintf("/task/finally/%s", n), llb.Scratch(), llb.AsPersistentCacheDir("finally/"+n+"/results", llb.CacheMountShared), llb.Readonly),
			)
		}
	}

	// Create dependencies on all task states
	depMounts := []llb.RunOption{}
	for i, state := range allStates {
		depMounts = append(depMounts,
			llb.AddMount(fmt.Sprintf("/.dep/%d", i), state, llb.SourcePath("/"), llb.Readonly),
		)
	}

	// Combine all mounts and run a simple command to produce output
	runOpts := []llb.RunOption{llb.Args([]string{"/bin/sh", "-c", "ls -la /task 2>/dev/null || true"})}
	runOpts = append(runOpts, depMounts...)
	runOpts = append(runOpts, resultCacheMounts...)
	runOpts = append(runOpts, llb.WithCustomName("[tekton] collecting results"))

	return llb.Image("alpine:latest", llb.WithMetaResolver(c)).
		Run(runOpts...).
		Root(), nil
}

func applyPipelineRunSubstitution(ctx context.Context, pr *v1.PipelineRun, ps *v1.PipelineSpec, pipelineName string) (v1.PipelineSpec, error) {
	var err error
	ps, err = resources.ApplyParameters(ps, pr)
	if err != nil {
		return v1.PipelineSpec{}, err
	}
	ps = resources.ApplyContexts(ps, pipelineName, pr)
	ps = resources.ApplyWorkspaces(ps, pr)

	if err := ps.Validate(ctx); err != nil {
		return *ps, err
	}

	return *ps, nil
}

func validatePipelineRun(ctx context.Context, pr *v1.PipelineRun) error {
	if pr.Name == "" && pr.GenerateName != "" {
		pr.Name = pr.GenerateName + "generated"
	}
	pr.SetDefaults(ctx)
	if err := pr.Validate(ctx); err != nil {
		return errors.Wrapf(err, "validation failed for PipelineRun %s", pr.Name)
	}
	// SilentlyIgnore ServiceAccountName
	// SilentlyIgnore ServiceAccountNames
	// SilentlyIgnore Status
	// SilentryIgnore Timeouts
	// if pr.Spec.Timeouts != nil {
	// 	return errors.New("Timeouts are not supported")
	// }
	// We might be able to silently ignore
	if pr.Spec.TaskRunTemplate.PodTemplate != nil {
		return errors.New("PodTemplate are not supported")
	}
	if pr.Spec.TaskRunSpecs != nil {
		return errors.New("TaskRunSpecs are not supported")
	}
	if pr.Spec.PipelineSpec != nil {
		return validatePipeline(ctx, *pr.Spec.PipelineSpec)
	}
	return nil
}

func validatePipeline(ctx context.Context, p v1.PipelineSpec) error {
	// Finally blocks are now supported
	// WhenExpressions are now supported (for regular tasks only, not finally)
	for _, pt := range p.Finally {
		if len(pt.When) > 0 {
			return errors.Errorf("Finally task %s: WhenExpressions not supported in finally blocks", pt.Name)
		}
		// Task Timeout is now supported (applied to each step)
		if pt.TaskSpec != nil {
			if !isTektonTask(pt.TaskSpec.TypeMeta) {
				return errors.Errorf("Finally task %s: Custom task not supported", pt.Name)
			}
			if err := validateTaskSpec(ctx, pt.TaskSpec.TaskSpec); err != nil {
				return err
			}
		}
	}
	for _, pt := range p.Tasks {
		// WhenExpressions are now supported - they are evaluated at LLB build time
		// Silently ignore Retries
		// Task Timeout is now supported (applied to each step)
		if pt.TaskSpec != nil {
			if !isTektonTask(pt.TaskSpec.TypeMeta) {
				return errors.Errorf("Task %s: Custom task not supported", pt.Name)
			}
			if err := validateTaskSpec(ctx, pt.TaskSpec.TaskSpec); err != nil {
				return err
			}
		}
	}
	return nil
}

func isTektonTask(typeMeta runtime.TypeMeta) bool {
	return (typeMeta.APIVersion == "" && typeMeta.Kind == "") ||
		(typeMeta.APIVersion == "tekton.dev/v1" && typeMeta.Kind == "Task")
}

// evaluateWhenExpressions evaluates all when expressions and returns true if all pass.
// WhenExpressions are evaluated after parameter substitution, so the Input field
// should contain the resolved value (not the $(params.xxx) reference).
func evaluateWhenExpressions(whens v1.WhenExpressions) bool {
	for _, when := range whens {
		if !evaluateWhenExpression(when) {
			return false
		}
	}
	return true
}

// evaluateWhenExpression evaluates a single when expression.
// Supports operators: "in" and "notin"
func evaluateWhenExpression(when v1.WhenExpression) bool {
	input := when.Input
	values := when.Values

	switch when.Operator {
	case "in":
		for _, v := range values {
			if input == v {
				return true
			}
		}
		return false
	case "notin":
		for _, v := range values {
			if input == v {
				return false
			}
		}
		return true
	default:
		// Unknown operator - default to true (permissive)
		return true
	}
}
