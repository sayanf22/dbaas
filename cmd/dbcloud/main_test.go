package main

import (
	"bytes"
	"testing"
)

// TestRun checks the exit codes and output of the Step 0.1 command set: `version` prints the build
// version, anything else prints usage to stderr and exits 2 (the conventional "bad usage" code).
func TestRun(t *testing.T) {
	tests := []struct {
		args     []string
		wantCode int
		wantOut  string
	}{
		{[]string{"version"}, 0, "dev\n"},
		{nil, 2, ""},
		{[]string{"nope"}, 2, ""},
	}
	for _, tt := range tests {
		var out, errOut bytes.Buffer
		if code := run(tt.args, &out, &errOut); code != tt.wantCode || out.String() != tt.wantOut {
			t.Errorf("run(%v) = %d, %q; want %d, %q", tt.args, code, out.String(), tt.wantCode, tt.wantOut)
		}
	}
}
