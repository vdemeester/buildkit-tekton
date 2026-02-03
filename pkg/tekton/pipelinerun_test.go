package tekton

import (
	"context"
	"fmt"
	"os"
	"testing"

	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidatePipeline_WithFinally(t *testing.T) {
	// RED: This test should pass once Finally blocks are supported
	// Currently it should fail because Finally blocks are rejected
	ctx := context.Background()
	spec := v1.PipelineSpec{
		Tasks: []v1.PipelineTask{{
			Name: "main-task",
			TaskSpec: &v1.EmbeddedTask{
				TaskSpec: v1.TaskSpec{
					Steps: []v1.Step{{
						Name:   "do-work",
						Image:  "alpine:latest",
						Script: "echo hello",
					}},
				},
			},
		}},
		Finally: []v1.PipelineTask{{
			Name: "cleanup",
			TaskSpec: &v1.EmbeddedTask{
				TaskSpec: v1.TaskSpec{
					Steps: []v1.Step{{
						Name:   "cleanup-step",
						Image:  "alpine:latest",
						Script: "echo cleanup",
					}},
				},
			},
		}},
	}

	err := validatePipeline(ctx, spec)
	if err != nil {
		t.Errorf("validatePipeline() with Finally should not error, got: %v", err)
	}
}

func TestValidatePipeline_WithoutFinally(t *testing.T) {
	// This test should always pass - no Finally blocks
	ctx := context.Background()
	spec := v1.PipelineSpec{
		Tasks: []v1.PipelineTask{{
			Name: "main-task",
			TaskSpec: &v1.EmbeddedTask{
				TaskSpec: v1.TaskSpec{
					Steps: []v1.Step{{
						Name:   "do-work",
						Image:  "alpine:latest",
						Script: "echo hello",
					}},
				},
			},
		}},
	}

	err := validatePipeline(ctx, spec)
	if err != nil {
		t.Errorf("validatePipeline() without Finally should not error, got: %v", err)
	}
}

func TestPipelineRunToLLB_WithFinally(t *testing.T) {
	// This test verifies that Finally blocks are processed correctly
	// The Finally tasks should execute after all regular tasks
	ctx := context.Background()

	pr := &v1.PipelineRun{
		ObjectMeta: metav1.ObjectMeta{Name: "test-finally-run"},
		Spec: v1.PipelineRunSpec{
			PipelineSpec: &v1.PipelineSpec{
				Tasks: []v1.PipelineTask{{
					Name: "main-task",
					TaskSpec: &v1.EmbeddedTask{
						TaskSpec: v1.TaskSpec{
							Steps: []v1.Step{{
								Name:   "do-work",
								Image:  "alpine:latest",
								Script: "echo main",
							}},
						},
					},
				}},
				Finally: []v1.PipelineTask{{
					Name: "cleanup",
					TaskSpec: &v1.EmbeddedTask{
						TaskSpec: v1.TaskSpec{
							Steps: []v1.Step{{
								Name:   "cleanup-step",
								Image:  "alpine:latest",
								Script: "echo cleanup",
							}},
						},
					},
				}},
			},
		},
	}

	pipelineRun := PipelineRun{
		main:      pr,
		tasks:     map[string]*v1.Task{},
		pipelines: map[string]*v1.Pipeline{},
	}

	// This should not error - Finally blocks are now supported
	_, err := PipelineRunToLLB(ctx, nil, pipelineRun)
	if err != nil {
		t.Errorf("PipelineRunToLLB() with Finally should not error, got: %v", err)
	}
}

// Suppress unused import warnings
var _ = fmt.Sprintf
var _ = os.Stderr
