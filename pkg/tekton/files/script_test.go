package files_test

import (
	"testing"

	"github.com/vdemeester/buildkit-tekton/pkg/tekton/files"
)

func TestScript(t *testing.T) {
	tests := []struct {
		name, script, expected string
	}{{
		name: "no-shebang",
		script: `echo hello world
cat foo`,
		expected: `#!/bin/sh
set -e
echo hello world
cat foo`,
	}, {
		name: "with shebang",
		script: `#!/usr/bin/env bash
echo foo`,
		expected: `#!/usr/bin/env bash
echo foo`,
	}}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			filename, state := files.Script("stepName", "scriptName", tc.script)
			// FIXME(vdemeester) exercise this better, most likely using buildkit testutil (integration)
			t.Logf("%s: %+v", filename, state)
		})
	}
}
