package files

import (
	"strings"

	"github.com/moby/buildkit/client/llb"
	"github.com/tektoncd/pipeline/pkg/names"
)

const (
	defaultScriptPreamble        = "#!/bin/sh\nset -e\n"
	defaultScriptPreambleContinue = "#!/bin/sh\n"
)

// Script creates an LLB state containing the script file.
// If continueOnError is true, the script will not use "set -e" so that
// errors are ignored and execution continues.
func Script(stepName, scriptName, script string, continueOnError bool) (string, llb.State) {
	// Check for a shebang, and add a default if it's not set.
	// The shebang must be the first non-empty line.
	cleaned := strings.TrimSpace(script)
	hasShebang := strings.HasPrefix(cleaned, "#!")

	if !hasShebang {
		if continueOnError {
			script = defaultScriptPreambleContinue + script
		} else {
			script = defaultScriptPreamble + script
		}
	}
	filename := names.SimpleNameGenerator.RestrictLengthWithRandomSuffix(scriptName)
	data := script
	scriptSt := llb.Scratch().Dir("/").File(
		llb.Mkfile(filename, 0755, []byte(data)),
		llb.WithCustomName("[tekton] "+stepName+": preparing script"),
	)
	return filename, scriptSt
}

// CommandWrapper creates a wrapper script for command+args execution.
// If continueOnError is true, the wrapper catches errors and exits 0.
func CommandWrapper(stepName, scriptName string, command []string, args []string, continueOnError bool) (string, llb.State) {
	// Build the command line
	var cmdParts []string
	cmdParts = append(cmdParts, command...)
	cmdParts = append(cmdParts, args...)

	var script string
	if continueOnError {
		// Wrap command to ignore errors
		script = "#!/bin/sh\n" + shellQuoteJoin(cmdParts) + " || true\n"
	} else {
		script = "#!/bin/sh\nset -e\n" + shellQuoteJoin(cmdParts) + "\n"
	}

	filename := names.SimpleNameGenerator.RestrictLengthWithRandomSuffix(scriptName)
	scriptSt := llb.Scratch().Dir("/").File(
		llb.Mkfile(filename, 0755, []byte(script)),
		llb.WithCustomName("[tekton] "+stepName+": preparing command wrapper"),
	)
	return filename, scriptSt
}

// shellQuoteJoin quotes and joins command parts for shell execution
func shellQuoteJoin(parts []string) string {
	quoted := make([]string, len(parts))
	for i, p := range parts {
		// Simple quoting - wrap in single quotes and escape existing single quotes
		escaped := strings.ReplaceAll(p, "'", "'\"'\"'")
		quoted[i] = "'" + escaped + "'"
	}
	return strings.Join(quoted, " ")
}
