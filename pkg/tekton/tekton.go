package tekton

import (
	"context"
	"fmt"

	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/frontend/gateway/client"
	"github.com/pkg/errors"
)

// TektonToLLB returns a function that converts a string representing a Tekton resource
// into a BuildKit LLB State.
// Only support TaskRun with embedded Task to start.
func TektonToLLB(c client.Client) func(context.Context, string, []string) (llb.State, error) {
	return func(ctx context.Context, l string, refs []string) (llb.State, error) {
		run, err := readResources(l, refs)
		if err != nil {
			return llb.State{}, errors.Wrap(err, "failed to read resources")
		}

		switch r := run.(type) {
		case TaskRun:
			return TaskRunToLLB(ctx, c, r)
		case PipelineRun:
			return PipelineRunToLLB(ctx, c, r)
		default:
			return llb.State{}, fmt.Errorf("Invalid state")
		}
	}
}
