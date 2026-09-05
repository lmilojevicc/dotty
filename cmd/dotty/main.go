package main

import (
	"io"
	"os"

	"github.com/lmilojevicc/dotty/internal/cli"
)

var version = "dev"

func main() {
	cli.SetVersion(version)
	cmd := cli.NewRootCommand(os.Stdout, os.Stderr)
	os.Exit(runExecution(cmd.Execute, os.Stderr))
}

func runExecution(execute func() error, errOut io.Writer) int {
	return cli.RenderExecutionError(errOut, execute())
}
