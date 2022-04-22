package tekton

import (
	"context"

	"github.com/pkg/errors"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
)

func resolveTaskNameAndSpec(ctx context.Context, taskspec *v1beta1.TaskSpec, taskref *v1beta1.TaskRef, tasks map[string]*v1beta1.Task, bf func(context.Context, v1beta1.TaskRef) (*v1beta1.Task, error)) (string, *v1beta1.TaskSpec, error) {
	var ts *v1beta1.TaskSpec
	var name string
	if taskspec != nil {
		ts = taskspec
		name = "embedded"
	} else if taskref != nil {
		name = taskref.Name
		if taskref.Bundle != "" {
			resolvedTask, err := bf(ctx, *taskref)
			if err != nil {
				return name, ts, err
			}
			resolvedTask.SetDefaults(ctx)
			ts = &resolvedTask.Spec
		} else if taskref != nil && taskref.Name != "" {
			t, ok := tasks[taskref.Name]
			if !ok {
				return name, ts, errors.Errorf("taskref %s not found in context", taskref.Name)
			}
			t.SetDefaults(ctx)
			ts = &t.Spec

		}
	}
	return name, ts, nil
}
