package files

import (
	"strings"

	"github.com/moby/buildkit/client/llb"
	"github.com/tektoncd/pipeline/pkg/names"
)

const (
	defaultScriptPreamble = "#!/bin/sh\nset -e\n"
)

func Script(stepName, scriptName, script string) (string, llb.State) {
	// Check for a shebang, and add a default if it's not set.
	// The shebang must be the first non-empty line.
	cleaned := strings.TrimSpace(script)
	hasShebang := strings.HasPrefix(cleaned, "#!")

	if !hasShebang {
		script = defaultScriptPreamble + script
	}
	filename := names.SimpleNameGenerator.RestrictLengthWithRandomSuffix(scriptName)
	data := script
	scriptSt := llb.Scratch().Dir("/").File(
		llb.Mkfile(filename, 0755, []byte(data)),
		llb.WithCustomName("[tekton] "+stepName+": preparing script"),
	)
	return filename, scriptSt
}
