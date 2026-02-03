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

func TestValidatePipeline_WithWhenExpressions(t *testing.T) {
	// RED: This test should pass once WhenExpressions are supported
	// Currently it should fail because WhenExpressions are rejected
	ctx := context.Background()
	spec := v1.PipelineSpec{
		Params: []v1.ParamSpec{{
			Name: "run-optional",
			Type: v1.ParamTypeString,
		}},
		Tasks: []v1.PipelineTask{{
			Name: "always-runs",
			TaskSpec: &v1.EmbeddedTask{
				TaskSpec: v1.TaskSpec{
					Steps: []v1.Step{{
						Name:   "step1",
						Image:  "alpine:latest",
						Script: "echo always",
					}},
				},
			},
		}, {
			Name: "conditional-task",
			When: v1.WhenExpressions{{
				Input:    "$(params.run-optional)",
				Operator: "in",
				Values:   []string{"yes", "true"},
			}},
			TaskSpec: &v1.EmbeddedTask{
				TaskSpec: v1.TaskSpec{
					Steps: []v1.Step{{
						Name:   "step1",
						Image:  "alpine:latest",
						Script: "echo conditional",
					}},
				},
			},
		}},
	}

	err := validatePipeline(ctx, spec)
	if err != nil {
		t.Errorf("validatePipeline() with WhenExpressions should not error, got: %v", err)
	}
}

func TestPipelineRunToLLB_WithWhenExpressions(t *testing.T) {
	// Test that WhenExpressions are evaluated and tasks are conditionally included
	ctx := context.Background()

	pr := &v1.PipelineRun{
		ObjectMeta: metav1.ObjectMeta{Name: "test-when-run"},
		Spec: v1.PipelineRunSpec{
			Params: []v1.Param{{
				Name:  "run-optional",
				Value: v1.ParamValue{Type: v1.ParamTypeString, StringVal: "yes"},
			}},
			PipelineSpec: &v1.PipelineSpec{
				Params: []v1.ParamSpec{{
					Name: "run-optional",
					Type: v1.ParamTypeString,
				}},
				Tasks: []v1.PipelineTask{{
					Name: "always-runs",
					TaskSpec: &v1.EmbeddedTask{
						TaskSpec: v1.TaskSpec{
							Steps: []v1.Step{{
								Name:   "step1",
								Image:  "alpine:latest",
								Script: "echo always",
							}},
						},
					},
				}, {
					Name: "conditional-task",
					When: v1.WhenExpressions{{
						Input:    "$(params.run-optional)",
						Operator: "in",
						Values:   []string{"yes", "true"},
					}},
					TaskSpec: &v1.EmbeddedTask{
						TaskSpec: v1.TaskSpec{
							Steps: []v1.Step{{
								Name:   "step1",
								Image:  "alpine:latest",
								Script: "echo conditional",
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

	// This should not error - WhenExpressions are now supported
	_, err := PipelineRunToLLB(ctx, nil, pipelineRun)
	if err != nil {
		t.Errorf("PipelineRunToLLB() with WhenExpressions should not error, got: %v", err)
	}
}

func TestValidateTaskSpec_WithStepTimeout(t *testing.T) {
	// RED: This test should pass once Step Timeout is supported
	// Currently it should fail because Step Timeout is rejected
	ctx := context.Background()
	timeout := &metav1.Duration{Duration: 30 * 1000000000} // 30 seconds
	spec := v1.TaskSpec{
		Steps: []v1.Step{{
			Name:    "step-with-timeout",
			Image:   "alpine:latest",
			Script:  "echo hello && sleep 5",
			Timeout: timeout,
		}},
	}

	err := validateTaskSpec(ctx, spec)
	if err != nil {
		t.Errorf("validateTaskSpec() with Step Timeout should not error, got: %v", err)
	}
}

func TestValidatePipeline_WithTaskTimeout(t *testing.T) {
	// RED: This test should pass once Task (PipelineTask) Timeout is supported
	// Currently it should fail because Task Timeout is rejected
	ctx := context.Background()
	timeout := &metav1.Duration{Duration: 60 * 1000000000} // 60 seconds
	spec := v1.PipelineSpec{
		Tasks: []v1.PipelineTask{{
			Name:    "task-with-timeout",
			Timeout: timeout,
			TaskSpec: &v1.EmbeddedTask{
				TaskSpec: v1.TaskSpec{
					Steps: []v1.Step{{
						Name:   "step1",
						Image:  "alpine:latest",
						Script: "echo hello && sleep 5",
					}},
				},
			},
		}},
	}

	err := validatePipeline(ctx, spec)
	if err != nil {
		t.Errorf("validatePipeline() with Task Timeout should not error, got: %v", err)
	}
}

// Suppress unused import warnings
var _ = fmt.Sprintf
var _ = os.Stderr
