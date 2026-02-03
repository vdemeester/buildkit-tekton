package files_test

import (
	"testing"

	"github.com/vdemeester/buildkit-tekton/pkg/tekton/files"
)

func TestScript(t *testing.T) {
	tests := []struct {
		name, script, expected string
		continueOnError        bool
	}{{
		name: "no-shebang",
		script: `echo hello world
cat foo`,
		expected: `#!/bin/sh
set -e
echo hello world
cat foo`,
		continueOnError: false,
	}, {
		name: "with shebang",
		script: `#!/usr/bin/env bash
echo foo`,
		expected: `#!/usr/bin/env bash
echo foo`,
		continueOnError: false,
	}, {
		name: "no-shebang-continue-on-error",
		script: `echo hello world
cat foo`,
		expected: `#!/bin/sh
echo hello world
cat foo`,
		continueOnError: true,
	}}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			filename, state := files.Script("stepName", "scriptName", tc.script, tc.continueOnError)
			// FIXME(vdemeester) exercise this better, most likely using buildkit testutil (integration)
			t.Logf("%s: %+v", filename, state)
		})
	}
}

func TestCommandWrapper(t *testing.T) {
	tests := []struct {
		name            string
		command         []string
		args            []string
		continueOnError bool
	}{{
		name:            "simple-command",
		command:         []string{"echo"},
		args:            []string{"hello", "world"},
		continueOnError: false,
	}, {
		name:            "command-continue-on-error",
		command:         []string{"false"},
		args:            []string{},
		continueOnError: true,
	}}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			filename, state := files.CommandWrapper("stepName", "cmdName", tc.command, tc.args, tc.continueOnError)
			t.Logf("%s: %+v", filename, state)
		})
	}
}
