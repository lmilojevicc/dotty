package main

import (
	"os"

	"github.com/lmilojevicc/dotty/internal/cli"
)

var version = "dev"

func main() {
	cli.SetVersion(version)
	cmd := cli.NewRootCommand(os.Stdout, os.Stderr)
	if err := cmd.Execute(); err != nil {
		cli.RenderError(os.Stderr, err)
		os.Exit(1)
	}
}
