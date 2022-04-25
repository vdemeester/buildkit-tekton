package tekton

import (
	"context"
	"fmt"
	"os"

	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/frontend/gateway/client"
	"github.com/pkg/errors"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	"github.com/tektoncd/pipeline/pkg/reconciler/pipeline/dag"
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

	var ps *v1beta1.PipelineSpec
	var name string
	if pr.Spec.PipelineSpec != nil {
		ps = pr.Spec.PipelineSpec
		name = "embedded"
	} else if pr.Spec.PipelineRef != nil && pr.Spec.PipelineRef.Bundle != "" {
		resolvedPipeline, err := resolvePipelineInBundle(ctx, c, *pr.Spec.PipelineRef)
		if err != nil {
			return llb.State{}, err
		}
		ps = &resolvedPipeline.Spec
		name = pr.Spec.PipelineRef.Name
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
	d, err := dag.Build(v1beta1.PipelineTaskList(spec.Tasks), v1beta1.PipelineTaskList(spec.Tasks).Deps())
	if err != nil {
		return llb.State{}, err
	}
	roots, err := dag.GetSchedulable(d)
	if err != nil {
		return llb.State{}, err
	}
	fmt.Println("roots", roots)
	tasks := map[string][]llb.State{}
	for _, root := range roots.List() {
		n := d.Nodes[root]
		task := n.Task.(v1beta1.PipelineTask)
		fmt.Println("n", n)
		fmt.Println("n.Task", n.Task)
		fmt.Println("n.Next", n.Next)
		var taskspec *v1beta1.TaskSpec
		if task.TaskSpec != nil {
			taskspec = &task.TaskSpec.TaskSpec
		}
		name, rts, err := resolveTaskNameAndSpec(ctx, taskspec, task.TaskRef, r.tasks, func(ctx context.Context, ref v1beta1.TaskRef) (*v1beta1.Task, error) {
			return resolveTaskInBundle(ctx, c, ref)
		})
		if err != nil {
			return llb.State{}, err
		}

		fmt.Println("name", name)
		fmt.Println("rts", rts)

		ts, err := applyTaskRunSubstitution(ctx, &v1beta1.TaskRun{
			Spec: v1beta1.TaskRunSpec{
				Params:   task.Params,
				TaskSpec: rts,
			},
		}, rts, name)
		if err != nil {
			return llb.State{}, errors.Wrapf(err, "variable interpolation failed for %s", task.Name)
		}

		taskWorkspaces := []mountOptionFn{}
		for _, w := range task.Workspaces {
			fn := pipelineWorkspaces[w.Workspace]
			taskWorkspaces = append(taskWorkspaces, fn("/workspace/"+w.Name))
		}
		steps, err := taskSpecToPSteps(ctx, c, ts, task.Name, taskWorkspaces)
		if err != nil {
			return llb.State{}, errors.Wrap(err, "couldn't translate TaskSpec to llb")
		}
		mounts := []llb.RunOption{}
		stepStates, err := pstepToState(c, steps, mounts)
		if err != nil {
			return llb.State{}, err
		}

		ft := llb.Scratch()
		fa := llb.Mkdir("/tekton/results", os.FileMode(int(0777)), llb.WithParents(true))
		state := stepStates[len(stepStates)-1]
		fa = fa.Copy(state, "/tekton/results", "/tekton/", &llb.CopyInfo{FollowSymlinks: true, CreateDestPath: true, AllowWildcard: true, AllowEmptyWildcard: true})
		fstate := ft.File(fa, llb.IgnoreCache)

		fdef, err := fstate.Marshal(ctx) // FIXME Ignore this later on
		if err != nil {
			return llb.State{}, err
		}
		fres, err := c.Solve(ctx, client.SolveRequest{
			Definition: fdef.ToPB(),
		})
		if err != nil {
			return llb.State{}, err
		}
		fref, err := fres.SingleRef()
		if err != nil {
			return llb.State{}, err
		}
		fmt.Println("fref", fref)

		// def, err := resultState.Marshal(ctx)
		// if err != nil {
		// 	return llb.State{}, err
		// }
		// res, err := c.Solve(ctx, client.SolveRequest{
		// 	Definition: def.ToPB(),
		// })
		// if err != nil {
		// 	return llb.State{}, err
		// }
		//
		// ref, err := res.SingleRef()
		// if err != nil {
		// 	return llb.State{}, err
		// }
		// fmt.Println("ref", ref)
		dirs, err := fref.ReadDir(ctx, client.ReadDirRequest{Path: "/tekton/results/"})
		if err != nil {
			return llb.State{}, err
		}
		fmt.Println("dirs", dirs)
		return llb.State{}, fmt.Errorf("dirs %+v", dirs)
		output := ""
		for _, d := range dirs {
			data, err := fref.ReadFile(ctx, client.ReadRequest{
				Filename: d.Path,
			})
			if err != nil {
				return llb.State{}, err
			}
			output += string(data)
			output += "\n"
		}
		return llb.State{}, fmt.Errorf("output", output)
		dt, err := fref.ReadFile(ctx, client.ReadRequest{
			Filename: "/tekton/sum",
		})
		if err != nil {
			return llb.State{}, err
		}
		fmt.Println("dt", dt)

	}
	fmt.Println("yo")

	for _, t := range spec.Tasks {
		var taskspec *v1beta1.TaskSpec
		if t.TaskSpec != nil {
			taskspec = &t.TaskSpec.TaskSpec
		}
		name, rts, err := resolveTaskNameAndSpec(ctx, taskspec, t.TaskRef, r.tasks, func(ctx context.Context, ref v1beta1.TaskRef) (*v1beta1.Task, error) {
			return resolveTaskInBundle(ctx, c, ref)
		})
		if err != nil {
			return llb.State{}, err
		}

		ts, err := applyTaskRunSubstitution(ctx, &v1beta1.TaskRun{
			Spec: v1beta1.TaskRunSpec{
				Params:   t.Params,
				TaskSpec: rts,
			},
		}, rts, name)
		if err != nil {
			return llb.State{}, errors.Wrapf(err, "variable interpolation failed for %s", t.Name)
		}

		taskWorkspaces := []mountOptionFn{}
		for _, w := range t.Workspaces {
			fn := pipelineWorkspaces[w.Workspace]
			taskWorkspaces = append(taskWorkspaces, fn("/workspace/"+w.Name))
		}
		steps, err := taskSpecToPSteps(ctx, c, ts, t.Name, taskWorkspaces)
		if err != nil {
			return llb.State{}, errors.Wrap(err, "couldn't translate TaskSpec to llb")
		}
		mounts := []llb.RunOption{}
		if len(t.RunAfter) > 0 {
			// RunAfter means, the first steps of the current Task needs to start after the last step of the referenced Task
			// We are going to use mounts here too.
			for _, a := range t.RunAfter {
				targetMount := fmt.Sprintf("/tekton/from-task/%s", a)
				mounts = append(mounts,
					llb.AddMount(targetMount, tasks[a][len(tasks[a])-1], llb.SourcePath("/tekton/results"), llb.Readonly),
				)
			}
		}
		// resultState := llb.Scratch()
		// stepStates, err := pstepToState(c, steps, resultState, mounts)
		stepStates, err := pstepToState(c, steps, mounts)
		if err != nil {
			return llb.State{}, err
		}
		tasks[t.Name] = stepStates
	}
	ft := llb.Scratch()
	fa := llb.Mkdir("/task", os.FileMode(int(0777)))
	for n, t := range tasks {
		state := t[len(t)-1]
		taskPath := fmt.Sprintf("/task/%s", n)
		fa = fa.Copy(state, "/tekton", taskPath, &llb.CopyInfo{FollowSymlinks: true, CreateDestPath: true, AllowWildcard: true, AllowEmptyWildcard: true})
	}
	return ft.File(fa, llb.WithCustomName("[tekton] buildking image from result (fake)"), llb.IgnoreCache), nil
}

func applyPipelineRunSubstitution(ctx context.Context, pr *v1beta1.PipelineRun, ps *v1beta1.PipelineSpec, pipelineName string) (v1beta1.PipelineSpec, error) {
	ps = resources.ApplyParameters(ps, pr)
	ps = resources.ApplyContexts(ps, pipelineName, pr)
	ps = resources.ApplyWorkspaces(ps, pr)

	if err := ps.Validate(ctx); err != nil {
		return *ps, err
	}

	return *ps, nil
}

func validatePipelineRun(ctx context.Context, pr *v1beta1.PipelineRun) error {
	if pr.Name == "" && pr.GenerateName != "" {
		pr.Name = pr.GenerateName + "generated"
	}
	pr.SetDefaults(ctx)
	if err := pr.Validate(ctx); err != nil {
		return errors.Wrapf(err, "validation failed for PipelineRun %s", pr.Name)
	}
	if len(pr.Spec.Resources) > 0 {
		return errors.New("PipelineResources are not supported")
	}
	// SilentlyIgnore ServiceAccountName
	// SilentlyIgnore ServiceAccountNames
	// SilentlyIgnore Status
	if pr.Spec.Timeouts != nil {
		return errors.New("Timeouts are not supported")
	}
	// We might be able to silently ignore
	if pr.Spec.PodTemplate != nil {
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

func validatePipeline(ctx context.Context, p v1beta1.PipelineSpec) error {
	if len(p.Resources) > 0 {
		return errors.New("PipelineResources are not supported")
	}
	if len(p.Finally) > 0 {
		return errors.New("Finally are not supporte (yet)")
	}
	for _, pt := range p.Tasks {
		if len(pt.Conditions) > 0 {
			return errors.Errorf("Task %s: Conditions not supported", pt.Name)
		}
		if len(pt.WhenExpressions) > 0 {
			return errors.Errorf("Task %s: WhenExpressions not supported", pt.Name)
		}
		// Silently ignore Retries
		if pt.Timeout != nil {
			return errors.Errorf("Task % s: Timeout not supported", pt.Name)
		}
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
		(typeMeta.APIVersion == "tekton.dev/v1beta1" && typeMeta.Kind == "Task")
}
