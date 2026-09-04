//go:build !darwin && !linux

package main

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

func TestFocusedUnsupportedPlatform(t *testing.T) {
	cmd := exec.Command("go", "version")
	var output bytes.Buffer
	code := execute(cmd, "TestPlain", &output, &output)
	if code != 1 || cmd.Process != nil ||
		!strings.Contains(output.String(), "requires Linux or macOS") {
		t.Fatalf(
			"code=%d process=%v output=%s; want pre-start capability refusal",
			code,
			cmd.Process,
			&output,
		)
	}
}
