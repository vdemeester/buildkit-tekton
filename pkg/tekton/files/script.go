package files

import (
	"fmt"
	"strings"
	"time"

	"github.com/moby/buildkit/client/llb"
	"github.com/tektoncd/pipeline/pkg/names"
)

const defaultScriptPreamble = "#!/bin/sh\nset -e\n"

// Script creates an LLB state containing the script file.
// If continueOnError is true, the script execution is wrapped to ignore exit codes.
// If timeout is non-nil, the script execution will be wrapped with the timeout command.
func Script(stepName, scriptName, script string, continueOnError bool, timeout *time.Duration) (string, llb.State) {
	// Check for a shebang, and add a default if it's not set.
	// The shebang must be the first non-empty line.
	cleaned := strings.TrimSpace(script)
	hasShebang := strings.HasPrefix(cleaned, "#!")

	if !hasShebang {
		script = defaultScriptPreamble + script
	}

	// If continueOnError or timeout is specified, wrap the script
	if continueOnError || timeout != nil {
		script = wrapScript(script, timeout, continueOnError)
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
// If timeout is non-nil, the command execution will be wrapped with the timeout command.
func CommandWrapper(stepName, scriptName string, command []string, args []string, continueOnError bool, timeout *time.Duration) (string, llb.State) {
	// Build the command line
	var cmdParts []string
	cmdParts = append(cmdParts, command...)
	cmdParts = append(cmdParts, args...)

	var script string
	cmdLine := shellQuoteJoin(cmdParts)

	// Apply timeout if specified
	if timeout != nil {
		cmdLine = fmt.Sprintf("timeout %s %s", formatDuration(*timeout), cmdLine)
	}

	if continueOnError {
		// Wrap command to ignore errors
		script = "#!/bin/sh\n" + cmdLine + " || true\n"
	} else {
		script = "#!/bin/sh\nset -e\n" + cmdLine + "\n"
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

// wrapScript creates a wrapper script that executes the original script with optional timeout
// and optional error suppression for onError: continue.
func wrapScript(script string, timeout *time.Duration, continueOnError bool) string {
	var timeoutPrefix string
	if timeout != nil {
		timeoutPrefix = fmt.Sprintf("timeout %s ", formatDuration(*timeout))
	}

	var errorSuffix string
	if continueOnError {
		errorSuffix = " || true"
	}

	// Create a wrapper that writes the original script to a temp file and runs it
	// This handles scripts with shebangs properly
	wrapper := fmt.Sprintf(`#!/bin/sh
set -e
SCRIPT_FILE=$(mktemp)
trap "rm -f $SCRIPT_FILE" EXIT
cat > $SCRIPT_FILE << 'TEKTON_SCRIPT_EOF'
%s
TEKTON_SCRIPT_EOF
chmod +x $SCRIPT_FILE
%s$SCRIPT_FILE%s
`, script, timeoutPrefix, errorSuffix)
	return wrapper
}

// formatDuration converts a time.Duration to a format suitable for the timeout command.
// The timeout command accepts formats like: 30s, 5m, 1h
func formatDuration(d time.Duration) string {
	// Use seconds for precision, as the timeout command supports this
	seconds := int64(d.Seconds())
	if seconds <= 0 {
		seconds = 1 // Minimum 1 second
	}
	return fmt.Sprintf("%ds", seconds)
}
