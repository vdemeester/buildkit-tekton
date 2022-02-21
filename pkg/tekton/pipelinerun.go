package tekton

import (
	"context"
	"fmt"
	"os"

	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/frontend/gateway/client"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	"github.com/tektoncd/pipeline/pkg/reconciler/pipelinerun/resources"
	"k8s.io/apimachinery/pkg/runtime"
)

func PipelineRunToLLB(ctx context.Context, c client.Client, pr *v1beta1.PipelineRun) (llb.State, error) {
	// Validation
	if err := validatePipelineRun(ctx, pr); err != nil {
		return llb.State{}, err
	}

	// Interpolation
	ps, err := applyPipelineRunSubstitution(ctx, pr)
	if err != nil {
		return llb.State{}, errors.Wrap(err, "variable interpolation failed")
	}

	// Execution
	pipelineWorkspaces := map[string]llb.MountOption{}
	for _, w := range ps.Workspaces {
		pipelineWorkspaces[w.Name] = llb.AsPersistentCacheDir(pr.Name+"/"+w.Name, llb.CacheMountShared)
	}
	logrus.Infof("pipelineWorkspaces: %+v", pipelineWorkspaces)
	tasks := map[string][]llb.State{}
	for _, t := range ps.Tasks {
		logrus.Infof("pipelinetask: %s", t.Name)
		logrus.Infof("pipelinetask: %+v", t)
		if t.TaskSpec == nil {
			return llb.State{}, errors.Errorf("%s: TaskRef not supported", t.Name)
		}

		logrus.Infof("pipelinetask.TaskSpec: %+v", t.TaskSpec)
		ts, err := applyTaskRunSubstitution(ctx, &v1beta1.TaskRun{
			Spec: v1beta1.TaskRunSpec{
				Params:   t.Params,
				TaskSpec: &t.TaskSpec.TaskSpec,
			},
		})
		if err != nil {
			return llb.State{}, errors.Wrapf(err, "variable interpolation failed for %s", t.Name)
		}

		logrus.Infof("pipelinetask.TaskSpec: %+v", ts)
		taskWorkspaces := map[string]llb.MountOption{}
		for _, w := range t.Workspaces {
			taskWorkspaces["/workspace/"+w.Name] = pipelineWorkspaces[w.Workspace]
		}
		logrus.Infof("taskWorkspaces: %+v", taskWorkspaces)
		steps, err := taskSpecToPSteps(ctx, c, ts, t.Name, taskWorkspaces)
		if err != nil {
			return llb.State{}, errors.Wrap(err, "couldn't translate TaskSpec to llb")
		}
		logrus.Infof("steps: %+v", steps)
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
		resultState := llb.Scratch()
		stepStates, err := pstepToState(c, steps, resultState, mounts)
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
	return ft.File(fa, llb.WithCustomName("[results] buildking image from result (fake)"), llb.IgnoreCache), nil
}

func applyPipelineRunSubstitution(ctx context.Context, pr *v1beta1.PipelineRun) (v1beta1.PipelineSpec, error) {
	ps := pr.Spec.PipelineSpec.DeepCopy()

	ps = resources.ApplyParameters(ps, pr)
	ps = resources.ApplyContexts(ps, "embedded", pr) // FIXME(vdemeester) handle this "embedded" better
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
	if pr.Spec.PipelineSpec == nil {
		return errors.New("PipelineRef not supported")
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
	// TODO(vdemeester) bail out on other unsupported field, like PipelineResources, Finally, …
	return validatePipeline(ctx, *pr.Spec.PipelineSpec)
}

func validatePipeline(ctx context.Context, p v1beta1.PipelineSpec) error {
	if len(p.Resources) > 0 {
		return errors.New("PipelineResources are not supported")
	}
	if len(p.Finally) > 0 {
		return errors.New("Finally are not supporte (yet)")
	}
	for _, pt := range p.Tasks {
		if pt.TaskSpec == nil {
			return errors.Errorf("Task %s: TaskRef not supported", pt.Name)
		}
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
		if !isTektonTask(pt.TaskSpec.TypeMeta) {
			return errors.Errorf("Task %s: Custom task not supported", pt.Name)
		}
		if err := validateTaskSpec(ctx, pt.TaskSpec.TaskSpec); err != nil {
			return err
		}
	}
	return nil
}

func isTektonTask(typeMeta runtime.TypeMeta) bool {
	return (typeMeta.APIVersion == "" && typeMeta.Kind == "") ||
		(typeMeta.APIVersion == "tekton.dev/v1beta1" && typeMeta.Kind == "Task")
}
